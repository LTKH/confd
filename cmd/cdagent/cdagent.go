package main

import (
    "log"
    "time"
    "os"
    "os/signal"
    "syscall"
    "flag"
    //"sync"
    "fmt"
    //"net/http"
    "net/url"
    "runtime"
    "os/exec"
    "context"
    "io/ioutil"
    "math/rand"
    "crypto/aes"
    "crypto/cipher"
    //"crypto/md5"
    //"encoding/hex"
    "encoding/json"
    "encoding/base64"
    //"github.com/gorilla/mux"
    "github.com/naoina/toml"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/ltkh/confd/internal/template"
    "github.com/ltkh/confd/internal/client"
    "github.com/ltkh/confd/internal/config"
)

var (
    Version      = "unknown"
    KeyString    = "khuyg743878g8s2:b970m-z0"
)

type Config struct {
    Global           *Global                 `toml:"global"`
    Templates        []*HTTPTemplate         `toml:"templates"`
}

type Global struct {
    URLs             []string                `toml:"urls"`
    ContentEncoding  string                  `toml:"content_encoding"`
    ChecksFile       string                  `toml:"checks_file"`
    PKey             string                  `toml:"pkey"`
}

type HTTPTemplate struct {
    URLs             []string                `toml:"urls"`
    Path             string                  `toml:"path"`
    Hash             string                  `toml:"-"`
    Create           bool                    `toml:"create"`
    Src              string                  `toml:"src"`
    SrcMatch         string                  `toml:"src_match"`
    Temp             string                  `toml:"temp"`
    Dest             string                  `toml:"dest"`
    CheckCmd         string                  `toml:"check_cmd"`
    ReloadCmd        string                  `toml:"reload_cmd"`
    Interval         string                  `toml:"interval"`
    Timeout          string                  `toml:"timeout"`
    ContentEncoding  string                  `toml:"content_encoding"`
    Headers          map[string]string       `toml:"headers"`
    funcMap          map[string]interface{}
    Username         string                  `toml:"username"`
    Password         string                  `toml:"password"`
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

type Resp struct {
    Status           string                  `json:"status"`
    Error            string                  `json:"error,omitempty"`
    Warnings         []string                `json:"warnings,omitempty"`
    Data             interface{}             `json:"data,omitempty"`
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

func encodeResp(resp *Resp) []byte {
    jsn, err := json.Marshal(resp)
    if err != nil {
        return encodeResp(&Resp{Status:"error", Error:err.Error(), Data:make([]int, 0)})
    }
    return jsn
}

func randURLs(urls []string) []string {
    rand.Seed(time.Now().UnixNano())
    rand.Shuffle(len(urls), func(i, j int) { urls[i], urls[j] = urls[j], urls[i] })
    return urls
}

func (t *HTTPTemplate) CreateConf(jsn interface{}) (int, error) {

    if t.SrcMatch == "" {
        t.SrcMatch = t.Src
    }

    cont, err := template.New(t.Src).ParseGlob(t.SrcMatch, jsn)
    if err != nil {
        return 3, fmt.Errorf("generating config: %v", err)
    }

    if _, err := os.Stat(t.Dest); err == nil {
        conf, err := ioutil.ReadFile(t.Dest)
        if err != nil {
            return 4, fmt.Errorf("reading config file %s: %v", t.Dest, err)
        }
        if config.GetHash(conf) != config.GetHash(cont) {
            if err := ioutil.WriteFile(t.Temp, cont, 0644); err != nil {
                return 4, fmt.Errorf("writing config file %s: %v", t.Dest, err)
            }
            return 1, nil
        }
    } else if os.IsNotExist(err) {
        if err := ioutil.WriteFile(t.Temp, cont, 0644); err != nil {
            return 4, fmt.Errorf("writing config file %s: %v", t.Dest, err)
        }
        return 1, nil
    } else {
        return 4, fmt.Errorf("reading config file status %s: %v", t.Dest, err)
    }

    return 0, nil
}

func (t *HTTPTemplate) CreateTemplate(httpClient *client.HttpClient, path, plugin string) error {
    if t.Temp == "" {
        t.Temp = t.Dest
    }

    httpConfig := client.HttpConfig{
        URLs: randURLs(t.URLs),
        ContentEncoding: t.ContentEncoding,
        Username: t.Username,
        Password: t.Password,
    }

    resp, err := httpClient.NewRequest("GET", path, t.Hash, nil, httpConfig)
    if err != nil {
        return err
    }

    if resp.StatusCode == 204 {
        return nil
    }

    if resp.StatusCode == 404 {
        if t.Create {
            // parse url path
            u, err := url.Parse(path)
            if err != nil {
                return err
            }

            httpClient.NewRequest("PUT", u.Path, "", []byte("dir=true"), httpConfig)
        }
        return nil
    }

    if resp.StatusCode == 403 {
        err := fmt.Errorf("when request to [%s] received status code: 403", path)
        return err
    }

    if resp.StatusCode != 200 {
        return nil
    }

    t.Hash = config.GetHash(resp.Body)

    var jsn interface{}
    if err := json.Unmarshal(resp.Body, &jsn); err != nil {
        if plugin == "telegraf" || plugin == "windows" {    
            fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 1)
        }
        return err
    }

    succ, err := t.CreateConf(jsn)
    if err != nil {
        if plugin == "telegraf" || plugin == "windows" {    
            fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, succ)
        }
        return err
    }

    if succ == 1 {
        if t.CheckCmd != "" {
            _, err := runCommand(t.CheckCmd, 10)
            if err != nil {
                if plugin == "telegraf" || plugin == "windows" {
                    fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 3)
                }
                return err
            } 
        }

        if t.Temp != t.Dest {
            err := os.Rename(t.Temp, t.Dest)
            if err != nil {
                if plugin == "telegraf" || plugin == "windows" {
                    fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 3)
                }
                return err
            }
        }

        if plugin == "telegraf" || plugin == "windows" {
            fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 1)
        }

        if t.ReloadCmd != "" {
            runCommand(t.ReloadCmd, 10)
        }

        return nil
    }

    if plugin == "telegraf" || plugin == "windows" {
        fmt.Printf("confd,src=%s,dest=%s success=%d\n", t.Src, t.Dest, 0)
    }

    return nil
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
func loadConfigFile(file string, dcrpt bool) (Config, error) {
    var cfg Config

    f, err := os.Open(file)
    if err != nil {
        return cfg, err
    }
    
    if err := toml.NewDecoder(f).Decode(&cfg); err != nil {
        return cfg, err
    }

    f.Close()

    for i, tmpl := range cfg.Templates {
        if dcrpt && tmpl.Password != "" {
            passwd, err := decrypt(tmpl.Password)
            if err != nil {
                log.Fatalf("[error] %v", err)
            }
            cfg.Templates[i].Password = passwd
        }
    }

    return cfg, nil
}

func main() {

    // Limits the number of operating system threads
    runtime.GOMAXPROCS(runtime.NumCPU())

    // Command-line flag parsing
    cfFile          := flag.String("config.file", "config/confd.toml", "config file")
    interval        := flag.Int("interval", 30, "interval")
    plugin          := flag.String("plugin", "", "plugin")
    lgFile          := flag.String("log.file", "", "log file")
    logMaxSize      := flag.Int("log.max-size", 1, "log max size") 
    logMaxBackups   := flag.Int("log.max-backups", 3, "log max backups")
    logMaxAge       := flag.Int("log.max-age", 10, "log max age")
    logCompress     := flag.Bool("log.compress", true, "log compress")
    version         := flag.Bool("version", false, "show cdagent version")
    encryptPass     := flag.String("encrypt", "", "encrypt string")
    decryptPass     := flag.Bool("decrypt", false, "decrypt password string")

    srcFile         := flag.String("src-file", "", "source file")
    srcTmpl         := flag.String("src-tmpl", "", "source template")
    srcMatch        := flag.String("src-match", "", "source match")
    destFile        := flag.String("dest-file", "", "destination file")

    flag.Parse()

    // Show version
    if *version {
        fmt.Printf("%v\n", Version)
        return
    }

    // Encrypt
    if *encryptPass != "" {
        passwd, err := encrypt(*encryptPass)
        if err != nil {
            log.Fatalf("[error] %v", err)
        }
        log.Printf("[pass] %s", passwd)
        return
    }

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

        if *srcMatch == "" {
            srcMatch = srcTmpl
        }

        cont, err := template.New(*srcTmpl).ParseGlob(*srcMatch, jsn)
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

    // loading configuration file
    cfg, err := loadConfigFile(*cfFile, *decryptPass)
    if err != nil {
        log.Fatalf("[error] reading config file: %v", err)
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

        // Set Interval
        if tl.Interval == "" {
            tl.Interval = "30s"
        }
        tlInterval, _ := time.ParseDuration(tl.Interval)
        if tlInterval == 0 {
            log.Fatal("[error] setting interval: invalid duration")
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

        go func(t *HTTPTemplate, path string) {
            for {
                if err := t.CreateTemplate(httpClient, path, *plugin); err != nil {
                    log.Printf("[error] %v", err)
                }
                time.Sleep(tlInterval)
            }
        }(tl, string(path))
    }

    // Daemon mode
    for (run) {
        if *plugin == "telegraf" {
            run = false
        }

        time.Sleep(time.Duration(*interval) * time.Second)
    }

}