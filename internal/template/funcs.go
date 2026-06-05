package template

import (
    "fmt"
    "reflect"
    "regexp"
    "strconv"
    "strings"
    "net/http"
    "time"
    "net"
    "sort"
    "os"
    "io/ioutil"
    "bytes"
    //"errors"
    "encoding/json"
)

func isArray(v interface{}) bool {
    rt := reflect.TypeOf(v)
    if rt.Kind() == reflect.Array {
        return true
    }

    return false
}

func isSlice(v interface{}) bool {
    rt := reflect.TypeOf(v)
    if rt.Kind() == reflect.Slice {
        return true
    }

    return false
}

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

func toString(data interface{}) (string, error) {
    return fmt.Sprintf("%v", data), nil
}

func toJson(data interface{}) (string, error) {
    result, err := json.MarshalIndent(&data, "", "\t")
    if err != nil {
        return "", err
    }

    return string(result), nil
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
            Proxy: http.ProxyFromEnvironment,
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

    if resp.StatusCode == code {
        return true
    }

    return false
}

func requestHttpFunc(method string, url string, data string, headers map[string]interface{}) (string, error) {
    client := &http.Client{
        Transport: &http.Transport{
            Proxy: http.ProxyFromEnvironment,
        },
        Timeout: time.Duration(5) * time.Second,
    }
    
    request, err := http.NewRequest(method, url, bytes.NewBufferString(data))
    if err != nil {
        return "", err
    }

    for key, val := range headers {
        request.Header.Set(key, fmt.Sprintf("%v", val))
    }

    resp, err := client.Do(request)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }

    return string(body), nil
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

// join is a version of strings.Join that can be piped
func join(sep string, a []interface{}) (string, error) {
    var arr []string
    for _, v := range a {
        arr = append(arr, v.(string))
    }
    return strings.Join(arr, sep), nil
}

func lookupIP(data string) []string {
    ips, err := net.LookupIP(data)
    if err != nil {
        return nil
    }
    // "Cast" IPs into strings and sort the array
    ipStrings := make([]string, len(ips))

    for i, ip := range ips {
        ipStrings[i] = ip.String()
    }
    sort.Strings(ipStrings)
    return ipStrings
}

func lookupIPV6(data string) []string {
    var addresses []string
    for _, ip := range lookupIP(data) {
        if strings.Contains(ip, ":") {
            addresses = append(addresses, ip)
        }
    }
    return addresses
}

func lookupIPV4(data string) []string {
    var addresses []string
    for _, ip := range lookupIP(data) {
        if strings.Contains(ip, ".") {
            addresses = append(addresses, ip)
        }
    }
    return addresses
}

func fileExist(f string) bool {
    _, err := os.Stat(f)
    if os.IsNotExist(err) {
        return false
    }
    return true
}

func hostname() (string, error) {
    hostname, err := os.Hostname()
    if err != nil {
        return "", err
    }

    return hostname, nil
}

func fromJson(data string) (interface{}, error) {
    var result interface{}

    if err := json.Unmarshal([]byte(data), &result); err != nil {
        return result, err
    }
    
    return result, nil
}

func fromJsonArray(data string) ([]interface{}, error) {
    var result []interface{}

    if err := json.Unmarshal([]byte(data), &result); err != nil {
        return result, err
    }
    
    return result, nil
}

func coalesce(args ...interface{}) interface{} {
    for _, arg := range args {
        // Простая проверка на nil/пустоту
        if arg != nil && arg != "" && arg != 0 && arg != false {
            return arg
        }
    }
    return nil
}

func concat[T any](slices ...[]T) []T {
    var result []T
    for _, s := range slices {
        result = append(result, s...)
    }
    return result
}

func deepCopy(src interface{}) interface{} {
    if src == nil {
        return nil
    }

    // Используем рефлексию для определения типа
    srcVal := reflect.ValueOf(src)

    switch srcVal.Kind() {
    case reflect.Ptr:
        if srcVal.IsNil() {
            return nil
        }
        // Создаем новый указатель и рекурсивно копируем содержимое
        newPtr := reflect.New(srcVal.Type().Elem())
        val := deepCopy(srcVal.Elem().Interface())
        newPtr.Elem().Set(reflect.ValueOf(val))
        return newPtr.Interface()

    case reflect.Map:
        if srcVal.IsNil() {
            return nil
        }
        // Создаем новую карту
        newMap := reflect.MakeMap(srcVal.Type())
        for _, key := range srcVal.MapKeys() {
            val := srcVal.MapIndex(key)
            newMap.SetMapIndex(key, reflect.ValueOf(deepCopy(val.Interface())))
        }
        return newMap.Interface()

    case reflect.Slice:
        if srcVal.IsNil() {
            return nil
        }
        // Создаем новый слайс
        newSlice := reflect.MakeSlice(srcVal.Type(), srcVal.Len(), srcVal.Cap())
        for i := 0; i < srcVal.Len(); i++ {
            val := srcVal.Index(i)
            newSlice.Index(i).Set(reflect.ValueOf(deepCopy(val.Interface())))
        }
        return newSlice.Interface()

    case reflect.Struct:
        // Для структур проще всего использовать копирование по значению через интерфейс,
        // если в них нет вложенных ссылочных типов.
        // Sprig для структур часто полагается на аналогичную рекурсию по полям.
        return src 

    default:
        // Базовые типы (int, string и т.д.) копируются по значению автоматически
        return src
    }
}

func set(m map[string]interface{}, key string, value interface{}) map[string]interface{} {
    m[key] = value
    return m
}

func getValueByPath(path string, obj interface{}) string {
    parts := strings.Split(path, ".")
    v := reflect.ValueOf(obj)

    for _, part := range parts {
        // Обработка указателей
        for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
            v = v.Elem()
        }

        if v.Kind() == reflect.Map {
            v = v.MapIndex(reflect.ValueOf(part))
        } else if v.Kind() == reflect.Struct {
            v = v.FieldByName(part)
        } else {
            return ""
        }

        if !v.IsValid() {
            return ""
        }
    }

    // Если конечный результат — массив или срез
    if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
        var strParts []string
        for i := 0; i < v.Len(); i++ {
            // Превращаем каждый элемент в строку и добавляем в список
            strParts = append(strParts, fmt.Sprintf("%v", v.Index(i).Interface()))
        }
        // Объединяем через ":"
        return strings.Join(strParts, ":")
    }

    return fmt.Sprintf("%v", v.Interface())
}

func sortByPath(path string, list []interface{}) []interface{} {
    if len(list) <= 1 {
        return list
    }

    result := make([]interface{}, len(list))
    copy(result, list)

    sort.SliceStable(result, func(i, j int) bool {
        valI := getValueByPath(path, result[i])
        valJ := getValueByPath(path, result[j])
        return valI < valJ
    })

    return result
}
