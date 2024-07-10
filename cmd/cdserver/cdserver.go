package main

import (
    "net/http"
    _ "net/http/pprof"
    "flag"
    "log"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "strings"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/ltkh/confd/internal/api/v1"
	"github.com/ltkh/confd/internal/api/v2"
    "github.com/ltkh/confd/internal/config"
)

var (
    Version = "unknown"
)

func main() {

    // Command-line flag parsing
    lsAddress      := flag.String("web.listen-address", ":8083", "listen address")
    cfFile         := flag.String("config.file", "config/config.yml", "config file")
    lgFile         := flag.String("log.file", "", "log file")
    logMaxSize     := flag.Int("log.max-size", 1, "log max size") 
    logMaxBackups  := flag.Int("log.max-backups", 3, "log max backups")
    logMaxAge      := flag.Int("log.max-age", 10, "log max age")
    logCompress    := flag.Bool("log.compress", true, "log compress")
    version        := flag.Bool("version", false, "show cdserver version")
    flag.Parse()

    // Show version
    if *version {
        fmt.Printf("%v\n", Version)
        return
    }

	// Program completion signal processing
    c := make(chan os.Signal, 2)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        log.Print("[info] cdserver stopped")
        os.Exit(0)
    }()

    // Loading configuration file
    cfg, err := config.LoadConfigFile(*cfFile)
    if err != nil {
        log.Fatalf("[error] loading configuration file: %v", err)
    }

	// Logging settings
    if *lgFile != "" {
        log.SetOutput(&lumberjack.Logger{
            Filename:   *lgFile,
            MaxSize:    *logMaxSize,    // megabytes after which new file is created
            MaxBackups: *logMaxBackups, // number of backups
            MaxAge:     *logMaxAge,     // days
            Compress:   *logCompress,   // using gzip
        })
    }

    http.HandleFunc("/-/healthy", func (w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain")
        w.Write([]byte("OK"))
    })

    for _, back := range cfg.Backends {

        back.Id = strings.TrimRight(back.Id, "/")

        if back.Backend == "etcd" {
            etcdClient, err := v2.GetEtcdClient(back)
            if err != nil {
                log.Fatalf("[error] %v", err)
            }
            http.Handle("/api/v2/"+back.Id+"/", etcdClient)
        }

        if back.Backend == "consul" {
            consulClient, err := v1.GetConsulClient(back)
            if err != nil {
                log.Fatalf("[error] %v", err)
            }
            http.Handle("/api/v1/"+back.Id+"/", &v1.ApiConsul{Id: back.Id, Client: consulClient})
        }

    }

    http.Handle("/metrics", promhttp.Handler())

    log.Print("[info] cdserver started")

    if cfg.Global.CertFile != "" && cfg.Global.CertKey != "" {
        if err := http.ListenAndServeTLS(*lsAddress, cfg.Global.CertFile, cfg.Global.CertKey, nil); err != nil {
            log.Fatalf("[error] %v", err)
        }
    } else {
        if err := http.ListenAndServe(*lsAddress, nil); err != nil {
            log.Fatalf("[error] %v", err)
        }
    }

}