package main

import (
	"errors"
	"strconv"
	"strings"
	"text/scanner"
	"time"
	"unicode"

	"github.com/urfave/cli"
	"github.com/valyala/fastjson"
)

type Format int

const (
	JSON Format = iota
	KeyValues
	Combined
	Common
)

type AccessLogParser func(string, *Entry, Logger) error

var parsers = map[Format]AccessLogParser{
	JSON:      parseJSON,
	KeyValues: parseKV,
	Combined:  parseCombined,
	Common:    parseCommon,
}

func GetFormat(c *cli.Context) (Format, error) {
	f := strings.TrimSpace(strings.ToLower(c.GlobalString("format")))
	switch f {
	case "json":
		return JSON, nil
	case "kv":
		return KeyValues, nil
	case "combined":
		return Combined, nil
	case "common":
		return Common, nil
	default:
		return 0, errors.New("unknown content format")
	}
}

func ParseAccessLogLine(f Format, content string, e *Entry, logger Logger) error {
	p := parsers[f]
	if p == nil {
		return errors.New("unknown access log format")
	}
	return p(content, e, logger)
}

func parseKV(content string, e *Entry, logger Logger) error {

	var s scanner.Scanner
	var t, key, value string
	var expectKey, expectValue, expectEqual bool
	var fl float64
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
			if value[0] == '"' {
				value, err = strconv.Unquote(value)
				if err == nil {
					e.SetString(key, value)
				}
				key = ""
				value = ""
				continue L
			}
			fl, err = strconv.ParseFloat(value, 64)
			if err == nil {
				e.Fields[key] = NewFloatValue(fl)
			}
			key = ""
			value = ""
		}
	}
	return nil
}

func parseCombined(content string, e *Entry, logger Logger) error {
	// Nginx
	// '$remote_addr - $remote_user [$time_local] '
	// '"$request" $status $body_bytes_sent '
	// '"$http_referer" "$http_user_agent"';

	// Apache
	// "%h %l %u %t \"%r\" %>s %b \"%{Referer}i\" \"%{User-agent}i\""

	tokens := make([]string, 12)
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
	var i int
	for tok := s.Scan(); tok != scanner.EOF && i <= 11; tok = s.Scan() {
		tokens[i] = s.TokenText()
		i++
	}
	e.SetString("remote_addr", tokens[0])
	e.SetString("remote_user", tokens[2])
	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", tokens[4]+" "+tokens[5])
	if err == nil {
		e.SetString("time_local", t.Format(time.RFC3339))
	}
	req, err := strconv.Unquote(tokens[7])
	if err == nil {
		e.SetString("request", req)
	}
	status, err := strconv.ParseInt(tokens[8], 10, 64)
	if err == nil {
		e.Fields["status"] = NewFloatValue(float64(status))
	}
	bytesBodySent, err := strconv.ParseInt(tokens[9], 10, 64)
	if err == nil {
		e.Fields["bytes_body_sent"] = NewFloatValue(float64(bytesBodySent))
	}
	referer, err := strconv.Unquote(tokens[10])
	if err == nil {
		e.SetString("referer", referer)
	}
	userAgent, err := strconv.Unquote(tokens[11])
	if err == nil {
		e.SetString("user_agent", userAgent)
	}
	return nil
}

func parseCommon(content string, e *Entry, logger Logger) error {
	// Apache
	// LogFormat "%h %l %u %t \"%r\" %>s %b" common
	// %h Remote hostname
	// %l Remote logname
	// %u Remote user
	// %t time [18/Sep/2011:19:18:28 -0400]
	// %r First line of request
	// %>s status
	// %b Size of response, excluding HTTP headers. '-' rather than a 0.

	// Caddy:
	// {remote} - {user} [{when}] \"{method} {uri} {proto}\" {status} {size}

	tokens := make([]string, 10)
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
	var i int
	for tok := s.Scan(); tok != scanner.EOF && i <= 9; tok = s.Scan() {
		tokens[i] = s.TokenText()
		i++
	}
	e.SetString("remote_addr", tokens[0])
	e.SetString("remote_user", tokens[2])
	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", tokens[4]+" "+tokens[5])
	if err == nil {
		e.SetString("time_local", t.Format(time.RFC3339))
	}
	req, err := strconv.Unquote(tokens[7])
	if err == nil {
		e.SetString("request", req)
	}
	status, err := strconv.ParseInt(tokens[8], 10, 64)
	if err == nil {
		e.Fields["status"] = NewFloatValue(float64(status))
	}
	bytesBodySent, err := strconv.ParseInt(tokens[9], 10, 64)
	if err == nil {
		e.Fields["bytes_body_sent"] = NewFloatValue(float64(bytesBodySent))
	}

	return nil
}

func parseJSON(content string, e *Entry, _ Logger) error {
	var p fastjson.Parser

	v, err := p.Parse(content)
	if err != nil {
		return err
	}
	o, err := v.Object()
	if err != nil {
		return err
	}

	o.Visit(func(k []byte, v *fastjson.Value) {
		switch v.Type() {
		case fastjson.TypeString:
			e.SetBytes(string(k), v.GetStringBytes())
		case fastjson.TypeNumber:
			e.Fields[string(k)] = NewFloatValue(v.GetFloat64())
		case fastjson.TypeTrue:
			e.Fields[string(k)] = trueValue
		case fastjson.TypeFalse:
			e.Fields[string(k)] = falseValue
		}
	})

	return nil
}
