package main
 
import (
    //"net/url"
    "net/http"
    "log"
    "os"
    "os/signal"
    "syscall"
    "sync"
    "encoding/json"
    "flag"
    "time"
    "bytes"
    "io"
    "io/ioutil"
    "regexp"
    "strings"
    "crypto/tls"
    "crypto/aes"
    "crypto/cipher"
    "encoding/base64"
    //"sort"
    //"errors"
)
 
var (
    httpClient = &http.Client{
        Timeout: 60 * time.Second,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }
    KeyString string = "abc&1*~#^2^#s0^=)^^7%b34"
)
 
type Error struct {
    Cause          string       `json:"cause"`
    Message        string       `json:"message"`
    ErrorCode      int          `json:"errorCode"`
}
 
type Cache struct {
    sync.RWMutex
    host           string
    keys           map[string]*Node
}
 
type Search struct {
    Action         string      `json:"action,omitempty"`
    Nodes          []*Node     `json:"nodes"`
}
 
type Event struct {
    Action         string      `json:"action,omitempty"`
    Node           *Node       `json:"node,omitempty"`
    Nodes          []*Node     `json:"nodes,omitempty"`
    Health         string      `json:"health,omitempty"`
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
 
type Store struct {
    Root           *Node
    CurrentIndex   uint64
    CurrentVersion int
    worldLock      sync.RWMutex // stop the world lock
    User           string
    Password       string
    EtcdURL        string
    GroupMask      string
    HostsMask      string
}
 
type Config struct {
    User           string
    Password       string
    Listen         string
    EtcdURL        string
    GroupMask      string
    HostsMask      string 
}
 
func encrypt(text string) (string, error) {
    block, err := aes.NewCipher([]byte(KeyString))
    if err != nil {
        return "", err
    }
    plainText := []byte(text)
    bytes := []byte{35, 46, 57, 24, 85, 35, 24, 74, 87, 35, 88, 98, 66, 32, 14, 05}
    cfb := cipher.NewCFBEncrypter(block, bytes)
    cipherText := make([]byte, len(plainText))
    cfb.XORKeyStream(cipherText, plainText)
    return base64.StdEncoding.EncodeToString(cipherText), nil
}
 
func decrypt(text string) (string, error) {
    block, err := aes.NewCipher([]byte(KeyString))
    if err != nil {
        return "", err
    }
    cipherText, err := base64.StdEncoding.DecodeString(text)
    if err != nil {
        return "", err
    }
    bytes := []byte{35, 46, 57, 24, 85, 35, 24, 74, 87, 35, 88, 98, 66, 32, 14, 05}
    cfb := cipher.NewCFBDecrypter(block, bytes)
    plainText := make([]byte, len(cipherText))
    cfb.XORKeyStream(plainText, cipherText)
    return string(plainText), nil
}
 
func newConfig(configPath string) (*Config, error) {
    config := &Config{}
     
    file, err := os.Open(configPath)
    if err != nil {
        return nil, err
    }
    defer file.Close()
 
    d := json.NewDecoder(file)
 
    if err := d.Decode(&config); err != nil {
        return nil, err
    }
     
    if config.Password != "" {
        pass, err := decrypt(config.Password)
        if err != nil {
            return nil, err
        }
         
        config.Password = pass
    }
 
    return config, nil
}
 
func newStore(config *Config) *Store {
    s := new(Store)
    s.CurrentVersion = 2
    s.CurrentIndex = 0
    s.Root = newDir(s, "", s.CurrentIndex, nil)
    s.User = config.User
    s.Password = config.Password
    s.EtcdURL = config.EtcdURL
    s.GroupMask = config.GroupMask
    s.HostsMask = config.HostsMask
    return s
}
 
func newDir(store *Store, nodePath string, createdIndex uint64, parent *Node) *Node {
    return &Node{
        Key:           nodePath,
        Dir:           true,
        CreatedIndex:  createdIndex,
        ModifiedIndex: createdIndex,
    }
}
 
func getTree(nodePath string) (keys []string) {
    array := strings.Split(nodePath, "/")
    for i := 0; i < len(array); i++ {
        k := strings.Join(array[:i+1], "/")
        keys = append(keys, k)
    }
    return keys
}
 
func removeIndex(s []*Node, index int) []*Node {
    return append(s[:index], s[index+1:]...)
}
 
func (s *Store) Set(node *Node, update bool) error {
    keys := getTree(node.Key)
     
    s.worldLock.Lock()
    defer s.worldLock.Unlock()
     
    nd := s.Root
    for k, path := range keys {
        if k == 0 && path == "" {
            continue
        }
        exists := false
        for i, n := range nd.Nodes {
            if n.Key == path {
                if k == len(keys)-1 && update {
                    nd.Nodes[i] = node
                    //log.Printf("[info] update %q", n.Key)
                }
                nd = nd.Nodes[i]
                exists = true
                continue
            }
        }
        if !exists {
            if k == len(keys)-1 {
                nd.Nodes = append(nd.Nodes, node)
                //log.Printf("[info] set %q", node.Key)
            } else {
                nd.Nodes = append(nd.Nodes, newDir(s, path, s.CurrentIndex, nil))
                //log.Printf("[info] set %q", path)
            }
            nd = nd.Nodes[len(nd.Nodes)-1]
        }
    }
     
    return nil
}
 
func (s *Store) Delete(key string) error {
    keys := getTree(key)
     
    s.worldLock.Lock()
    defer s.worldLock.Unlock()
     
    nd := s.Root
    for k, path := range keys {
        if k == 0 && path == "" {
            continue
        }
        for i, n := range nd.Nodes {
            if n.Key == path {
                if k == len(keys)-1 {
                    nd.Nodes = removeIndex(nd.Nodes, i)
                    //log.Printf("[info] del %q", n.Key)
                    return nil
                }
                nd = nd.Nodes[i]
                continue
            }
        }
    }
     
    return nil
}
 
func (s *Store) GetCache(key string) interface{} {   
    keys := getTree(key)
     
    s.worldLock.RLock()
    defer s.worldLock.RUnlock()
     
    nd := s.Root
    for k, path := range keys {
        if k == 0 && path == "" {
            continue
        }
        exists := false
        for i, n := range nd.Nodes {
            if n.Key == path {
                nd = nd.Nodes[i]
                exists = true
                continue
            }
        }
        if !exists {
            return Error{ ErrorCode: 100, Message: "Key not found", Cause: key}
        }
    }
 
    return Event{ Action: "get", Node: nd }
}
 
func getNodes(ns []*Node, re *regexp.Regexp, dir string) []*Node {
    var nodes []*Node
    for _, n := range ns {
        if re.MatchString(n.Key) {
            if dir == "" || n.Dir == (dir == "true") {
                nodes = append(
                    nodes,
                    &Node {
                        Key:   n.Key,
                        Value: n.Value,
                        Dir:   n.Dir,
                    },
                )
            }
        }
        nodes = append(nodes, getNodes(n.Nodes, re, dir)...)
    }
     
    return nodes
}
 
func (s *Store) SearchNodes(key, dir string) (interface{}, error) {
    var nodes []*Node
     
    re, err := regexp.Compile(`^`+key+`$`)
    if err != nil {
        return nodes, err
    }
     
    s.worldLock.RLock()
    defer s.worldLock.RUnlock()
 
    nodes = getNodes(s.Root.Nodes, re, dir)
     
    if len(nodes) == 0 {
        nodes = make([]*Node, 0)
    }
 
    return Search{ Action: "search", Nodes: nodes }, nil
}
 
func (s *Store) Request(method, URN string, data io.Reader) ([]byte, Event, int, error) {
    var event Event
     
    req, err := http.NewRequest(method, s.EtcdURL+URN, data)
    if err != nil {
        return nil, event, 0, err
    }
    if method == "PUT" {
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
    }
     
    if s.User != "" && s.Password != "" {
        req.SetBasicAuth(s.User, s.Password)
    }
     
    res, err := httpClient.Do(req)
    if err != nil {
        return nil, event, 500, err
    }
    defer res.Body.Close()
     
    body, err := ioutil.ReadAll(res.Body)
    if err != nil {
        return nil, event, res.StatusCode, err
    }
     
    if err := json.Unmarshal(body, &event); err != nil {
        return body, event, res.StatusCode, err
    }
     
    if event.Node != nil {
        matched, _ := regexp.MatchString(`^\/ps\/hosts(\/[^/]+)?$`, event.Node.Key)
        if matched {
            var nodes []*Node
            mstring := s.HostsMask
            if event.Node.Key == "/ps/hosts" { mstring = s.GroupMask }
            for _, n := range event.Node.Nodes {
                m, _ := regexp.MatchString(mstring, n.Key)
                if m {
                    nodes = append(nodes, n)
                }
            }
            event.Node.Nodes = nodes
        }
         
        body, err = json.Marshal(event)
        if err != nil {
            return nil, event, res.StatusCode, err
        }
    }
     
    return body, event, res.StatusCode, nil
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
 
func (s *Store) Api(w http.ResponseWriter, r *http.Request) {
    matched, _ := regexp.MatchString(`\/v2\/keys.*`, r.URL.Path)
    if !matched {
        w.WriteHeader(404)
        return
    }
     
    w.Header().Set("Content-Type", "application/json")
     
    if r.Method == http.MethodGet {
        if strings.ToLower(r.URL.Query().Get("recursive")) == "true" {
            key := regexp.MustCompile(`^\/v2\/keys`).ReplaceAllString(r.URL.Path, "")
            key = regexp.MustCompile(`\/$`).ReplaceAllString(key, "")
             
            jsn, _ := json.Marshal(s.GetCache(key))
            log.Printf("[info] \"CACHE %v\" %v %v", r.URL.String(), 200, ReadUserIP(r))
            w.Write(jsn)
            return
        }
         
        body, data, code, err := s.Request("GET", r.URL.String(), nil)
        if err != nil {
            jsn, _ := json.Marshal(&Error{ Cause: r.URL.Path, Message: err.Error(), ErrorCode: code })
            w.WriteHeader(code)
            w.Write(jsn)
            return
        }
         
        if code < 300 && data.Node != nil {
            s.Set(data.Node, false)
        }
         
        log.Printf("[info] \"GET %v\" %v %v", r.URL.String(), code, ReadUserIP(r))
         
        w.WriteHeader(code)
        w.Write(body)
    }
     
    if r.Method == http.MethodPut {
        buffer, err := ioutil.ReadAll(r.Body)
        if err != nil {
            w.WriteHeader(400)
            w.Write([]byte(err.Error()))
            return
        }
        defer r.Body.Close()
         
        body, data, code, err := s.Request("PUT", r.URL.String(), bytes.NewReader(buffer))
        if err != nil {
            jsn, _ := json.Marshal(&Error{ Cause: r.URL.Path, Message: err.Error(), ErrorCode: code })
            w.WriteHeader(code)
            w.Write(jsn)
            return
        }
         
        if code < 300 {
            s.Set(data.Node, true)
        }
     
        log.Printf("[info] \"PUT %v\" %v %v", r.URL.String(), code, ReadUserIP(r))
         
        w.WriteHeader(code)
        w.Write(body)
    }
     
    if r.Method == http.MethodDelete {
        body, _, code, err := s.Request("DELETE", r.URL.String(), nil)
        if err != nil {
            jsn, _ := json.Marshal(&Error{ Cause: r.URL.Path, Message: err.Error(), ErrorCode: code })
            w.WriteHeader(code)
            w.Write(jsn)
            return
        }
         
        if code < 300 {
            s.Delete(strings.Replace(r.URL.Path, "/v2/keys", "", 1))
        }
         
        log.Printf("[info] \"DELETE %v\" %v %v", r.URL.String(), code, ReadUserIP(r))
         
        w.WriteHeader(code)
        w.Write(body)
    }
     
    if r.Method == http.MethodPost {
        jsn, _ := json.Marshal(&Error{ Cause: r.URL.Path, Message: "method not allowed", ErrorCode: 405 })
        log.Printf("[info] \"POST %v\" %v %v", r.URL.String(), 405, ReadUserIP(r))
        w.WriteHeader(405)
        w.Write(jsn)
    }
     
    return
}
 
func (s *Store) Search(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
     
    event, err := s.SearchNodes(r.URL.Query().Get("key"), r.URL.Query().Get("dir"))
    if err != nil {
        jsn, _ := json.Marshal(&Error{ Cause: r.URL.Path, Message: err.Error(), ErrorCode: 400 })
        w.WriteHeader(400)
        w.Write(jsn)
        return
    }
     
    jsn, _ := json.Marshal(event)
    log.Printf("[info] \"SEARCH %v\" %v %v", r.URL.String(), 200, ReadUserIP(r))
    w.Write(jsn)
}
 
func (s *Store) Health(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
     
    jsn, _ := json.Marshal(&Event{Health:"true"})
     
    w.WriteHeader(200)
    w.Write(jsn)
}
 
func main() {
    // Command-line flag parsing
    encrypt_pass := flag.String("encrypt", "", "encrypt string")
    configFile   := flag.String("config-file", "config.json", "config file")
    flag.Parse()
     
    // Encrypt
    if *encrypt_pass != "" {
        passwd, err := encrypt(*encrypt_pass)
        if err != nil {
            log.Fatalf("[error] %v", err)
        }
        log.Printf("[pass] %s", passwd)
        return
    }
     
    config, err := newConfig(*configFile)
    if err != nil {
        log.Fatalf("[error] %v", err)
    }
 
    // Program completion signal processing
    c := make(chan os.Signal, 2)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        log.Print("[info] etcdcache stopped")
        os.Exit(0)
    }()
     
    log.Print("[info] etcdcache started")
     
    store := newStore(config)
    go func(s *Store) {
        for {
            _, data, code, err := s.Request("GET", "/v2/keys/ps/config?recursive=true", nil)
            if err == nil && code == 200 {
                s.Set(data.Node, true)
            }
            _, data, code, err = s.Request("GET", "/v2/keys/ps/data?recursive=true", nil)
            if err == nil && code == 200 {
                s.Set(data.Node, true)
            }
            _, data, code, err = s.Request("GET", "/v2/keys/ps/hosts", nil)
            if err == nil && code == 200 {
                for _, node := range data.Node.Nodes {
                    _, dt, cd, er := s.Request("GET", "/v2/keys"+node.Key, nil)
                    if er == nil && cd == 200 {
                        for _, nd := range dt.Node.Nodes {
                            _, d, c, e := s.Request("GET", "/v2/keys"+nd.Key+"?recursive=true", nil)
                            if e == nil && c == 200 {
                                s.Set(d.Node, true)
                            }
                        }
                    }
                }
            }
            time.Sleep(600 * time.Second)
        }
    }(store)
 
    http.HandleFunc("/health", store.Health)
    http.HandleFunc("/search", store.Search)
    http.HandleFunc("/", store.Api)
 
    if err := http.ListenAndServe(config.Listen, nil); err != nil {
        log.Fatalf("[error] %v", err)
    }
}