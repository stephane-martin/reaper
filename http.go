package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
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

func HTTPRoutes(ctx context.Context, router *gin.Engine, nsqdTCPAddr, nsqdHTTPAddr string, filterOut []string, logger Logger) {

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
			case entries <- EntryACK{Entry: string(e.serialized.B), ACK: ack}:
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
