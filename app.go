package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/go-redis/redis"
	"github.com/nsqio/go-nsq"
	"github.com/urfave/cli"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var Version string

func listenSignals(cancel context.CancelFunc) {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for range sigchan {
			cancel()
		}
	}()
}

type kafkaProducer struct {
	sarama.AsyncProducer
	closedOnce *sync.Once
}

func (p kafkaProducer) AsyncClose() {
	p.closedOnce.Do(func() { p.AsyncProducer.AsyncClose() })
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
		ctx, cancel := context.WithCancel(context.Background())
		listenSignals(cancel)
		g, lctx := errgroup.WithContext(ctx)
		return action(lctx, g, c, nil, nil, logger)
	}

	app.Commands = []cli.Command{
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
			Name:  "file",
			Usage: "write access logs to file",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:   "filename",
					Usage:  "the file to write to",
					Value:  "/tmp/access.log",
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
			Name:  "redis",
			Usage: "push access logs to a redis list",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:   "addr",
					Usage:  "redis address",
					Value:  "127.0.0.1:6379",
					EnvVar: "REAPER_TO_REDIS_ADDR",
				},
				cli.StringFlag{
					Name:   "listname",
					Usage:  "list to publish in",
					Value:  "external",
					EnvVar: "REAPER_TO_REDIS_LIST",
				},
				cli.IntFlag{
					Name:   "database",
					Usage:  "redis database number",
					Value:  0,
					EnvVar: "REAPER_TO_REDIS_DATABASE",
				},
				cli.StringFlag{
					Name:   "password",
					Usage:  "redis password",
					Value:  "",
					EnvVar: "REAPER_TO_REDIS_PASSWORD",
				},
			},
			Action: func(c *cli.Context) error {
				logger := NewLogger(c.GlobalString("loglevel"))
				ctx, cancel := context.WithCancel(context.Background())
				listenSignals(cancel)
				g, lctx := errgroup.WithContext(ctx)
				redisAddress := c.String("addr")
				listname := c.String("listname")
				password := c.String("password")
				database := c.Int("database")

				client := redis.NewClient(&redis.Options{
					Addr:     redisAddress,
					Password: password,
					DB:       database,
				})

				reconnect := func() error { return client.Ping().Err() }

				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					b, err := json.Marshal(entry)
					if err != nil {
						logger.Warn("Failed to marshal message", "error", err, "uid", entry.UID)
						ack(nil)
						return nil
					}
					err = client.RPush(listname, b).Err()
					if err == nil {
						ack(nil)
					}
					return err
				}

				return action(lctx, g, c, h, reconnect, logger)
			},
		},
		{
			Name:  "kafka",
			Usage: "push access logs to kafka",
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:   "broker",
					Usage:  "kafka broker to connect to (can be specified multiple times)",
					EnvVar: "REAPER_TO_KAFKA_BROKER",
				},
				cli.StringFlag{
					Name:   "topic",
					Usage:  "Kafka topic to publish to",
					Value:  "external",
					EnvVar: "REAPER_TO_KAFKA_TOPIC",
				},
			},
			Action: func(c *cli.Context) error {
				logger := NewLogger(c.GlobalString("loglevel"))
				sarama.Logger = AdaptLoggerSarama(logger)

				ctx, cancel := context.WithCancel(context.Background())
				listenSignals(cancel)
				g, lctx := errgroup.WithContext(ctx)

				config := sarama.NewConfig()
				config.Net.KeepAlive = 30 * time.Second
				config.Producer.RequiredAcks = sarama.WaitForLocal
				config.Producer.Compression = sarama.CompressionGZIP
				config.Producer.Return.Errors = true
				config.Producer.Return.Successes = true
				config.Producer.Retry.Max = 6
				config.ClientID = "reaper_to_kafka"
				config.Version = sarama.V1_0_0_0


				brokers := c.StringSlice("broker")
				if len(brokers) == 0 {
					return cli.NewExitError("No brokers specified", 1)
				}
				topic := c.String("topic")

				var producer atomic.Value

				reconnect := func() error {
					p := producer.Load()
					if p != nil {
						p.(kafkaProducer).AsyncClose()
					}
					p2, err := sarama.NewAsyncProducer(brokers, config)
					if err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					succ := p2.Successes()
					errs := p2.Errors()
					g.Go(func() error {
						for {
							if succ == nil && errs == nil {
								return nil
							}
							select {
							case s, ok := <-succ:
								if !ok {
									succ = nil
								} else {
									s.Metadata.(func(error))(nil)
								}
							case e, ok := <-errs:
								if !ok {
									errs = nil
								} else {
									e.Msg.Metadata.(func(error))(e.Err)
								}
							case <-lctx.Done():
								return lctx.Err()
							}
						}
					})
					producer.Store(kafkaProducer{
						AsyncProducer: p2,
						closedOnce: &sync.Once{},
					})
					return nil
				}

				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					p := producer.Load()
					if p == nil {
						return errors.New("not connected to kafka")
					}
					b, err := json.Marshal(entry)
					if err != nil {
						logger.Warn("Failed to marshal message", "error", err, "uid", entry.UID)
						ack(nil)
						return nil
					}
					msg := &sarama.ProducerMessage{
						Metadata: ack,
						Value:    sarama.ByteEncoder(b),
						Topic:    topic,
					}
					select {
					case <-done:
						return context.Canceled
					case p.(kafkaProducer).Input() <- msg:
						return nil
					}
				}

				return action(lctx, g, c, h, reconnect, logger)
			},
		},
		{
			Name:  "nsq",
			Usage: "push access logs to an external nsq server",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:   "addr",
					Usage:  "TCP address of the external nsqd",
					Value:  "127.0.0.1:14150",
					EnvVar: "REAPER_TO_NSQD_ADDR",
				},
				cli.StringFlag{
					Name:   "topic",
					Usage:  "topic to publish to",
					Value:  "external",
					EnvVar: "REAPER_TO_NSQD_TOPIC",
				},
			},
			Action: func(c *cli.Context) error {
				logger := NewLogger(c.GlobalString("loglevel"))
				ctx, cancel := context.WithCancel(context.Background())
				listenSignals(cancel)
				g, lctx := errgroup.WithContext(ctx)
				tcpAddress := c.String("addr")
				topic := c.String("topic")

				cfg := nsq.NewConfig()
				cfg.ClientID = "reaper_nsq_to_nsq"
				p, err := nsq.NewProducer(tcpAddress, cfg)
				if err != nil {
					logger.Error("failed to create external nsq producer", "error", err.Error())
					return err
				}
				doneChan := make(chan *nsq.ProducerTransaction)
				g.Go(func() error {
					done := doneChan
					for {
						select {
						case <-lctx.Done():
							return lctx.Err()
						case t, ok := <-done:
							if !ok {
								done = nil
							} else {
								t.Args[0].(func(error))(t.Error)
							}
						}
					}
				})
				p.SetLogger(AdaptLoggerNSQD(logger), nsq.LogLevelInfo)
				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					b, err := json.Marshal(entry)
					if err != nil {
						logger.Warn("Failed to marshal message", "error", err, "uid", entry.UID)
						ack(nil)
						return nil
					}
					return p.PublishAsync(topic, b, doneChan, ack)
				}
				reconnect := func() error {
					logger.Info("Connecting to external nsqd")
					return p.Ping()
				}
				return action(lctx, g, c, h, reconnect, logger)
			},
		},
	}

	return app
}

func main() {
	_ = BuildApp().Run(os.Args)
}

func action(ctx context.Context, g *errgroup.Group, c *cli.Context, h Handler, reconnect func() error, logger Logger) error {
	nsqdOpts, err := buildNSQDOptions(c, logger)
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}

	incoming := make(chan *Entry, 10000)
	tcpAddrs := c.GlobalStringSlice("tcp")
	udpAddrs := c.GlobalStringSlice("udp")

	g.Go(func() error {
		return NSQD(ctx, nsqdOpts, incoming, h, reconnect, logger)
	})

	g.Go(func() error {
		return listen(ctx, tcpAddrs, udpAddrs, incoming, logger)
	})

	err = g.Wait()
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}
	return nil
}

func actionWriter(c *cli.Context, w io.Writer) error {
	// TODO: gzip
	logger := NewLogger(c.GlobalString("loglevel"))
	ctx, cancel := context.WithCancel(context.Background())
	listenSignals(cancel)
	g, lctx := errgroup.WithContext(ctx)
	var l sync.Mutex
	bufw := bufio.NewWriter(w)
	//noinspection GoUnhandledErrorResult
	defer bufw.Flush()
	handler := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
		b := entry.JSON()
		if b != nil {
			l.Lock()
			//noinspection GoUnhandledErrorResult
			bufw.Write(b)
			//noinspection GoUnhandledErrorResult
			bufw.WriteByte('\n')
			l.Unlock()
		}
		ack(nil)
		return nil
	}
	return action(lctx, g, c, handler, nil, logger)
}
