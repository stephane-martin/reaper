package main

import (
	"context"
	"encoding/json"
	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/nsqd"
	"golang.org/x/sync/errgroup"
	"runtime"
	"sync"
	"time"
)

func NSQD(ctx context.Context, opts *nsqd.Options, incoming chan *Entry, h Handler, reconnect func() error, logger Logger) error {
	daemon := nsqd.New(opts)
	logger.Info("Starting NSQD")
	daemon.Main()

	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return pushEntries(lctx, opts.TCPAddress, incoming, logger)
	})

	if h != nil {
		g.Go(func() error {
			for {
				err := pullEntries(lctx, opts.TCPAddress, h, logger)
				if err == nil || err == context.Canceled {
					return context.Canceled
				}
				logger.Warn("Handler failed", "error", err)
				if reconnect == nil {
					return err
				}
			Reconnect:
				for {
					logger.Info("Reconnecting handler")
					err = reconnect()
					if err == nil {
						logger.Info("Handler reconnected")
						break Reconnect
					}
					logger.Warn("Failed to reconnect handler", "error", err)
					select {
					case <-lctx.Done():
						return lctx.Err()
					case <-time.After(5 * time.Second):
					}
				}
			}
		})
	}
	err := g.Wait()
	logger.Info("Stopping NSQD")
	daemon.Exit()
	logger.Info("Stopped NSQD")
	return err
}

type Handler func(<-chan struct{}, *Entry) error

type handler struct {
	th     Handler
	logger Logger
	done   <-chan struct{}
	errs   chan error
	once   sync.Once
}

func (h *handler) HandleMessage(message *nsq.Message) error {
	var entry Entry
	err := json.Unmarshal(message.Body, &entry)
	if err != nil {
		h.logger.Warn("Failed to unmarshal message from nsqd")
		return nil
	}
	err = h.th(h.done, &entry)
	if err != nil {
		h.once.Do(func() {
			h.logger.Warn("Failed to handle message", "error", err)
			select {
			case h.errs <- err:
				close(h.errs)
			case <-h.done:
				close(h.errs)
			}
		})
	}
	return err
}

func pullEntries(ctx context.Context, tcpAddress string, h Handler, logger Logger) error {
	cfg := nsq.NewConfig()
	cfg.ClientID = "reaper_puller"
	c, err := nsq.NewConsumer("embedded", "reaper_puller", cfg)
	if err != nil {
		return err
	}
	c.SetLogger(logger, nsq.LogLevelInfo)
	errs := make(chan error)

	c.AddConcurrentHandlers(&handler{
		th:     h,
		logger: logger,
		done:   ctx.Done(),
		errs:   errs,
	}, runtime.NumCPU())

	err = c.ConnectToNSQD(tcpAddress)
	if err != nil {
		logger.Warn("Failed to connect to embedded nsqd", "error", err)
		return err
	}
	defer func() {
		c.Stop()
		<-c.StopChan
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err, ok := <-errs:
		if !ok {
			return context.Canceled
		}
		return err
	}
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

	doneChan := make(chan *nsq.ProducerTransaction)

	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		done := doneChan
		for {
			select {
			case <-lctx.Done():
				return lctx.Err()
			case t, ok := <-done:
				if !ok {
					done = nil
				} else if t.Error != nil {
					return t.Error
				}
			}
		}

	})

	g.Go(func() error {
	L:
		for {
			select {
			case <-lctx.Done():
				return lctx.Err()
			case entry, ok := <-entries:
				if !ok {
					entries = nil
					continue L
				}
				b, err := json.Marshal(entry)
				if err != nil {
					logger.Warn("Failed to marshal access log entry", "error", err)
				} else {
					err := p.PublishAsync("embedded", b, doneChan)
					if err != nil {
						return err
					}
				}

			}
		}
	})

	return g.Wait()
}
