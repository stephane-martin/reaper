package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/dop251/goja"
	fflib "github.com/pquerna/ffjson/fflib/v1"
)

var serPool = &sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 4096)
	},
}

var t bool = true
var f bool = false

var trueValue = Value{
	B: &t,
}

var falseValue = Value{
	B: &f,
}

func getBuffer() []byte {
	buf := serPool.Get().([]byte)
	return buf[:0]
}

func releaseBuffer(buf []byte) {
	if buf != nil {
		serPool.Put(buf)
	}
}

func NewEntry() *Entry {
	entry := entryPool.Get().(*Entry)
	for k := range entry.Fields {
		delete(entry.Fields, k)
	}

	entry.UID = NewULID().String()
	entry.Fields["uid"] = Value{S: []byte(entry.UID)}
	entry.Host = ""
	entry.serialized = nil

	return entry
}

func ReleaseEntry(e *Entry) {
	if e == nil {
		return
	}
	releaseBuffer(e.serialized)
	e.serialized = nil
	entryPool.Put(e)
}

func ToFields(e *Entry, fields []string) []interface{} {
	res := make([]interface{}, 0, len(fields))
	var (
		f string
		i interface{}
	)
	for _, f = range fields {
		i = e.Fields[f].V()
		if i != nil {
			res = append(res, e.Fields[f])
		}
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
	buf := getBuffer()
	fbuf := fflib.NewBuffer(buf)
	err = entry.Fields.MarshalJSONBuf(fbuf)
	if err != nil {
		releaseBuffer(buf)
		return nil, err
	}
	entry.serialized = fbuf.Bytes()
	if len(entry.serialized) == 0 {
		releaseBuffer(buf)
		return nil, nil
	}
	return entry, nil
}

func newEntry() *Entry {
	return &Entry{
		Fields: make(map[string]Value),
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

func (e *Entry) SetBytes(key string, value []byte) {
	e.SetString(key, string(value))
}

func (e *Entry) SetString(key string, value string) {
	if value == "-" || value == "_" {
		value = ""
	}
	e.Fields[key] = Value{S: []byte(value)}
	key = strings.ToLower(key)
	if key == "host" {
		e.Host = value
	} else if key == "server" && e.Host == "" {
		e.Host = value
	}
}

var undef = goja.Undefined()

func (e Entry) ToVM(runtime *goja.Runtime, filters []*goja.Program) (bool, error) {
	var k string
	var v Value
	var i interface{}

	for k, v = range e.Fields {
		i = v.V()
		if i != nil {
			runtime.Set(k, v.V())
		}
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

func (j Fields) MarshalJSON() ([]byte, error) {
	var buf fflib.Buffer
	err := j.MarshalJSONBuf(&buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (j *Fields) MarshalJSONBuf(buf fflib.EncodingBuffer) error {
	if j == nil {
		return nil
	}
	if len(*j) == 0 {
		return nil
	}
	keys := make([]string, 0, len(*j))
	for key := range *j {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for _, key := range keys {
		value := (*j)[key]
		if value.S == nil && value.B == nil && value.F == nil {
			continue
		}
		fflib.WriteJsonString(buf, key)
		buf.WriteString(`:`)
		err := value.MarshalJSONBuf(buf)
		if err != nil {
			return err
		}
		buf.WriteByte(',')
	}
	buf.Rewind(1)
	buf.WriteByte('}')
	return nil
}

func NewBoolValue(b bool) Value {
	return Value{B: &b}
}

func NewFloatValue(f float64) Value {
	return Value{F: &f}
}

func (j Value) V() interface{} {
	if j.S != nil {
		return string(j.S)
	}
	if j.B != nil {
		return *j.B
	}
	if j.F != nil {
		return *j.F
	}
	return nil
}

func (j Value) MarshalJSON() ([]byte, error) {
	var buf fflib.Buffer
	err := j.MarshalJSONBuf(&buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (j *Value) MarshalJSONBuf(buf fflib.EncodingBuffer) error {
	if j == nil {
		// TODO
		return nil
	}
	if j.B == nil && j.F == nil && j.S == nil {
		// TODO
	}
	var err error
	var obj []byte
	_ = obj
	_ = err
	if j.S != nil {
		fflib.WriteJson(buf, j.S)
	} else if j.B != nil {
		if *j.B {
			buf.WriteString(`true`)
		} else {
			buf.WriteString(`false`)
		}
	} else if j.F != nil {
		fflib.AppendFloat(buf, float64(*j.F), 'g', -1, 64)
	}
	return nil
}
