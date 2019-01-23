package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

func NewEntry() *Entry {
	entry := entryPool.Get().(*Entry)
	entry.UID = NewULID().String()
	entry.Host = ""
	for k := range entry.Fields {
		delete(entry.Fields, k)
	}
	return entry
}

func ReleaseEntry(e *Entry) {
	if e == nil {
		return
	}
	entryPool.Put(e)
}

func MarshalEntry(e *Entry) ([]byte, error) {
	if e == nil {
		return nil, nil
	}
	b, err := e.MarshalMsg(nil)
	if err != nil {
		panic(err)
	}
	ReleaseEntry(e)
	return b, nil
}

func JMarshalEntry(e *Entry) ([]byte, error) {
	if e == nil {
		return nil, nil
	}
	b, err := json.Marshal(e.Fields)
	if err != nil {
		return nil, err
	}
	ReleaseEntry(e)
	return b, nil
}

func Fields(e *Entry, fields []string) []interface{} {
	res := make([]interface{}, 0, len(fields))
	for _, f := range fields {
		res = append(res, e.Fields[f])
	}
	ReleaseEntry(e)
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


