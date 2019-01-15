package main

import (
	"context"
	"encoding/json"
	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/nsqd"
)

func NSQD(ctx context.Context, opts *nsqd.Options, entries chan *Entry, logger Logger) error {
	daemon := nsqd.New(opts)
	logger.Info("Starting NSQD")
	daemon.Main()

	err := pushEntries(ctx, opts.TCPAddress, entries, logger)
	logger.Info("Stopping NSQD")
	daemon.Exit()
	logger.Info("Stopped NSQD")
	return err
}

func pushEntries(ctx context.Context, tcpAddress string, entries chan *Entry, logger Logger) error {
	cfg := nsq.NewConfig()
	cfg.ClientID = "reaper_producer"
	p, err := nsq.NewProducer(tcpAddress, cfg)
	if err != nil {
		return err
	}
	p.SetLogger(logger, nsq.LogLevelInfo)
	logger.Info("Ping embedded NSQD")
	err = p.Ping()
	if err != nil {
		return err
	}
	logger.Info("Ping embedded NSQD OK")
	defer p.Stop()
	logger.Info("Start publish messages to embedded NSQD")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry, ok := <-entries:
			if !ok {
				entries = nil
			} else {
				b, err := json.Marshal(entry)
				if err == nil {
					err = p.Publish("embedded", b)
					if err != nil {
						return err
					}
				}
			}
		}
	}
}
