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
	// contents is the string contents for the template. It is either given
	// during template creation or read from disk when initialized.
	contents string

	// source is the original location of the template. This may be undefined if
	// the template was dynamically defined.
	source string
}

// ExecuteResult is the result of the template execution.
type ExecuteResult struct {
	// Output is the rendered result.
	Output []byte
}

func NewTemplate(source string) (*Template, error) {

    var t Template
	t.source = source

    return &t, nil
}

func (t *Template) Execute(jsn interface{}) (*ExecuteResult, error) {
    funcMap := template.FuncMap{
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
    }

    tmpl := template.New(filepath.Base(t.source))
    tmpl.Funcs(funcMap)

    tmpl, err := tmpl.ParseFiles(t.source)
	if err != nil {
		return nil, errors.Wrap(err, "parse")
	}

	// Execute the template into the writer
	var b bytes.Buffer
	if err := tmpl.Execute(&b, &jsn); err != nil {
		return nil, errors.Wrap(err, "execute")
	}

    return &ExecuteResult{
		Output:  b.Bytes(),
	}, nil
}
