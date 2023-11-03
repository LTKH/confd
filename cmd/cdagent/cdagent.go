package main

import (
    "log"
    "time"
    "os"
    "os/signal"
    "syscall"
    "flag"
    "sync"
    "fmt"
    "runtime"
    "os/exec"
    "context"
    "encoding/json"
    "io/ioutil"
    "math/rand"
    "crypto/md5"
    "encoding/hex"
    "path/filepath"
    "github.com/naoina/toml"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/ltkh/confd/internal/template"
    "github.com/ltkh/confd/internal/client"
)

type Config struct {
    Global           *Global                 `toml:"global"`
    Templates        []*HTTPTemplate         `toml:"templates"`
}

type Global struct {
    URLs             []string                `toml:"urls"`
    ContentEncoding  string                  `toml:"content_encoding"`
	ChecksFile       string                  `toml:"checks_file"`
}

type HTTPTemplate struct {
    URLs             []string                `toml:"urls"`
    Path             string                  `toml:"path"`
    Create           bool                    `toml:"create"`
    Src              string                  `toml:"src"`
    Temp             string                  `toml:"temp"`
    Dest             string                  `toml:"dest"`
    CheckCmd         string                  `toml:"check_cmd"`
    ReloadCmd        string                  `toml:"reload_cmd"`
    Timeout          string                  `toml:"timeout"`

    ContentEncoding  string                  `toml:"content_encoding"`

    Headers          map[string]string       `toml:"headers"`

    funcMap          map[string]interface{}
}

type Checks struct {
    Checks           []Check                 `toml:"checks"`
}

type Check struct {
    Key              string                  `toml:"key"`
    Value            string                  `toml:"value"`
    File             string                  `toml:"file"`
    Http             string                  `toml:"http"`
    Timeout          string                  `toml:"timeout"`
}

func randURLs(urls []string) []string {
    rand.Seed(time.Now().UnixNano())
    rand.Shuffle(len(urls), func(i, j int) { urls[i], urls[j] = urls[j], urls[i] })
    return urls
}

func getHash(data []byte) string {
    hsh := md5.New()
    hsh.Write(data)
    return hex.EncodeToString(hsh.Sum(nil))
}

func (h *HTTPTemplate) CreateConf(jsn interface{}) (int, error) {

    files, err := filepath.Glob(h.Src)
    if err != nil {
        return 3, fmt.Errorf("generating config: %v", err)
    }

    if len(files) > 0 { 

        cont, err := template.New(files[0]).ParseFiles(files, jsn)
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

    // Command-line flag parsing
    cfFile          := flag.String("config.file", "config/confd.toml", "config file")
    interval        := flag.Int("interval", 30, "interval")
    plugin          := flag.String("plugin", "", "plugin")
    lgFile          := flag.String("log.file", "", "log file")
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
        data, err := ioutil.ReadFile(*srcFile)
        if err != nil {
            log.Fatalf("[error] reading source file: %v", err)
        }

        var jsn interface{}
        if err := json.Unmarshal(data, &jsn); err != nil {
            log.Fatalf("[error] parsing json file: %v", err)
        }

        cont, err := template.New(*srcTmpl).ParseFile(*srcTmpl, jsn)
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

        for _, tl := range cfg.Templates {

            // Set default URLs
            if len(tl.URLs) == 0 {
                for _, u := range cfg.Global.URLs {
                    tl.URLs = append(tl.URLs, u)
                }
            }
            if len(tl.URLs) == 0 {
                continue
            }

            // Set default ContentEncoding
            if tl.ContentEncoding == "" {
                tl.ContentEncoding = cfg.Global.ContentEncoding
            }

            // Set Timeout
            if tl.Timeout == "" {
                tl.Timeout = "5s"
            }
            tlTimeout, _ := time.ParseDuration(tl.Timeout)
            if tlTimeout == 0 {
                log.Fatal("[error] setting timeout: invalid duration")
            }

            httpClient := client.NewHttpClient(tlTimeout)

            path, err := template.New(tl.Path).Execute(tl.Path, nil)
            if err != nil {
                log.Printf("[error] %v", err)
                continue
            }

            wg.Add(1)

            go func(t *HTTPTemplate, path string) {
                defer wg.Done()

                if t.Temp == "" {
                    t.Temp = t.Dest
                }

                config := client.HttpConfig{
                    URLs: randURLs(t.URLs),
                    ContentEncoding: t.ContentEncoding,
                }

                resp, err := httpClient.NewRequest("GET", path, nil, config)
                if err != nil {
                    if resp.StatusCode == 404 && t.Create {
                        httpClient.NewRequest("PUT", path, []byte("dir=true"), config)
                    }
                    log.Printf("[error] %v", err)
                    return
                }

                var jsn interface{}
                if err := json.Unmarshal(resp.Body, &jsn); err != nil {
                    log.Printf("[error] %v", err)
                    if *plugin == "telegraf" || *plugin == "windows" {    
                        fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 1)
                    }
                    return
                }

                succ, err := t.CreateConf(jsn)
                if err != nil {
                    log.Printf("[error] %v", err)
                    if *plugin == "telegraf" || *plugin == "windows" {    
                        fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, succ)
                    }
                    return
                }

                if succ == 1 {
                    if t.CheckCmd != "" {
                        _, err := runCommand(t.CheckCmd, 10)
                        if err != nil {
                            log.Printf("[error] %v", err)
                            if *plugin == "telegraf" || *plugin == "windows" {
                                fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 3)
                            }
                            return
                        } 
                    }

                    if t.Temp != t.Dest {
                        err := os.Rename(t.Temp, t.Dest)
                        if err != nil {
                            log.Printf("[error] %v", err)
                            if *plugin == "telegraf" || *plugin == "windows" {
                                fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 3)
                            }
                            return
                        }
                    }

                    if *plugin == "telegraf" || *plugin == "windows" {
                        fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 1)
                    }

                    if t.ReloadCmd != "" {
                        runCommand(t.ReloadCmd, 10)
                    }

                    return
                }

                if *plugin == "telegraf" || *plugin == "windows" {
                    fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 0)
                }

            }(tl, string(path))
        }
        
        wg.Wait()

        time.Sleep(time.Duration(*interval) * time.Second)
    }

}