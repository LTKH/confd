package template

import (
    "strings"
    "time"
    "text/template"
    "path/filepath"
    "bytes"
    "github.com/pkg/errors"
)

// Template is the internal representation of an individual template to process.
// The template retains the relationship between it's contents and is
// responsible for it's own execution.
type Template struct {
    contents  string
    funcMap   template.FuncMap
}

func NewTemplate() (*Template, error) {

    var t Template
    t.funcMap = template.FuncMap{
        "isArray":         isArray,
        "isSlice":         isSlice,
        "toInt":           toInt,
        "toFloat":         toFloat,
        "add":             addFunc,
        "strQuote":        strQuote,
        "base":            filepath.Base,
        "split":           strings.Split,
        "dir":             filepath.Dir,
        "createMap":       createMap,
        "pushToMap":       pushToMap,
        "createArr":       createArray,
        "pushToArr":       pushToArray,
        "join":            join,
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
        "connectHttp":     connectHttpFunc,
        "regexReplaceAll": regexReplaceAll,
        "regexMatch":      regexMatch,
        "replaceAll":      replaceAll,
        "lookupIPV4":      lookupIPV4,
        "lookupIPV6":      lookupIPV6,
        "fileExist":       fileExist,
        "hostname":        hostname,
    }

    return &t, nil
}

func (t *Template) Execute(source string, jsn interface{}) ([]byte, error) {

    tmpl, err := template.New("new").Funcs(t.funcMap).Parse(source)
    if err != nil {
        return nil, errors.Wrap(err, "parse")
    }

    // Execute the template into the writer
    var b bytes.Buffer
    if err := tmpl.Execute(&b, &jsn); err != nil {
        return nil, errors.Wrap(err, "execute")
    }

    return b.Bytes(), nil
}

func (t *Template) ParseFile(source string, jsn interface{}) ([]byte, error) {

    tmpl := template.New(filepath.Base(source))
    tmpl.Funcs(t.funcMap)

    tmpl, err := tmpl.ParseFiles(source)
    if err != nil {
        return nil, errors.Wrap(err, "parse")
    }

    // Execute the template into the writer
    var b bytes.Buffer
    if err := tmpl.Execute(&b, &jsn); err != nil {
        return nil, errors.Wrap(err, "execute")
    }

    return b.Bytes(), nil
}
