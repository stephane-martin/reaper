package main

import "time"

type Entry struct {
	UID              string            `json:"uid"`
	SyslogHostname   string            `json:"syslog_hostname,omitempty"`
	SyslogTimestamp  *time.Time        `json:"syslog_timestamp,omitempty"`
	Fields           map[string]string `json:"fields,omitempty"`
	SyslogRemoteAddr string            `json:"syslog_remote_addr,omitempty"`
}
