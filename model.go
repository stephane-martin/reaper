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

type Entry map[string]interface{}

func NewEntry() Entry {
	m := make(map[string]interface{})
	m["UID"] = NewULID().String()
	return m
}

func (e Entry) UID() string {
	return e["UID"].(string)
}

func (e *Entry) Set(key string, value interface{}) {
	if value != nil {
		if v, ok := value.(string); ok && (v == "-" || v == "_") {
			(*e)[key] = ""
		} else {
			(*e)[key] = value
		}
	}
}

func (e Entry) String() string {
	var b strings.Builder
	for k, v := range e {
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(fmt.Sprintf("%v", v))
		b.WriteString(`" `)
	}
	return b.String()
}


func (e *Entry) JSON() []byte {
	b, err := json.Marshal(e)
	if err != nil {
		return nil
	}
	return b
}