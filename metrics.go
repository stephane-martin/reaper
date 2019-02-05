package main

import (
	"github.com/nsqio/nsq/nsqd"
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	IncomingConnections *prometheus.CounterVec
	IncomingMessages    *prometheus.CounterVec
	DestinationLatency  prometheus.Summary
	EndToEndLatency     prometheus.Summary
	Registry            *prometheus.Registry
}

var Metrics = newMetrics()

func newMetrics() *metrics {
	m := new(metrics)

	m.IncomingConnections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "incoming_connections_total",
			Help: "Number of incoming connections",
		},
		[]string{"client_addr", "connection_type"},
	)

	m.IncomingMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "incoming_messages_total",
			Help: "Number of received messages",
		},
		[]string{"client_addr", "connection_type"},
	)

	m.DestinationLatency = prometheus.NewSummary(prometheus.SummaryOpts{
		Help:       "observe the duration in milliseconds of the delivery of messages (counting from the instant the message was out of nsqd)",
		MaxAge:     prometheus.DefMaxAge,
		BufCap:     prometheus.DefBufCap,
		AgeBuckets: prometheus.DefAgeBuckets,
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001, 0.999: 0.0001},
		Name:       "destination_latency",
	})

	m.EndToEndLatency = prometheus.NewSummary(prometheus.SummaryOpts{
		Help:       "observe the duration in milliseconds of the delivery of messages (counting from the creation instant of the message)",
		MaxAge:     prometheus.DefMaxAge,
		BufCap:     prometheus.DefBufCap,
		AgeBuckets: prometheus.DefAgeBuckets,
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001, 0.999: 0.0001},
		Name:       "end_to_end_latency",
	})

	m.Registry = prometheus.NewRegistry()
	m.Registry.MustRegister(
		m.IncomingConnections,
		m.IncomingMessages,
		m.DestinationLatency,
		m.EndToEndLatency,
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		prometheus.NewGoCollector(),
	)
	return m
}

type nsqdCollector struct {
	depth        *prometheus.Desc
	backendDepth *prometheus.Desc
	messageCount *prometheus.Desc

	channelDepth        *prometheus.Desc
	channelBackendDepth *prometheus.Desc
	channelInFlight     *prometheus.Desc
	channelDeferred     *prometheus.Desc
	channelMessage      *prometheus.Desc
	channelRequeue      *prometheus.Desc
	channelTimeout      *prometheus.Desc
	channelNbClients    *prometheus.Desc

	daemon *nsqd.NSQD
}

func (c *nsqdCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.messageCount
}

func (c *nsqdCollector) Collect(ch chan<- prometheus.Metric) {
	for _, t := range c.daemon.GetStats("", "") {
		ch <- prometheus.MustNewConstMetric(
			c.depth,
			prometheus.GaugeValue,
			float64(t.Depth),
			t.TopicName,
		)
		ch <- prometheus.MustNewConstMetric(
			c.backendDepth,
			prometheus.GaugeValue,
			float64(t.BackendDepth),
			t.TopicName,
		)

		ch <- prometheus.MustNewConstMetric(
			c.messageCount,
			prometheus.CounterValue,
			float64(t.MessageCount),
			t.TopicName,
		)

		for _, chnl := range t.Channels {
			ch <- prometheus.MustNewConstMetric(
				c.channelDepth,
				prometheus.GaugeValue,
				float64(chnl.Depth),
				t.TopicName, chnl.ChannelName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.channelBackendDepth,
				prometheus.GaugeValue,
				float64(chnl.BackendDepth),
				t.TopicName, chnl.ChannelName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.channelInFlight,
				prometheus.GaugeValue,
				float64(chnl.InFlightCount),
				t.TopicName, chnl.ChannelName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.channelDeferred,
				prometheus.GaugeValue,
				float64(chnl.DeferredCount),
				t.TopicName, chnl.ChannelName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.channelMessage,
				prometheus.CounterValue,
				float64(chnl.MessageCount),
				t.TopicName, chnl.ChannelName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.channelRequeue,
				prometheus.CounterValue,
				float64(chnl.RequeueCount),
				t.TopicName, chnl.ChannelName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.channelTimeout,
				prometheus.CounterValue,
				float64(chnl.TimeoutCount),
				t.TopicName, chnl.ChannelName,
			)

			ch <- prometheus.MustNewConstMetric(
				c.channelNbClients,
				prometheus.GaugeValue,
				float64(len(chnl.Clients)),
				t.TopicName, chnl.ChannelName,
			)
		}
	}
}

func NewNSQDCollector(daemon *nsqd.NSQD) prometheus.Collector {
	return &nsqdCollector{
		depth: prometheus.NewDesc(
			"nsqd_topic_depth",
			"Depth for each nsqd topic.",
			[]string{"topic"},
			nil,
		),

		backendDepth: prometheus.NewDesc(
			"nsqd_topic_backend_depth",
			"Backend depth for each nsqd topic.",
			[]string{"topic"},
			nil,
		),

		messageCount: prometheus.NewDesc(
			"nsqd_topic_messages",
			"Number of messages for each nsqd topic.",
			[]string{"topic"},
			nil,
		),

		channelDepth: prometheus.NewDesc(
			"nsqd_channel_depth",
			"Depth for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		channelBackendDepth: prometheus.NewDesc(
			"nsqd_channel_backend_depth",
			"Backend depth for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		channelInFlight: prometheus.NewDesc(
			"nsqd_channel_inflight",
			"Inflight message count for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		channelDeferred: prometheus.NewDesc(
			"nsqd_channel_defered",
			"Deferred message count for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		channelMessage: prometheus.NewDesc(
			"nsqd_channel_messages",
			"Message count for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		channelRequeue: prometheus.NewDesc(
			"nsqd_channel_requeue",
			"Requeued message count for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		channelTimeout: prometheus.NewDesc(
			"nsqd_channel_timeout",
			"Timeout message count for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		channelNbClients: prometheus.NewDesc(
			"nsqd_channel_clients",
			"Number of connected clients for each nsqd channel.",
			[]string{"topic", "channel"},
			nil,
		),

		daemon: daemon,
	}
}
