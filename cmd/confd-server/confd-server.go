package main

import (
    "flag"
    "log"
    "net/http"
    "os"
    "os/signal"
    "runtime"
    "syscall"
    "gopkg.in/natefinch/lumberjack.v2"
	"github.com/ltkh/confd/internal/api/v2"
    "github.com/ltkh/confd/internal/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {

    //limits the number of operating system threads
    runtime.GOMAXPROCS(runtime.NumCPU())

    //command-line flag parsing
    cfFile          := flag.String("config", "confd-server.yml", "config file")
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
        log.Print("[info] confd-server stopped")
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

    log.Print("[info] confd-server started")

    //mux := http.NewServeMux()

    etcdClientV2, err := v2.GetEtcdClient()
    if err != nil {
        log.Fatalf("[error] %v", err)
    }
    http.Handle("/api/v2/etcd/", &v2.ApiEtcd{etcdClientV2})
    //mux.Handle("/api/v2/etcd*", &v2.ApiEtcd{etcdClientV2})

    http.Handle("/metrics", promhttp.Handler())
    
	http.ListenAndServe(cfg.Global.Listen, nil)

}