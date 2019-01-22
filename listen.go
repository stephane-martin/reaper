package main

import (
	"bufio"
	"context"
	"github.com/influxdata/go-syslog"
	"github.com/influxdata/go-syslog/nontransparent"
	"github.com/influxdata/go-syslog/rfc5424"
	"golang.org/x/sync/errgroup"
	"net"
	"os"
	"strings"
	"time"
)

func Listen(ctx context.Context, tcp []string, udp []string, stdin bool, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
	defer close(entries)
	g, lctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return listenTCP(lctx, tcp, f, useRFC5424, entries, l)
	})
	g.Go(func() error {
		return listenUDP(lctx, udp, f, useRFC5424, entries, l)
	})
	if stdin {
		g.Go(func() error {
			s := WithContext(lctx, bufio.NewScanner(os.Stdin))
			L:
			for s.Scan() {
				Metrics.Incoming.WithLabelValues("", "stdin").Inc()
				e := NewEntry()
				err := ParseAccessLogLine(f, s.Text(), e, l)
				if err != nil {
					l.Warn("Failed to parse access log", "error", err)
					continue L
				}
				select {
				case <-lctx.Done():
					return ctx.Err()
				case entries <- e:
				}
			}
			return s.Err()
		})

	}
	return g.Wait()
}


func listenUDP(ctx context.Context, udp []string, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
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
			return handleUDP(lctx, pconn, f, useRFC5424, entries, l)
		})
	}

	err := g.Wait()
	if err != nil {
		l.Debug("Listen UDP returned", "error", err)
	}
	return nil
}

func listenTCP(ctx context.Context, tcp []string, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
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
				return nil
			})
			for {
				conn, err := listener.Accept()
				if err != nil {
					return err
				}
				Metrics.SyslogConnections.WithLabelValues(conn.RemoteAddr().String()).Inc()
				g.Go(func() error {
					<-lctx.Done()
					_ = conn.Close()
					return nil
				})
				g.Go(func() error {
					return handleTCP(lctx, conn, f, useRFC5424, entries, l)
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


func handleTCP(ctx context.Context, conn net.Conn, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
	if useRFC5424 {
		p := nontransparent.NewParser(
			syslog.WithBestEffort(),
			syslog.WithListener(func(res *syslog.Result) {
				if res.Error != nil {
					l.Info("Failed to parse RFC5424 stream", "error", res.Error)
					return
				}
				if res.Message.Message() == nil {
					return
				}
				Metrics.Incoming.WithLabelValues(conn.RemoteAddr().String(), "tcp").Inc()
				msg := strings.TrimSpace(*res.Message.Message())
				if msg == "" {
					return
				}
				entry := NewEntry()
				if res.Message.Hostname() != nil {
					entry.Set("syslog_hostname", *(res.Message.Hostname()))
				}
				if res.Message.Timestamp() != nil {
					entry.Set("syslog_timestamp", (*(res.Message.Timestamp())).Format(time.RFC3339))
				}
				err := ParseAccessLogLine(f, msg, entry, l)
				if err != nil {
					l.Info("Failed to parse TCP/RFC5424 message", "error", err)
					return
				}
				select {
				case <-ctx.Done():
				case entries <- entry:
				}
			}),
		)
		p.Parse(conn)
		return nil
	}
	scanner := WithContext(ctx, bufio.NewScanner(conn))
	for scanner.Scan() {
		Metrics.Incoming.WithLabelValues(conn.RemoteAddr().String(), "tcp").Inc()
		entry, err := parseRFC3164(scanner.Bytes(), f, l)
		if err != nil {
			l.Info("Failed to parse TCP/RFC3164 message", "error", err)
			continue
		}
		if entry == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case entries <- entry:
		}
	}
	err := scanner.Err()
	if err != nil {
		l.Warn("Scanning error in TCP/RFC3164 stream", "error", err)
	}
	return nil
}

var rfc5424Parser = rfc5424.NewParser(rfc5424.WithBestEffort())

func parseRFC5424(buf []byte, f Format, l Logger) (*Entry, error) {
	m, err := rfc5424Parser.Parse(buf)
	if err != nil {
		return nil, err
	}
	if m.Message() == nil {
		return nil, ErrEmptyMessage
	}

	entry := NewEntry()
	if m.Hostname() != nil {
		entry.Set("syslog_hostname", *m.Hostname())
	}
	if m.Timestamp() != nil {
		entry.Set("syslog_timestamp", (*m.Timestamp()).Format(time.RFC3339))
	}
	err = ParseAccessLogLine(f, *m.Message(), entry, l)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func parseRFC3164(buf []byte, f Format, l Logger) (*Entry, error) {
	m, err := p3164(buf)
	if err != nil {
		return nil, err
	}
	if m.Message == "" {
		return nil, ErrEmptyMessage
	}

	entry := NewEntry()
	if m.HostName != "" {
		entry.Set("syslog_hostname", m.HostName)
	}
	if m.Time != nil {
		entry.Set("syslog_timestamp", m.Time.Format(time.RFC3339))
	}

	err = ParseAccessLogLine(f, m.Message, entry, l)
	if err != nil {
		return nil, err
	}

	return entry, nil
}

func handleUDP(ctx context.Context, conn net.PacketConn, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
	buf := make([]byte, 65536)
	var (
		addr net.Addr
		err, pErr error
		n int
		entry *Entry
	)

	L:
	for {
		n, addr, err = conn.ReadFrom(buf)
		if n > 0 {
			Metrics.Incoming.WithLabelValues(addr.String(), "udp").Inc()
			if useRFC5424 {
				entry, pErr = parseRFC5424(buf[:n], f, l)
				if pErr != nil {
					l.Info("Failed to parse UDP/RFC5424 message", "error", err)
					continue L
				}
			} else {
				entry, pErr = parseRFC3164(buf[:n], f, l)
				if pErr != nil {
					l.Info("Failed to parse UDP/RFC3164 message", "error", err)
					continue L
				}
			}

			if entry == nil {
				continue L
			}
			entry.Set("syslog_remote_addr", addr.String())

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