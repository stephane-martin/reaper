package main

import (
	"context"
	"encoding/json"
	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/nsqd"
	"golang.org/x/sync/errgroup"
)

func NSQD(ctx context.Context, opts *nsqd.Options, incoming chan *Entry, outcoming chan *Entry, logger Logger) error {
	daemon := nsqd.New(opts)
	logger.Info("Starting NSQD")
	daemon.Main()

	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return pushEntries(lctx, opts.TCPAddress, incoming, logger)
	})

	if outcoming != nil {
		g.Go(func() error {
			return pullEntries(lctx, opts.TCPAddress, outcoming, logger)
		})
	}
	err := g.Wait()
	logger.Info("Stopping NSQD")
	daemon.Exit()
	logger.Info("Stopped NSQD")
	return err
}

type handler struct {
	entries chan *Entry
	logger  Logger
	done    <-chan struct{}
}

func (h handler) HandleMessage(message *nsq.Message) error {
	var entry Entry
	err := json.Unmarshal(message.Body, &entry)
	if err != nil {
		h.logger.Warn("Failed to unmarshal message from nsqd")
		return nil
	}
	select {
	case <-h.done:
		return context.Canceled
	case h.entries <- &entry:
		return nil
	}
}

func pullEntries(ctx context.Context, tcpAddress string, entries chan *Entry, logger Logger) error {
	defer close(entries)
	cfg := nsq.NewConfig()
	cfg.ClientID = "reaper_puller"
	c, err := nsq.NewConsumer("embedded", "reaper_puller", cfg)
	if err != nil {
		return err
	}
	c.SetLogger(logger, nsq.LogLevelInfo)
	c.AddHandler(handler{
		entries: entries,
		logger:  logger,
		done:    ctx.Done(),
	})
	err = c.ConnectToNSQD(tcpAddress)
	if err != nil {
		return err
	}
	<-ctx.Done()
	c.Stop()
	<-c.StopChan
	return ctx.Err()
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
