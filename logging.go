package main

import (
	"github.com/inconshreveable/log15"
	"strings"
)

type Logger struct {
	log15.Logger
}

func (l Logger) Output(maxDepth int, s string) error {
	// TODO
	if len(s) < 5 {
		l.Logger.Info(s)
		return nil
	}
	prefix := s[:4]
	msg := strings.TrimSpace(s[4:])
	switch prefix {
	case "INF ":
		l.Logger.Info(msg)
	case "WRN ":
		l.Logger.Warn(msg)
	case "ERR ":
		l.Logger.Error(msg)
	default:
		parts := strings.SplitN(s, ":", 2)
		if len(parts) < 2 {
			l.Logger.Info(s)
		} else {
			msg := strings.TrimSpace(parts[1])
			switch strings.TrimSpace(parts[0]) {
			case "DEBUG":
				l.Logger.Debug(msg)
			case "INFO":
				l.Logger.Info(msg)
			case "WARNING":
				l.Logger.Warn(msg)
			case "ERROR":
				l.Logger.Error(msg)
			case "FATAL":
				l.Logger.Crit(msg)
			default:
				l.Logger.Info(s)
			}
		}
	}
	return nil
}

func NewLogger(loglevel string) Logger {
	lvl, _ := log15.LvlFromString(loglevel)
	logger := log15.New()

	logger.SetHandler(
		log15.LvlFilterHandler(
			lvl,
			log15.StderrHandler,
		),
	)
	return Logger{Logger: logger}
}
