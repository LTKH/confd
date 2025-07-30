package client

import (
    "io"
    "log"
    "bytes"
    //"net/url"
    "net/http"
    "time"
    "io/ioutil"
    "fmt"
    "crypto/tls"
    "compress/gzip"
)

type HttpClient struct {
    client           *http.Client
}

type HttpConfig struct {
    URLs             []string
    Headers          map[string]string
    ContentEncoding  string
    Username         string
    Password         string
}

type Response struct {
    Body             []byte
    StatusCode       int
    Header           http.Header
}

func NewHttpClient(timeout time.Duration) *HttpClient {
    client := &HttpClient{ 
        client: &http.Client{
            Transport: &http.Transport{
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
                DisableCompression:  false,
                TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
            },
            Timeout: time.Duration(timeout) * time.Second,
        },
    }
    return client
}

func (h *HttpClient) NewRequest(method, path, hash string, data []byte, cfg HttpConfig) (Response, error) {

    var resp Response

    for _, cfgUrl := range cfg.URLs {

        req, err := http.NewRequest(method, cfgUrl+path, bytes.NewReader(data))
        if err != nil {
            log.Printf("[error] %v", err)
            continue
        }
        if method == "PUT"{
            req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        }

        for name, value := range cfg.Headers {
            req.Header.Set(name, value)
        }

        if cfg.Username != "" && cfg.Password != "" {
            req.SetBasicAuth(cfg.Username, cfg.Password)
        }

        req.Header.Set("Accept-Encoding", "gzip")
        req.Header.Set("X-Custom-Hash", hash)

        r, err := h.client.Do(req)
        if err != nil {
            log.Printf("[error] %v", err)
            continue
        }
        defer r.Body.Close()
        resp.StatusCode = r.StatusCode

        var reader io.ReadCloser

        // Check that the server actual sent compressed data
        switch r.Header.Get("Content-Encoding") {
            case "gzip":
                reader, err = gzip.NewReader(r.Body)
                if err != nil {
                    log.Printf("[error] %s - %v", path, err)
                    continue
                }
                defer reader.Close()
            default:
                reader = r.Body
        }

        if r.StatusCode >= 500 {
            log.Printf("[error] when request to [%s] received status code: %d", path, r.StatusCode)
            continue
        }

        body, err := ioutil.ReadAll(reader)
        if err != nil {
            log.Printf("[error] when reading to [%s] received error: %v", path, err)
            continue
        }
        resp.Body = body

        return resp, nil
    }

    return resp, fmt.Errorf("failed to complete any request")
}
