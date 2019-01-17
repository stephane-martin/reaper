package main

import (
	"encoding/json"
	"errors"
	"github.com/urfave/cli"
	"strconv"
	"strings"
	"text/scanner"
	"time"
	"unicode"
)

type Format int

const (
	JSON Format = iota
	KeyValues
	Combined
	ApacheCommon
)

func GetFormat(c *cli.Context) (Format, error) {
	f := strings.TrimSpace(strings.ToLower(c.GlobalString("format")))
	switch f {
	case "json":
		return JSON, nil
	case "kv":
		return KeyValues, nil
	case "combined":
		return Combined, nil
	case "apache_common":
		return ApacheCommon, nil
	default:
		return 0, errors.New("unknown content format")
	}
}


func ParseContent(f Format, content string, e *Entry, logger Logger) error {
	content = strings.TrimSpace(content)
	switch f {
	case JSON:
		return parseJSON(content, e, logger)
	case KeyValues:
		return parseKV(content, e, logger)
	case Combined:
		return parseCombined(content, e, logger)
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
	s.Error = func(_ *scanner.Scanner, msg string) {
		logger.Debug("error in text scanner", "msg", msg)
	}

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
				} else {
					e.Set(key, value)
				}
			}
			key = ""
			value = ""
		}
	}
	return nil
}

func parseCombined(content string, e *Entry, logger Logger) error {
	// log_format combined '$remote_addr - $remote_user [$time_local] '
	//                     '"$request" $status $body_bytes_sent '
	//                     '"$http_referer" "$http_user_agent"';
	tokens := make([]string, 0, 12)
	var s scanner.Scanner
	s.Error = func(_ *scanner.Scanner, msg string) {
		logger.Debug("error in text scanner", "msg", msg)
	}
	s.IsIdentRune = func(ch rune, i int) bool {
		if ch == '.' || ch == '/' || ch == ':' || ch == '+' || ch == '-' {
			return true
		}
		if unicode.IsLetter(ch) {
			return true
		}
		if unicode.IsDigit(ch) {
			return true
		}
		return false
	}
	s.Init(strings.NewReader(content))
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		tokens = append(tokens, s.TokenText())
	}
	e.Set("remote_addr", tokens[0])
	e.Set("remote_user", tokens[2])
	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", tokens[4] + " " + tokens[5])
	if err == nil {
		e.Set("time_local", t.Format(time.RFC3339))
	}
	req, err := strconv.Unquote(tokens[7])
	if err == nil {
		e.Set("request", req)
	}
	status, err := strconv.ParseInt(tokens[8], 10, 64)
	if err == nil {
		e.Set("status", status)
	}
	bytesBodySent, err := strconv.ParseInt(tokens[9], 10, 64)
	if err == nil {
		e.Set("bytes_body_sent", bytesBodySent)
	}
	referer, err := strconv.Unquote(tokens[10])
	if err == nil {
		e.Set("referer", referer)
	}
	userAgent, err := strconv.Unquote(tokens[11])
	if err == nil {
		e.Set("user_agent", userAgent)
	}
	return nil
}

func parseApacheCommon(content string, e *Entry, logger Logger) error {
	// LogFormat "%h %l %u %t \"%r\" %>s %b" common
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