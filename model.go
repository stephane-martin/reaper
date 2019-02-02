package main

type Entry struct {
	UID        string `msg:"uid"`
	Host       string `msg:"host"`
	Fields     Fields `msg:"fields"`
	serialized []byte `msg:"-"`
}

type Fields map[string]Value

type Value struct {
	S []byte
	B *bool
	F *float64
}
