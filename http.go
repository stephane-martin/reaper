package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
	"golang.org/x/text/encoding/htmlindex"
)

func validClientID(clientID string) bool {
	if !utf8.ValidString(clientID) {
		return false
	}
	var char rune
	for _, char = range clientID {
		if char == '-' {
			continue
		}
		if char == '_' {
			continue
		}
		if unicode.IsLetter(char) {
			continue
		}
		if unicode.IsDigit(char) {
			continue
		}
		return false
	}
	return true
}

type EntryACK struct {
	Entry string
	ACK   func(error)
}

func HTTPRoutes(ctx context.Context, g *errgroup.Group, router *gin.Engine, nsqdTCPAddr, nsqdHTTPAddr string, filterOut []string, frmt Format, entries chan<- []*Entry, logger Logger) {

	router.GET("/status", func(c *gin.Context) {
		c.Status(200)
	})

	router.Any("/metrics", gin.WrapH(
		promhttp.HandlerFor(
			Metrics.Registry,
			promhttp.HandlerOpts{
				DisableCompression:  true,
				ErrorLog:            AdaptLoggerPrometheus(logger),
				ErrorHandling:       promhttp.HTTPErrorOnError,
				MaxRequestsInFlight: -1,
				Timeout:             -1,
			},
		),
	))

	router.POST("/upload", func(c *gin.Context) {
		Metrics.IncomingConnections.WithLabelValues("http", c.ClientIP()).Inc()
		ct := c.ContentType()
		mt, params, err := mime.ParseMediaType(ct)
		if err != nil {
			c.AbortWithError(400, fmt.Errorf("error parsing media type: %s", err))
			return
		}

		charset := strings.TrimSpace(params["charset"])
		logger.Debug("/upload", "mediatype", mt, "charset", charset)
		if charset == "" {
			charset = "utf-8"
		}
		encoding, err := htmlindex.Get(charset)
		if err != nil {
			c.AbortWithError(500, fmt.Errorf("failed to get decoder for charset '%s': %s", charset, err))
			return
		}
		flusher := newFlush(entries)
		rawLines := make(chan string)
		defer close(rawLines)

		g.Go(func() error {
			for line := range rawLines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				entry := NewEntry()
				err := ParseAccessLogLine(frmt, line, entry, logger)
				if err != nil {
					ReleaseEntry(entry)
					continue
				}
				if len(entry.Fields) == 0 {
					ReleaseEntry(entry)
					continue
				}
				entry.SetString("client_ip", c.ClientIP())
				Metrics.IncomingMessages.WithLabelValues(c.ClientIP(), "http").Inc()
				flusher.Add(entry)
			}
			flusher.Flush(true)
			return nil
		})

		switch mt {
		case "text/plain":
			decoder := encoding.NewDecoder()
			s := bufio.NewScanner(decoder.Reader(c.Request.Body))
			for s.Scan() {
				rawLines <- s.Text()
			}
		case "application/x-www-form-urlencoded":
			decoder := encoding.NewDecoder()
			var err error
			for _, line := range c.PostFormArray("line") {
				line, err = decoder.String(line)
				if err == nil {
					rawLines <- line
				}
			}
		case "multipart/form-data":
			decoder := encoding.NewDecoder()
			var err error
			for _, line := range c.PostFormArray("line") {
				line, err = decoder.String(line)
				if err == nil {
					rawLines <- line
				}
			}
			h, err := c.FormFile("file")
			if err != nil {
				return
			}
			f, err := h.Open()
			if err != nil {
				return
			}
			defer f.Close()
			s := bufio.NewScanner(f)
			for s.Scan() {
				rawLines <- s.Text()
			}
		default:
			c.AbortWithError(400, fmt.Errorf("mediatype not handled: %s", mt))
			return
		}

	})

	router.DELETE("/download/:clientid", func(c *gin.Context) {
		clientID := c.Param("clientid")
		if !validClientID(clientID) {
			_ = c.AbortWithError(400, errors.New("invalid client ID"))
			return
		}
		channel := fmt.Sprintf("reaper_http_download_%s", clientID)
		u := fmt.Sprintf("http://%s/channel/delete?topic=embedded&channel=%s", nsqdHTTPAddr, channel)
		resp, err := http.Post(u, "", nil)
		if err != nil {
			_ = c.AbortWithError(500, err)
			return
		}
		//noinspection GoUnhandledErrorResult
		defer resp.Body.Close()
		c.Status(resp.StatusCode)
		_, err = io.Copy(c.Writer, resp.Body)
		if err != nil {
			logger.Info("Channel delete error", "error", err)
		}
	})

	router.POST("/download/:clientid", func(c *gin.Context) {
		clientID := c.Param("clientid")
		if !validClientID(clientID) {
			_ = c.AbortWithError(400, errors.New("invalid client ID"))
			return
		}

		sizeStr := c.DefaultQuery("size", "1000")
		size, err := strconv.Atoi(sizeStr)
		if err != nil {
			_ = c.AbortWithError(400, err)
			return
		}
		if size <= 0 {
			_ = c.AbortWithError(400, errors.New("size is negative"))
			return
		}

		waitStr := c.DefaultQuery("wait", "3000")
		wait, err := strconv.Atoi(waitStr)
		if err != nil {
			_ = c.AbortWithError(400, err)
			return
		}
		if wait <= 0 {
			_ = c.AbortWithError(400, errors.New("wait is negative"))
		}
		waitDuration := time.Duration(wait) * time.Millisecond

		channel := fmt.Sprintf("reaper_http_download_%s", clientID)
		nsqClientID := fmt.Sprintf("reaper_http_download_%s", NewULID().String())
		logger.Debug("HTTP Download", "clientID", clientID, "channel", channel, "size", size)

		entries := make(chan EntryACK)

		handler := func(hctx context.Context, e *Entry, ack func(error)) error {
			select {
			case <-hctx.Done():
				return ErrPullFinished
			case entries <- EntryACK{Entry: string(e.serialized), ACK: ack}:
				return nil
			}
		}

		g, lctx := errgroup.WithContext(ctx)

		g.Go(func() error {
			err := pullEntries(lctx, nsqClientID, channel, nsqdTCPAddr, handler, filterOut, size, 1, logger)
			close(entries)
			if err == ErrPullFinished {
				return nil
			}
			return err
		})

		g.Go(func() error {
			rCtx := c.Request.Context()

			for {
				select {
				case <-lctx.Done():
					return nil
				case <-rCtx.Done():
					return rCtx.Err()
				case <-time.After(waitDuration):
					return context.DeadlineExceeded
				case entryACK, ok := <-entries:
					if !ok {
						return nil
					}
					_, err = io.WriteString(c.Writer, entryACK.Entry)
					if err == nil {
						_, err = c.Writer.Write(newLine)
					}
					entryACK.ACK(err)
					if err != nil {
						return err
					}
					c.Writer.Flush()
				}
			}
		})

		err = g.Wait()
		if err != nil {
			logger.Info("HTTP download handler returns with error", "error", err)
		}

	})

}
