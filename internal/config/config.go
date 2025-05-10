package config

import (
    "os"
    "log"
    "regexp"
    "strings"
    "io/ioutil"
    "crypto/md5"
    "encoding/hex"
    "gopkg.in/yaml.v2"
)

type Config struct {
    Global           Global                  `yaml:"global"`
    Logger           Logger                  `yaml:"logger"`
    Backends         []Backend               `yaml:"backends"`
}

type Global struct {
    CertFile         string                  `yaml:"cert_file"`
    CertKey          string                  `yaml:"cert_key"`
    Users            []*UserInfo             `yaml:"users"`
}

type Logger struct {
    Urls             []string                `yaml:"urls"`
    //Methods          []string                `yaml:"methods"`
}

type Backend struct {
    Backend          string                  `yaml:"backend"`
    Id               string                  `yaml:"id"`
    Nodes            []string                `yaml:"nodes"`
    Write            Attributes              `yaml:"write"`
    Read             Attributes              `yaml:"read"`
    Checks           []*Scheme               `yaml:"checks"`
    Users            map[string]string           
}

type Scheme struct {
    Path             string                  `yaml:"path"`
    RePath           *regexp.Regexp
    Regexp           string                  `yaml:"regexp"`
    ReRegexp         *regexp.Regexp
    Method           string                  `yaml:"method"`
    Schema           string                  `yaml:"schema"`
    Users            []string                `yaml:"users"`
    Dir              string                  `yaml:"dir"`
}

type Attributes struct {
    Username         string                  `yaml:"username"`
    Password         string                  `yaml:"password"`
}

type UserInfo struct {
    Username         string                  `yaml:"username"`
    Password         string                  `yaml:"password"`
}

type Action struct {
    Login            string                  `json:"login"`
    Action           string                  `json:"action"`
    Object           string                  `json:"object"`
    Attributes       map[string]interface{}  `json:"attributes"`
    Description      string                  `json:"description"`
    Timestamp        int64                   `json:"timestamp"`
}

func getEnv(value string) string {
    if len(value) > 0 && string(value[0]) == "$" {
        val, ok := os.LookupEnv(strings.TrimPrefix(value, "$"))
        if !ok {
            log.Printf("[error] no value found for %v", value)
            return ""
        }
        return val
    }

    return value
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

    for u, usr := range cfg.Global.Users {
        cfg.Global.Users[u].Username = getEnv(usr.Username)
        cfg.Global.Users[u].Password = getEnv(usr.Password)
    }

    for b, backend := range cfg.Backends {
        cfg.Backends[b].Users = map[string]string{}

        for _, usr := range cfg.Global.Users {
            cfg.Backends[b].Users[usr.Username] = usr.Password
        }

        cfg.Backends[b].Read.Username = getEnv(backend.Read.Username)
        cfg.Backends[b].Read.Password = getEnv(backend.Read.Password)
        cfg.Backends[b].Write.Username = getEnv(backend.Write.Username)
        cfg.Backends[b].Write.Password = getEnv(backend.Write.Password)

        for _, check := range backend.Checks {
            if check.Path != "" {
                re, err := regexp.Compile(check.Path)
                if err != nil {
                    log.Fatalf("[error] %v", err)
                }
                check.RePath = re
            }
            if check.Regexp != "" {
                re, err := regexp.Compile(check.Regexp)
                if err != nil {
                    log.Fatalf("[error] %v", err)
                }
                check.ReRegexp = re
            }
        }
    }
    
    return cfg, nil
}