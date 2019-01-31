package main

import (
	"encoding/json"
	"fmt"
	"github.com/dop251/goja"
	"github.com/valyala/bytebufferpool"
	"strings"
	"sync"
)

var serializePool bytebufferpool.Pool

func NewEntry() *Entry {
	entry := entryPool.Get().(*Entry)
	entry.UID = NewULID().String()
	entry.Host = ""
	entry.serialized = nil
	for k := range entry.Fields {
		delete(entry.Fields, k)
	}
	return entry
}

func ReleaseEntry(e *Entry) {
	if e == nil {
		return
	}
	if e.serialized != nil {
		serializePool.Put(e.serialized)
	}
	e.serialized = nil
	entryPool.Put(e)
}

func ToFields(e *Entry, fields []string) []interface{} {
	res := make([]interface{}, 0, len(fields))
	for _, f := range fields {
		res = append(res, e.Fields[f])
	}
	return res
}

func UnmarshalEntry(b []byte) (*Entry, error) {
	if len(b) == 0 {
		return nil, nil
	}
	entry := entryPool.Get().(*Entry)
	_, err := entry.UnmarshalMsg(b)
	if err != nil {
		return nil, err
	}
	buf := serializePool.Get()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	err = enc.Encode(entry.Fields)
	if err != nil {
		serializePool.Put(buf)
		return nil, err
	}
	entry.serialized = buf
	return entry, nil
}

func newEntry() *Entry {
	return &Entry{
		Fields: make(map[string]interface{}),
	}
}

var entryPool *sync.Pool

func init() {
	entryPool = &sync.Pool{
		New: func() interface{} {
			return newEntry()
		},
	}
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

var undef = goja.Undefined()

func (e Entry) ToVM(runtime *goja.Runtime, filters []*goja.Program) (bool, error) {
	var k string
	var v interface{}

	for k, v = range e.Fields {
		runtime.Set(k, v)
	}
	for _, filter := range filters {
		result, err := runtime.RunProgram(filter)
		if err != nil {
			for k = range e.Fields {
				runtime.Set(k, undef)
			}
			return false, err
		}
		if result.ToBoolean() {
			for k = range e.Fields {
				runtime.Set(k, undef)
			}
			return true, nil
		}
	}
	for k = range e.Fields {
		runtime.Set(k, undef)
	}
	return false, nil
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
