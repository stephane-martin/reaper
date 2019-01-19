package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"
	"net"
	"net/http"
	"time"
)

var GinMode string
var upgrader = websocket.Upgrader{}

const pongWait = 60 * time.Second
const pingPeriod = (pongWait * 9) / 10
const writeWait = 10 * time.Second

func init() {
	gin.SetMode(GinMode)
	gin.DisableConsoleColor()
}

func ListenWebsocket(ctx context.Context, listenAddr, nsqdAddr string, logger Logger) error {
	router := gin.Default()

	router.GET("/status", func(c *gin.Context) {
		c.Status(200)
	})

	g, lctx := errgroup.WithContext(ctx)

	router.Any("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logger.Warn("Websocket upgrade failed", "error", err)
			return
		}
		err = WaitNSQD(ctx)
		if err != nil {
			return
		}
		clientID := NewULID()
		channel := fmt.Sprintf("reaper_websocket_%s#ephemeral", clientID.String())
		entries := make(chan *Entry)
		handler := func(done <-chan struct{}, e *Entry, ack func(error)) error {
			select {
			case <-done:
				return context.Canceled
			case entries <- e:
				ack(nil)
				return nil
			}
		}

		g.Go(func() error {
			return wsReader(conn)
		})

		g.Go(func() error {
			err := pullEntries(lctx, channel, channel, nsqdAddr, handler, logger)
			close(entries)
			return err
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

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = g.Wait()
		_ = listener.Close()
	}()
	err = http.Serve(listener, router)
	if err != nil {
		logger.Debug("Websocket returned", "error", err)
	}
	return nil
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

func wsWriter(ctx context.Context, conn *websocket.Conn, entries chan *Entry) error {
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
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			return nil
		case entry, ok := <-entries:
			if !ok {
				entries = nil
			} else {
				b := entry.JSON()
				if b != nil {
					err := conn.WriteMessage(websocket.TextMessage, b)
					if err != nil {
						return err
					}
				}
			}
		}
	}
}