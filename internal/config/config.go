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
    Users            GlobUsers               `yaml:"users"`
}

type GlobUsers map[string]string

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
    Checks           map[string][]*Scheme    `yaml:"checks"`
    //Users            map[string]string        
    Cache            bool                    `yaml:"cache"`   
}

type Scheme struct {
    //Method           string                  `yaml:"method"`
    Pattern          string                  `yaml:"pattern"`
    Path             string                  `yaml:"path"`
    RePath           *regexp.Regexp
    Regexp           string                  `yaml:"regexp"`
    ReRegexp         *regexp.Regexp
    Schema           string                  `yaml:"schema"`
    Users            Users                   `yaml:"users"`
    Dir              string                  `yaml:"dir"`
}

type Users map[string]string

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
            log.Fatalf("[error] no value found for %v", value)
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

func (u *GlobUsers) UnmarshalYAML(unmarshal func(interface{}) error) error {
    // Временная структура для чтения массива
    var users []UserInfo
    if err := unmarshal(&users); err != nil {
        return err
    }

    // Создаем карту и заполняем её
    result := make(map[string]string)
    for _, usr := range users {
        usr.Username = getEnv(usr.Username)
        usr.Password = getEnv(usr.Password)
        result[usr.Username] = usr.Password
    }
    *u = result
    return nil
}

func (u *Users) UnmarshalYAML(unmarshal func(interface{}) error) error {
    // Временная структура для чтения массива
    var arr []string
    if err := unmarshal(&arr); err != nil {
        return err
    }

    // Создаем карту и заполняем её
    result := make(map[string]string)
    for _, item := range arr {
        result[item] = ""
    }
    *u = result
    return nil
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

    //for u, usr := range cfg.Global.Users {
    //    cfg.Global.Users[u].Username = getEnv(usr.Username)
    //    cfg.Global.Users[u].Password = getEnv(usr.Password)
    //}

    for b, backend := range cfg.Backends {
        //cfg.Backends[b].Users = map[string]string{}

        //for _, usr := range cfg.Global.Users {
        //    cfg.Backends[b].Users[usr.Username] = usr.Password
        //}

        cfg.Backends[b].Read.Username = getEnv(backend.Read.Username)
        cfg.Backends[b].Read.Password = getEnv(backend.Read.Password)
        cfg.Backends[b].Write.Username = getEnv(backend.Write.Username)
        cfg.Backends[b].Write.Password = getEnv(backend.Write.Password)

        for method, _ := range backend.Checks {
            for _, check := range backend.Checks[method] {
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
                for u, _ := range check.Users {
                    if pass, ok := cfg.Global.Users[u]; ok {
                        check.Users[u] = pass
                    }
                }
            }
        }
    }
    
    return cfg, nil
}