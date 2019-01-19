package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/go-redis/redis"
	"github.com/nsqio/go-nsq"
	"github.com/olivere/elastic"
	"github.com/orcaman/concurrent-map"
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
		cli.BoolFlag{
			Name:   "stdin",
			Usage:  "receive logs on stdin",
			EnvVar: "REAPER_STDIN",
		},
		cli.StringFlag{
			Name:   "embedded-nsqd-address",
			Usage:  "bind address for the embedded nsqd",
			EnvVar: "REAPER_EMB_NSQD_ADDR",
			Value:  "127.0.0.1",
		},
		cli.IntFlag{
			Name:   "embedded-nsqd-tcp-port",
			Usage:  "TCP port for the embedded nsqd",
			EnvVar: "REAPER_EMB_NSQD_TCP_PORT",
			Value:  4150,
		},
		cli.IntFlag{
			Name:   "embedded-nsqd-http-port",
			Usage:  "HTTP port for the embedded nsqd",
			EnvVar: "REAPER_EMB_NSQD_HTTP_PORT",
			Value:  4151,
		},
		cli.StringFlag{
			Name:   "embedded-nsqd-data-path",
			Usage:  "data path for the embedded nsqd",
			EnvVar: "REAPER_EMB_NSQD_DATA_PATH",
			Value:  "/tmp/reaper/nsqd",
		},
		cli.StringFlag{
			Name:   "format",
			Usage:  "access log format [json, kv, apache_combined, apache_common, nginx_common]",
			Value:  "json",
			EnvVar: "REAPER_ACCESS_LOG_FORMAT",
		},
		cli.StringFlag{
			Name: "websocket-address",
			Usage: "listen address for the websocket service (eg '127.0.0.1:8080', leave empty to disable)",
			Value: "",
			EnvVar: "REAPER_WEBSOCKET_ADDRESS",
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
				return actionWriter(c, os.Stdout, false, 0)
			},
		},
		{
			Name:  "stderr",
			Usage: "write access logs to stdout",
			Action: func(c *cli.Context) error {
				return actionWriter(c, os.Stderr, false, 0)
			},
		},
		{
			Name: "rabbitmq",
			Usage: "write access logs to rabbitmq",
			Flags: []cli.Flag{
			},
		},
		{
			Name:  "elasticsearch",
			Usage: "write access logs to elaticsearch",
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:   "url",
					Usage:  "Elasticsearch URL (eg. http://127.0.0.1:9200, can be specified multiple times)",
					EnvVar: "REAPER_ELASTICSEARCH_URL",
				},
				cli.StringFlag{
					Name:   "index",
					Usage:  "Elasticsearch index to write access logs to",
					Value:  "reaper",
					EnvVar: "REAPER_ELASTICSEARCH_INDEX",
				},
			},
			Action: func(c *cli.Context) error {
				logger := NewLogger(c.GlobalString("loglevel"))
				ctx, cancel := context.WithCancel(context.Background())
				listenSignals(cancel)
				g, lctx := errgroup.WithContext(ctx)

				urls := c.StringSlice("url")
				if len(urls) == 0 {
					return cli.NewExitError("No Elasticsearch URL provided", 1)
				}
				indexName := c.String("index")
				if indexName == "" {
					return cli.NewExitError("Elasticsearch index name not provided", 1)
				}

				esClient, err := elastic.NewClient(
					elastic.SetURL(urls...),
					elastic.SetSniff(false),
					elastic.SetHealthcheck(false),
					elastic.SetInfoLog(AdaptInfoLoggerElasticsearch(logger)),
					elastic.SetErrorLog(AdaptErrorLoggerElasticsearch(logger)),
				)
				if err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				defer esClient.Stop()

				callbacks := cmap.New()
				doAck := func(uid string, err error) {
					if v, ok := callbacks.Pop(uid); ok {
						v.(func(error))(err)
					}
				}

				var processorRef atomic.Value

				getProcessor := func() *elastic.BulkProcessor {
					p := processorRef.Load()
					if p == nil {
						return nil
					}
					return p.(*elastic.BulkProcessor)
				}

				closeProcessor := func() {
					p := getProcessor()
					if p != nil {
						_ = p.Close()
					}
				}

				defer closeProcessor()

				reconnect := func() error {
					closeProcessor()

					resp, err := esClient.ClusterHealth().Do(lctx)
					if err != nil {
						return err
					}
					if resp.Status != "green" && resp.Status != "yellow" {
						return fmt.Errorf("elasticsearch cluster status is not green/yellow: '%s'", resp.Status)
					}

					after := func(executionId int64, _ []elastic.BulkableRequest, resp *elastic.BulkResponse, err error) {
						if err == nil {
							// all good, ack the current messages
							for _, item := range resp.Succeeded() {
								doAck(item.Id, nil)
							}
							return
						}
						if resp == nil {
							logger.Error("Elasticsearch bulk processor global error", "error", err)
							// nack everything we have
							for kv := range callbacks.IterBuffered() {
								doAck(kv.Key, err)
							}
							return
						}
						for _, item := range resp.Succeeded() {
							doAck(item.Id, nil)
						}

						for _, item := range resp.Failed() {
							if item.Error != nil {
								doAck(item.Id, &elastic.Error{
									Status:  item.Status,
									Details: item.Error,
								})
							}
						}

					}

					processor, err := esClient.BulkProcessor().
						Name("reaper_to_es").
						Workers(1).
						Stats(false).
						BulkActions(400).
						BulkSize(5 * 1024 * 1024).
						FlushInterval(5 * time.Second).
						After(after).
						Do(context.Background())

					if err != nil {
						return err
					}

					processorRef.Store(processor)
					return nil
				}

				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					p := getProcessor()
					if p == nil {
						return errors.New("not connected to Elasticsearch")
					}
					b := entry.JSON()
					if b == nil {
						ack(nil)
						return nil
					}
					uid := entry.UID()

					p.Add(
						elastic.NewBulkIndexRequest().
							Index(indexName).
							Type(indexName).
							Id(uid).
							Doc(json.RawMessage(b)),
					)

					callbacks.Set(uid, ack)
					return nil
				}

				return action(lctx, g, c, h, reconnect, logger)
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
				cli.BoolFlag{
					Name:   "gzip",
					Usage:  "use gzip compression",
					EnvVar: "REAPER_OUT_FILE_GZIP",
				},
				cli.IntFlag{
					Name:   "gziplevel",
					Usage:  "gzip level",
					Value:  6,
					EnvVar: "REAPER_OUT_FILE_GZIP_LEVEL",
				},
			},
			Action: func(c *cli.Context) error {
				f, err := os.OpenFile(c.String("filename"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					return cli.NewExitError(fmt.Sprintf("Failed to open file: %s", err.Error()), 1)
				}
				//noinspection GoUnhandledErrorResult
				defer f.Close()
				return actionWriter(c, f, c.Bool("gzip"), c.Int("gziplevel"))
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
					Value:  "reaper",
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
					b := entry.JSON()
					if b == nil {
						ack(nil)
						return nil
					}
					err := client.RPush(listname, b).Err()
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
					Value:  "reaper",
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
						closedOnce:    &sync.Once{},
					})
					return nil
				}

				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					p := producer.Load()
					if p == nil {
						return errors.New("not connected to kafka")
					}
					b := entry.JSON()
					if b == nil {
						ack(nil)
						return nil
					}
					msg := &sarama.ProducerMessage{
						Metadata: ack,
						Value:    sarama.ByteEncoder(b),
						Topic:    topic,
					}
					host := entry.Host()
					if host != "" {
						msg.Key = sarama.StringEncoder(host)
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
					Value:  "reaper",
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
					b := entry.JSON()
					if b == nil {
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
	format, err := GetFormat(c)
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}

	incoming := make(chan *Entry, 10000)
	tcpAddrs := c.GlobalStringSlice("tcp")
	udpAddrs := c.GlobalStringSlice("udp")
	stdin := c.GlobalBool("stdin")
	websocketAddr := c.GlobalString("websocket-address")

	g.Go(func() error {
		return NSQD(ctx, nsqdOpts, incoming, h, reconnect, logger)
	})

	g.Go(func() error {
		return listen(ctx, tcpAddrs, udpAddrs, stdin, format, incoming, logger)
	})

	if websocketAddr != "" {
		g.Go(func() error {
			return ListenWebsocket(ctx, websocketAddr, nsqdOpts.TCPAddress, logger)
		})
	}

	err = g.Wait()
	if err != nil && err != context.Canceled {
		return cli.NewExitError(err.Error(), 1)
	}
	return nil
}

func actionWriter(c *cli.Context, w io.Writer, gzipEnabled bool, gzipLevel int) error {
	logger := NewLogger(c.GlobalString("loglevel"))
	ctx, cancel := context.WithCancel(context.Background())
	listenSignals(cancel)
	g, lctx := errgroup.WithContext(ctx)
	var l sync.Mutex

	bufw := bufio.NewWriter(w)
	//noinspection GoUnhandledErrorResult
	defer bufw.Flush()
	var writer io.Writer

	if gzipEnabled {
		gzipw, err := gzip.NewWriterLevel(bufw, gzipLevel)
		if err != nil {
			return err
		}
		//noinspection GoUnhandledErrorResult
		defer gzipw.Close()
		writer = gzipw
	} else {
		writer = bufw
	}

	handler := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
		b := entry.JSON()
		if b == nil {
			ack(nil)
			return nil
		}
		b = append(b, '\n')
		l.Lock()
		_, err := writer.Write(b)
		l.Unlock()
		if err == nil {
			ack(nil)
			return nil
		}
		return err
	}
	return action(lctx, g, c, handler, nil, logger)
}
