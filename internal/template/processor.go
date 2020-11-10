package template

import (
	"fmt"
	"log"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
	"text/template"
	"path/filepath"
	"bytes"
	"encoding/json"
	"crypto/md5"
	"encoding/hex"
	"os"
)

type HTTPTemplate struct {
	URLs                []string           `toml:"urls"`
	Timeout             time.Duration      `toml:"timeout"`
	Src                 string             `toml:"src"`
	Dest                string             `toml:"dest"`
	CheckCmd            string             `toml:"check_cmd"`
	ReloadCmd           string             `toml:"reload_cmd"`

	ContentEncoding     string             `toml:"content_encoding"`

	Headers             map[string]string  `toml:"headers"`

	// HTTP Basic Auth Credentials
	Username            string             `toml:"username"`
	Password            string             `toml:"password"`

	// Absolute path to file with Bearer token
	BearerToken         string             `toml:"bearer_token"`

	client              *http.Client
}

func getHash(data []byte) string {
	hsh := md5.New()
	hsh.Write(data)
	return hex.EncodeToString(hsh.Sum(nil))
}

func New(h HTTPTemplate) HTTPTemplate {

	// Set default timeout
	if h.Timeout == 0 {
		h.Timeout = 5000
	}

	h.client = &http.Client{
		Transport: &http.Transport{
			//TLSClientConfig: tlsCfg,
			Proxy:           http.ProxyFromEnvironment,
		},
		//Timeout: h.Timeout,
	}

	return h
}

func (h *HTTPTemplate) getGonfig(jsn interface{}) ([]byte, error) {
	funcMap := template.FuncMap{
		"toInt":           toInt,
		"toFloat":         toFloat,
		"add":             addFunc,
		"regexReplace":    regexReplaceAll,
		"strQuote":        strQuote,
		"base":            filepath.Base,
		"split":           strings.Split,
		"dir":             filepath.Dir,
		"createMap":       createMap,
		"pushToMap":       pushToMap,
		"createArray":     createArray,
		"pushToArray":     pushToArray,
		"join":            strings.Join,
		"datetime":        time.Now,
		"toUpper":         strings.ToUpper,
		"toLower":         strings.ToLower,
		"contains":        strings.Contains,
		"replace":         strings.Replace,
		"trimSuffix":      strings.TrimSuffix,
		"sub":             func(a, b int) int { return a - b },
		"div":             func(a, b int) int { return a / b },
		"mod":             func(a, b int) int { return a % b },
		"mul":             func(a, b int) int { return a * b },
	}

	tmpl, err := template.New(filepath.Base(h.Src)).Funcs(funcMap).ParseFiles(h.Src)
	if err != nil {
		return nil, err
	}

	var conf bytes.Buffer
	if err = tmpl.Execute(&conf, &jsn); err != nil {
		return nil, err
	}

	return conf.Bytes(), nil
}

func (h *HTTPTemplate) GetResponse() ([]byte, error) {

	for _, url := range h.URLs {

		request, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Printf("[error] %s - %v", url, err)
			continue
		}

		if h.BearerToken != "" {
			token, err := ioutil.ReadFile(h.BearerToken)
			if err != nil {
				log.Printf("[error] %s - %v", url, err)
				continue
			}
			bearer := "Bearer " + strings.Trim(string(token), "\n")
			request.Header.Set("Authorization", bearer)
		}

		if h.ContentEncoding == "gzip" {
			request.Header.Set("Content-Encoding", "gzip")
		}

		for k, v := range h.Headers {
			if strings.ToLower(k) == "host" {
				request.Host = v
			} else {
				request.Header.Add(k, v)
			}
		}

		if h.Username != "" || h.Password != "" {
			request.SetBasicAuth(h.Username, h.Password)
		}

		resp, err := h.client.Do(request)
		if err != nil {
			log.Printf("[error] %s - %v", url, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			log.Printf("[error] %s - received status code %d (%s), expected any value out of 200", url, resp.StatusCode, http.StatusText(resp.StatusCode))
			continue
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		return body, nil
	}

	return nil, fmt.Errorf("error failed to complete any request")
}

func (h *HTTPTemplate) GatherURL() (bool, error) {
	
	body, err := h.GetResponse()
	if err != nil {
		return false, err
	}

	var jsn interface{}
	if err := json.Unmarshal(body, &jsn); err != nil {
		return false, fmt.Errorf("error reading json from response body: %s", err)
	}

	cont, err := h.getGonfig(jsn)
	if err != nil {
		return false, fmt.Errorf("error generating config: %s", err)
	}

	if _, err := os.Stat(h.Dest); err == nil {
		conf, err := ioutil.ReadFile(h.Dest)
		if err != nil {
			return false, fmt.Errorf("error reading config file %s: %s", h.Dest, err)
		}
		if getHash(conf) != getHash(cont) {
			if err := ioutil.WriteFile(h.Dest, cont, 0644); err != nil {
				return false, fmt.Errorf("error writing config file %s: %s", h.Dest, err)
			}
            return true, nil
		}
	} else if os.IsNotExist(err) {
		if err := ioutil.WriteFile(h.Dest, cont, 0644); err != nil {
			return false, fmt.Errorf("error writing config file %s: %s", h.Dest, err)
		}
		return true, nil
	} else {
		return false, fmt.Errorf("error reading config file status %s: %s", h.Dest, err)
	}

	return false, nil
}
