package main

import (
    "log"
    "time"
    "os"
    "os/signal"
    "syscall"
    "runtime"
    "flag"
    "sync"
    "fmt"
    "os/exec"
    "context"
    "encoding/json"
    "io/ioutil"
    //"path/filepath"
    "math/rand"
    "strings"
    "net/http"
    "crypto/md5"
    "encoding/hex"
    //"text/template"
    "github.com/naoina/toml"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/ltkh/confd/internal/template"
)

type Config struct {
    Global              Global             `toml:"global"`
    Templates           []HTTPTemplate
}

type Global struct {
    URLs                []string           `toml:"urls"`
	ChecksFile          string             `toml:"checks_file"`
}

type HTTPTemplate struct {
    URLs                []string           `toml:"urls"`
    Key                 string             `toml:"key"`
    Timeout             string             `toml:"timeout"`
    Src                 string             `toml:"src"`
    Temp                string             `toml:"temp"`
    Dest                string             `toml:"dest"`
    CheckCmd            string             `toml:"check_cmd"`
    ReloadCmd           string             `toml:"reload_cmd"`

    ContentEncoding     string             `toml:"content_encoding"`

    Headers             map[string]string  `toml:"headers"`

    // HTTP Basic Auth Credentials
    Username            string             `toml:"username"`
    Password            string             `toml:"password"`

    // Absolute path to file with Bearer token
    BearerToken         string             `toml:"bearer_token"`

    funcMap             map[string]interface{}
    client              *http.Client
}

type Checks struct {
    Checks              []Check            `toml:"checks"`
}

type Check struct {
    Key                 string             `toml:"key"`
    Value               string             `toml:"value"`
    File                string             `toml:"file"`
    Http                string             `toml:"http"`
    Timeout             string             `toml:"timeout"`
}

func NewHttpTemplate(h *HTTPTemplate) *HTTPTemplate {

    // Set default timeout
    if h.Timeout == "" {
        h.Timeout = "10s"
    }

    timeout, _ := time.ParseDuration(h.Timeout)

    h.client = &http.Client{
        Transport: &http.Transport{
            Proxy:           http.ProxyFromEnvironment,
        },
        Timeout: timeout,
    }

    rand.Seed(time.Now().UnixNano())
    rand.Shuffle(len(h.URLs), func(i, j int) { h.URLs[i], h.URLs[j] = h.URLs[j], h.URLs[i] })

    return h
}

func (h *HTTPTemplate) httpRequest() ([]byte, error) {

    for _, url := range h.URLs {

        tmpl, err := template.NewTemplate()
        if err != nil {
            log.Printf("[error] %v", err)
            continue
        }

        path, err := tmpl.Execute(url, nil)
        if err != nil {
            log.Printf("[error] %v", err)
            continue
        }

        request, err := http.NewRequest("GET", string(path), nil)
        if err != nil {
            log.Printf("[error] %v", err)
            continue
        }

        if h.BearerToken != "" {
            token, err := ioutil.ReadFile(h.BearerToken)
            if err != nil {
                log.Printf("[error] %v", err)
                continue
            }
            bearer := "Bearer " + strings.Trim(string(token), "\n")
            request.Header.Set("Authorization", bearer)
        }

        if h.ContentEncoding == "gzip" {
            request.Header.Set("Content-Encoding", "gzip")
        }

        if h.Username != "" || h.Password != "" {
            request.SetBasicAuth(h.Username, h.Password)
        }

        resp, err := h.client.Do(request)
        if err != nil {
            log.Printf("[error] %v", err)
            continue
        }
        defer resp.Body.Close()

        body, err := ioutil.ReadAll(resp.Body)

        if resp.StatusCode >= 300 {
            log.Printf("[error] %s - received status code: %d", url, resp.StatusCode)
            continue
        }
        if err != nil {
            log.Printf("[error] %s - received error: %v", url, err)
            continue
        }

        return body, nil
    }

    return nil, fmt.Errorf("failed to complete any request")
}

func (h *HTTPTemplate) Gather() (interface{}, error) {
    
    body, err := h.httpRequest()
    if err != nil {
        return nil, err
    }

    var jsn interface{}
    if err := json.Unmarshal(body, &jsn); err != nil {
        return nil, fmt.Errorf("reading json from response body: %v", err)
    }

    return jsn, nil

}

func getHash(data []byte) string {
    hsh := md5.New()
    hsh.Write(data)
    return hex.EncodeToString(hsh.Sum(nil))
}

func (h *HTTPTemplate) CreateConf(jsn interface{}) (int, error) {

    tmpl, err := template.NewTemplate()
    if err != nil {
        return 3, fmt.Errorf("reading template: %v", err)
    }

    cont, err := tmpl.ParseFile(h.Src, jsn)
    if err != nil {
        return 3, fmt.Errorf("generating config: %v", err)
    }

    if _, err := os.Stat(h.Dest); err == nil {
        conf, err := ioutil.ReadFile(h.Dest)
        if err != nil {
            return 4, fmt.Errorf("reading config file %s: %v", h.Dest, err)
        }
        if getHash(conf) != getHash(cont) {
            if err := ioutil.WriteFile(h.Temp, cont, 0644); err != nil {
                return 4, fmt.Errorf("writing config file %s: %v", h.Dest, err)
            }
            return 1, nil
        }
    } else if os.IsNotExist(err) {
        if err := ioutil.WriteFile(h.Temp, cont, 0644); err != nil {
            return 4, fmt.Errorf("writing config file %s: %v", h.Dest, err)
        }
        return 1, nil
    } else {
        return 4, fmt.Errorf("reading config file status %s: %v", h.Dest, err)
    }

    return 0, nil
}

func runCommand(scmd string, timeout time.Duration) ([]byte, error) {
    log.Printf("[info] running '%s'", scmd)
    // Create a new context and add a timeout to it
    ctx, cancel := context.WithTimeout(context.Background(), timeout * time.Second)
    defer cancel() // The cancel should be deferred so resources are cleaned up

    // Create the command with our context
    var cmd *exec.Cmd
    if runtime.GOOS == "windows" {
        cmd = exec.CommandContext(ctx, "cmd", "/C", scmd)
    } else {
        cmd = exec.CommandContext(ctx, "/bin/sh", "-c", scmd)
    }

    // This time we can simply use Output() to get the result.
    out, err := cmd.Output()

    // Check the context error to see if the timeout was executed
    if ctx.Err() == context.DeadlineExceeded {
        return nil, fmt.Errorf("command timed out '%s'", scmd)
    }

    // If there's no context error, we know the command completed (or errored).
    if err != nil {
        return nil, fmt.Errorf("non-zero exit code: %v '%s'", err, scmd)
    }

    log.Printf("[info] finished '%s'", scmd)
    return out, nil
}

// loading configuration file
func loadConfigFile(file string) (Config, error) {
    var cfg Config

    f, err := os.Open(file)
    if err != nil {
        return cfg, err
    }
    
    if err := toml.NewDecoder(f).Decode(&cfg); err != nil {
        return cfg, err
    }

    f.Close()

    return cfg, nil
}

// loading checks file
func loadChecksFile(file string) (Checks, error) {
    var chs Checks

    f, err := os.Open(file)
    if err != nil {
        return chs, err
    }

    if err := toml.NewDecoder(f).Decode(&chs); err != nil {
        return chs, err
    }

    f.Close()

    return chs, nil
}

func main() {

    //limits the number of operating system threads
    runtime.GOMAXPROCS(runtime.NumCPU())

    //command-line flag parsing
    cfFile          := flag.String("config", "config/confd.toml", "config file")
    lgFile          := flag.String("logfile", "", "log file")
    interval        := flag.Int("interval", 30, "interval")
    plugin          := flag.String("plugin", "", "plugin")
    logMaxSize      := flag.Int("log.max-size", 1, "log max size") 
    logMaxBackups   := flag.Int("log.max-backups", 3, "log max backups")
    logMaxAge       := flag.Int("log.max-age", 10, "log max age")
    logCompress     := flag.Bool("log.compress", true, "log compress")

    srcFile         := flag.String("src-file", "", "source file")
    srcTmpl         := flag.String("src-tmpl", "", "source template")
    destFile        := flag.String("dest-file", "", "destination file")

    flag.Parse()

    // Generate configuration
    if *srcFile != "" {
        tmpl, err := template.NewTemplate()
        if err != nil {
            log.Fatalf("[error] generating template: %v", err)
        }

        data, err := ioutil.ReadFile(*srcFile)
        if err != nil {
            log.Fatalf("[error] reading source file: %v", err)
        }

        var jsn interface{}
        if err := json.Unmarshal(data, &jsn); err != nil {
            log.Fatalf("[error] parsing json file: %v", err)
        }

        cont, err := tmpl.ParseFile(*srcTmpl, jsn)
        if err != nil {
            log.Fatalf("[error] generating config file: %v", err)
        }

        if err := ioutil.WriteFile(*destFile, cont, 0644); err != nil {
            log.Fatalf("[error] writing config file: %v", err)
        }

        os.Exit(0)
    }

    // Logging settings
    if *lgFile != "" || *plugin != "" {
        log.SetOutput(&lumberjack.Logger{
            Filename:   *lgFile,
            MaxSize:    *logMaxSize,    // megabytes after which new file is created
            MaxBackups: *logMaxBackups, // number of backups
            MaxAge:     *logMaxAge,     // days
            Compress:   *logCompress,   // using gzip
        })
    }

    log.Print("[info] cdagent started -_-")
    
    run := true

    // Program signal processing
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
    go func(){
        for {
            s := <-c
            switch s {
                case syscall.SIGHUP:
                    run = true
                case syscall.SIGINT:
                    log.Print("[info] cdagent stopped")
                    os.Exit(0)
                case syscall.SIGTERM:
                    log.Print("[info] cdagent stopped")
                    os.Exit(0)
                default:
                    log.Print("[info] unknown signal received")
            }
        }
    }()

    // loading configuration file
    cfg, err := loadConfigFile(*cfFile)
    if err != nil {
        log.Fatalf("[error] reading config file: %v", err)
    }

    // Daemon mode
    for (run) {

        if *plugin == "telegraf" {
            run = false
        }

        var wg sync.WaitGroup

        for _, tmpl := range cfg.Templates {
            wg.Add(1)
            go func(t HTTPTemplate) {
                defer wg.Done()
                
                if len(t.URLs) == 0 {
                    for _, u := range cfg.Global.URLs {
                        t.URLs = append(t.URLs, u+t.Key)
                    }
                }

                if t.Temp == "" {
                    t.Temp = t.Dest
                }

                newTemplate := NewHttpTemplate(&t)

                jsn, err := newTemplate.Gather()
                if err != nil {
                    log.Printf("[error] %v", err)
                    if *plugin == "telegraf" {    
                        fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 1)
                    }
                    return
                }

                succ, err := newTemplate.CreateConf(jsn)
                if err != nil {
                    log.Printf("[error] %v", err)
                    if *plugin == "telegraf" {    
                        fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, succ)
                    }
                    return
                }

                if succ == 1 {
                    if t.CheckCmd != "" {
                        _, err := runCommand(t.CheckCmd, 10)
                        if err != nil {
                            log.Printf("[error] %v", err)
                            if *plugin == "telegraf" {
                                fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 3)
                            }
                            return
                        } 
                    }

                    if t.Temp != t.Dest {
                        err := os.Rename(t.Temp, t.Dest)
                        if err != nil {
                            log.Printf("[error] %v", err)
                            if *plugin == "telegraf" {
                                fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 3)
                            }
                            return
                        }
                    }

                    if *plugin == "telegraf" {
                        fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 1)
                    }

                    if t.ReloadCmd != "" {
                        runCommand(t.ReloadCmd, 10)
                    }

                    return
                }

                if *plugin == "telegraf" {
                    fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 0)
                }

            }(tmpl)
        }
        
        wg.Wait()

        time.Sleep(time.Duration(*interval) * time.Second)
    }

}