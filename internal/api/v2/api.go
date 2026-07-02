package v2

import (
    //"io"
    "io/ioutil"
    "log"
    "net/http"
    "net/url"
    "sync"
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
    "go.etcd.io/etcd/client/v2"
    "github.com/xeipuuv/gojsonschema"
    "github.com/ltkh/confd/internal/config"
)

var (
    keyRegexp = regexp.MustCompile(`.*/([^/]+)$`)
    store = newStore()
)

type ApiEtcd struct {
    Id            string
    ReadClient    *client.Client
    WriteClient   *client.Client
    Backend       *config.Backend
    Actions       chan *config.Action
    Debug         bool
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

type Store struct {
    Lock           sync.RWMutex
    Root           *client.Node
    Index          map[string]*client.Node
}

type Options struct {
	Recursive      bool
	Sort           bool
	Quorum         bool
    Wait           bool
    Dir            bool
}

func getIPAddress(r *http.Request) string {
    IPAddress := r.Header.Get("X-Real-Ip")
    if IPAddress == "" {
        IPAddress = r.Header.Get("X-Forwarded-For")
    } 
    if IPAddress == "" {
        IPAddress = r.RemoteAddr
    }
    if IPAddress == "" { 
        IPAddress = "unknown" 
    }
    return IPAddress
}

func encodeResp(resp *errResp) []byte {
    jsn, err := json.Marshal(resp)
    if err != nil {
        return encodeResp(&errResp{Error:500, Message:err.Error(), Cause: resp.Cause})
    }
    return jsn
}

func backendChecks(backend *config.Backend, params map[string]string, path, user, pass, method string) (int, int, error) {
    proceed := false

    if _, ok := backend.Checks[method]; !ok {
        return 405, 405, errors.New("Method Not Allowed")
    }
    
    for _, check := range backend.Checks[method] {
        code := 400;

        if check.Continue && proceed {
            continue
        }
        if check.Path != "" {
            if !check.RePath.MatchString(path){
                continue
            }
        }
        if check.Pattern != "" {
            if matched, _ := filepath.Match(check.Pattern, path); !matched { 
                continue
            }
        }
        if len(check.Users) > 0 {
            usr, ok := check.Users[user]
            if !ok || usr.Password != pass {
                return 403, 403, errors.New("Access is denied")
            }
            if usr.ErrCode != 0 {
                code = usr.ErrCode
            }
        }
        if method == "put" || method == "post" {
            if check.Dir == "true" && params["dir"] != "true" {
                return 400, 400, errors.New("Invalid parameter type: Directory expected")
            }

            if check.Dir == "false" && params["dir"] == "true" {
                return 400, 400, errors.New("Invalid parameter type: Not directory expected")
            }

            if check.Regexp != "" {
                if params["dir"] != "true" && !check.ReRegexp.MatchString(params["value"]){
                    return code, 400, errors.New("Invalid parameter value")
                }
                if params["dir"] == "true" && !check.ReRegexp.MatchString(params["dir"]){
                    return code, 400, errors.New("Invalid parameter name")
                }
            }

            if check.Schema != "" {
                schema := gojsonschema.NewReferenceLoader(check.Schema)
                document := gojsonschema.NewStringLoader(params["value"])

                result, err := gojsonschema.Validate(schema, document)
                if err != nil {
                    return code, 400, err
                }

                if !result.Valid() {
                    for _, desc := range result.Errors() {
                        return code, 400, errors.New(desc.String())
                    }
                }
            }
        }
        if check.SkipCont {
            proceed = true
        }
        if check.Continue {
            continue
        }
        break
    }

    return 0, 0, nil
}

func getAllowedNodes(backend *config.Backend, nodes client.Nodes, user, pass, method string) client.Nodes {
    var nnodes client.Nodes

    for _, node := range nodes {

        code, _, _ := backendChecks(backend, nil, node.Key, user, pass, method)

        if code == 0 {
            // Создаем абсолютно новый объект в памяти (выделяем новый адрес)
            clonedNode := &client.Node{
                Key:           node.Key,
                Dir:           node.Dir,
                Value:         node.Value,
                CreatedIndex:  node.CreatedIndex,
                ModifiedIndex: node.ModifiedIndex,
                Expiration:    node.Expiration, 
                TTL:           node.TTL,
            }

            clonedNode.Nodes = getAllowedNodes(backend, node.Nodes, user, pass, method)
            nnodes = append(nnodes, clonedNode)
        }
    }

    return nnodes
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

func getTree(nodePath string) (keys []string) {
    array := strings.Split(nodePath, "/")

    for i := 0; i < len(array); i++ {
        k := strings.Join(array[:i+1], "/")
        if k == "/" { continue }
        keys = append(keys, k)
    }

    return keys
}

func removeIndex(s []*client.Node, index int) []*client.Node {
    return append(s[:index], s[index+1:]...)
}

func newStore() *Store {
    s := new(Store)
    s.Root = &client.Node{Key:"", Dir:true}
    return s
}

func (s *Store) Update(action string, node *client.Node) error {
    keys := getTree(node.Key)
     
    s.Lock.Lock()
    defer s.Lock.Unlock()

    nd := s.Root

    switch action {
    case "set":
        for k, key := range keys {
            if k == 0 && key == "" {
                continue
            }

            exists := false
            for i, n := range nd.Nodes {
                if n.Key == key {
                    if k == len(keys)-1 {
                        nd.Nodes[i] = node
                    }
                    nd = nd.Nodes[i]
                    exists = true
                    continue
                }
            }

            if !exists {
                if k == len(keys)-1 {
                    nd.Nodes = append(nd.Nodes, node)
                } else {
                    nd.Nodes = append(nd.Nodes, &client.Node{
                        Key:           key,
                        Dir:           true,
                        CreatedIndex:  0,
                        ModifiedIndex: 0,
                    })
                }
                nd = nd.Nodes[len(nd.Nodes)-1]
            }
        }
    case "delete":
        for k, key := range keys {
            if k == 0 && key == "" {
                continue
            }

            for i, n := range nd.Nodes {
                if n.Key == key {
                    if k == len(keys)-1 {
                        nd.Nodes = removeIndex(nd.Nodes, i)
                        return nil
                    }
                    nd = nd.Nodes[i]
                    continue
                }
            }
        }
    }
     
    return nil
}

func (s *Store) GetCache(path string) (*client.Node, bool) {   
    keys := getTree(path)

    s.Lock.RLock()
    defer s.Lock.RUnlock()

    nd := s.Root
    //nd := &client.Node{}
    //copy(nd, s.Root)
    //nd := deepcopy.Copy(s.Root).(*client.Node) 
    
    for _, key := range keys {
        if key == "" {
            continue
        }

        exists := false
        for i, n := range nd.Nodes {
            if n.Key == key {
                nd = nd.Nodes[i]
                exists = true
                continue
            }
        }

        if !exists || (nd.CreatedIndex == 0 && nd.ModifiedIndex == 0) {
            return nd, false
        }
    }
 
    return nd, true
}

func StoreUpdate(kapi client.KeysAPI) error {
    resp, err := kapi.Get(context.Background(), "/", &client.GetOptions{ Recursive: true, Sort: true })
    if err != nil {
        return err
    }
    for _, node := range resp.Node.Nodes {
        store.Update("set", node)
    }
    return nil
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

        // Заполняем cache
        if err := StoreUpdate(kapi); err != nil {
            return nil, err
        }

        // Создаем watcher на ключ или префикс
        watcher := kapi.Watcher("/", &client.WatcherOptions{ Recursive: true })

        // Запускаем цикл получения событий
        go func() {
            for {
                resp, err := watcher.Next(context.Background())
                if err != nil {
                    log.Printf("[error] %v", err)
                    time.Sleep(10 * time.Second)
                    StoreUpdate(kapi)
                    continue
                }
                store.Update(resp.Action, resp.Node)
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
        Debug:         backend.Debug,
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

func (a *ApiEtcd) SetAction(tp, user, err, cache string, r *http.Request, code int) {
    if (a.Debug && tp == "debug") || tp != "debug" {
        if user == "" { user = "-" }
        log.Printf("[%s] %s - %s \"%s %s%s\" %v %s", tp, getIPAddress(r), user, r.Method, r.URL.Path, cache, code, err)
    }
    /*
    if len(a.Actions) < 1000 {
        a.Actions <- &config.Action{
            Login:        user,
            Action:       "storage request",
            Object:       r.Header.Get("X-Forwarded-For"),
            Attributes:   map[string]interface{}{
                "method": r.Method,
                "path":   r.URL.Path,
                "code":   code,
                "error":  err
            },
            Description:  r.URL.Path,
            Timestamp:    time.Now().UTC().Unix(),
        }
    }
    */
}

func (a *ApiEtcd) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    path := strings.Replace(r.URL.Path, "/api/v2/"+a.Id, "", 1)
    user, pass, _ := r.BasicAuth()
    cache := ""

    params, err := parseForm(r)
    if err != nil {
        a.SetAction("error", user, err.Error(), cache, r, 400)
        w.WriteHeader(400)
        w.Write(encodeResp(&errResp{Error:400, Message:err.Error(), Cause: path}))
        return
    }

    opts := &Options{}
    if strings.ToLower(r.URL.Query().Get("recursive")) == "true" {
        opts.Recursive = true
    }
    if strings.ToLower(r.URL.Query().Get("sorted")) == "true" {
        opts.Sort = true
    }
    if strings.ToLower(r.URL.Query().Get("wait")) == "true" {
        opts.Wait = true
    }
    if strings.ToLower(params["dir"]) == "true" {
        opts.Dir = true
    }

    code, errCode, err := backendChecks(a.Backend, params, path, user, pass, strings.ToLower(r.Method))
    if err != nil {
        a.SetAction("error", user, err.Error(), cache, r, code)
        w.WriteHeader(code)
        w.Write(encodeResp(&errResp{Error:errCode, Message:err.Error(), Cause: path}))
        return
    }

    if r.Method == http.MethodGet {
        kapi := client.NewKeysAPI(*a.ReadClient)
        resp := &client.Response{}

        if opts.Wait {
            // Создаем watcher на ключ или префикс
            watcher := kapi.Watcher(path, &client.WatcherOptions{ Recursive: opts.Recursive })
            resp, err = watcher.Next(context.Background())
            if err != nil {
                a.SetAction("error", user, err.Error(), cache, r, 500)
                w.WriteHeader(500)
                w.Write(encodeResp(&errResp{Error:500, Message:err.Error(), Cause: path}))
                return
            }
        } else if !opts.Recursive || !a.Backend.Cache {
            resp, err = kapi.Get(context.Background(), path, &client.GetOptions{ Recursive: opts.Recursive, Sort: opts.Sort })
            if err != nil {
                if etcdErr, ok := err.(client.Error); ok {
                    httpCode := 200
                    if etcdErr.Code == 100 { httpCode = 404 }
                    a.SetAction("debug", user, etcdErr.Message, cache, r, httpCode)
                    w.WriteHeader(httpCode)
                    w.Write(encodeResp(&errResp{Error:etcdErr.Code, Message:etcdErr.Message, Cause: etcdErr.Cause}))
                    return
                }
                a.SetAction("error", user, err.Error(), cache, r, 500)
                w.WriteHeader(500)
                return
            }
        } else if opts.Recursive && a.Backend.Cache {
            node, exists := store.GetCache(path)
            if !exists {
                resp, err = kapi.Get(context.Background(), path, &client.GetOptions{ Recursive: opts.Recursive, Sort: opts.Sort })
                if err != nil {
                    if etcdErr, ok := err.(client.Error); ok {
                        httpCode := 200
                        if etcdErr.Code == 100 { httpCode = 404 }
                        a.SetAction("debug", user, etcdErr.Message, cache, r, httpCode)
                        w.WriteHeader(httpCode)
                        w.Write(encodeResp(&errResp{Error:etcdErr.Code, Message:etcdErr.Message, Cause: etcdErr.Cause}))
                        return
                    }
                    a.SetAction("error", user, err.Error(), cache, r, 500)
                    w.WriteHeader(500)
                    return
                }
                store.Update("set", resp.Node)
            } else {
                cache = " (cache)"
                resp = &client.Response{ Node: node }
            }
        }

        // Применение ролевой модели ко всему дереву ключей
        nodes := getAllowedNodes(a.Backend, resp.Node.Nodes, user, pass, strings.ToLower(r.Method))

        // Формирование ответа для агента confd
        if r.Header.Get("X-Custom-Format") == "confd" {
            jsn := getEtcdNodes(nodes)

            data, err := json.Marshal(jsn)
            if err != nil {
                a.SetAction("error", user, err.Error(), cache, r, 500)
                w.WriteHeader(500)
                return
            }

            hash := config.GetHash(data)
            if r.Header.Get("X-Custom-Hash") == hash {
                w.WriteHeader(204)
                return
            }

            a.SetAction("debug", user, "", cache, r, 200)
            w.Header().Set("X-Custom-Hash", hash)
            w.Write(data)
            return
        }

        data, err := json.Marshal(&client.Response{ Action: "get", Node: &client.Node{ Nodes: nodes }})
        if err != nil {
            a.SetAction("error", user, err.Error(), cache, r, 500)
            w.WriteHeader(500)
            return
        }

        a.SetAction("debug", user, "", cache, r, 200)   
        w.Write(data)
        return
    }

    if r.Method == http.MethodPut {
        kapi := client.NewKeysAPI(*a.WriteClient)

        resp, err := kapi.Set(context.Background(), path, params["value"], &client.SetOptions{ Dir: opts.Dir })
        if err != nil {
            if etcdErr, ok := err.(client.Error); ok {
                httpCode := 200
                if etcdErr.Code == 102 { httpCode = 400 }
                a.SetAction("debug", user, etcdErr.Message, cache, r, httpCode)
                w.Write(encodeResp(&errResp{Error:etcdErr.Code, Message:etcdErr.Message, Cause: etcdErr.Cause}))
                return
            }
            a.SetAction("error", user, err.Error(), cache, r, 500)
            w.WriteHeader(500)
            return
        } 

        data, err := json.Marshal(resp)
        if err != nil {
            a.SetAction("error", user, err.Error(), cache, r, 500)
            w.WriteHeader(500)
            return
        }
        
        a.SetAction("debug", user, "", cache, r, 200)
        w.WriteHeader(200)
        w.Write(data)
        return
    }

    if r.Method == http.MethodDelete {
        kapi := client.NewKeysAPI(*a.WriteClient)

        resp, err := kapi.Delete(context.Background(), path, &client.DeleteOptions{ Recursive: opts.Recursive })
        if err != nil {
            if etcdErr, ok := err.(client.Error); ok {
                httpCode := 200
                if etcdErr.Code == 100 { httpCode = 404 }
                a.SetAction("debug", user, etcdErr.Message, cache, r, httpCode)
                w.WriteHeader(httpCode)
                w.Write(encodeResp(&errResp{Error:etcdErr.Code, Message:etcdErr.Message, Cause: etcdErr.Cause}))
                return
            }
            a.SetAction("error", user, err.Error(), cache, r, 500)
            w.WriteHeader(500)
            return
        } 

        data, err := json.Marshal(resp)
        if err != nil {
            a.SetAction("error", user, err.Error(), cache, r, 500)
            w.WriteHeader(500)
            return
        }

        a.SetAction("debug", user, "", cache, r, 200)
        w.Write(data)
        return
    }
    
    w.WriteHeader(204)
}