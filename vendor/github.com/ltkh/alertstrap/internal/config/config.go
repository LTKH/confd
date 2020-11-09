package config

import (
	//"time"
	"os"
	"github.com/naoina/toml"
)

type Config struct {
	DB               DB
	Alerts           Alerts
	Server           Server
	Menu             Menu
	Monit struct {
		Listen       string
	}
}

type Menu []struct {
	Text         string      `json:"text"`
	Type         string      `json:"-"`
	Href         string      `json:"href"`
	Nodes        []Node      `json:"nodes,omitempty"`
}

type Node struct {         
	Text         string      `json:"text"`
	Href         string      `json:"href"`
	Nodes        []Node      `json:"nodes,omitempty"`
}

type Server struct {
	Listen       string
	Cert_file    string
	Cert_key     string
}

type DB struct {
	Client       string
	Conn_string  string
	History_days int
	Alerts_table string
	Users_table  string
}

type Alerts struct {
	Limit           int
	Resolve         int64
	Delete          int64
}

func New(filename string) (cfg Config, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return cfg, err
	}
	defer f.Close()

	return cfg, toml.NewDecoder(f).Decode(&cfg)
}
