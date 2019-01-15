package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/nsqio/nsq/nsqd"
	"github.com/urfave/cli"
	"golang.org/x/sync/errgroup"
	"os"
	"os/signal"
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
		return action(c, nil)
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
			Usage: "just write access logs to stdout",
			Action: func(c *cli.Context) error {
				handler := func(done <-chan struct{}, entry *Entry) error {
					fmt.Println(entry.String())
					return nil
				}
				return action(c, handler)
			},
		},
	}

	return app
}

func main() {
	_ = BuildApp().Run(os.Args)
}

func action(c *cli.Context, h Handler) error {
	logger := NewLogger(c.GlobalString("loglevel"))
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
		return NSQD(lctx, nsqdOpts, incoming, h, logger)
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