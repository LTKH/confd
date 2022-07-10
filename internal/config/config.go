package config

import (
    "io/ioutil"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Global           Global             `yaml:"global"`
	Backends         []Backend          `yaml:"backends"`
}

type Backend struct {
	Backend          string             `yaml:"backend"`
	Id               string             `yaml:"id"`
    Nodes            []string           `yaml:"nodes"`
    Username         string             `yaml:"username"`
	Password         string             `yaml:"password"`
}

type Global struct {
	CertFile         string             `yaml:"cert_file"`
	CertKey          string             `yaml:"cert_key"`
}

func LoadConfigFile(filename string) (*Config, error) {
	cfg := &Config{}

    content, err := ioutil.ReadFile(filename)
    if err != nil {
       return cfg, err
    }

    if err := yaml.UnmarshalStrict(content, cfg); err != nil {
        return cfg, err
	}
	
	return cfg, nil
}