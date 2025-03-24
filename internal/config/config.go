package config

import (
    "io/ioutil"
	"crypto/md5"
    "encoding/hex"
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
	Write            Attributes         `yaml:"write"`
	Read             Attributes         `yaml:"read"`
}

type Attributes struct {
	Username         string             `yaml:"username"`
	Password         string             `yaml:"password"`
	KeyMasks         []string           `yaml:"keys"`
}

type Global struct {
	CertFile         string             `yaml:"cert_file"`
	CertKey          string             `yaml:"cert_key"`
}

func GetHash(data []byte) string {
    hsh := md5.New()
    hsh.Write(data)
    return hex.EncodeToString(hsh.Sum(nil))
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