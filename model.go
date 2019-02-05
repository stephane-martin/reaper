package main

import "time"

type Entry struct {
	UID        string    `msg:"uid"`
	Host       string    `msg:"host"`
	Fields     Fields    `msg:"fields"`
	Created    time.Time `msg:"created"`
	serialized []byte    `msg:"-"`
}

type Fields map[string]Value

type Value struct {
	S []byte
	B *bool
	F *float64
}
