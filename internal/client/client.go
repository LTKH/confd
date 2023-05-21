package client

import (
    "io"
    "log"
    "bytes"
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

type Response struct {
    Body             []byte
    StatusCode       int
    Header           http.Header
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

func (h *HttpClient) NewRequest(method, path string, data []byte, cfg HttpConfig) (Response, error) {

    var resp Response

    for _, url := range cfg.URLs {

        var reader io.ReadCloser

        req, err := http.NewRequest(method, url+path, bytes.NewReader(data))
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
        resp.StatusCode = r.StatusCode

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
            log.Printf("[error] when request to [%s] received status code: %d", url+path, r.StatusCode)
            continue
        }

        body, err := ioutil.ReadAll(reader)
        if err != nil {
            log.Printf("[error] when reading to [%s] received error: %v", url+path, err)
            continue
        }
        resp.Body = body

        return resp, nil
    }

    return resp, fmt.Errorf("failed to complete any request")
}
