package v2

import (
    //"io"
    "io/ioutil"
    "log"
    "net/http"
    //"net/url"
    "time"
    "bytes"
    "regexp"
    "errors"
    "strings"
    "context"
    "encoding/json"
    "github.com/coreos/etcd/client"
    "github.com/xeipuuv/gojsonschema"
    "github.com/ltkh/confd/internal/config"
)

var (
    keyRegexp = regexp.MustCompile(`.*/([^/]+)$`)
)

type ApiEtcd struct {
    Id            string
    ReadClient    *client.Client
    WriteClient   *client.Client
    Backend       *config.Backend
    Actions       chan *config.Action
}

type errResp struct {
    Error         int                      `json:"errorCode"`
    Message       string                   `json:"message"` 
    Cause         string                   `json:"cause"`
}

type PutRequest struct {
    Key           string                   `json:"key"`
    Value         string                   `json:"value"`
}

type Actions struct {
    Array         []config.Action          `json:"actions"`
}

func encodeResp(resp *errResp) []byte {
    jsn, err := json.Marshal(resp)
    if err != nil {
        return encodeResp(&errResp{Error:500, Message:err.Error(), Cause: resp.Cause})
    }
    return jsn
}

func getEtcdNodes(nodes client.Nodes) (map[string]interface{}) {
    jsn := map[string]interface{}{}

    if nodes != nil {
    
        for _, node := range nodes {
            key := keyRegexp.ReplaceAllString(node.Key, "$1")
            if node.Dir != true {
                var v interface{}
                err := json.Unmarshal([]byte(node.Value), &v)
                if err == nil {
                    jsn[key] = v
                } else {
                    jsn[key] = node.Value
                }
            } else {
                jsn[key] = make(map[string]string, 0)
            }
            if node.Nodes != nil {
                jsn[key] = getEtcdNodes(node.Nodes)
            }
        }

    }

    return jsn
}

func inArray(val string, arr []string) bool {
    for _, v := range arr {
        if v == val {
            return true
        }
    }
    return false
}

func parseForm(r *http.Request) (map[string]string, error) {
    result := map[string]string{
        "dir":   "",
        "value": "",
    }

    if r.Method != http.MethodPut {
        return result, nil
    }

    bodyBytes, err := ioutil.ReadAll(r.Body)
    if err != nil {
        return result, err
    }
    defer r.Body.Close()

    query := strings.TrimSpace(string(bodyBytes))
    pairs := strings.Split(query, "&")
    
    for _, pair := range pairs {
        // Разделяем ключ и значение
        kv := strings.SplitN(pair, "=", 2)
        if len(kv) != 2 {
            log.Printf("[error] invalid pair: %v", pair)
            continue
        }

        // Декодируем ключ и значение
        //decodedKey, err := url.QueryUnescape(kv[0])
        //if err != nil {
        //    return nil, err
        //}
        
        //decodedValue, err := url.QueryUnescape(kv[1])
        //if err != nil {
        //    return nil, err
        //}

        // Добавляем значение в результат
        //result[decodedKey] = decodedValue
        result[kv[0]] = kv[1]

    }

    return result, nil
    
    /*
    var putRequest PutRequest
    if err := json.NewDecoder(r.Body).Decode(&putRequest); err != nil {
        log.Printf("[error] %v", err)
        return "", "", err
    }

    log.Printf("[debug] %v", putRequest.Value)

    // Читаем тело запроса
    bodyBytes, err := ioutil.ReadAll(r.Body)
    if err != nil {
        return "", "", err
    }
    defer r.Body.Close()

    percent := regexp.MustCompile(`%(25)?`)
    bodyBytes = percent.ReplaceAllLiteral(bodyBytes, []byte("%25"))

    semicolon := regexp.MustCompile(`;`)
    bodyBytes = semicolon.ReplaceAllLiteral(bodyBytes, []byte("%3B"))

    // Парсим тело как url-encoded параметры
    params, err := url.ParseQuery(string(bodyBytes))
    if err != nil {
        return "", "", err
    }

    return params.Get("dir"), params.Get("value"), nil
    */
}

func backendChecks(backend *config.Backend, params map[string]string, path, user, pass, method string) (int, error) {
    for _, check := range backend.Checks {
        if method != check.Method {
            continue
        }
        if check.Path != "" && !check.RePath.MatchString(path){
            continue
        }
        if len(check.Users) > 0 {
            if !inArray(user, check.Users) {
                return 403, errors.New("Access is denied")
            }

            if val, ok := backend.Users[user]; ok && val != pass {
                return 403, errors.New("Access is denied")
            }
        }
        if method == "PUT" || method == "POST" {
            if check.Dir == "true" && params["dir"] != "true" {
                return 400, errors.New("Invalid parameter type: Directory expected")
            }

            if check.Dir == "false" && params["dir"] == "true" {
                return 400, errors.New("Invalid parameter type: Not directory expected")
            }

            if check.Regexp != "" {
                if params["dir"] != "true" && !check.ReRegexp.MatchString(params["value"]){
                    return 400, errors.New("Invalid parameter value")
                }
                if params["dir"] == "true" && !check.ReRegexp.MatchString(params["dir"]){
                    return 400, errors.New("Invalid parameter name")
                }
            }

            if check.Schema != "" {
                schema := gojsonschema.NewReferenceLoader(check.Schema)
                document := gojsonschema.NewStringLoader(params["value"])

                result, err := gojsonschema.Validate(schema, document)
                if err != nil {
                    //if err != io.EOF {
                    //    log.Print("[debug] EOF")
                    //}
                    //log.Print("[debug] test")
                    return 400, err
                }

                if !result.Valid() {
                    for _, desc := range result.Errors() {
                        return 400, errors.New(desc.String())
                    }
                }
            }
        }
        break
    } 

    return 0, nil
}

func GetEtcdClient(backend config.Backend, logger config.Logger) (*ApiEtcd, error) {

    readClient, err := client.New(client.Config{
        Endpoints:               backend.Nodes,
        Username:                backend.Read.Username,
        Password:                backend.Read.Password,
        Transport:               client.DefaultTransport,
        HeaderTimeoutPerRequest: 5 * time.Second,
    })
    if err != nil {
        return nil, err
    }

    writeClient, err := client.New(client.Config{
        Endpoints:               backend.Nodes,
        Username:                backend.Write.Username,
        Password:                backend.Write.Password,
        Transport:               client.DefaultTransport,
        HeaderTimeoutPerRequest: 5 * time.Second,
    })
    if err != nil {
        return nil, err
    }

    api := &ApiEtcd{
        Id:            backend.Id,
        ReadClient:    &readClient,
        WriteClient:   &writeClient,
        Backend:       &backend,
        Actions:       make(chan *config.Action, 1000),
    }

    // Send new action
    go func(logger config.Logger){
        client := &http.Client{
            Transport: &http.Transport{
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
                DisableCompression:  false,
            },
            Timeout: 10 * time.Second,
        }

        for {
            actions := Actions{}
            for i := 0; i < len(api.Actions); i++ {
                action := <-api.Actions
                actions.Array = append(actions.Array, *action)
            }
            if len(actions.Array) > 0 {
                api.SendActions(client, actions, logger.Urls)
            }
            time.Sleep(5 * time.Second)
        }

    }(logger)

    return api, nil
}

func errorResp(err error) (int, error) {
    log.Printf("[error] %v", err.Error())

    if strings.Contains(err.Error(), "connect: connection refused") {
        return 502, errors.New("Etcd cluster is unavailable")
    } else if strings.Contains(err.Error(), "100: Key not found") {
        return 404, errors.New("Key not found")
    }

    return 400, err
}

func (a *ApiEtcd) SendActions(client *http.Client, actions Actions, urls []string) {
    data, err := json.Marshal(actions)
    if err != nil {
        log.Printf("[error] sending to logger: %v", err)
        return
    }

    for _, url := range urls {
        req, err := http.NewRequest("POST", url+"/api/v1/actions", bytes.NewBuffer(data))
        req.Header.Set("Content-Type", "application/json")

        resp, err := client.Do(req)
        if err != nil {
            log.Printf("[error] sending to logger: %v", err)
            continue
        }
        defer resp.Body.Close()
    }

    return
}

func (a *ApiEtcd) SetAction(action *config.Action, code int, err error) {
    if len(a.Actions) < 1000 {
        if code != 0 { action.Attributes["code"] = code }
        if err != nil { action.Attributes["error"] = err.Error() }
        a.Actions <- action
    }
}

func (a *ApiEtcd) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    path := strings.Replace(r.URL.Path, "/api/v2/"+a.Id, "", 1)
    user, pass, _ := r.BasicAuth()

    action := &config.Action{
        Login:        user,
        Action:       "storage request",
        Attributes:   map[string]interface{}{
            "method": r.Method,
            "path":   r.URL.Path,
        },
        Description:  path,
        Timestamp:    time.Now().UTC().Unix(),
    }

    if r.Header.Get("X-Custom-User") != "" {
        action.Login = r.Header.Get("X-Custom-User")
    }
    if r.Header.Get("X-Forwarded-For") != "" {
        action.Object = r.Header.Get("X-Forwarded-For")
    }

    params, err := parseForm(r)
    if err != nil {
        log.Printf("[error] %v (%v)", err.Error(), r.URL.Path)
        w.WriteHeader(400)
        w.Write(encodeResp(&errResp{Error:400, Message:err.Error(), Cause: path}))
        go a.SetAction(action, 400, err)
        return
    }

    code, err := backendChecks(a.Backend, params, path, user, pass, r.Method)
    if err != nil {
        log.Printf("[error] %v (%v)", err.Error(), r.URL.Path)
        w.WriteHeader(code)
        w.Write(encodeResp(&errResp{Error:code, Message:err.Error(), Cause: path}))
        go a.SetAction(action, code, err)
        return
    }

    if r.Method == http.MethodGet {

        kapi := client.NewKeysAPI(*a.ReadClient)
        
        opts := &client.GetOptions{}
        if r.URL.Query().Get("recursive") == "true" {
            opts.Recursive = true
        }
        if r.URL.Query().Get("sorted") == "true" {
            opts.Sort = true
        }

        resp, err := kapi.Get(context.Background(), path, opts)
        if err != nil {
            code, err := errorResp(err)
            w.WriteHeader(code)
            w.Write(encodeResp(&errResp{Error:code, Message:err.Error(), Cause: path}))
            go a.SetAction(action, code, err)
            return
        }

        if r.Header.Get("X-Custom-Format") == "base" {
            data, err := json.Marshal(resp)
            if err != nil {
                log.Printf("[error] %v (%v)", err, r.URL.Path)
                w.WriteHeader(500)
                go a.SetAction(action, 500, err)
                return
            }
            w.Write(data)
            go a.SetAction(action, 200, nil)
            return
        }

        jsn := getEtcdNodes(resp.Node.Nodes)

        data, err := json.Marshal(jsn)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            go a.SetAction(action, 500, err)
            return
        }

        hash := config.GetHash(data)
        if r.Header.Get("X-Custom-Hash") == hash {
            w.WriteHeader(204)
            return
        }

        w.Header().Set("X-Custom-Hash", hash)
        w.Write(data)
        go a.SetAction(action, 200, nil)
        return
    }

    if r.Method == http.MethodPut {

        kapi := client.NewKeysAPI(*a.WriteClient)

        opts := &client.SetOptions{}

        if params["dir"] == "true" {
            opts.Dir = true
        }

        resp, err := kapi.Set(context.Background(), path, params["value"], opts)
        if err != nil {
            code, err := errorResp(err)
            w.WriteHeader(code)
            w.Write(encodeResp(&errResp{Error:code, Message:err.Error(), Cause: path}))
            go a.SetAction(action, code, err)
            return
        } 

        data, err := json.Marshal(resp)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            go a.SetAction(action, 500, err)
            return
        }

        w.Write(data)
        go a.SetAction(action, 200, nil)
        return
    }

    if r.Method == http.MethodDelete {

        kapi := client.NewKeysAPI(*a.WriteClient)

        opts := &client.DeleteOptions{}
        if r.URL.Query().Get("recursive") == "true" {
            opts.Recursive = true
        }

        resp, err := kapi.Delete(context.Background(), path, opts)
        if err != nil {
            code, err := errorResp(err)
            w.WriteHeader(code)
            w.Write(encodeResp(&errResp{Error:code, Message:err.Error(), Cause: path}))
            go a.SetAction(action, code, err)
            return
        } 

        data, err := json.Marshal(resp)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            go a.SetAction(action, 500, err)
            return
        }

        w.Write(data)
        go a.SetAction(action, 200, nil)
        return
    }
    
    w.WriteHeader(405)
    go a.SetAction(action, 405, nil)
}