package main

type Entry struct {
	UID    string                 `msg:"uid"`
	Host   string                 `msg:"host"`
	Fields map[string]interface{} `msg:"fields"`
}

