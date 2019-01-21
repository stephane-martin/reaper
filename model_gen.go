package main

// Code generated by github.com/tinylib/msgp DO NOT EDIT.

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *Entry) DecodeMsg(dc *msgp.Reader) (err error) {
	var field []byte
	_ = field
	var zb0001 uint32
	zb0001, err = dc.ReadMapHeader()
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	for zb0001 > 0 {
		zb0001--
		field, err = dc.ReadMapKeyPtr()
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		switch msgp.UnsafeString(field) {
		case "uid":
			z.UID, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "UID")
				return
			}
		case "host":
			z.Host, err = dc.ReadString()
			if err != nil {
				err = msgp.WrapError(err, "Host")
				return
			}
		case "fields":
			var zb0002 uint32
			zb0002, err = dc.ReadMapHeader()
			if err != nil {
				err = msgp.WrapError(err, "Fields")
				return
			}
			if z.Fields == nil {
				z.Fields = make(map[string]interface{}, zb0002)
			} else if len(z.Fields) > 0 {
				for key := range z.Fields {
					delete(z.Fields, key)
				}
			}
			for zb0002 > 0 {
				zb0002--
				var za0001 string
				var za0002 interface{}
				za0001, err = dc.ReadString()
				if err != nil {
					err = msgp.WrapError(err, "Fields")
					return
				}
				za0002, err = dc.ReadIntf()
				if err != nil {
					err = msgp.WrapError(err, "Fields", za0001)
					return
				}
				z.Fields[za0001] = za0002
			}
		default:
			err = dc.Skip()
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z *Entry) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 3
	// write "uid"
	err = en.Append(0x83, 0xa3, 0x75, 0x69, 0x64)
	if err != nil {
		return
	}
	err = en.WriteString(z.UID)
	if err != nil {
		err = msgp.WrapError(err, "UID")
		return
	}
	// write "host"
	err = en.Append(0xa4, 0x68, 0x6f, 0x73, 0x74)
	if err != nil {
		return
	}
	err = en.WriteString(z.Host)
	if err != nil {
		err = msgp.WrapError(err, "Host")
		return
	}
	// write "fields"
	err = en.Append(0xa6, 0x66, 0x69, 0x65, 0x6c, 0x64, 0x73)
	if err != nil {
		return
	}
	err = en.WriteMapHeader(uint32(len(z.Fields)))
	if err != nil {
		err = msgp.WrapError(err, "Fields")
		return
	}
	for za0001, za0002 := range z.Fields {
		err = en.WriteString(za0001)
		if err != nil {
			err = msgp.WrapError(err, "Fields")
			return
		}
		err = en.WriteIntf(za0002)
		if err != nil {
			err = msgp.WrapError(err, "Fields", za0001)
			return
		}
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z *Entry) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 3
	// string "uid"
	o = append(o, 0x83, 0xa3, 0x75, 0x69, 0x64)
	o = msgp.AppendString(o, z.UID)
	// string "host"
	o = append(o, 0xa4, 0x68, 0x6f, 0x73, 0x74)
	o = msgp.AppendString(o, z.Host)
	// string "fields"
	o = append(o, 0xa6, 0x66, 0x69, 0x65, 0x6c, 0x64, 0x73)
	o = msgp.AppendMapHeader(o, uint32(len(z.Fields)))
	for za0001, za0002 := range z.Fields {
		o = msgp.AppendString(o, za0001)
		o, err = msgp.AppendIntf(o, za0002)
		if err != nil {
			err = msgp.WrapError(err, "Fields", za0001)
			return
		}
	}
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *Entry) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var zb0001 uint32
	zb0001, bts, err = msgp.ReadMapHeaderBytes(bts)
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	for zb0001 > 0 {
		zb0001--
		field, bts, err = msgp.ReadMapKeyZC(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		switch msgp.UnsafeString(field) {
		case "uid":
			z.UID, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "UID")
				return
			}
		case "host":
			z.Host, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Host")
				return
			}
		case "fields":
			var zb0002 uint32
			zb0002, bts, err = msgp.ReadMapHeaderBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "Fields")
				return
			}
			if z.Fields == nil {
				z.Fields = make(map[string]interface{}, zb0002)
			} else if len(z.Fields) > 0 {
				for key := range z.Fields {
					delete(z.Fields, key)
				}
			}
			for zb0002 > 0 {
				var za0001 string
				var za0002 interface{}
				zb0002--
				za0001, bts, err = msgp.ReadStringBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Fields")
					return
				}
				za0002, bts, err = msgp.ReadIntfBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Fields", za0001)
					return
				}
				z.Fields[za0001] = za0002
			}
		default:
			bts, err = msgp.Skip(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
		}
	}
	o = bts
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *Entry) Msgsize() (s int) {
	s = 1 + 4 + msgp.StringPrefixSize + len(z.UID) + 5 + msgp.StringPrefixSize + len(z.Host) + 7 + msgp.MapHeaderSize
	if z.Fields != nil {
		for za0001, za0002 := range z.Fields {
			_ = za0002
			s += msgp.StringPrefixSize + len(za0001) + msgp.GuessSize(za0002)
		}
	}
	return
}