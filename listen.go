package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/influxdata/go-syslog"
	"github.com/influxdata/go-syslog/nontransparent"
	"github.com/influxdata/go-syslog/rfc5424"
	"golang.org/x/sync/errgroup"
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
			err := s.Err()
			if err != nil {
				l.Info("Error scanning stdin", "error", err)
			}
			return nil
		})

	}
	return g.Wait()
}

func listenUDP(ctx context.Context, udp []string, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
	g, lctx := errgroup.WithContext(ctx)

	for _, udpAddr := range udp {
		addr := udpAddr
		var pConn net.PacketConn

		l.Info("Listen on UDP", "addr", addr)
		_, _, err := net.SplitHostPort(addr)
		if err == nil {
			udpAddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				return err
			}
			c, err := net.ListenUDP("udp", udpAddr)
			if err != nil {
				return err
			}
			err = c.SetReadBuffer(65536)
			if err != nil {
				l.Warn("SetReadBuffer for UDP connection failed", "error", err)
			}
			pConn = c
		} else {
			unixAddr, err := net.ResolveUnixAddr("unixgram", addr)
			if err != nil {
				return err
			}
			c, err := net.ListenUnixgram("unixgram", unixAddr)
			if err != nil {
				return err
			}
			err = c.SetReadBuffer(65536)
			if err != nil {
				l.Warn("SetReadBuffer for unix datagram connection failed", "error", err)
			}
			pConn = c
			defer os.Remove(addr)
		}

		g.Go(func() error {
			<-lctx.Done()
			_ = pConn.Close()
			return nil
		})
		g.Go(func() error {
			err := handleUDP(lctx, pConn, f, useRFC5424, entries, l)
			if err != nil {
				l.Warn("UDP handler returned with error", "error", err)
			} else {
				l.Debug("UDP handler returned")
			}
			_ = pConn.Close()
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		l.Warn("All UDP handlers have returned", "error", err)
	} else {
		l.Debug("All UDP handlers have returned")
	}
	return nil
}

func listenTCP(ctx context.Context, tcp []string, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
	g, lctx := errgroup.WithContext(ctx)

	for _, tcpAddr := range tcp {
		addr := tcpAddr
		var listener net.Listener
		l.Info("Listen on TCP", "addr", addr)
		_, _, err := net.SplitHostPort(addr)
		if err == nil {
			tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
			if err != nil {
				return err
			}
			listener, err = net.ListenTCP("tcp", tcpAddr)
			if err != nil {
				return err
			}
		} else {
			unixAddr, err := net.ResolveUnixAddr("unix", addr)
			if err != nil {
				return err
			}
			listener, err = net.ListenUnix("unix", unixAddr)
			if err != nil {
				return err
			}
			defer os.Remove(addr)
		}

		g.Go(func() error {
			<-lctx.Done()
			_ = listener.Close()
			return nil
		})
		g.Go(func() error {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return err
				}
				if tcpConn, ok := conn.(*net.TCPConn); ok {
					// TODO: check the errors and log
					_ = tcpConn.SetReadBuffer(65536)
					_ = tcpConn.SetKeepAlive(true)
					_ = tcpConn.SetKeepAlivePeriod(time.Minute)
				} else if unixConn, ok := conn.(*net.UnixConn); ok {
					err := unixConn.SetReadBuffer(65536)
					if err != nil {
						l.Warn("SetReadBuffer failed on Unix socket", "error", err)
					}
				}
				Metrics.SyslogConnections.WithLabelValues(conn.RemoteAddr().String()).Inc()
				g.Go(func() error {
					<-lctx.Done()
					_ = conn.Close()
					return nil
				})
				g.Go(func() error {
					err := handleTCP(lctx, conn, f, useRFC5424, entries, l)
					_ = conn.Close()
					if err != nil {
						l.Warn("TCP handler returned with error", "error", err)
					} else {
						l.Debug("TCP handler returned")
					}
					return nil
				})
			}
		})
	}
	err := g.Wait()
	if err != nil {
		l.Warn("Listen TCP has returned", "error", err)
	} else {
		l.Debug("Listen TCP has returned")
	}
	return nil
}

func handleTCP(ctx context.Context, conn net.Conn, f Format, useRFC5424 bool, entries chan<- *Entry, l Logger) error {
	if useRFC5424 {
		p := nontransparent.NewParser(
			syslog.WithBestEffort(),
			syslog.WithListener(func(res *syslog.Result) {
				if res.Error != nil {
					l.Warn("Failed to parse RFC5424 stream", "error", res.Error)
					_ = conn.Close()
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
					entry.SetString("syslog_hostname", *(res.Message.Hostname()))
				}
				if res.Message.Timestamp() != nil {
					entry.SetString("syslog_timestamp", (*(res.Message.Timestamp())).Format(time.RFC3339))
				}
				err := ParseAccessLogLine(f, msg, entry, l)
				if err != nil {
					l.Info("Failed to parse TCP/RFC5424 message", "error", err)
					return
				}
				if len(entry.Fields) == 0 {
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

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		Metrics.Incoming.WithLabelValues(conn.RemoteAddr().String(), "tcp").Inc()
		entry, err := parseRFC3164(scanner.Bytes(), f, l)
		if err != nil {
			if _, ok := err.(ErrProtocolSyslog); ok {
				return fmt.Errorf("Failed to parse TCP/RFC3164 message: %s", err)
			}
			l.Info("Failed to parse access log entry from TCP/RFC3164", "error", err)
			continue
		}
		if entry == nil {
			continue
		}
		if len(entry.Fields) == 0 {
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
		return fmt.Errorf("TCP/RFC3164 stream scan error: %s", err)
	}
	return nil
}

var rfc5424Parser = rfc5424.NewParser(rfc5424.WithBestEffort())

type ErrProtocolSyslog struct {
	Err error
}

func (e ErrProtocolSyslog) Error() string {
	return fmt.Sprintf("Syslog protocol error: %s", e.Err)
}

func parseRFC5424(buf []byte, f Format, l Logger) (*Entry, error) {
	m, err := rfc5424Parser.Parse(buf)
	if err != nil {
		return nil, ErrProtocolSyslog{Err: err}
	}
	if m.Message() == nil {
		return nil, ErrEmptyMessage
	}

	entry := NewEntry()
	if m.Hostname() != nil {
		entry.SetString("syslog_hostname", *m.Hostname())
	}
	if m.Timestamp() != nil {
		entry.SetString("syslog_timestamp", (*m.Timestamp()).Format(time.RFC3339))
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
		return nil, ErrProtocolSyslog{Err: err}
	}
	if m.Message == "" {
		return nil, ErrEmptyMessage
	}

	entry := NewEntry()
	if m.HostName != "" {
		entry.SetString("syslog_hostname", m.HostName)
	}
	if m.Time != nil {
		entry.SetString("syslog_timestamp", m.Time.Format(time.RFC3339))
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
		addr      net.Addr
		err, pErr error
		n         int
		entry     *Entry
	)

	var parse func(buf []byte, f Format, l Logger) (*Entry, error)
	if useRFC5424 {
		parse = parseRFC5424
	} else {
		parse = parseRFC3164
	}

	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, addr, err = conn.ReadFrom(buf)
		if n > 0 {
			Metrics.Incoming.WithLabelValues(addr.String(), "udp").Inc()
			entry, pErr = parse(buf[:n], f, l)
			if pErr != nil {
				if _, ok := pErr.(ErrProtocolSyslog); ok {
					l.Warn("Failed to parse UDP message", "error", pErr)
				} else {
					l.Info("Failed to parse UDP message", "error", pErr)
				}
				continue
			}
			if entry == nil {
				continue
			}
			if len(entry.Fields) == 0 {
				continue
			}
			entry.SetString("syslog_remote_addr", addr.String())

			select {
			case <-ctx.Done():
				return nil
			case entries <- entry:
			}
		}
		if err != nil && !isNetTimeout(err) {
			return err
		}
	}
}

func isNetTimeout(e error) bool {
	if err, ok := e.(net.Error); ok {
		return err.Timeout()
	}
	return false
}
