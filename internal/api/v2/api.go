package v2

import (
    "log"
    "net/http"
    "time"
    "regexp"
    "context"
    "strings"
    "encoding/json"
	"github.com/coreos/etcd/client"
    "github.com/ltkh/confd/internal/config"
)

type ApiEtcd struct {
    Id             string
    Client         client.KeysAPI
    KeyMasks       []string
}

func getEtcdNodes(nodes client.Nodes) (map[string]interface{}) {
    jsn := map[string]interface{}{}

    if nodes != nil {

        re := regexp.MustCompile(`.*/([^/]+)$`)
    
        for _, node := range nodes {
            key := re.ReplaceAllString(node.Key, "$1")
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

func GetEtcdClient(back config.Backend) (*client.KeysAPI, error) {

    conf := client.Config{
        Endpoints:               back.Nodes,
        Username:                back.Username,
        Password:                back.Password,
        Transport:               client.DefaultTransport,
        HeaderTimeoutPerRequest: 5 * time.Second,
    }

    etcd, err := client.New(conf)
    if err != nil {
        return nil, err
    }

    kapi := client.NewKeysAPI(etcd)

    return &kapi, nil
}

func (a *ApiEtcd) ServeHTTP(w http.ResponseWriter, r *http.Request) {

    path := strings.Replace(r.URL.Path, "/api/v2/"+a.Id, "", 1)

    if len(a.KeyMasks) > 0 {
        for _, mask := range a.KeyMasks {
            matched, _ := regexp.MatchString(mask, path)
            if matched {
                goto work
            }
        }
        log.Printf("[error] 403: Access is denied (%v)", path)
        w.WriteHeader(403)
        w.Write([]byte("Access is denied"))
        return
    }

    work:

        if r.Method == http.MethodGet {
            
            opts := &client.GetOptions{}

            rec, ok := r.URL.Query()["recursive"]
            if ok && rec[0] == "true" {
                opts.Recursive = true
            }

            srt, ok := r.URL.Query()["sorted"]
            if ok && srt[0] == "true" {
                opts.Sort = true
            }

            resp, err := a.Client.Get(context.Background(), path, opts)
            if err != nil {
                log.Printf("[error] %v", err)
                w.WriteHeader(404)
                w.Write([]byte(err.Error()))
                return
            }

            jsn := getEtcdNodes(resp.Node.Nodes)

            data, err := json.Marshal(jsn)
            if err != nil {
                log.Printf("[error] %v", err)
                w.WriteHeader(500)
                return
            }

            w.Header().Set("Content-Type", "application/json")
            w.Write(data)

            return

        }

        if r.Method == http.MethodPut {

            opts := &client.SetOptions{}

            err := r.ParseForm()
            if err != nil {
                log.Printf("[error] %v - %s", err, r.URL.Path)
                w.WriteHeader(400)
                w.Write([]byte(err.Error()))
                return
            }

            if r.PostForm.Get("dir") == "true" {
                opts.Dir = true
            }

            resp, err := a.Client.Set(context.Background(), path, r.PostForm.Get("value"), opts)
            if err != nil {
                log.Printf("[error] %v", err)
                w.WriteHeader(502)
                w.Write([]byte(err.Error()))
                return
            } 

            data, err := json.Marshal(resp)
            if err != nil {
                log.Printf("[error] %v", err)
                w.WriteHeader(500)
                return
            }

            w.Header().Set("Content-Type", "application/json")
            w.Write(data)

            return

        }
        
    w.WriteHeader(405)
}