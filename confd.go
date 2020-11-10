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
	"github.com/naoina/toml"
	"gopkg.in/natefinch/lumberjack.v2"
	"github.com/ltkh/confd/internal/template"
)

type Config struct {
	Server struct {
		Interval         time.Duration  `toml:"interval"`
		CheckCmd         string         `toml:"check_cmd"`
		ReloadCmd        string         `toml:"reload_cmd"`
		LogMaxSize       int            `toml:"log_max_size"`
	    LogMaxBackups    int            `toml:"log_max_backups"`
	    LogMaxAge        int            `toml:"log_max_age"`
	    LogCompress      bool           `toml:"log_compress"`
	}
	Template       []template.HTTPTemplate
}

func checkCmd() error {
	/*
	var cmdBuffer bytes.Buffer
	data := make(map[string]string)
	data["src"] = t.StageFile.Name()
	tmpl, err := template.New("checkcmd").Parse(t.CheckCmd)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(&cmdBuffer, data); err != nil {
		return err
	}
	return runCommand(cmdBuffer.String())
	*/
	return nil
}

func runCommand(check, cmd string) error {
	log.Printf("[info] running %s", cmd)
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/C", cmd)
	} else {
		c = exec.Command("/bin/sh", "-c", cmd)
	}

	output, err := c.CombinedOutput()
	if err != nil {
		log.Printf("[error] %q", string(output))
		return err
	}
	log.Printf("[info] %q", string(output))
	return nil
}

func main() {

	//limits the number of operating system threads
	runtime.GOMAXPROCS(runtime.NumCPU())

	//command-line flag parsing
	cfFile := flag.String("config", "", "config file")
	lgFile := flag.String("logfile", "", "log file")
	plugin := flag.String("plugin", "", "plugin mode")
	flag.Parse()

	//logging settings
	if *lgFile != "" || *plugin == "true"{
		log.SetOutput(&lumberjack.Logger{
			Filename:   *lgFile,
			MaxSize:    1,      // megabytes after which new file is created
			MaxBackups: 3,      // number of backups
			MaxAge:     10,     // days
			Compress:   true,   // using gzip
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

	log.Print("[info] confd started -_-")

    //daemon mode
	for {

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
					if *plugin == "true" {
						fmt.Printf("templates,src=%s,dest=%s success=0,updated=0\n", tmpl.Src, tmpl.Dest)
					}
					return
				}

				if reload {
					rl = true
					if *plugin == "true" {
						fmt.Printf("templates,src=%s,dest=%s success=1,updated=1\n", tmpl.Src, tmpl.Dest)
					}
					if tmpl.ReloadCmd != "" {
						runCommand(tmpl.CheckCmd, tmpl.ReloadCmd)
					}
				} else {
					if *plugin == "true" {
						fmt.Printf("templates,src=%s,dest=%s success=1,updated=0\n", tmpl.Src, tmpl.Dest)
					}
				}
				
			}(t)
		}

		wg.Wait()

		if rl {
            if cfg.Server.ReloadCmd != "" {
				runCommand(cfg.Server.CheckCmd, cfg.Server.ReloadCmd)
			}
		}

		time.Sleep(cfg.Server.Interval * time.Second)
	}

}

