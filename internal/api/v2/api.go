package v2

import (
    //"io"
    "io/ioutil"
    "log"
    "net/http"
    "net/url"
    "time"
    "bytes"
    "regexp"
    "errors"
    "strings"
    "context"
    "crypto/tls"
    "crypto/x509"
    "encoding/json"
    "path/filepath"
    //"github.com/coreos/etcd/client"
    "go.etcd.io/etcd/client/v2"
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

        // Декодируем ключ
        decodedKey, err := url.QueryUnescape(kv[0])
        if err != nil {
            return nil, err
        }
        
        // Декодируем значение
        decodedValue, err := url.QueryUnescape(kv[1])
        if err != nil {
            return nil, err
        }

        // Добавляем значение в результат
        result[decodedKey] = decodedValue

    }

    return result, nil
}

func backendChecks(backend *config.Backend, params map[string]string, path, user, pass, method string) (int, error) {
    for _, check := range backend.Checks[method] {
        if check.Pattern != "" {
            if matched, _ := filepath.Match(check.Pattern, path); !matched {
                continue
            }
        }
        if check.Path != "" {
            if !check.RePath.MatchString(path){
                continue
            }
        }
        if len(check.Users) > 0 {
            if ps, ok := check.Users[user]; !ok || ps != pass {
                return 403, errors.New("Access is denied")
            }
        }
        if method == "put" || method == "post" {
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

    // DefaultTransport
    transport := client.DefaultTransport

    if backend.UseSSL {
        caCertPool := x509.NewCertPool()

        // Читаем CA сертификат
        if backend.TrustedCaFile != "" {
            caCert, err := ioutil.ReadFile(backend.TrustedCaFile)
            if err != nil {
                return nil, err
            }
            caCertPool.AppendCertsFromPEM(caCert)
        }

        // Читаем клиентский сертификат и ключ
        cert, err := tls.LoadX509KeyPair(backend.CertFile, backend.CertKey)
        if err != nil {
            return nil, err
        }

        // Настраиваем TLS
        tlsConfig := &tls.Config{
            RootCAs:      caCertPool,
            Certificates: []tls.Certificate{cert},
        }

        transport = &http.Transport{TLSClientConfig: tlsConfig}
    }

    // Конфигурация клиента
    readClient, err := client.New(client.Config{
        Endpoints:               backend.Nodes,
        Username:                backend.Read.Username,
        Password:                backend.Read.Password,
        Transport:               transport,
        HeaderTimeoutPerRequest: 5 * time.Second,
    })
    if err != nil {
        return nil, err
    }

    if backend.Cache == true {
        kapi := client.NewKeysAPI(readClient)

        // Создаем watcher на ключ или префикс
        watcher := kapi.Watcher("/", &client.WatcherOptions{ Recursive: true })

        // Запускаем цикл получения событий
        go func() {
            for {
                resp, err := watcher.Next(context.Background())
                if err != nil {
                    log.Println("Watcher error:", err)
                    continue
                }
                log.Printf("[cache] %v: %v", resp.Action, resp.Node.Key)
                //log.Printf("[cache] %v: %v - %v", resp.Action, resp.Node.Key, resp.Node.Value)
            }
        }()
    }

    writeClient, err := client.New(client.Config{
        Endpoints:               backend.Nodes,
        Username:                backend.Write.Username,
        Password:                backend.Write.Password,
        Transport:               transport,
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
                TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
            },
            Timeout: 10 * time.Second,
        }

        actions := Actions{}
        ticker := time.NewTicker(5 * time.Second)
        defer ticker.Stop()

        for {
            select {
            case act, ok := <-api.Actions:
                if !ok { return }
                actions.Array = append(actions.Array, *act) 
            case <-ticker.C:
                if len(actions.Array) > 0 {
                    api.SendActions(client, actions, logger.Urls)
                    actions.Array = nil
                }
            }
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

func (a *ApiEtcd) SetAction(action *config.Action, method string, code int, err error) {
    if len(a.Actions) < 1000 {
        if method == http.MethodPut || method == http.MethodDelete {
            action.Attributes["warnings"] = []string{"action to update a parameter"}
        }
        if code != 0 { 
            action.Attributes["code"] = code 
        }
        if err != nil { 
            action.Attributes["error"] = err.Error() 
        }
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
        //go a.SetAction(action, r.Method, 400, err)
        return
    }

    code, err := backendChecks(a.Backend, params, path, user, pass, strings.ToLower(r.Method))
    if err != nil {
        log.Printf("[error] %v (%v)", err.Error(), r.URL.Path)
        w.WriteHeader(code)
        w.Write(encodeResp(&errResp{Error:code, Message:err.Error(), Cause: path}))
        //go a.SetAction(action, r.Method, code, err)
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
            //go a.SetAction(action, r.Method, code, err)
            return
        }

        if r.Header.Get("X-Custom-Format") == "confd" {
            jsn := getEtcdNodes(resp.Node.Nodes)

            data, err := json.Marshal(jsn)
            if err != nil {
                log.Printf("[error] %v (%v)", err, r.URL.Path)
                w.WriteHeader(500)
                //go a.SetAction(action, r.Method, 500, err)
                return
            }

            hash := config.GetHash(data)
            if r.Header.Get("X-Custom-Hash") == hash {
                w.WriteHeader(204)
                return
            }

            w.Header().Set("X-Custom-Hash", hash)
            w.Write(data)
            //go a.SetAction(action, r.Method, 200, nil)
            return
        }

        data, err := json.Marshal(resp)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            //go a.SetAction(action, r.Method, 500, err)
            return
        }
        w.Write(data)
        //go a.SetAction(action, r.Method, 200, nil)        
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
            //go a.SetAction(action, r.Method, code, err)
            return
        } 

        data, err := json.Marshal(resp)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            //go a.SetAction(action, r.Method, 500, err)
            return
        }

        w.Write(data)
        //go a.SetAction(action, r.Method, 200, nil)
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
            //go a.SetAction(action, r.Method, code, err)
            return
        } 

        data, err := json.Marshal(resp)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            //go a.SetAction(action, r.Method, 500, err)
            return
        }

        w.Write(data)
        //go a.SetAction(action, r.Method, 200, nil)
        return
    }
    
    w.WriteHeader(405)
    //go a.SetAction(action, r.Method, 405, nil)
}