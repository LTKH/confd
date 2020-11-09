package config

import (
	"time"
	"os"
	"github.com/naoina/toml"
)

type Config struct {
	DB                   DB
	Server struct {
		Conf_dir         string
		Alerts_interval  time.Duration
		Check_enabled    bool
		Check_status     []string
		Check_interval   time.Duration
		Check_delay      time.Duration
		Check_resolve    int64
		Log_max_size     int
		Log_max_backups  int
		Log_max_age      int
		Log_compress     bool
	}
	Alerts struct {
		Login            string
		Passwd           string
	}
	Jira struct {
		Api              string
		Login            string
		Passwd           string
	}
	Monit struct {
		Listen           string
	}
}

type DB struct {
	Client               string
	Conn_string          string
	Tasks_table          string
}

type Task struct {
	Group_id             string
	Status_id            string
	Status_name          string
	Task_id              string
	Task_key             string
	Task_self            string
	Created              int64
	Updated              int64
	Template             string
}

func New(filename string) (cfg Config, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return cfg, err
	}
	defer f.Close()

	return cfg, toml.NewDecoder(f).Decode(&cfg)
}
