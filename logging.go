package main

import (
	"github.com/inconshreveable/log15"
)

type Logger struct {
	log15.Logger
}

func (l Logger) Output(maxdepth int, s string) error {
	// TODO
	l.Logger.Info(s)
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
