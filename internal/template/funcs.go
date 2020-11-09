package template

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sort"
	"net"
	"strconv"
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

func regexReplaceAll(re, pl, s string) (string, error) {
	compiled, err := regexp.Compile(re)
	if err != nil {
		return "", err
	}
	return compiled.ReplaceAllString(s, pl), nil
}

func LookupIP(data string) []string {
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


func LookupIPV6(data string) []string {
	var addresses []string
	for _, ip := range LookupIP(data) {
		if strings.Contains(ip, ":") {
			addresses = append(addresses, ip)
		}
	}
	return addresses
}

func LookupIPV4(data string) []string {
	var addresses []string
	for _, ip := range LookupIP(data) {
		if strings.Contains(ip, ".") {
			addresses = append(addresses, ip)
		}
	}
	return addresses
}

func strQuote(data string) (string, error) {
	s := strconv.Quote(data)
	return s[1:len(s)-1], nil
}

