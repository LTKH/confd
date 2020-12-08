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
    "github.com/naoina/toml"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/ltkh/confd/internal/template"
)

type Config struct {
    Template       []template.HTTPTemplate
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
    cfFile          := flag.String("config", "", "config file")
    lgFile          := flag.String("logfile", "", "log file")
    interval        := flag.Int("interval", 30, "interval")
    plugin          := flag.String("plugin", "", "plugin")
    log_max_size    := flag.Int("log_max_size", 1, "log max size") 
    log_max_backups := flag.Int("log_max_backups", 3, "log max backups")
    log_max_age     := flag.Int("log_max_age", 10, "log max age")
    log_compress    := flag.Bool("log_compress", true, "log compress")
    flag.Parse()

    // Logging settings
    if *lgFile != "" || *plugin != "" {
        log.SetOutput(&lumberjack.Logger{
            Filename:   *lgFile,
            MaxSize:    *log_max_size,    // megabytes after which new file is created
            MaxBackups: *log_max_backups, // number of backups
            MaxAge:     *log_max_age,     // days
            Compress:   *log_compress,    // using gzip
        })
    }

    log.Print("[info] confd started -_-")
    
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
                    log.Print("[info] confd stopped")
                    os.Exit(0)
                case syscall.SIGTERM:
                    log.Print("[info] confd stopped")
                    os.Exit(0)
                default:
                    log.Print("[info] unknown signal received")
            }
        }
    }()

    // Daemon mode
    for (run) {

        if *plugin == "telegraf" {
            run = false
        }

        //loading configuration file
        f, err := os.Open(*cfFile)
        if err != nil {
            log.Fatalf("[error] %v", err)
        }
        defer f.Close()
        
        var cfg Config
        if err := toml.NewDecoder(f).Decode(&cfg); err != nil {
            log.Fatalf("[error] %v", err)
        }

        var wg sync.WaitGroup
        var rl bool

        for _, t := range cfg.Template {
            wg.Add(1)
            go func(tmpl template.HTTPTemplate) {
                defer wg.Done()

                tmp := template.New(tmpl)

                reload, err := tmp.GatherURL()
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
                    rl = true
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