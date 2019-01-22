package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	"github.com/nsqio/go-nsq"
	"github.com/olivere/elastic"
	"github.com/orcaman/concurrent-map"
	"github.com/streadway/amqp"
	"github.com/urfave/cli"
	utomic "go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

type KafkaProducer struct {
	sarama.AsyncProducer
	closedOnce *sync.Once
}

func (p KafkaProducer) AsyncClose() {
	p.closedOnce.Do(func() { p.AsyncProducer.AsyncClose() })
}

type RabbitMQChannel struct {
	Channel   *amqp.Channel
	Callbacks cmap.ConcurrentMap
	Current   utomic.Uint64
}

type ElasticProcessor struct {
	Processor *elastic.BulkProcessor
	Callbacks cmap.ConcurrentMap
}

var NotConnectedError = errors.New("not connected to destination")

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
			Usage:  "listen to syslog/TCP on that address (eg. 127.0.0.1:1514, can be specified multiple times)",
			EnvVar: "REAPER_TCP_ADDRESS",
		},
		cli.StringSliceFlag{
			Name:   "udp",
			Usage:  "listen to syslog/UDP on that address (eg. 127.0.0.1:1514, can be specified multiple times)",
			EnvVar: "REAPER_UDP_ADDRESS",
		},
		cli.BoolFlag{
			Name:   "rfc5424",
			Usage:  "when receiving with syslog, use RFC5424 format",
			EnvVar: "REAPER_RFC5424",
		},
		cli.BoolFlag{
			Name:   "stdin",
			Usage:  "receive raw access logs on stdin (useful for piping access logs from Apache)",
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
			Usage:  "data path for the embedded nsqd (change to a non-volatile location)",
			EnvVar: "REAPER_EMB_NSQD_DATA_PATH",
			Value:  "/tmp/reaper/nsqd",
		},
		cli.StringFlag{
			Name:   "format",
			Usage:  "access log format [json, kv, combined, common]",
			Value:  "json",
			EnvVar: "REAPER_ACCESS_LOG_FORMAT",
		},
		cli.StringFlag{
			Name:   "websocket-address",
			Usage:  "listen address for the websocket service (eg '127.0.0.1:8080', leave empty to disable)",
			Value:  "",
			EnvVar: "REAPER_WEBSOCKET_ADDRESS",
		},
		cli.StringFlag{
			Name:   "http-address",
			Usage:  "listen address for the websocket service (eg '127.0.0.1:8080', leave empty to disable)",
			Value:  "",
			EnvVar: "REAPER_HTTP_ADDRESS",
		},
		cli.IntFlag{
			Name:   "max-inflight",
			Usage:  "maximum number of concurrent messages that will be sent downstream to destinations",
			Value:  1000,
			EnvVar: "REAPER_MAX_INFLIGHT",
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
			Usage: "write access logs to stderr",
			Action: func(c *cli.Context) error {
				return actionWriter(c, os.Stderr, false, 0)
			},
		},
		{
			Name:  "rabbitmq",
			Usage: "write access logs to rabbitmq",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:   "uri",
					Usage:  "rabbitmq connection uri",
					Value:  "amqp://guest:guest@localhost:5672/",
					EnvVar: "REAPER_RABBITMQ_URI",
				},
				cli.StringFlag{
					Name:   "exchange",
					Usage:  "rabbitmq exchange to publish to",
					Value:  "",
					EnvVar: "REAPER_RABBITMQ_EXCHANGE",
				},
				cli.StringFlag{
					Name:   "type",
					Usage:  "rabbitmq exchange type",
					Value:  "direct",
					EnvVar: "REAPER_RABBITMQ_EXCHANGE_TYPE",
				},
				cli.StringFlag{
					Name:  "routing-key",
					Usage: "rabbitmq routing key",
					Value: "reaper",
				},
			},
			Action: func(c *cli.Context) error {
				logger := NewLogger(c.GlobalString("loglevel"))
				ctx, cancel := context.WithCancel(context.Background())
				listenSignals(cancel)
				g, lctx := errgroup.WithContext(ctx)

				uri := c.String("uri")
				exchangeName := c.String("exchange")
				exchangeType := c.String("type")
				routingKey := c.String("routing-key")
				maxInFlight := c.GlobalInt("max-inflight")
				if maxInFlight <= 0 {
					maxInFlight = 1000
				}

				var channelRef atomic.Value

				getChannel := func() *RabbitMQChannel {
					p := channelRef.Load()
					if p == nil {
						return nil
					}
					return p.(*RabbitMQChannel)
				}

				closeChannel := func() {
					p := getChannel()
					if p != nil {
						_ = p.Channel.Close()
					}
				}

				defer closeChannel()

				reconnect := func() error {
					closeChannel()

					conn, err := amqp.Dial(uri)
					if err != nil {
						return err
					}

					channel, err := conn.Channel()
					if err != nil {
						return err
					}

					if exchangeName != "" {
						err := channel.ExchangeDeclare(
							exchangeName,
							exchangeType,
							true,
							false,
							false,
							false,
							nil,
						)
						if err != nil {
							return err
						}
					}

					err = channel.Confirm(false)
					if err != nil {
						return err
					}

					callbacks := cmap.New()
					confirmations := make(chan amqp.Confirmation, maxInFlight+1)
					channel.NotifyPublish(confirmations)

					g.Go(func() error {
						for {
							select {
							case confirm, ok := <-confirmations:
								if !ok {
									return nil
								}
								ack, ok := callbacks.Pop(strconv.FormatUint(confirm.DeliveryTag, 10))
								if !ok {
									return fmt.Errorf("can't find callback for rabbitmq delivery tag: %d", confirm.DeliveryTag)
								} else {
									//logger.Debug("RabbitMQ confirmation", "tag", confirm.DeliveryTag)
									if confirm.Ack {
										ack.(func(error))(nil)
									} else {
										ack.(func(error))(fmt.Errorf("delivery to rabbitmq failed for tag: %d", confirm.DeliveryTag))
									}
								}
							case <-lctx.Done():
								return nil
							}
						}
					})

					closes := make(chan *amqp.Error, 1)
					channel.NotifyClose(closes)

					g.Go(func() error {
						for {
							select {
							case cl, ok := <-closes:
								if !ok {
									return nil
								}
								logger.Info("RabbitMQ broker notified closing", "error", cl.Error())
								return nil
							case <-lctx.Done():
								return nil
							}
						}
					})

					channelRef.Store(&RabbitMQChannel{
						Channel:   channel,
						Callbacks: callbacks,
					})
					return nil
				}

				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					ch := getChannel()
					if ch == nil {
						return NotConnectedError
					}
					b, err := JMarshalEntry(entry)
					if err != nil {
						return err
					}
					if b == nil {
						ack(nil)
						return nil
					}

					currentTag := strconv.FormatUint(ch.Current.Inc(), 10)
					ch.Callbacks.Set(currentTag, ack)

					msg := amqp.Publishing{
						ContentType:     "application/json",
						ContentEncoding: "utf-8",
						DeliveryMode:    amqp.Transient,
						MessageId:       entry.UID,
						Timestamp:       time.Now(),
						Type:            "accesslog",
						AppId:           "reaper",
						Body:            b,
					}

					//logger.Debug("Push to rabbitmq", "uid", entry.UID, "tag", currentTag)

					return ch.Channel.Publish(
						exchangeName,
						routingKey,
						false,
						false,
						msg,
					)
				}

				return action(lctx, g, c, h, reconnect, logger)

			},
		},
		{
			Name:  "elasticsearch",
			Usage: "write access logs to Elasticsearch",
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

				var processorRef atomic.Value

				getProcessor := func() *ElasticProcessor {
					p := processorRef.Load()
					if p == nil {
						return nil
					}
					return p.(*ElasticProcessor)
				}

				closeProcessor := func() {
					p := getProcessor()
					if p != nil {
						_ = p.Processor.Close()
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

					callbacks := cmap.New()
					doAck := func(uid string, err error) {
						v, ok := callbacks.Pop(uid)
						if !ok {
							logger.Error("Can't find callback for Elasticsearch entry", "uid", uid)
						} else {
							v.(func(error))(err)
						}
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

					processorRef.Store(&ElasticProcessor{
						Processor: processor,
						Callbacks: callbacks,
					})
					return nil
				}

				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					p := getProcessor()
					if p == nil {
						return NotConnectedError
					}
					b, err := JMarshalEntry(entry)
					if err != nil {
						return err
					}
					if b == nil {
						ack(nil)
						return nil
					}

					p.Callbacks.Set(entry.UID, ack)

					p.Processor.Add(
						elastic.NewBulkIndexRequest().
							Index(indexName).
							Type(indexName).
							Id(entry.UID).
							Doc(json.RawMessage(b)),
					)

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
					b, err := JMarshalEntry(entry)
					if err != nil {
						return err
					}
					if b == nil {
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
						p.(KafkaProducer).AsyncClose()
					}
					p2, err := sarama.NewAsyncProducer(brokers, config)
					if err != nil {
						return err
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
					producer.Store(KafkaProducer{
						AsyncProducer: p2,
						closedOnce:    &sync.Once{},
					})
					return nil
				}

				h := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
					p := producer.Load()
					if p == nil {
						return NotConnectedError
					}
					b, err := JMarshalEntry(entry)
					if err != nil {
						return err
					}
					if b == nil {
						ack(nil)
						return nil
					}
					msg := &sarama.ProducerMessage{
						Metadata: ack,
						Value:    sarama.ByteEncoder(b),
						Topic:    topic,
					}
					if entry.Host != "" {
						msg.Key = sarama.StringEncoder(entry.Host)
					}
					select {
					case <-done:
						return context.Canceled
					case p.(KafkaProducer).Input() <- msg:
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
				cli.BoolFlag{
					Name:   "json",
					Usage:  "publish messages in JSON format",
					EnvVar: "REAPER_TO_NSQD_JSON",
				},
			},
			Action: func(c *cli.Context) error {
				logger := NewLogger(c.GlobalString("loglevel"))
				ctx, cancel := context.WithCancel(context.Background())
				listenSignals(cancel)

				g, lctx := errgroup.WithContext(ctx)

				tcpAddress := c.String("addr")
				topic := c.String("topic")
				exportJSON := c.Bool("json")

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
					var (
						b   []byte
						err error
					)
					if exportJSON {
						b, err = JMarshalEntry(entry)
						if err != nil {
							return err
						}
						if b == nil {
							ack(nil)
							return nil
						}
					} else {
						b, err = MarshalEntry(entry)
						if err != nil {
							return err
						}
						if b == nil {
							ack(nil)
							return nil
						}
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

func action(ctx context.Context, g *errgroup.Group, c *cli.Context, h Handler, reconnect func() error, logger Logger) (err error) {
	defer func() {
		if err != nil {
			err = cli.NewExitError(err.Error(), 1)
		}
	}()

	nsqdOpts, err := buildNSQDOptions(c, logger)
	if err != nil {
		return err
	}

	format, err := GetFormat(c)
	if err != nil {
		return err
	}

	incoming := make(chan *Entry, 10000)
	tcpAddrs := c.GlobalStringSlice("tcp")
	udpAddrs := c.GlobalStringSlice("udp")
	stdin := c.GlobalBool("stdin")
	useRFC5424 := c.GlobalBool("rfc5424")
	websocketAddr := c.GlobalString("websocket-address")
	httpAddr := c.GlobalString("http-address")
	maxInFlight := c.GlobalInt("max-inflight")

	var httpRoutes, websocketRoutes *gin.Engine
	var httpListener, websocketListener net.Listener

	if httpAddr != "" {
		httpRoutes = gin.Default()
		HTTPRoutes(ctx, httpRoutes, nsqdOpts.TCPAddress, nsqdOpts.HTTPAddress, logger)
	}

	if websocketAddr != "" {
		websocketRoutes = httpRoutes
		if websocketAddr != httpAddr {
			websocketRoutes = gin.Default()
		}
		WebsocketRoutes(ctx, websocketRoutes, nsqdOpts.TCPAddress, logger)
	}

	if httpRoutes != nil {
		httpListener, err = net.Listen("tcp", httpAddr)
		if err != nil {
			return err
		}
	}

	if websocketRoutes != nil && websocketAddr != httpAddr {
		websocketListener, err = net.Listen("tcp", websocketAddr)
		if err != nil {
			return err
		}
	}

	g.Go(func() error {
		return NSQD(ctx, nsqdOpts, incoming, h, reconnect, maxInFlight, logger)
	})

	g.Go(func() error {
		return Listen(ctx, tcpAddrs, udpAddrs, stdin, format, useRFC5424, incoming, logger)
	})

	if httpListener != nil {
		g.Go(func() error {
			err := http.Serve(httpListener, httpRoutes)
			logger.Debug("HTTP returned", "error", err)
			return nil
		})
		g.Go(func() error {
			<-ctx.Done()
			_ = httpListener.Close()
			return nil
		})
	}

	if websocketListener != nil {
		g.Go(func() error {
			err := http.Serve(websocketListener, websocketRoutes)
			logger.Debug("Websocket returned", "error", err)
			return nil
		})
		g.Go(func() error {
			<-ctx.Done()
			_ = websocketListener.Close()
			return nil
		})
	}

	err = g.Wait()
	if err != nil && err != context.Canceled {
		return err
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

	var deadline atomic.Value
	deadline.Store(time.Now().Add(time.Second))

	// flush the buffered writer when there is no activity for one second
	g.Go(func() error {
		for {
			if deadline.Load().(time.Time).Before(time.Now()) {
				l.Lock()
				err := bufw.Flush()
				l.Unlock()
				if err != nil {
					return err
				}
			}
			select {
			case <-time.After(time.Second):
			case <-lctx.Done():
				return nil
			}
		}
	})

	handler := func(done <-chan struct{}, entry *Entry, ack func(error)) error {
		deadline.Store(time.Now().Add(time.Second))

		b, err := JMarshalEntry(entry)
		if err != nil {
			logger.Error("Failed to JSON-marshal entry", "error", err)
			return nil
		}
		if b == nil {
			ack(nil)
			return nil
		}
		b = append(b, '\n')
		l.Lock()
		_, err = writer.Write(b)
		l.Unlock()
		if err == nil {
			ack(nil)
			return nil
		}
		return err
	}
	return action(lctx, g, c, handler, nil, logger)
}

func main() {
	_ = BuildApp().Run(os.Args)
}
