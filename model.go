package main

import "github.com/valyala/bytebufferpool"

type Entry struct {
	UID        string                     `msg:"uid"`
	Host       string                     `msg:"host"`
	Fields     map[string]interface{}     `msg:"fields"`
	serialized *bytebufferpool.ByteBuffer `msg:"-" json:"-"`
}
