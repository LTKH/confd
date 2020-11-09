package main

import (
	"time"
	"log"
	"os"
	"os/signal"
	"syscall"
	"runtime"
	"flag"
	"gopkg.in/natefinch/lumberjack.v2"
	"github.com/ltkh/confd/backends"
	"github.com/ltkh/confd/internal/config"
	"github.com/ltkh/confd/internal/template"
)

func main() {

  	//limits the number of operating system threads
	runtime.GOMAXPROCS(runtime.NumCPU())

	//command-line flag parsing
	cfFile := flag.String("config", "", "config file")
	cfgDir := flag.String("confdir", "", "config dir") 
	lgFile := flag.String("logfile", "", "log file") 
	flTest := flag.String("test", "", "config test") 
	flag.Parse()

	//loading configuration file
	cfg, err := config.New(*cfFile)
	if err != nil {
		log.Fatalf("[error] %v", err)
	}

	if *lgFile != "" {
		if cfg.Server.Log_max_size == 0 {
			cfg.Server.Log_max_size = 1
		}
		if cfg.Server.Log_max_backups == 0 {
			cfg.Server.Log_max_backups = 3
		}
		if cfg.Server.Log_max_age == 0 {
			cfg.Server.Log_max_age = 28
		}
		log.SetOutput(&lumberjack.Logger{
			Filename:   *lgFile,
			MaxSize:    cfg.Server.Log_max_size,    // megabytes after which new file is created
			MaxBackups: cfg.Server.Log_max_backups, // number of backups
			MaxAge:     cfg.Server.Log_max_age,     // days
			Compress:   cfg.Server.Log_compress,    // using gzip
		})
	}

	//program completion signal processing
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<- c
		log.Print("[info] confd stopped")
		os.Exit(0)
	}()

	//checking the status of tasks
	if cfg.Server.Check_enabled {

		go func(cfg *config.Config, clnt db.DbClient) {

			if cfg.Server.Check_interval == 0 {
				cfg.Server.Check_interval = 600
			}

			for {

				//geting tasks from database
				tasks, err := clnt.LoadTasks()
				if err != nil {
					log.Printf("[error] %v", err)
					continue
				}

				for _, task := range tasks {
					time.Sleep(cfg.Server.Check_delay * time.Second)

					//task.Updated = time.Now().UTC().Unix()
					issue, err := template.UpdateTaskStatus(task, cfg, clnt)
					if err != nil {
						log.Printf("[error] task update id %s: %v", task.Task_id, err)
						continue
					}

					if issue.Fields.Status.Id != task.Status_id {
						log.Printf("[info] task status updated: %s", task.Task_self)
					}

					if task.Updated + cfg.Server.Check_resolve < time.Now().UTC().Unix() {
						for _, s := range cfg.Server.Check_status {
							if issue.Fields.Status.Id == s {
								if err := clnt.DeleteTask(task.Group_id); err != nil {
									log.Printf("[error] task delete id %s: %v", task.Task_id, err)
									continue
								}
								log.Printf("[info] task is removed from the database: %v", task.Task_self)
							}
						}
					}

				}

				time.Sleep(cfg.Server.Check_interval * time.Second)
			}
		}(&cfg, client)
	}

	log.Print("[info] confd running")

	if cfg.Server.Alerts_interval == 0 {
		cfg.Server.Alerts_interval = 600
	}

	//daemon mode
	for {

		if err := template.Process(&cfg, client, flTest); err != nil {
			log.Printf("[error] %v", err)
		}

		time.Sleep(cfg.Server.Alerts_interval * time.Second)
	}
}
