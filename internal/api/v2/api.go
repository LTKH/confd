package v2

import (
    "log"
    "net/http"
    "time"
    "regexp"
    "strings"
    "context"
    "encoding/json"
    "github.com/coreos/etcd/client"
    "github.com/ltkh/confd/internal/config"
)

type ApiEtcd struct {
    Id             string
    ReadClient     *client.Client
    WriteClient    *client.Client
    ReadMasks      []string
    WriteMasks     []string
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

func GetEtcdClient(back config.Backend) (*ApiEtcd, error) {
    if back.Read.Username == "" && back.Username != "" {
        back.Read.Username = back.Username
        if back.Read.Password == "" && back.Password != "" {
            back.Read.Password = back.Password
        }
    }
    if back.Write.Username == "" && back.Username != "" {
        back.Write.Username = back.Username
        if back.Write.Password == "" && back.Password != "" {
            back.Write.Password = back.Password
        }
    }
    
    readClient, err := client.New(client.Config{
        Endpoints:               back.Nodes,
        Username:                back.Read.Username,
        Password:                back.Read.Password,
        Transport:               client.DefaultTransport,
        HeaderTimeoutPerRequest: 5 * time.Second,
    })
    if err != nil {
        return nil, err
    }

    writeClient, err := client.New(client.Config{
        Endpoints:               back.Nodes,
        Username:                back.Write.Username,
        Password:                back.Write.Password,
        Transport:               client.DefaultTransport,
        HeaderTimeoutPerRequest: 5 * time.Second,
    })
    if err != nil {
        return nil, err
    }

    api := &ApiEtcd{
        Id:            back.Id,
        ReadClient:    &readClient,
        WriteClient:   &writeClient,
        ReadMasks:     back.Read.KeyMasks,
        WriteMasks:    back.Write.KeyMasks,
    }

    return api, nil
}

func (a *ApiEtcd) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    path := strings.Replace(r.URL.Path, "/api/v2/"+a.Id, "", 1)

    if r.Method == http.MethodGet {

        kapi := client.NewKeysAPI(*a.ReadClient)

        if len(a.ReadMasks) > 0 {
            matched := false
            for _, mask := range a.ReadMasks {
                matched, _ = regexp.MatchString(mask, path)
                if matched { break }
            }
            if !matched {
                log.Printf("[error] 403: Access is denied (%v)", path)
                w.WriteHeader(403)
                w.Write([]byte("Access is denied"))
                return
            }
        }
        
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

        hash, ok := r.URL.Query()["hash"]
        if ok && hash[0] != "" {
            if hash[0] == config.GetHash(data) {
                w.WriteHeader(204)
                return
            }
        }

        w.Header().Set("Content-Type", "application/json")
		w.Write(data)

        return

    }

    if r.Method == http.MethodPut {

        kapi := client.NewKeysAPI(*a.WriteClient)

        if len(a.WriteMasks) > 0 {
            matched := false
            for _, mask := range a.WriteMasks {
                matched, _ = regexp.MatchString(mask, path)
                if matched { break }
            }
            if !matched {
                log.Printf("[error] 403: Access is denied (%v)", path)
                w.WriteHeader(403)
                w.Write([]byte("Access is denied"))
                return
            }
        }

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