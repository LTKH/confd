package v1

import (
    "log"
    "net/http"
    "regexp"
    "strings"
    "io/ioutil"
    "encoding/json"
    "github.com/hashicorp/consul/api"
    "github.com/ltkh/confd/internal/config"
)

type ApiConsul struct {
    Id             string
    Client         *api.Client
}

func getConsulNodes(nodes api.KVPairs) (map[string]interface{}) {
    jsn := map[string]interface{}{}
    re := regexp.MustCompile(`^(/?[^/]+).*$`)

    for _, node := range nodes {
        key := re.ReplaceAllString(node.Key, "$1")
        k := strings.Replace(key, "/", "", 1)
        //log.Printf("[debug] %v", node)
        if key == node.Key {
            var v interface{}
            err := json.Unmarshal(node.Value, &v)
            if err == nil {
                jsn[k] = v
            } else {
                jsn[k] = string(node.Value)
            }
        } else {
            var nds api.KVPairs
            for _, node := range nodes {
                if strings.Index(node.Key, key) == 0 {
                    nds = append(nds, &api.KVPair{ 
                        Key: strings.Replace(node.Key, key, "", 1), 
                        Value: node.Value,
                    })
                }
            }
            jsn[k] = getConsulNodes(nds)
        }
    }

    return jsn
}

func GetConsulClient(back config.Backend) (*api.Client, error) {

    conf := api.DefaultConfig()

    if len(back.Nodes) > 0 {
		conf.Address = back.Nodes[0]
	}
    
    //if back.Username != "" && back.Password != "" {
    //    conf.HttpAuth = &api.HttpBasicAuth{
    //        Username: back.Username,
    //        Password: back.Password,
    //    }
    //}

    client, err := api.NewClient(conf)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (a *ApiConsul) ServeHTTP(w http.ResponseWriter, r *http.Request) {

    path := strings.Replace(r.URL.Path, "/api/v1/"+a.Id, "", 1)
    clkv := a.Client.KV()

    if r.Method == http.MethodGet {

        resp, _, err := clkv.List(path, nil)
        if err != nil {
            log.Printf("[error] %v", err)
            w.WriteHeader(400)
            w.Write([]byte(err.Error()))
            return
        }

        if resp != nil {
            jsn := getConsulNodes(resp)

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

        w.WriteHeader(404)
        return
        
    }

    if r.Method == http.MethodPut {

        body, err := ioutil.ReadAll(r.Body)
        if err != nil {
            log.Printf("[error] %v - %s", err, r.URL.Path)
            w.WriteHeader(400)
            w.Write([]byte(err.Error()))
            return
        }
        defer r.Body.Close()

        p := &api.KVPair{Key: strings.TrimLeft(path, "/"), Flags: 42, Value: body}
        resp, err := clkv.Put(p, nil)
        if err != nil {
            log.Printf("[error] %v - %s", err, r.URL.Path)
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