package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/dop251/goja"

	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/nsqd"
	"github.com/urfave/cli"
	utomic "go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
)

func buildNSQDOptions(c *cli.Context, l Logger) (*nsqd.Options, error) {
	listenAddr := c.GlobalString("nsqd-address")
	tcpPort := c.GlobalInt("nsqd-tcp-port")
	httpPort := c.GlobalInt("nsqd-http-port")
	opts := nsqd.NewOptions()
	opts.Logger = AdaptLoggerNSQD(l)
	opts.DataPath = c.GlobalString("data-path")
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

	lookupd := c.GlobalStringSlice("lookupd")
	if len(lookupd) > 0 {
		opts.NSQLookupdTCPAddresses = lookupd
	}

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

var nsqdReady = make(chan struct{})

func WaitNSQD(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-nsqdReady:
		return nil
	}
}

func NSQD(ctx context.Context, opts *nsqd.Options, filterOut []string, incoming <-chan []*Entry, h Handler, reconnect func(context.Context) error, maxInFlight int, logger Logger) error {
	if maxInFlight <= 0 {
		maxInFlight = 1000
	}

	daemon := nsqd.New(opts)
	logger.Info("Starting embedded nsqd",
		"tcp", opts.TCPAddress,
		"http", opts.HTTPAddress,
		"datapath", opts.DataPath,
	)
	i, err := os.Stat(opts.DataPath)
	if err != nil {
		return err
	}
	if !i.IsDir() {
		return errors.New("data path is not a directory")
	}
	// test we have rights permissions in data path
	tf, err := ioutil.TempFile(opts.DataPath, "reaper_temp_*")
	if err != nil {
		return err
	}
	_ = tf.Close()
	_ = os.Remove(tf.Name())

	daemon.Main()
	close(nsqdReady)
	Metrics.Registry.MustRegister(NewNSQDCollector(daemon))

	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return pushEntries(opts.TCPAddress, incoming, logger)
	})

	if h != nil {
		g.Go(func() error {
			for {
				err := pullEntries(
					lctx,
					"reaper_puller",
					"reaper_puller",
					opts.TCPAddress,
					h, filterOut,
					-1,
					maxInFlight,
					logger,
				)
				if err == nil || err == context.Canceled {
					return context.Canceled
				}
				logger.Warn("Handler failed", "error", err)
				if reconnect == nil {
					return err
				}

				bo := backoff.NewExponentialBackOff()
				bo.InitialInterval = time.Second
				bo.MaxElapsedTime = 0

			Reconnect:

				for {
					logger.Info("Reconnecting handler")
					to, cancel := context.WithTimeout(lctx, 30*time.Second)
					err = reconnect(to)
					cancel()
					if err == nil {
						logger.Info("Handler reconnected")
						break Reconnect
					}
					pause := bo.NextBackOff()
					logger.Warn("Failed to reconnect handler", "error", err, "backoff", pause.String())
					select {
					case <-lctx.Done():
						return nil
					case <-time.After(pause):
					}
				}
			}
		})
	}
	err = g.Wait()
	<-ctx.Done()
	logger.Info("Stopping NSQD")
	daemon.Exit()
	logger.Info("Stopped NSQD")
	return err
}

type Handler func(context.Context, *Entry, func(error)) error

type handler struct {
	th         Handler
	logger     Logger
	ctx        context.Context
	errs       chan error
	once       sync.Once
	returned   utomic.Int64
	maxEntries int64
	vm         *goja.Runtime
	filters    []*goja.Program
}

func (h *handler) markError(err error) {
	if err == nil {
		return
	}
	h.once.Do(func() {
		if err != ErrPullFinished {
			h.logger.Warn("Failed to handle message", "error", err)
		}

		select {
		case h.errs <- err:
		case <-h.ctx.Done():
		}
		close(h.errs)
	})
}

var ErrPullFinished = errors.New("pull finished")

func (h *handler) HandleMessage(message *nsq.Message) error {
	message.DisableAutoResponse()

	if h.maxEntries != -1 {
		returned := h.returned.Inc()
		if returned == h.maxEntries {
			h.markError(ErrPullFinished)
		} else if returned > h.maxEntries {
			message.Requeue(0)
			h.markError(ErrPullFinished)
			return ErrPullFinished
		}
		h.logger.Debug("debug", "returned", returned)
	}
	if message.Body == nil {
		h.logger.Debug("Empty message from nsqd ?!")
		message.Finish()
		return nil
	}

	entry, err := UnmarshalEntry(message.Body)
	if err != nil {
		h.logger.Warn("Failed to unmarshal entry from nsqd", "error", err)
		message.Finish()
		return nil
	}
	if entry == nil {
		h.logger.Debug("Unmarshal entry is nil ?!")
		message.Finish()
		return nil
	}
	if len(entry.serialized) == 0 {
		h.logger.Debug("Empty message ?!")
		ReleaseEntry(entry)
		message.Finish()
		return nil
	}

	if h.vm != nil {
		out, err := entry.ToVM(h.vm, h.filters)
		if err != nil {
			h.logger.Warn("Error evaluating filterOut function", "error", err)
		} else if out {
			ReleaseEntry(entry)
			message.Finish()
			return nil
		}
	}

	err = h.th(h.ctx, entry, func(e error) {
		ReleaseEntry(entry)
		h.markError(e)
		if e == nil {
			message.Finish()
		} else {
			message.Requeue(-1)
		}
	})
	h.markError(err)
	if err == ErrNotConnected {
		ReleaseEntry(entry)
		message.Requeue(0)
	} else if err != nil {
		ReleaseEntry(entry)
		message.Requeue(-1)
	}
	return err
}

func newHandler(ctx context.Context, h Handler, filterOut []string, l Logger, errs chan error, maxReturned int) *handler {
	h2 := &handler{
		th:         h,
		logger:     l,
		ctx:        ctx,
		errs:       errs,
		maxEntries: int64(maxReturned),
	}
	for _, filter := range filterOut {
		if filter == "" {
			continue
		}
		prg, err := goja.Compile("filterOut", filter, true)
		if err != nil {
			l.Warn("Invalid filter ignored", "filter", filter, "error", err)
			continue
		}
		h2.filters = append(h2.filters, prg)
		if h2.vm == nil {
			h2.vm = goja.New()
		}
	}
	return h2
}

func pullEntries(ctx context.Context, cID, chnl, nsqAddr string, h Handler, filterOut []string, maxReturned, maxInFlight int, l Logger) error {
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
	cfg.MaxRequeueDelay = 15 * time.Minute
	cfg.DefaultRequeueDelay = 90 * time.Second

	c, err := nsq.NewConsumer("embedded", chnl, cfg)
	if err != nil {
		return err
	}
	c.SetLogger(AdaptLoggerNSQD(l), nsq.LogLevelInfo)
	errs := make(chan error)

	c.AddHandler(newHandler(ctx, h, filterOut, l, errs, maxReturned))

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

func pushEntries(tcpAddress string, entriesBatches <-chan []*Entry, logger Logger) error {
	cfg := nsq.NewConfig()
	cfg.ClientID = "reaper_producer"
	p, err := nsq.NewProducer(tcpAddress, cfg)
	if err != nil {
		return err
	}
	p.SetLogger(AdaptLoggerNSQD(logger), nsq.LogLevelInfo)
	err = p.Ping()
	if err != nil {
		return err
	}
	logger.Info("Start publish to nsqd")

	doneChan := make(chan *nsq.ProducerTransaction)

	go func() {
		for d := range doneChan {
			for _, entry := range d.Args[0].([]*Entry) {
				ReleaseEntry(entry)
			}
		}
	}()

	for batch := range entriesBatches {
		if len(batch) == 0 {
			continue
		}
		serialized := make([][]byte, 0, len(batch))
		goodOnes := batch[:0]
		for _, entry := range batch {
			ser, err := entry.MarshalMsg(getBuffer())
			if err == nil {
				entry.serialized = ser
				serialized = append(serialized, ser)
				goodOnes = append(goodOnes, entry)
			} else {
				ReleaseEntry(entry)
				logger.Warn("Failed to message-pack entry", "error", err)
			}
		}

		if len(goodOnes) == 0 {
			continue
		}
		err := p.MultiPublishAsync("embedded", serialized, doneChan, goodOnes)
		if err != nil {
			return err
		}
	}
	p.Stop()
	return nil
}
