package main

import (
	"encoding/json"
	"errors"
	"github.com/urfave/cli"
	"strconv"
	"strings"
	"text/scanner"
)

type Format int

const (
	JSON Format = iota
	KeyValues
	NginxCombined
	ApacheCombined
	ApacheCommon
)

func GetFormat(c *cli.Context) (Format, error) {
	f := strings.TrimSpace(strings.ToLower(c.GlobalString("format")))
	switch f {
	case "json":
		return JSON, nil
	case "kv":
		return KeyValues, nil
	case "nginx_combined":
		return NginxCombined, nil
	case "apache_combined":
		return ApacheCombined, nil
	case "apache_common":
		return ApacheCommon, nil
	default:
		return 0, errors.New("unknown content format")
	}
}


func ParseContent(f Format, content string, e *Entry, logger Logger) error {
	switch f {
	case JSON:
		return parseJSON(content, e, logger)
	case KeyValues:
		return parseKV(content, e, logger)
	case NginxCombined:
		return parseNginxCombined(content, e, logger)
	case ApacheCombined:
		return parseApacheCombined(content, e, logger)
	case ApacheCommon:
		return parseApacheCommon(content, e, logger)
	default:
		return errors.New("unknown content format")
	}
}

func parseKV(content string, e *Entry, logger Logger) error {

	var s scanner.Scanner
	var t, key, value string
	var expectKey, expectValue, expectEqual bool
	var fl float64
	var in int64
	var err error

	expectKey = true
	s.Init(strings.NewReader(content))
	s.Error = func(*scanner.Scanner, string) {}

	L:
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		t = s.TokenText()
		if expectKey {
			if len(t) == 0 {
				continue L
			}
			key = t
			expectKey = false
			expectEqual = true
			continue L
		}
		if expectEqual {
			if t != "=" {
				expectKey = true
				expectEqual = false
				key = ""
				continue L
			}
			expectEqual = false
			expectValue = true
			continue L
		}
		if expectValue {
			value = t
			expectValue = false
			expectKey = true
			if value == "-" || value == "_" || value == "" {
				key = ""
				value = ""
				continue L
			}
			if value == `"_"` || value == `"-"` {
				e.Set(key, "")
				key = ""
				value = ""
				continue L
			}
			if value[0] == '"' {
				value, err = strconv.Unquote(value)
				if err == nil {
					if value == "-" || value == "_" {
						value = ""
					}
					e.Set(key, value)
				}
				key = ""
				value = ""
				continue L
			}
			fl, err = strconv.ParseFloat(value, 64)
			if err == nil {
				e.Set(key, fl)
			} else {
				in, err = strconv.ParseInt(value, 10, 64)
				if err == nil {
					e.Set(key, in)
				}
			}
			key = ""
			value = ""
		}
	}

	return nil
}

func parseNginxCombined(content string, e *Entry, logger Logger) error {
	// log_format combined '$remote_addr - $remote_user [$time_local] '
	//                     '"$request" $status $body_bytes_sent '
	//                     '"$http_referer" "$http_user_agent"';
	return nil
}

func parseApacheCombined(content string, e *Entry, logger Logger) error {
	return nil
}

func parseApacheCommon(content string, e *Entry, logger Logger) error {
	return nil
}

func parseJSON(content string, e *Entry, logger Logger) error {
	fields := make(map[string]interface{})
	err := json.Unmarshal([]byte(content), &fields)
	if err != nil {
		return err
	}
	for k, v := range fields {
		if v != nil {
			e.Set(k, v)
		}
	}
	return nil
}