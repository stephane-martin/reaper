package main

import (
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/inconshreveable/log15"
	"github.com/nsqio/nsq/nsqd"
	"strings"
)

type Logger struct {
	log15.Logger
}

type adaptedNSQD struct {
	Logger
}

type adaptedSarama struct {
	Logger
}

func (l adaptedSarama) Print(v ...interface{}) {
	l.Logger.Info("[kafka] " + fmt.Sprint(v...))
}

func (l adaptedSarama) Printf(format string, v ...interface{}) {
	l.Logger.Info("[kafka] " + fmt.Sprintf(format, v...))
}

func (l adaptedSarama) Println(v ...interface{}) {
	l.Logger.Info("[kafka] " + fmt.Sprint(v...))
}

func AdaptLoggerNSQD(l Logger) nsqd.Logger {
	return adaptedNSQD{Logger: l}
}

func AdaptLoggerSarama(l Logger) sarama.StdLogger {
	return adaptedSarama{Logger: l}
}

func (l adaptedNSQD) Output(maxDepth int, s string) error {
	if len(s) < 5 {
		l.Logger.Info(s)
		return nil
	}
	prefix := s[:4]
	msg := "[nsq] " + strings.TrimSpace(s[4:])
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
			msg := "[nsqd] " + strings.TrimSpace(parts[1])
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
