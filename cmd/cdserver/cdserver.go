package main

import (
    "flag"
    "log"
    "net/http"
    "os"
    "os/signal"
    "runtime"
    "syscall"
    "strings"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/ltkh/confd/internal/api/v1"
	"github.com/ltkh/confd/internal/api/v2"
    "github.com/ltkh/confd/internal/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {

    //limits the number of operating system threads
    runtime.GOMAXPROCS(runtime.NumCPU())

    //command-line flag parsing
    cfFile          := flag.String("config", "config/cdserver.yml", "config file")
    lgFile          := flag.String("logfile", "", "log file")
    logMaxSize      := flag.Int("log.max-size", 1, "log max size") 
    logMaxBackups   := flag.Int("log.max-backups", 3, "log max backups")
    logMaxAge       := flag.Int("log.max-age", 10, "log max age")
    logCompress     := flag.Bool("log.compress", true, "log compress")
    flag.Parse()

	//program completion signal processing
    c := make(chan os.Signal, 2)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        log.Print("[info] cdserver stopped")
        os.Exit(0)
    }()

    //loading configuration file
    cfg, err := config.LoadConfigFile(*cfFile)
    if err != nil {
        log.Fatalf("[error] loading configuration file: %v", err)
    }

	//logging settings
    if *lgFile != "" {
        log.SetOutput(&lumberjack.Logger{
            Filename:   *lgFile,
            MaxSize:    *logMaxSize,    // megabytes after which new file is created
            MaxBackups: *logMaxBackups, // number of backups
            MaxAge:     *logMaxAge,     // days
            Compress:   *logCompress,   // using gzip
        })
    }

    for _, back := range cfg.Backends {

        back.Id = strings.TrimRight(back.Id, "/")

        if back.Backend == "etcd" {
            etcdClient, err := v2.GetEtcdClient(back)
            if err != nil {
                log.Fatalf("[error] %v", err)
            }
            http.Handle("/api/v2/"+back.Id+"/", &v2.ApiEtcd{Id: back.Id, Client: etcdClient})
        }

        if back.Backend == "consul" {
            consulClient, err := v1.GetConsulClient(back)
            if err != nil {
                log.Fatalf("[error] %v", err)
            }
            http.Handle("/api/v1/"+back.Id+"/", &v1.ApiConsul{Id: back.Id, Client: consulClient})
        }

    }

    http.Handle("/health", &v1.ApiHealth{})
    http.Handle("/metrics", promhttp.Handler())

    log.Print("[info] cdserver started")

    if err := http.ListenAndServe(cfg.Global.Listen, nil); err != nil {
        log.Fatalf("[error] %v", err)
    }

}