package v2

import (
    "log"
    "net/http"
    "time"
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
    Id             string
    ReadClient     *client.Client
    WriteClient    *client.Client
    Backend        *config.Backend
}

type errResp struct {
    Error        int                       `json:"errorCode"`
    Message      string                    `json:"message"` 
    Cause        string                    `json:"cause"`
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

func parseForm(r *http.Request) (string, string, error) {
    if err := r.ParseForm(); err != nil {
        return "", "", err
    }

    return r.PostForm.Get("dir"), r.PostForm.Get("value"), nil
}

func backendChecks(backend *config.Backend, path string, r *http.Request) (int, error) {
    for _, check := range backend.Checks {
        if r.Method != check.Method {
            continue
        }
        if check.Path != "" && !check.RePath.MatchString(path){
            continue
        }
        if len(check.Users) > 0 {
            user, pass, auth := r.BasicAuth()

            if !auth {
                return 401, errors.New("Unauthorized")
            }

            if !inArray(user, check.Users) {
                return 403, errors.New("Access is denied")
            }

            if val, ok := backend.Users[user]; !ok || val != pass {
                return 403, errors.New("Access is denied")
            }
        }
        if r.Method == "PUT" || r.Method == "POST" {
            dir, val, err := parseForm(r)
            if err != nil {
                return 400, err
            }

            if check.Dir != dir {
                return 400, errors.New("Invalid parameter type")
            }

            if check.Regexp != "" {
                if dir == "" && !check.ReRegexp.MatchString(val){
                    return 400, errors.New("Invalid parameter value")
                }
                if dir != "" && !check.ReRegexp.MatchString(dir){
                    return 400, errors.New("Invalid parameter name")
                }
            }

            if check.Schema != "" {
                schema := gojsonschema.NewReferenceLoader(check.Schema)
                document := gojsonschema.NewStringLoader(val)

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

func GetEtcdClient(backend config.Backend) (*ApiEtcd, error) {

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
    }

    return api, nil
}

func (a *ApiEtcd) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    path := strings.Replace(r.URL.Path, "/api/v2/"+a.Id, "", 1)

    code, err := backendChecks(a.Backend, path, r)
    if err != nil {
        log.Printf("[error] %d: %v (%v)", code, err.Error(), r.URL.Path)
        w.WriteHeader(code)
        w.Write(encodeResp(&errResp{Error:code, Message:err.Error(), Cause: path}))
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
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            if strings.Contains(err.Error(), "100: Key not found") {
                w.WriteHeader(404)
                w.Write(encodeResp(&errResp{Error:404, Message:err.Error(), Cause: path}))
            } else {
                w.WriteHeader(500)
            }
            return
        }

        if r.Header.Get("X-Custom-Format") == "base" {
            data, err := json.Marshal(resp)
            if err != nil {
                log.Printf("[error] %v (%v)", err, r.URL.Path)
                w.WriteHeader(500)
                return
            }

            w.Write(data)
            return
        }

        jsn := getEtcdNodes(resp.Node.Nodes)

        data, err := json.Marshal(jsn)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            return
        }

        hash := config.GetHash(data)
        if r.Header.Get("X-Custom-Hash") == hash {
            w.WriteHeader(204)
            return
        }

        w.Header().Set("X-Custom-Hash", hash)
        w.Write(data)
        return
    }

    if r.Method == http.MethodPut {

        kapi := client.NewKeysAPI(*a.WriteClient)

        opts := &client.SetOptions{}

        dir, val, err := parseForm(r)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(400)
            w.Write(encodeResp(&errResp{Error:400, Message:err.Error(), Cause: path}))
            return
        }

        if dir == "true" {
            opts.Dir = true
        }

        resp, err := kapi.Set(context.Background(), path, val, opts)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(502)
            return
        } 

        data, err := json.Marshal(resp)
        if err != nil {
            log.Printf("[error] %v (%v)", err, r.URL.Path)
            w.WriteHeader(500)
            return
        }

        w.Write(data)
        return
    }
    
    w.WriteHeader(405)
}