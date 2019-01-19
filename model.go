package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

/*type Entry struct {
	UID              string            `json:"uid"`
	SyslogHostname   string            `json:"syslog_hostname,omitempty"`
	SyslogTimestamp  *time.Time        `json:"syslog_timestamp,omitempty"`
	Fields           map[string]string `json:"fields,omitempty"`
	SyslogRemoteAddr string            `json:"syslog_remote_addr,omitempty"`
}*/

type Entry struct {
	UID    string                 `msg:"uid"`
	Host   string                 `msg:"host"`
	Fields map[string]interface{} `msg:"fields"`
}

func NewEntry() *Entry {
	m := &Entry{
		UID:    NewULID().String(),
		Fields: make(map[string]interface{}),
	}
	m.Fields["UID"] = m.UID
	return m
}

func (e *Entry) Set(key string, value interface{}) {
	if value != nil {
		if v, ok := value.(string); ok && (v == "-" || v == "_") {
			e.Fields[key] = ""
		} else {
			e.Fields[key] = value
			key = strings.ToLower(key)
			if key == "host" {
				e.Host = fmt.Sprintf("%s", value)
			} else if key == "server" && e.Host == "" {
				e.Host = fmt.Sprintf("%s", value)
			}
		}
	}
}

func (e Entry) String() string {
	var b strings.Builder
	for k, v := range e.Fields {
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(fmt.Sprintf("%v", v))
		b.WriteString(`" `)
	}
	return b.String()
}

func (e *Entry) JSON() []byte {
	b, err := json.Marshal(e.Fields)
	if err != nil {
		return nil
	}
	return b
}
