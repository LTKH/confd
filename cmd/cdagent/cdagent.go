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
    "math/rand"
    "strings"
    "net/http"
    "crypto/md5"
    "encoding/hex"
    "github.com/naoina/toml"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/ltkh/confd/internal/template"
)

type Config struct {
    Templates           []HTTPTemplate
}

type HTTPTemplate struct {
    URLs                []string           `toml:"urls"`
    Timeout             time.Duration      `toml:"timeout"`
    Src                 string             `toml:"src"`
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

    client              *http.Client
}

func NewHttpTemplate(h *HTTPTemplate) *HTTPTemplate {

    // Set default timeout
    if h.Timeout == 0 {
        h.Timeout = 5000
    }

    h.client = &http.Client{
        Transport: &http.Transport{
            Proxy:           http.ProxyFromEnvironment,
        },
        //Timeout: h.Timeout,
    }

    rand.Seed(time.Now().UnixNano())
    rand.Shuffle(len(h.URLs), func(i, j int) { h.URLs[i], h.URLs[j] = h.URLs[j], h.URLs[i] })

    return h
}

func (h *HTTPTemplate) httpRequest() ([]byte, error) {

    for _, url := range h.URLs {

        request, err := http.NewRequest("GET", url, nil)
        if err != nil {
            log.Printf("[error] %s - %v", url, err)
            continue
        }

        if h.BearerToken != "" {
            token, err := ioutil.ReadFile(h.BearerToken)
            if err != nil {
                log.Printf("[error] %s - %v", url, err)
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
            log.Printf("[error] %s - %v", url, err)
            continue
        }
        defer resp.Body.Close()

        body, err := ioutil.ReadAll(resp.Body)

        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
            log.Printf("[error] when writing to [%s] received status code: %d", url, resp.StatusCode)
            continue
        }
        if err != nil {
            log.Printf("[error] when writing to [%s] received error: %v", url, err)
            continue
        }

        return body, nil
    }

    return nil, fmt.Errorf("failed to complete any request")
}

func (h *HTTPTemplate) gather() (bool, error) {
    
    body, err := h.httpRequest()
    if err != nil {
        return false, err
    }

    var jsn interface{}
    if err := json.Unmarshal(body, &jsn); err != nil {
        return false, fmt.Errorf("reading json from response body: %v", err)
    }

    tmpl, err := template.NewTemplate(h.Src)
    if err != nil {
        log.Fatalf("[error] %v", err)
    }

    cont, err := tmpl.Execute(jsn)
    if err != nil {
        return false, fmt.Errorf("generating config: %v", err)
    }

    if _, err := os.Stat(h.Dest); err == nil {
        conf, err := ioutil.ReadFile(h.Dest)
        if err != nil {
            return false, fmt.Errorf("reading config file %s: %v", h.Dest, err)
        }
        if getHash(conf) != getHash(cont.Output) {
            if err := ioutil.WriteFile(h.Dest, cont.Output, 0644); err != nil {
                return false, fmt.Errorf("writing config file %s: %v", h.Dest, err)
            }
            return true, nil
        }
    } else if os.IsNotExist(err) {
        if err := ioutil.WriteFile(h.Dest, cont.Output, 0644); err != nil {
            return false, fmt.Errorf("writing config file %s: %v", h.Dest, err)
        }
        return true, nil
    } else {
        return false, fmt.Errorf("reading config file status %s: %v", h.Dest, err)
    }

    return false, nil
}

func getHash(data []byte) string {
    hsh := md5.New()
    hsh.Write(data)
    return hex.EncodeToString(hsh.Sum(nil))
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
        tmpl, err := template.NewTemplate(*srcTmpl)
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

        cont, err := tmpl.Execute(jsn)
        if err != nil {
            log.Fatalf("[error] generating config file: %v", err)
        }

        if err := ioutil.WriteFile(*destFile, cont.Output, 0644); err != nil {
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

    //loading configuration file
    f, err := os.Open(*cfFile)
    if err != nil {
        log.Fatalf("[error] reading config file: %v", err)
    }
    defer f.Close()
    
    var cfg Config
    if err := toml.NewDecoder(f).Decode(&cfg); err != nil {
        log.Fatalf("[error] parsing config file: %v", err)
    }

    // Daemon mode
    for (run) {

        if *plugin == "telegraf" {
            run = false
        }

        var wg sync.WaitGroup

        for _, t := range cfg.Templates {
            wg.Add(1)
            go func(tmpl HTTPTemplate) {
                defer wg.Done()

                newHttp := NewHttpTemplate(&tmpl)

                reload, err := newHttp.gather()
                if err != nil {
                    log.Printf("[error] %v", err)
                    if *plugin == "telegraf" {
                        fmt.Printf("confd,src=%s,dest=%s success=0\n", tmpl.Src, tmpl.Dest)
                    }
                    return
                }

                if *plugin == "telegraf" {    
                    fmt.Printf("confd,src=%s,dest=%s success=1\n", tmpl.Src, tmpl.Dest)
                }

                if reload {
                    if tmpl.ReloadCmd != "" {
                        runCommand(tmpl.ReloadCmd, 5)
                    }
                } 
            }(t)
        }

        wg.Wait()

        time.Sleep(time.Duration(*interval) * time.Second)
    }

}