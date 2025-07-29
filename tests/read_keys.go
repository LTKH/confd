package main
 
import (
    //"os"
    //"os/exec"
    //"bufio"
    "fmt"
    "log"
    "flag"
    "sync"
    "time"
    "bytes"
    //"regexp"
    //"strings"
    "net/url"
    "net/http"
    "crypto/tls"
    "io/ioutil"
    "encoding/json"
)
 
var (
    wg sync.WaitGroup
    cActions = make(chan Action, 1000)
)
 
type Data struct {
    Action       string      `json:"action"`
    Node         Node        `json:"node"`
}
 
type Node struct {
    Key          string      `json:"key"`
    Dir          bool        `json:"dir"`
    Value        string      `json:"value"`
    Nodes        []Node      `json:"nodes"`
}
 
type Value struct {
    Value        string      `json:"value"`
}
 
type Action struct {
    Login            string                  `json:"login"`
    Action           string                  `json:"action"`
    Object           string                  `json:"object"`
    Attributes       map[string]interface{}  `json:"attributes"`
    Description      string                  `json:"description"`
    Timestamp        int64                   `json:"timestamp"`
}
 
type Actions struct {
    Array        []Action    `json:"actions"`
}
 
func worker(sem chan Node, wg *sync.WaitGroup) {
    wg.Add(1)
    defer wg.Done()
     
    time.Sleep(5000000)
    //time.Sleep(500000000)
 
    node := <-sem
     
    client := &http.Client{ Timeout: 20 * time.Second }
    data := []byte("value=" + url.QueryEscape(node.Value))
             
    req, err := http.NewRequest("PUT", "http://sib-mon-dev01:30200/api/v2/etcd03"+node.Key, bytes.NewBuffer(data))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
     
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("\033[31m[error] %v\033[00m", err)
        return
    }
    defer resp.Body.Close()
     
    //Test alerttrap
    action := Action{
        Login:        "test",
        Action:       "storage request",
        Attributes:   map[string]interface{}{
            "method": "PUT",
            "path":   "/api/v2/etcd03"+node.Key,
        },
        Description:  node.Key,
        Timestamp:    time.Now().UTC().Unix(),
    }
     
    if len(cActions) < 1000 {
        cActions <- action
    }
     
    if resp.StatusCode > 201 {
        log.Printf("\033[31m%d - %s\033[00m", resp.StatusCode, node.Key)
         
        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            log.Printf("[error] %v", err)
            return
        }
        log.Printf("%s", string(body))
     
        //match, _ := regexp.MatchString(".*/emon_json$", node.Key)
        //if match == true {
        //    node.Key = strings.Replace(node.Key, "emon_json", "xmon_json", 1)
        //   
        //    req, err := http.NewRequest("PUT", "http://sib-mon-dev01:30200/api/v2/etcd03"+node.Key, bytes.NewBuffer(data))
        //    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        //    //req.Header.Set("Authorization","Basic YXV0bzo0WmokS2V4QDVDJmgK")
        //    req.SetBasicAuth("auto", "4Zj$Kex@5C&h")
        //   
        //    resp, err := client.Do(req)
        //    if err != nil {
        //        log.Printf("[error] %v", err)
        //        return
        //    }
        //    defer resp.Body.Close()
        //   
        //    body, err := ioutil.ReadAll(resp.Body)
        //    if err != nil {
        //        log.Printf("[error] %v", err)
        //        return
        //    }
        //   
        //    if resp.StatusCode > 201 {
        //        log.Printf("\033[31m%d - %s\033[00m", resp.StatusCode, node.Key)
        //       
        //        mtch, _ := regexp.MatchString("(invalid semicolon separator in query|unexpected EOF)", string(body))
        //        if mtch == true {
        //            return
        //        }
        //       
        //        log.Fatalf("%s", string(body))
        //    } else {
        //        log.Printf("%d - %s", resp.StatusCode, node.Key)
        //    }
        //   
        //    return
        //} else {
        //    log.Fatal("")
        //}
         
        log.Fatal("")
 
    } else {
        log.Printf("%d - %s", resp.StatusCode, node.Key)
    }
}
 
func getNodes(sem chan Node, nodes []Node) {
    for _, node := range nodes {
        if len(node.Nodes) > 0 {
            getNodes(sem, node.Nodes)
            continue
        }
 
        if node.Dir == true {
            continue
        }
         
        //match, _ := regexp.MatchString("^/ps/(hosts/dv|hosts/kvk|data|config)", node.Key)
        //if match == true {
        //    continue
        //}
         
        //sem <- node
        //go worker(sem, &wg)
         
    }
}
 
func httpRequest(method, url, login, pass string) (map[string]interface{}, error) {
    start := time.Now()
    //data := Data{}
    data := map[string]interface{}{}
 
    client := &http.Client{
        Transport: &http.Transport{
            MaxIdleConnsPerHost: 10,
            IdleConnTimeout:     90 * time.Second,
            DisableCompression:  false,
            TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
        },
        Timeout: 10 * time.Second,
    }
         
    req, err := http.NewRequest(method, url, nil) 
    if err != nil {
        return data, err
    }
     
    req.SetBasicAuth(login, pass)
    req.Header.Set("X-Custom-Format", "confd")
     
    resp, err := client.Do(req)
    if err != nil {
        return data, err
    }
    defer resp.Body.Close()
     
    if resp.StatusCode > 204 {
        return data, fmt.Errorf("%v - %v (%d)", method, url, resp.StatusCode)
    }
     
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return data, err
    }
 
    if err := json.Unmarshal(body, &data); err != nil {
        return data, err
    }
     
    log.Printf("[info] %v - %v (%v)", method, url, time.Since(start))
    return data, nil
}
 
func main() {  
	login := flag.String("login", "", "login")
    password := flag.String("password", "", "password")
    flag.Parse()
     
    start := time.Now()
    
    url := "http://localhost:8083/api/v2/etcd"
     
    data, err := httpRequest("GET", url+"/ps/hosts", *login, *password)
    if err != nil {
        log.Printf("[error] %v", err)
        return
    }
     
    for group, _ := range data {
        data, err := httpRequest("GET", url+"/ps/hosts/"+group, *login, *password)
        if err != nil {
            log.Printf("[error] %v", err)
            return
        }
        for host, _ := range data {
            _, err := httpRequest("GET", url+"/ps/hosts/"+group+"/"+host+"/cmdb", *login, *password)
            if err != nil {
                log.Printf("[error] %v", err)
                continue
            }
        }
    }
     
    log.Printf("[info] total time spent reading - %v", time.Since(start))
     
    //sem := make(chan Node, 1)
    //getNodes(sem, data.Node.Nodes)
    //wg.Wait()
}