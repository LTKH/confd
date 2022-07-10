package client

import (
    "io"
    "log"
    "net/http"
    "time"
    "io/ioutil"
    "fmt"
    "compress/gzip"
)

type HttpClient struct {
    client           *http.Client
}

type HttpConfig struct {
    URLs             []string
    Headers          map[string]string
    ContentEncoding  string
}

func NewHttpClient() *HttpClient {
    client := &HttpClient{ 
        client: &http.Client{
            Transport: &http.Transport{
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
                DisableCompression:  false,
            },
            Timeout: 5 * time.Second,
        },
    }
    return client
}

func (h *HttpClient) ReadJson(cfg HttpConfig, path string) ([]byte, error) {

    for _, url := range cfg.URLs {

        var reader io.ReadCloser

        req, err := http.NewRequest("GET", url+path, nil)
        if err != nil {
            log.Printf("[error] %s - %v", url, err)
            continue
        }

        for name, value := range cfg.Headers {
            req.Header.Set(name, value)
        }

        req.Header.Set("Accept-Encoding", "gzip")

        r, err := h.client.Do(req)
        if err != nil {
            log.Printf("[error] %s - %v", url, err)
            continue
        }
        defer r.Body.Close()

        // Check that the server actual sent compressed data
        switch r.Header.Get("Content-Encoding") {
            case "gzip":
                reader, err = gzip.NewReader(r.Body)
                if err != nil {
                    log.Printf("[error] %s - %v", url, err)
                    continue
                }
                defer reader.Close()
            default:
                reader = r.Body
        }

        if r.StatusCode >= 400 {
            log.Printf("[error] when reading to [%s] received status code: %d", url+path, r.StatusCode)
            continue
        }

        body, err := ioutil.ReadAll(reader)
        if err != nil {
            log.Printf("[error] when reading to [%s] received error: %v", url+path, err)
            continue
        }

        return body, nil
    }

    return nil, fmt.Errorf("failed to complete any request")
}