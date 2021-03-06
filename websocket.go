package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"
)

var GinMode string
var up = websocket.Upgrader{}

const pongWait = 60 * time.Second
const pingPeriod = (pongWait * 9) / 10
const writeWait = 10 * time.Second

func init() {
	gin.SetMode(GinMode)
	gin.DisableConsoleColor()
}

var staticServer = http.FileServer(AssetFile())

func staticRessources(router *gin.Engine, paths []string, subDirectory string) {
	for _, p := range paths {
		path := p
		router.GET(path, func(c *gin.Context) {
			staticServer.ServeHTTP(c.Writer, c.Request)
		})
	}
}

func WebsocketRoutes(ctx context.Context, router *gin.Engine, nsqdAddr string, filterOut []string, logger Logger) {
	staticRessources(router, []string{"/stream.html"}, "static")

	router.Any("/stream", func(c *gin.Context) {
		conn, err := up.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logger.Warn("Websocket upgrade failed", "error", err)
			_ = c.AbortWithError(500, err)
			return
		}
		err = WaitNSQD(ctx)
		if err != nil {
			return
		}
		clientID := NewULID()
		channel := fmt.Sprintf("reaper_websocket_%s#ephemeral", clientID.String())
		entries := make(chan string)
		handler := func(hctx context.Context, e *Entry, ack func(error)) error {
			select {
			case <-hctx.Done():
				return ErrPullFinished
			case entries <- string(e.serialized):
				ack(nil)
				return nil
			}
		}

		g, lctx := errgroup.WithContext(ctx)

		g.Go(func() error {
			err := pullEntries(lctx, channel, channel, nsqdAddr, handler, filterOut, -1, 1, logger)
			close(entries)
			return err
		})

		g.Go(func() error {
			_ = wsReader(conn)
			return nil
		})

		g.Go(func() error {
			err := wsWriter(lctx, conn, entries)
			_ = conn.Close()
			return err
		})

		err = g.Wait()
		if err != nil && err != context.Canceled {
			logger.Debug("Websocket error", "error", err)
		}
	})

}

func wsReader(conn *websocket.Conn) error {
	//noinspection GoUnhandledErrorResult
	defer conn.Close()
	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return err
		}
	}
}

func wsWriter(ctx context.Context, conn *websocket.Conn, entries chan string) error {
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()
	for {
		select {
		case <-pingTicker.C:
			err := conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err != nil {
				return err
			}
			err = conn.WriteMessage(websocket.PingMessage, []byte{})
			if err != nil {
				return err
			}
		case <-ctx.Done():
			_ = conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, ""),
			)
			return nil
		case entry, ok := <-entries:
			if !ok {
				entries = nil
			} else {
				err := conn.WriteMessage(websocket.TextMessage, []byte(entry))
				if err != nil {
					return err
				}
			}
		}
	}
}
