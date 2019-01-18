package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/nsqd"
	"github.com/urfave/cli"
	"golang.org/x/sync/errgroup"
)

func buildNSQDOptions(c *cli.Context, l Logger) (*nsqd.Options, error) {
	listenAddr := c.GlobalString("embedded-nsqd-address")
	tcpPort := c.GlobalInt("embedded-nsqd-tcp-port")
	httpPort := c.GlobalInt("embedded-nsqd-http-port")
	opts := nsqd.NewOptions()
	opts.Logger = AdaptLoggerNSQD(l)
	opts.DataPath = c.GlobalString("embedded-nsqd-data-path")
	opts.BroadcastAddress = listenAddr
	opts.TCPAddress = net.JoinHostPort(listenAddr, fmt.Sprintf("%d", tcpPort))
	opts.HTTPAddress = net.JoinHostPort(listenAddr, fmt.Sprintf("%d", httpPort))
	opts.MaxDeflateLevel = 9
	opts.MaxBodySize = 5242880
	opts.MaxMsgSize = 1048576
	opts.MaxMsgTimeout = 15 * time.Minute
	opts.MsgTimeout = time.Minute
	opts.MaxRdyCount = 2500
	opts.MemQueueSize = 10000
	opts.StatsdMemStats = false
	opts.StatsdPrefix = "nsqd.embedded.%s"
	l.Info("Starting embedded nsqd",
		"addr", listenAddr,
		"tcp", opts.TCPAddress,
		"http", opts.HTTPAddress,
		"datapath", opts.DataPath,
	)
	opts.SnappyEnabled = true
	opts.DeflateEnabled = true

	i, err := os.Stat(opts.DataPath)
	if err != nil && os.IsNotExist(err) {
		err = os.MkdirAll(opts.DataPath, 0755)
		if err != nil {
			return nil, err
		}
		i, err = os.Stat(opts.DataPath)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if !i.IsDir() {
		return nil, errors.New("data path is not a directory")
	}
	f, err := os.Open(opts.DataPath)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return opts, nil
}

func NSQD(ctx context.Context, opts *nsqd.Options, incoming chan Entry, h Handler, reconnect func() error, logger Logger) error {
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

type Handler func(<-chan struct{}, *Entry, func(error)) error

type handler struct {
	th     Handler
	logger Logger
	done   <-chan struct{}
	errs   chan error
	once   sync.Once
}

func (h *handler) markError(err error) {
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

func (h *handler) HandleMessage(message *nsq.Message) error {
	message.DisableAutoResponse()
	var entry Entry
	err := json.Unmarshal(message.Body, &entry)
	if err != nil {
		h.logger.Warn("Failed to unmarshal message from nsqd", "error", err)
		message.Finish()
		return nil
	}
	err = h.th(h.done, &entry, func(e error) {
		if e == nil {
			message.Finish()
		} else {
			message.Requeue(-1)
			h.markError(e)
		}
	})
	if err != nil {
		message.Requeue(-1)
		h.markError(err)
	}
	return err
}

func pullEntries(ctx context.Context, tcpAddress string, h Handler, logger Logger) error {
	cfg := nsq.NewConfig()
	cfg.ClientID = "reaper_puller"
	cfg.MaxInFlight = 1000
	cfg.MaxAttempts = 0
	cfg.Snappy = true
	cfg.MaxRequeueDelay = 15 * time.Minute
	cfg.DefaultRequeueDelay = 90 * time.Second

	c, err := nsq.NewConsumer("embedded", "reaper_puller", cfg)
	if err != nil {
		return err
	}
	c.SetLogger(AdaptLoggerNSQD(logger), nsq.LogLevelInfo)
	errs := make(chan error)

	c.AddHandler(&handler{
		th:     h,
		logger: logger,
		done:   ctx.Done(),
		errs:   errs,
	})

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

func pushEntries(ctx context.Context, tcpAddress string, entries chan Entry, logger Logger) error {
	cfg := nsq.NewConfig()
	cfg.ClientID = "reaper_producer"
	cfg.Snappy = true
	p, err := nsq.NewProducer(tcpAddress, cfg)
	if err != nil {
		return err
	}
	p.SetLogger(AdaptLoggerNSQD(logger), nsq.LogLevelInfo)
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
				b := entry.JSON()
				if b != nil {
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
