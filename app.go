package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nsqio/go-nsq"
	"github.com/nsqio/nsq/nsqd"
	"github.com/urfave/cli"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var Version string

func buildNSQDOptions(c *cli.Context, l Logger) (*nsqd.Options, error) {
	opts := nsqd.NewOptions()
	opts.Logger = l
	opts.DataPath = c.GlobalString("embedded-nsqd-data-path")
	opts.TCPAddress = c.GlobalString("embedded-nsqd-tcp-address")
	opts.HTTPAddress = c.GlobalString("embedded-nsqd-http-address")
	opts.SnappyEnabled = true
	opts.DeflateEnabled = true

	i, err := os.Stat(opts.DataPath)
	if err != nil {
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

func listenSignals(cancel context.CancelFunc) {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for range sigchan {
			cancel()
		}
	}()
}

func BuildApp() *cli.App {
	app := cli.NewApp()
	app.Name = "reaper"
	app.Version = Version
	app.Usage = "access logs to queues"
	app.Description = "reaper receives access logs from a web server and pushes the logs to an external message queue"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "loglevel",
			Usage:  "logging level",
			EnvVar: "REAPER_LOGLEVEL",
			Value:  "info",
		},
		cli.StringSliceFlag{
			Name:   "tcp",
			Usage:  "tcp address to listen on (eg. 127.0.0.1:1514)",
			EnvVar: "REAPER_TCP_ADDRESS",
		},
		cli.StringSliceFlag{
			Name:   "udp",
			Usage:  "tcp address to listen on (eg. 127.0.0.1:1514)",
			EnvVar: "REAPER_UDP_ADDRESS",
		},
		cli.StringFlag{
			Name:   "embedded-nsqd-tcp-address",
			Usage:  "TCP listen address for the embedded nsqd",
			EnvVar: "REAPER_EMB_NSQD_TCP_ADDR",
			Value:  "127.0.0.1:4150",
		},
		cli.StringFlag{
			Name:   "embedded-nsqd-http-address",
			Usage:  "HTTP listen address for the embedded nsqd",
			EnvVar: "REAPER_EMB_NSQD_HTTP_ADDR",
			Value:  "127.0.0.1:4151",
		},
		cli.StringFlag{
			Name:   "embedded-nsqd-data-path",
			Usage:  "data path for the embedded nsqd",
			EnvVar: "REAPER_EMB_NSQD_DATA_PATH",
			Value:  "/tmp/reaper/nsqd",
		},
	}

	app.Action = func(c *cli.Context) error {
		logger := NewLogger(c.GlobalString("loglevel"))
		return action(c, nil, nil, logger)
	}

	app.Commands = []cli.Command{
		{
			Name:  "kafka",
			Usage: "push access logs to kafka",
			Action: func(c *cli.Context) error {
				fmt.Println("kafka action")
				return nil
			},
		},
		{
			Name:  "stdout",
			Usage: "write access logs to stdout",
			Action: func(c *cli.Context) error {
				return actionWriter(c, os.Stdout)
			},
		},
		{
			Name:  "stderr",
			Usage: "write access logs to stdout",
			Action: func(c *cli.Context) error {
				return actionWriter(c, os.Stderr)
			},
		},
		{
			Name: "file",
			Usage: "write access logs to file",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name: "filename",
					Usage: "the file to write to",
					Value: "/tmp/access.log",
					EnvVar: "REAPER_OUT_FILE",
				},
			},
			Action: func(c *cli.Context) error {
				f, err := os.OpenFile(c.String("filename"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					return cli.NewExitError(fmt.Sprintf("Failed to open file: %s", err.Error()), 1)
				}
				//noinspection GoUnhandledErrorResult
				defer f.Close()
				return actionWriter(c, f)
			},

		},
		{
			Name: "nsq",
			Usage: "push access logs to an external nsq server",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name: "addr",
					Usage: "TCP address of the external nsqd",
					Value: "127.0.0.1:14150",
					EnvVar: "REAPER_TO_NSQD_ADDR",
				},
				cli.StringFlag{
					Name: "topic",
					Usage: "topic to publish to",
					Value: "external",
					EnvVar: "REAPER_TO_NSQD_TOPIC",
				},
			},
			Action: func(c *cli.Context) error {
				logger := NewLogger(c.GlobalString("loglevel"))
				tcpAddress := c.String("addr")
				topic := c.String("topic")

				cfg := nsq.NewConfig()
				cfg.ClientID = "reaper_nsq_to_nsq"
				p, err := nsq.NewProducer(tcpAddress, cfg)
				if err != nil {
					logger.Error("failed to create external nsq producer", "error", err.Error())
					return err
				}
				p.SetLogger(logger, nsq.LogLevelInfo)
				h := func(done <-chan struct{}, entry *Entry) error {
					b, err := json.Marshal(entry)
					if err == nil {
						return p.Publish(topic, b)
					}
					logger.Warn("Failed to marshal message", "error", err, "uid", entry.UID)
					return nil
				}
				reconnect := func() error {
					logger.Info("Connecting to external nsqd")
					return p.Ping()
				}
				return action(c, h, reconnect, logger)
			},
		},
	}

	return app
}

func main() {
	_ = BuildApp().Run(os.Args)
}

func action(c *cli.Context, h Handler, reconnect func() error, logger Logger) error {
	nsqdOpts, err := buildNSQDOptions(c, logger)
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	listenSignals(cancel)
	g, lctx := errgroup.WithContext(ctx)
	incoming := make(chan *Entry, 10000)
	tcpAddrs := c.GlobalStringSlice("tcp")
	udpAddrs := c.GlobalStringSlice("udp")

	g.Go(func() error {
		return NSQD(lctx, nsqdOpts, incoming, h, reconnect, logger)
	})

	g.Go(func() error {
		return listen(lctx, tcpAddrs, udpAddrs, incoming, logger)
	})

	err = g.Wait()
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}
	return nil
}

func actionWriter(c *cli.Context, w io.Writer) error {
	logger := NewLogger(c.GlobalString("loglevel"))
	var l sync.Mutex
	bufw := bufio.NewWriter(w)
	//noinspection GoUnhandledErrorResult
	defer bufw.Flush()
	handler := func(done <-chan struct{}, entry *Entry) error {
		l.Lock()
		b := entry.JSON()
		if b != nil {
			//noinspection GoUnhandledErrorResult
			bufw.Write(b)
			//noinspection GoUnhandledErrorResult
			bufw.WriteByte('\n')
		}
		l.Unlock()
		return nil
	}
	return action(c, handler, nil, logger)
}