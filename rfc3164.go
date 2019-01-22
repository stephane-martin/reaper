package main

import (
	"bytes"
	"errors"
	"strconv"
	"time"
	uni "unicode"
)

var space = []byte(" ")

// <PRI>Mmm dd hh:mm:ss HOSTNAME TAG MSG
// or <PRI>Mmm dd hh:mm:ss TAG MSG
// or <PRI>RFC3339 HOSTNAME TAG MSG
// or <PRI>RFC3339 TAG MSG

// TAG may be "TAG" or "TAG:" or "TAG[PID]" or "TAG[PID]:"
// HOSTNAME can be IPv4 (multiple "."), IPv6 (multiple ":"), or a hostname without domain.

// if <PRI> is not present, we assume it is just MSG

func pair2str(s1 []byte, s2 []byte) (string, string) {
	return string(s1), string(s2)
}

type RFC5424Message struct {
	Priority int
	Facility int
	Severity int
	Time *time.Time
	HostName string
	AppName string
	ProcID string
	Message string
}

var (
	NoPriorityError = errors.New("message does not have a priority field")
	PriorityNotIntError = errors.New("the priority field is not an integer")
	EmptyMessageError = errors.New("empty syslog message")
	BadTimestampError = errors.New("invalid timestamp format")
)

func p3164(m []byte) (*RFC5424Message, error) {
	m = bytes.TrimSpace(m)

	smsg := &RFC5424Message{}

	if !bytes.HasPrefix(m, []byte("<")) {
		return nil, NoPriorityError
	}
	priEnd := bytes.Index(m, []byte(">"))
	if priEnd <= 1 {
		return nil, NoPriorityError
	}
	priStr := m[1:priEnd]
	priNum, err := strconv.Atoi(string(priStr))
	if err != nil {
		return nil, PriorityNotIntError
	}
	smsg.Priority = priNum
	smsg.Facility = priNum / 8
	smsg.Severity = priNum % 8

	if len(m) <= (priEnd + 1) {
		return nil, EmptyMessageError
	}
	m = bytes.TrimSpace(m[priEnd+1:])
	if len(m) == 0 {
		return nil, EmptyMessageError
	}

	s := bytes.Split(m, space)
	if m[0] >= byte('0') && m[0] <= byte('9') {
		// RFC3339
		s0 := string(s[0])
		t1, e := time.Parse(time.RFC3339Nano, s0)
		if e != nil {
			t2, e := time.Parse(time.RFC3339, s0)
			if e != nil {
				return nil, BadTimestampError
			}
			smsg.Time = &t2
		} else {
			smsg.Time = &t1
		}
		if len(s) == 1 {
			return nil, EmptyMessageError
		}
		s = s[1:]
	} else {
		// old unix timestamp
		if len(s) < 3 {
			return nil, BadTimestampError
		}
		t, e := time.ParseInLocation(time.Stamp, string(bytes.Join(s[0:3], space)), time.Local)
		if e != nil {
			return nil, BadTimestampError
		}
		t = t.AddDate(time.Now().Year(), 0, 0)

		smsg.Time = &t
		if len(s) == 3 {
			return smsg, nil
		}
		s = s[3:]
	}

	if len(s) == 1 {
		return nil, EmptyMessageError
	}

	if len(s) == 2 {
		// we either have HOSTNAME/MESSAGE or TAG/MESSAGE or HOSTNAME/TAG
		if bytes.Count(s[0], []byte(":")) == 7 || bytes.Count(s[0], []byte(".")) == 3 {
			// looks like an IPv6/IPv4 address
			smsg.HostName = string(s[0])
			if bytes.ContainsAny(s[1], "[]:") {
				smsg.AppName, smsg.ProcID = pair2str(parseTag(s[1]))
			} else {
				smsg.Message = string(s[1])
			}
			return smsg, nil
		}
		if bytes.ContainsAny(s[0], "[]:") {
			smsg.AppName, smsg.ProcID = pair2str(parseTag(s[0]))
			smsg.Message = string(s[1])
			return smsg, nil
		}
		if bytes.ContainsAny(s[1], "[]:") {
			smsg.HostName = string(s[0])
			smsg.AppName, smsg.ProcID = pair2str(parseTag(s[0]))
			return smsg, nil
		}
		smsg.AppName = string(s[0])
		smsg.Message = string(s[1])
		return smsg, nil
	}

	if bytes.ContainsAny(s[0], "[]:") || !isHostname(s[0]) {
		// hostname is omitted
		smsg.AppName, smsg.ProcID = pair2str(parseTag(s[0]))
		smsg.Message = string(bytes.Join(s[1:], space))
		return smsg, nil
	}
	smsg.HostName = string(s[0])
	smsg.AppName, smsg.ProcID = pair2str(parseTag(s[1]))
	smsg.Message = string(bytes.Join(s[2:], space))
	return smsg, nil
}

func parseTag(tag []byte) (appname []byte, procid []byte) {
	tag = bytes.Trim(tag, ":")
	i := bytes.Index(tag, []byte("["))
	if i >= 0 && len(tag) > (i+1) {
		j := bytes.Index(tag, []byte("]"))
		if j > i {
			procid = tag[(i + 1):j]
		} else {
			procid = tag[(i + 1):]
		}
		if i > 0 {
			appname = tag[0:i]
		}
	} else {
		appname = tag
	}
	return
}

func isHostname(s []byte) bool {
	for _, r := range string(s) {
		if (!uni.IsLetter(r)) && (!uni.IsNumber(r)) && r != '.' && r != ':' && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

