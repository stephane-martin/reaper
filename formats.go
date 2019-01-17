package main

import (
	"encoding/json"
	"errors"
	"github.com/urfave/cli"
	"strconv"
	"strings"
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
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return nil
	}

	var key string
	var value string
	var err error
	var fl float64
	var in int64

	for _, field := range fields {
		fieldParts := strings.SplitN(field, "=", 2)
		if len(fieldParts) == 2 {
			key = fieldParts[0]
			value = fieldParts[1]
			if len(value) > 0 {
				if value[0] == '"' {
					v, err := strconv.Unquote(value)
					if err == nil {
						e.Set(key, v)
					}
				} else if value != "-" {
					fl, err = strconv.ParseFloat(value, 64)
					if err == nil {
						e.Set(key, fl)
					} else {
						in, err = strconv.ParseInt(value, 10, 64)
						if err == nil {
							e.Set(key, in)
						} else {
							logger.Info("Failed to parse key/value", "key", key, "value", value)
						}
					}
				}
			}
		}
	}
	return nil
}

func parseNginxCombined(content string, e *Entry, logger Logger) error {
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