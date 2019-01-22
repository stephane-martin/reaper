package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/nsqd"
	"github.com/urfave/cli"
	utomic "go.uber.org/atomic"
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

var nsqdReadyCtx context.Context
var nsqdReady context.CancelFunc

func init() {
	nsqdReadyCtx, nsqdReady = context.WithCancel(context.Background())
}

func WaitNSQD(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-nsqdReadyCtx.Done():
		return nil
	}
}

func NSQD(ctx context.Context, opts *nsqd.Options, incoming <-chan *Entry, h Handler, reconnect func() error, maxInFlight int, logger Logger) error {
	if maxInFlight <= 0 {
		maxInFlight = 1000
	}

	daemon := nsqd.New(opts)
	logger.Info("Starting embedded nsqd",
		"tcp", opts.TCPAddress,
		"http", opts.HTTPAddress,
		"datapath", opts.DataPath,
	)
	daemon.Main()
	nsqdReady()
	Metrics.Registry.MustRegister(NewNSQDCollector(daemon))

	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return pushEntries(lctx, opts.TCPAddress, incoming, logger)
	})

	if h != nil {
		g.Go(func() error {
			for {
				err := pullEntries(lctx, "reaper_puller", "reaper_puller", opts.TCPAddress, h, -1, maxInFlight, logger)
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
	th         Handler
	logger     Logger
	done       <-chan struct{}
	errs       chan error
	once       sync.Once
	returned   utomic.Int64
	maxEntries int64
}

func (h *handler) markError(err error) {
	if err == nil {
		return
	}
	h.once.Do(func() {
		if err != PullFinished {
			h.logger.Warn("Failed to handle message", "error", err)
		}

		select {
		case h.errs <- err:
			close(h.errs)
		case <-h.done:
			close(h.errs)
		}
	})
}

var PullFinished = errors.New("pull finished")

func (h *handler) HandleMessage(message *nsq.Message) error {
	message.DisableAutoResponse()

	if h.maxEntries != -1 {
		returned := h.returned.Inc()
		if returned == h.maxEntries {
			h.markError(PullFinished)
		} else if returned > h.maxEntries {
			message.Requeue(0)
			h.markError(PullFinished)
			return PullFinished
		}
		h.logger.Debug("debug", "returned", returned)
	}

	entry, err := UnmarshalEntry(message.Body)
	if err != nil {
		h.logger.Warn("Failed to messagepack-unmarshal entry from nsqd", "error", err)
		message.Finish()
		return nil
	}
	err = h.th(h.done, entry, func(e error) {
		h.markError(e)
		if e == nil {
			message.Finish()
		} else {
			message.Requeue(-1)
		}
	})
	h.markError(err)
	if err == NotConnectedError {
		message.Requeue(0)
	} else if err != nil {
		message.Requeue(-1)
	}
	return err
}

func pullEntries(ctx context.Context, cID, chnl, nsqAddr string, h Handler, maxReturned, maxInFlight int, l Logger) error {
	if maxInFlight <= 0 {
		maxInFlight = 1000
	}
	if maxReturned <= 0 {
		maxReturned = -1
	}
	cfg := nsq.NewConfig()
	cfg.ClientID = cID
	cfg.MaxInFlight = maxInFlight
	cfg.MaxAttempts = 0
	cfg.Snappy = true
	cfg.MaxRequeueDelay = 15 * time.Minute
	cfg.DefaultRequeueDelay = 90 * time.Second

	c, err := nsq.NewConsumer("embedded", chnl, cfg)
	if err != nil {
		return err
	}
	c.SetLogger(AdaptLoggerNSQD(l), nsq.LogLevelInfo)
	errs := make(chan error)

	c.AddHandler(&handler{
		th:     h,
		logger: l,
		done:   ctx.Done(),
		errs:   errs,
		maxEntries: int64(maxReturned),
	})

	err = c.ConnectToNSQD(nsqAddr)
	if err != nil {
		l.Warn("Failed to connect to embedded nsqd", "error", err)
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

func pushEntries(ctx context.Context, tcpAddress string, entries <-chan *Entry, logger Logger) error {
	cfg := nsq.NewConfig()
	cfg.ClientID = "reaper_producer"
	cfg.Snappy = true
	p, err := nsq.NewProducer(tcpAddress, cfg)
	if err != nil {
		return err
	}
	p.SetLogger(AdaptLoggerNSQD(logger), nsq.LogLevelInfo)
	logger.Info("Ping nsqd")
	err = p.Ping()
	if err != nil {
		return err
	}
	logger.Info("Pong nsqd")
	defer p.Stop()
	logger.Info("Start publish to nsqd")

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
				b, err := MarshalEntry(entry)
				if err != nil {
					logger.Warn("Failed to messagepack-encode entry", "error", err)
				} else if b != nil {
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
