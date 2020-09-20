package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"log"

	"gopkg.in/natefinch/lumberjack.v2"
	"github.com/ltkh/confd/internal/backends"
	"github.com/ltkh/confd/internal/resource/template"
)

func main() {
	flag.Parse()
	if config.PrintVersion {
		fmt.Printf("confd %s (Git SHA: %s, Go Version: %s)\n", Version, GitSHA, runtime.Version())
		os.Exit(0)
	}

	if err := initConfig(); err != nil {
		log.Fatal(err.Error())
	}

	if config.LogFile != "" {
		if config.LogMaxSize == 0 {
			config.LogMaxSize = 1
		}
		if config.LogMaxBackups == 0 {
			config.LogMaxBackups = 3
		}
		if config.LogMaxAge == 0 {
			config.LogMaxAge = 28
		}
		log.SetOutput(&lumberjack.Logger{
			Filename:   config.LogFile,
			MaxSize:    config.LogMaxSize,    // megabytes after which new file is created
			MaxBackups: config.LogMaxBackups, // number of backups
			MaxAge:     config.LogMaxAge,     // days
			Compress:   config.LogCompress,   // using gzip
		})
	}

	log.Print("Starting confd")

	storeClient, err := backends.New(config.BackendsConfig)
	if err != nil {
		log.Print(err.Error())
	}

	config.TemplateConfig.StoreClient = storeClient
	if config.OneTime {
		if err := template.Process(config.TemplateConfig); err != nil {
			log.Print(err.Error())
		}
		os.Exit(0)
	}

	stopChan := make(chan bool)
	doneChan := make(chan bool)
	errChan := make(chan error, 10)

	var processor template.Processor
	switch {
	case config.Watch:
		processor = template.WatchProcessor(config.TemplateConfig, stopChan, doneChan, errChan)
	default:
		processor = template.IntervalProcessor(config.TemplateConfig, stopChan, doneChan, errChan, config.Interval)
	}

	go processor.Process()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case err := <-errChan:
			log.Print(err.Error())
		case s := <-signalChan:
			log.Print(fmt.Sprintf("Captured %v. Exiting...", s))
			close(doneChan)
		case <-doneChan:
			os.Exit(0)
		}
	}
}
