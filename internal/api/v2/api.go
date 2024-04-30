package v2

import (
    "log"
    "net/http"
    "time"
    "regexp"
    "io"
    "io/ioutil"
    "strings"
    "bytes"
    "encoding/json"
    "github.com/ltkh/confd/internal/config"
)

var httpClient = &http.Client{Timeout: 60 * time.Second}

type ApiEtcd struct {
    Id             string
    Client         *http.Client
    KeyMasks       []string
    Username       string
    Password       string
    Nodes          []string
}

type Event struct {
    Action         string      `json:"action,omitempty"`
    Node           *Node       `json:"node,omitempty"`
}

type Node struct {
    Key            string      `json:"key,omitempty"`
    Value          *string     `json:"value,omitempty"`
    Dir            bool        `json:"dir"`
    ExpireTime     time.Time   `json:"-"`
    TTL            int64       `json:"ttl,omitempty"`
    Nodes          []*Node     `json:"nodes,omitempty"`
    ModifiedIndex  uint64      `json:"-"`
    CreatedIndex   uint64      `json:"-"`
}

func getEtcdNodes(nodes []*Node) (map[string]interface{}) {
    jsn := map[string]interface{}{}

    if nodes != nil {

        re := regexp.MustCompile(`.*/([^/]+)$`)
    
        for _, node := range nodes {
            key := re.ReplaceAllString(node.Key, "$1")
            if node.Dir != true {
                var v interface{}
                err := json.Unmarshal([]byte(*node.Value), &v)
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

    conf := &ApiEtcd{
        Nodes:       back.Nodes,
        Client:      &http.Client{Timeout: 60 * time.Second},
        Username:    back.Username,
        Password:    back.Password,
    }

    return conf, nil
}

func (a *ApiEtcd) Request(method, URN string, data io.Reader) (body []byte, code int, err error) {
    for _, node := range a.Nodes {

        req, err := http.NewRequest(method, node+URN, data)
        if err != nil {
            continue
        }
        if a.Username != "" && a.Password != "" {
            req.SetBasicAuth(a.Username, a.Password)
        }
        if method == "PUT" {
            req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
        }
        
        res, err := httpClient.Do(req)
        if err != nil {
            code = 500
            continue
        }
        defer res.Body.Close()
        
        body, err := ioutil.ReadAll(res.Body)
        if err != nil {
            code = res.StatusCode
            continue
        }
        
        return body, res.StatusCode, nil

    }

    return body, code, err
}

func ReadUserIP(r *http.Request) string {
    IPAddress := r.Header.Get("X-Real-Ip")
    if IPAddress == "" {
        IPAddress = r.Header.Get("X-Forwarded-For")
    }
    if IPAddress == "" {
        IPAddress = r.RemoteAddr
    }
    return IPAddress
}

func (a *ApiEtcd) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    path := strings.Replace(r.URL.String(), "/api/v2/"+a.Id, "/v2/keys", 1)

    if len(a.KeyMasks) > 0 {
        matched := false
        for _, mask := range a.KeyMasks {
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

    if r.Method == http.MethodGet {

        var event Event

        body, code, err := a.Request("GET", path, nil)
        if err != nil || code >= 400 {
            log.Printf("[error] %v: GET (%v) [%v]", code, r.URL.String(), ReadUserIP(r))
            w.WriteHeader(code)
            w.Write(body)
            return
        } 

        if err := json.Unmarshal(body, &event); err != nil {
            log.Printf("[error] 500: GET (%v) [%v]", r.URL.String(), ReadUserIP(r))
            w.WriteHeader(500)
            return
        }

        data, err := json.Marshal(getEtcdNodes(event.Node.Nodes))
        if err != nil {
            log.Printf("[error] 500: GET (%v) [%v]", r.URL.String(), ReadUserIP(r))
            w.WriteHeader(500)
            return
        }

        log.Printf("[info] %v: GET (%v) [%v]", code, r.URL.String(), ReadUserIP(r))
        w.WriteHeader(code)
        w.Write(data)
        return
    }

    if r.Method == http.MethodPut {

        buffer, err := ioutil.ReadAll(r.Body)
        if err != nil {
            log.Printf("[error] 400: PUT (%v) [%v]", r.URL.String(), ReadUserIP(r))
            w.WriteHeader(400)
            w.Write([]byte(err.Error()))
            return
        }
        defer r.Body.Close()

        body, code, err := a.Request("PUT", path, bytes.NewReader(buffer))
        if err != nil {
            log.Printf("[error] %v: PUT (%v) [%v]", code, r.URL.String(), ReadUserIP(r))
            w.WriteHeader(code)
            w.Write(body)
            return
        }

        log.Printf("[info] %v: PUT (%v) [%v]", code, r.URL.String(), ReadUserIP(r))
        w.WriteHeader(code)
        w.Write(body)
        return

    }
        
    w.WriteHeader(405)
}