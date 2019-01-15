package main

import (
	"context"
	"github.com/inconshreveable/log15"
	"golang.org/x/sync/errgroup"
	"gopkg.in/mcuadros/go-syslog.v2/format"
	"net"
	"strconv"
	"strings"
	"time"
)

func listen(ctx context.Context, tcp []string, udp []string, entries chan *Entry, l Logger) error {
	g, lctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return listenTCP(lctx, tcp, entries, l)
	})
	g.Go(func() error {
		return listenUDP(lctx, udp, entries, l)
	})
	return g.Wait()
}


func listenUDP(ctx context.Context, udp []string, entries chan *Entry, l Logger) error {
	g, lctx := errgroup.WithContext(ctx)

	for _, udpAddr := range udp {
		addr := udpAddr
		l.Info("Listen on UDP", "addr", addr)
		g.Go(func() error {
			pconn, err := net.ListenPacket("udp", addr)
			if err != nil {
				return err
			}
			g.Go(func() error {
				<-lctx.Done()
				_ = pconn.Close()
				return lctx.Err()
			})
			return handleUDP(lctx, pconn, entries, l)
		})
	}

	return g.Wait()
}

func listenTCP(ctx context.Context, tcp []string, entries chan *Entry, l log15.Logger) error {
	g, lctx := errgroup.WithContext(ctx)

	for _, tcpAddr := range tcp {
		addr := tcpAddr
		l.Info("Listen on TCP", "addr", addr)
		g.Go(func() error {
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			g.Go(func() error {
				<-lctx.Done()
				_ = listener.Close()
				return lctx.Err()
			})
			for {
				conn, err := listener.Accept()
				if err != nil {
					return err
				}
				g.Go(func() error {
					<-lctx.Done()
					_ = conn.Close()
					return lctx.Err()
				})
				g.Go(func() error {
					return handleTCP(lctx, conn, entries, l)
				})
			}
		})
	}

	return g.Wait()
}


func handleTCP(ctx context.Context, conn net.Conn, entries chan *Entry, l log15.Logger) error {
	return nil
}

func handleUDP(ctx context.Context, conn net.PacketConn, entries chan *Entry, l log15.Logger) error {
	buf := make([]byte, 65536)
	var f format.RFC3164

	L:
	for {
		n, addr, err := conn.ReadFrom(buf)
		if n > 0 {
			p := f.GetParser(buf[:n])
			err := p.Parse()
			if err != nil {
				continue L
			}
			parts := p.Dump()

			hostname := ""
			if parts["hostname"] != nil {
				h, ok := parts["hostname"].(string)
				if !ok {
					continue L
				}
				hostname = h
			}

			var timestamp *time.Time
			if parts["timestamp"] != nil {
				t, ok := parts["timestamp"].(time.Time)
				if !ok {
					continue L
				}
				timestamp = &t
			}

			entry := &Entry{
				UID: NewULID().String(),
				SyslogHostname: hostname,
				SyslogTimestamp: timestamp,
				SyslogRemoteAddr: addr.String(),
			}

			if parts["content"] != nil {
				content, ok := parts["content"].(string)
				if !ok {
					continue L
				}
				fields := strings.Fields(content)
				if len(fields) == 0 {
					continue L
				}
				m := make(map[string]string)
				for _, field := range fields {
					fieldParts := strings.SplitN(field, "=", 2)
					if len(fieldParts) == 2 {
						v, err := strconv.Unquote(fieldParts[1])
						if err == nil {
							m[fieldParts[0]] = v
						}
					}
				}
				if len(m) > 0 {
					entry.Fields = m
				}
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case entries <- entry:
			}
		}
		if err != nil {
			return err
		}

	}
}