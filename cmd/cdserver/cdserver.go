package main

import (
    "net"
    "net/url"
    "net/http"
    _ "net/http/pprof"
    "flag"
    "log"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "sync"
    "sort"
    "time"
    //"strings"
    "gopkg.in/natefinch/lumberjack.v2"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/ltkh/confd/internal/api/v1"
    "github.com/ltkh/confd/internal/api/v2"
    "github.com/ltkh/confd/internal/config"
)

var (
    Version = "unknown"
)

type Result struct {
    Address string
    Latency time.Duration
    Error   error
}

func checkAddr(name, addr string, timeout time.Duration, wg *sync.WaitGroup, results chan<- Result) {
    defer wg.Done()

    start := time.Now()
    conn, err := net.DialTimeout("tcp", addr, timeout)
    duration := time.Since(start)

    if err == nil {
        conn.Close()
    }

    results <- Result{
        Address: name,
        Latency: duration,
        Error:   err,
    }
}

func checkServers(servers []string) ([]string, error) {
    resultsChan := make(chan Result, len(servers))
    var wg sync.WaitGroup

    // Запускаем проверки параллельно
    for _, addr := range servers {
        u, err := url.Parse(addr)
        if err != nil {
            return []string{}, err
        }

        wg.Add(1)
        go checkAddr(addr, u.Host, 5 * time.Second, &wg, resultsChan)
    }

    // Ждем завершения и закрываем канал
    wg.Wait()
    close(resultsChan)

    // Собираем результаты в слайс
    var resultsStruct []Result
    for res := range resultsChan {
        resultsStruct = append(resultsStruct, res)
    }

    // Сортируем: сначала рабочие по времени, затем — упавшие
    sort.Slice(resultsStruct, func(i, j int) bool {
        // 1. Если у i ошибка, а у j нет — i должен быть в конце (возвращаем false)
        if resultsStruct[i].Error != nil && resultsStruct[j].Error == nil {
            return false
        }
        // 2. Если у j ошибка, а у i нет — i должен быть в начале (возвращаем true)
        if resultsStruct[i].Error == nil && resultsStruct[j].Error != nil {
            return true
        }
        // 3. Если оба либо с ошибками, либо оба ОК — сортируем по времени
        return resultsStruct[i].Latency < resultsStruct[j].Latency
    })

    var results []string
    for _, res := range resultsStruct {
        //log.Printf("[info] latency %v: %v (%v), err: %v", i, res.Address, res.Latency, res.Error)
        results = append(results, res.Address)
    }

    return results, nil
}

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

    http.Handle("/metrics", promhttp.Handler())

    for _, back := range cfg.Backends {

        //log.Printf("[info] latency check for \"%v\"", back.Id)
        nodes, err := checkServers(back.Nodes)
        if err != nil {
            log.Fatalf("[error] %v", err)
        }
        back.Nodes = nodes

        if back.Backend == "etcd" {
            etcdClient, err := v2.GetEtcdClient(back, cfg.Logger)
            if err != nil {
                log.Fatalf("[error] %v", err)
            }
            http.Handle("/api/v2/"+back.Id, etcdClient)
            http.Handle("/api/v2/"+back.Id+"/", etcdClient)
        }

        if back.Backend == "consul" {
            consulClient, err := v1.GetConsulClient(back)
            if err != nil {
                log.Fatalf("[error] %v", err)
            }
            http.Handle("/api/v1/"+back.Id, &v1.ApiConsul{Id: back.Id, Client: consulClient})
            http.Handle("/api/v1/"+back.Id+"/", &v1.ApiConsul{Id: back.Id, Client: consulClient})
        }

    }

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