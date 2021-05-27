package v2

import (
    "log"
    //"net"
    //"fmt"
    "net/http"
    "time"
    "regexp"
    "context"
    //"sync"
    //"reflect"
    //"strconv"
    "strings"
    //"io/ioutil"
    "encoding/json"
    //"google.golang.org/grpc/grpclog"
	"github.com/coreos/etcd/client"
    //"github.com/ltkh/confd/internal/config"
)

type Response struct {
    Status         string                 `json:"status"`
    Error          string                 `json:"error,omitempty"`
    Warnings       []string               `json:"warnings,omitempty"`
    Data           interface{}            `json:"data"`
}

type ApiEtcd struct {
    Client         client.Client
}

func getNodes(nodes client.Nodes) (map[string]interface{}) {
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
                jsn[key] = getNodes(node.Nodes)
            }
        }

    }

    return jsn
}

func GetEtcdClient() (client.Client, error) {

    cfg := client.Config{
        Endpoints:               []string{"http://127.0.0.1:2379"},
        Transport:               client.DefaultTransport,
        HeaderTimeoutPerRequest: 5 * time.Second,
    }

    client, err := client.New(cfg)
    if err != nil {
        return nil, err
    }

    return client, nil
}

func (a *ApiEtcd) ServeHTTP(w http.ResponseWriter, r *http.Request) {

    path := strings.Replace(r.URL.Path, "/api/v2/etcd", "", 1)
    kapi := client.NewKeysAPI(a.Client)

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

        resp, err := kapi.Get(context.Background(), path, opts)
        if err != nil {
            log.Printf("[error] %v", err)
            w.WriteHeader(400)
            w.Write([]byte(err.Error()))
            return
        }

        jsn := getNodes(resp.Node.Nodes)

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

        val := r.PostForm.Get("value")
        dir := r.PostForm.Get("dir")

        if dir == "true" {
            opts.Dir = true
        }

        resp, err := kapi.Set(context.Background(), path, val, opts)
        if err != nil {
            log.Printf("[error] %v", err)
            w.WriteHeader(400)
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