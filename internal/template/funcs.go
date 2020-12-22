package template

import (
    "fmt"
    "reflect"
    "regexp"
    "strconv"
    "strings"
    "net/http"
    "time"
)

func toInt(i interface{}) (int64, error) {
    iv := reflect.ValueOf(i)
    
    switch iv.Kind() {
        case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
            return iv.Int(), nil
        case reflect.Float32, reflect.Float64:
            return int64(iv.Float()), nil
    }

    return 0, fmt.Errorf("unknown type - %T", i)    
}

func toFloat(i interface{}) (float64, error) {
    iv := reflect.ValueOf(i)
    
    switch iv.Kind() {
        case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
            return float64(iv.Int()), nil
        case reflect.Float32, reflect.Float64:
            return iv.Float(), nil    
    }

    return 0, fmt.Errorf("unknown type - %T", i)
}

func addFunc(b, a interface{}) (float64, error) {
    av := reflect.ValueOf(a)
    bv := reflect.ValueOf(b)

    switch av.Kind() {
        case reflect.Int:
            switch bv.Kind() {
                case reflect.Int:
                    return float64(av.Int() + bv.Int()), nil
                case reflect.Float64:
                    return float64(av.Int()) + bv.Float(), nil
                default:
                    return 0, fmt.Errorf("unknown type - %T", b)
            }
        case reflect.Float64:
            switch bv.Kind() {
                case reflect.Int:
                    return av.Float() + float64(bv.Int()), nil
                case reflect.Float64:
                    return av.Float() + bv.Float(), nil
                default:
                    return 0, fmt.Errorf("unknown type - %T", b)
            }
        default:
            return 0, fmt.Errorf("unknown type - %T", a)
    }
}

func strQuote(data string) (string, error) {
    s := strconv.Quote(data)
    return s[1:len(s)-1], nil
}

func createMap() map[string]interface{} {
    return map[string]interface{}{}
}

func pushToMap(mp map[string]interface{}, k string, vl interface{}) map[string]interface{} {
    mp[k] = vl
    return mp
}

func createArray() []interface{} {
    return []interface{}{}
}

func pushToArray(arr []interface{}, vl interface{}) []interface{} {
    return append(arr, vl)
}

func connectHttpFunc(method string, url string, code int) bool {
    client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			//TLSClientConfig:   tlsCfg,
		},
		Timeout: time.Duration(5) * time.Second,
    }
    
    request, err := http.NewRequest(method, url, nil)
    if err != nil {
        return false
    }

    resp, err := client.Do(request)
    if err != nil {
        return false
    }
    defer resp.Body.Close()

    if resp.StatusCode != code {
        return false
    }

    return true
}

// replaceAll replaces all occurrences of a value in a string with the given
// replacement value.
func replaceAll(f, t, s string) (string, error) {
	return strings.Replace(s, f, t, -1), nil
}

// regexReplaceAll replaces all occurrences of a regular expression with
// the given replacement value.
func regexReplaceAll(re, pl, s string) (string, error) {
	compiled, err := regexp.Compile(re)
	if err != nil {
		return "", err
	}
	return compiled.ReplaceAllString(s, pl), nil
}

// regexMatch returns true or false if the string matches
// the given regular expression
func regexMatch(re, s string) (bool, error) {
	compiled, err := regexp.Compile(re)
	if err != nil {
		return false, err
	}
	return compiled.MatchString(s), nil
}