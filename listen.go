package main

import (
	"bufio"
	"context"
	"golang.org/x/sync/errgroup"
	"gopkg.in/mcuadros/go-syslog.v2/format"
	"net"
	"os"
	"time"
)

func listen(ctx context.Context, tcp []string, udp []string, stdin bool, f Format, entries chan *Entry, l Logger) error {
	defer close(entries)
	g, lctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return listenTCP(lctx, tcp, f, entries, l)
	})
	g.Go(func() error {
		return listenUDP(lctx, udp, f, entries, l)
	})
	if stdin {
		g.Go(func() error {
			s := WithContext(lctx, bufio.NewScanner(os.Stdin))
			L:
			for s.Scan() {
				e := NewEntry()
				err := ParseContent(f, s.Text(), &e, l)
				if err != nil {
					l.Warn("Failed to parse access log", "error", err)
					continue L
				}
				select {
				case <-lctx.Done():
					return ctx.Err()
				case entries <- &e:
				}
			}
			return s.Err()
		})

	}
	return g.Wait()
}


func listenUDP(ctx context.Context, udp []string, f Format, entries chan *Entry, l Logger) error {
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
			return handleUDP(lctx, pconn, f, entries, l)
		})
	}

	err := g.Wait()
	if err != nil {
		l.Debug("Listen UDP returned", "error", err)
	}
	return nil
}

func listenTCP(ctx context.Context, tcp []string, f Format, entries chan *Entry, l Logger) error {
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
					return handleTCP(lctx, conn, f, entries, l)
				})
			}
		})
	}
	err := g.Wait()
	if err != nil {
		l.Debug("Listen TCP returned", "error", err)
	}
	return nil
}


func handleTCP(ctx context.Context, conn net.Conn, f Format, entries chan *Entry, l Logger) error {
	return nil
}

func handleUDP(ctx context.Context, conn net.PacketConn, f Format, entries chan *Entry, l Logger) error {
	buf := make([]byte, 65536)
	var parser format.RFC3164

	L:
	for {
		n, addr, err := conn.ReadFrom(buf)
		if n > 0 {
			p := parser.GetParser(buf[:n])
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

			entry := NewEntry()
			entry.Set("syslog_hostname", hostname)
			entry.Set("syslog_timestamp", timestamp)
			entry.Set("syslog_remote_addr", addr.String())

			if parts["content"] != nil {
				content, ok := parts["content"].(string)
				if !ok {
					continue L
				}
				err := ParseContent(f, content, &entry, l)
				if err != nil {
					l.Warn("Failed to parse access log", "error", err)
					continue L
				}
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case entries <- &entry:
			}
		}
		if err != nil {
			return err
		}

	}
}