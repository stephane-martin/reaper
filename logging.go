package main

import (
	"bytes"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/gin-gonic/gin"
	"github.com/inconshreveable/log15"
	"github.com/nsqio/nsq/nsqd"
	"github.com/olivere/elastic"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli"
	"log"
	"log/syslog"
	"strconv"
	"strings"
	"time"
)

type Logger struct {
	log15.Logger
}

type adaptedNSQD struct {
	Logger
}

type adaptedSarama struct {
	Logger
}

type adaptedErrorElastic struct {
	Logger
}

type adaptedInfoElastic struct {
	Logger
}

type adaptedPrometheus struct {
	Logger
}

func (a adaptedPrometheus) Println(v ...interface{}) {
	a.Logger.Error(fmt.Sprintln(v...))
}

func (l adaptedErrorElastic) Printf(format string, v ...interface{}) {
	l.Logger.Warn("[elastic] " + fmt.Sprintf(format, v...))
}

func (l adaptedInfoElastic) Printf(format string, v ...interface{}) {
	l.Logger.Info("[elastic] " + fmt.Sprintf(format, v...))
}

func (l adaptedSarama) Print(v ...interface{}) {
	l.Logger.Info("[kafka] " + fmt.Sprint(v...))
}

func (l adaptedSarama) Printf(format string, v ...interface{}) {
	l.Logger.Info("[kafka] " + fmt.Sprintf(format, v...))
}

func (l adaptedSarama) Println(v ...interface{}) {
	l.Logger.Info("[kafka] " + fmt.Sprint(v...))
}

func AdaptLoggerNSQD(l Logger) nsqd.Logger {
	return adaptedNSQD{Logger: l}
}

func AdaptLoggerSarama(l Logger) sarama.StdLogger {
	return adaptedSarama{Logger: l}
}

func AdaptErrorLoggerElasticsearch(l Logger) elastic.Logger {
	return adaptedErrorElastic{Logger: l}
}

func AdaptInfoLoggerElasticsearch(l Logger) elastic.Logger {
	return adaptedInfoElastic{Logger: l}
}


func AdaptLoggerPrometheus(l Logger) promhttp.Logger {
	return &adaptedPrometheus{
		Logger: l,
	}
}


func (l adaptedNSQD) Output(maxDepth int, s string) error {
	if len(s) < 5 {
		l.Logger.Info(s)
		return nil
	}
	prefix := s[:4]
	msg := "[nsq] " + strings.TrimSpace(s[4:])
	switch prefix {
	case "INF ":
		l.Logger.Info(msg)
	case "WRN ":
		l.Logger.Warn(msg)
	case "ERR ":
		l.Logger.Error(msg)
	default:
		parts := strings.SplitN(s, ":", 2)
		if len(parts) < 2 {
			l.Logger.Info(s)
		} else {
			msg := "[nsqd] " + strings.TrimSpace(parts[1])
			switch strings.TrimSpace(parts[0]) {
			case "DEBUG":
				l.Logger.Debug(msg)
			case "INFO":
				l.Logger.Info(msg)
			case "WARNING":
				l.Logger.Warn(msg)
			case "ERROR":
				l.Logger.Error(msg)
			case "FATAL":
				l.Logger.Crit(msg)
			default:
				l.Logger.Info(s)
			}
		}
	}
	return nil
}

func syslogFormat() log15.Format {
	return log15.FormatFunc(func(r *log15.Record) []byte {
		var buf bytes.Buffer
		buf.WriteString("msg=")
		formatLogfmtValue(r.Msg, &buf)

		for i := 0; i < len(r.Ctx); i += 2 {
			k, ok := r.Ctx[i].(string)
			if !ok {
				continue
			}
			buf.WriteByte(' ')
			buf.WriteString(k)
			buf.WriteByte('=')
			formatLogfmtValue(r.Ctx[i+1], &buf)
		}
		buf.WriteByte('\n')
		return buf.Bytes()
	})
}

func formatLogfmtValue(value interface{}, buf *bytes.Buffer) {
	if value == nil {
		buf.WriteString("nil")
		return
	}

	if t, ok := value.(time.Time); ok {
		buf.WriteString(t.Format(time.RFC3339))
		return
	}
	switch v := value.(type) {
	case bool:
		buf.WriteString(strconv.FormatBool(v))
	case float32:
		buf.WriteString(strconv.FormatFloat(float64(v), 'f', 3, 64))
	case float64:
		buf.WriteString(strconv.FormatFloat(v, 'f', 3, 64))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		buf.WriteString(fmt.Sprintf("%d", value))
	case error:
		escapeString(v.Error(), buf)
	case fmt.Stringer:
		escapeString(v.String(), buf)
	case string:
		escapeString(v, buf)
	default:
		escapeString(fmt.Sprintf("%+v", value), buf)
	}
}

func escapeString(s string, buf *bytes.Buffer) {
	needsQuotes := false
	needsEscape := false
	for _, r := range s {
		if r <= ' ' || r == '=' || r == '"' {
			needsQuotes = true
		}
		if r == '\\' || r == '"' || r == '\n' || r == '\r' || r == '\t' {
			needsEscape = true
		}
	}
	if needsEscape == false && needsQuotes == false {
		buf.WriteString(s)
		return
	}
	if needsQuotes {
		buf.WriteByte('"')
	}
	for _, r := range s {
		switch r {
		case '\\', '"':
			buf.WriteByte('\\')
			buf.WriteByte(byte(r))
		case '\n':
			buf.WriteString("\\n")
		case '\r':
			buf.WriteString("\\r")
		case '\t':
			buf.WriteString("\\t")
		default:
			buf.WriteRune(r)
		}
	}
	if needsQuotes {
		buf.WriteByte('"')
	}
}


func NewLogger(c *cli.Context) Logger {
	loglevel := c.GlobalString("loglevel")
	useSyslog := c.GlobalBool("syslog")
	lvl, _ := log15.LvlFromString(loglevel)
	logger := log15.New()

	if useSyslog {
		h, err := log15.SyslogHandler(
			syslog.LOG_DAEMON | syslog.LOG_INFO,
			"reaper",
			syslogFormat(),
		)
		if err != nil {
			panic(err)
		}
		logger.SetHandler(
			log15.LvlFilterHandler(
				lvl,
				h,
			),
		)
	} else {
		logger.SetHandler(
			log15.LvlFilterHandler(
				lvl,
				log15.StderrHandler,
			),
		)
	}

	initGinLogging(logger)
	return Logger{Logger: logger}
}

func initGinLogging(l log15.Logger) {
	wr := &GinLogger{Logger: l}
	gin.DefaultWriter = wr
	gin.DefaultErrorWriter = wr
	log.SetOutput(wr)
}

type GinLogger struct {
	Logger log15.Logger
}

func (w GinLogger) Write(b []byte) (int, error) {
	l := len(b)
	dolog := w.Logger.Info
	b = bytes.TrimSpace(b)
	b = bytes.Replace(b, []byte{'\t'}, []byte{' '}, -1)
	b = bytes.Replace(b, []byte{'"'}, []byte{'\''}, -1)
	if bytes.HasPrefix(b, []byte("[GIN-debug] ")) {
		b = b[12:]
	}
	if bytes.HasPrefix(b, []byte("[WARNING] ")) {
		b = b[10:]
		dolog = w.Logger.Warn
	}
	lines := bytes.Split(b, []byte{'\n'})
	for _, line := range lines {
		dolog(string(line))
	}
	return l, nil
}