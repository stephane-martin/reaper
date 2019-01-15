package main

import (
	"strings"
	"time"
)

type Entry struct {
	UID              string            `json:"uid"`
	SyslogHostname   string            `json:"syslog_hostname,omitempty"`
	SyslogTimestamp  *time.Time        `json:"syslog_timestamp,omitempty"`
	Fields           map[string]string `json:"fields,omitempty"`
	SyslogRemoteAddr string            `json:"syslog_remote_addr,omitempty"`
}

func (e *Entry) String() string {
	var b strings.Builder
	for k, v := range e.Fields {
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(v)
		b.WriteString(`" `)
	}
	return b.String()
}
