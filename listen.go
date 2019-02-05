package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/influxdata/go-syslog"
	"github.com/influxdata/go-syslog/nontransparent"
	"github.com/influxdata/go-syslog/rfc5424"
	"golang.org/x/sync/errgroup"
)

type timeoutI interface {
	Timeout() bool
}

func isTimeout(err error) bool {
	if e, ok := err.(timeoutI); ok {
		return e.Timeout()
	}
	return false
}

type timeoutReader struct {
	callback    func()
	setDeadline func()
	reader      io.Reader
}

func (r *timeoutReader) Read(p []byte) (int, error) {
	r.setDeadline()
	n, err := r.reader.Read(p)
	if err != nil {
		if isTimeout(err) {
			r.callback()
			return n, nil
		}
	}
	return n, err
}

type flush struct {
	last  time.Time
	cache []*Entry
	ch    chan<- []*Entry
}

func newFlush(ch chan<- []*Entry) *flush {
	return &flush{cache: make([]*Entry, 0, 1024), ch: ch, last: time.Now()}
}

func (f *flush) Flush(force bool) {
	now := time.Now()
	if len(f.cache) == 0 {
		f.last = now
		return
	}
	if force || len(f.cache) >= 1024 || now.Sub(f.last) >= time.Second {
		copyCache := make([]*Entry, len(f.cache))
		copy(copyCache, f.cache)
		f.cache = f.cache[0:0]
		f.ch <- copyCache
		f.last = now
	}
}

func (f *flush) Add(entry *Entry) {
	f.cache = append(f.cache, entry)
	f.Flush(false)
}

func Listen(ctx context.Context, tcp []string, udp []string, fifos []string, stdin bool, f Format, useRFC5424 bool, entries chan<- []*Entry, l Logger) error {
	g, lctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return listenTCP(lctx, tcp, f, useRFC5424, entries, l)
	})
	g.Go(func() error {
		return listenUDP(lctx, udp, f, useRFC5424, entries, l)
	})
	g.Go(func() error {
		return listenFIFO(lctx, fifos, f, entries, l)
	})

	if stdin {
		g.Go(func() error {
			return listenStdin(lctx, f, entries, l)
		})
	}
	return g.Wait()
}

func listenFIFO(ctx context.Context, fifos []string, f Format, entries chan<- []*Entry, l Logger) error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	handles := make([]*os.File, 0, len(fifos))
	for _, fifo := range fifos {
		err := syscall.Mkfifo(fifo, 0666)
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("impossible to create FIFO: %s", err)
		}
		fifoh, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0600)
		if err != nil {
			return fmt.Errorf("impossible to open FIFO '%s': %s", fifo, err)
		}
		defer fifoh.Close()
		infos, err := fifoh.Stat()
		if err != nil {
			return fmt.Errorf("impossible to stat() FIFO '%s': %s", fifo, err)
		}
		if infos.Mode()&os.ModeNamedPipe == 0 {
			return fmt.Errorf("not a FIFO: %s", fifo)
		}
		handles = append(handles, fifoh)
	}

	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-lctx.Done()
		for _, h := range handles {
			_ = h.Close()
		}
		return nil
	})

	flusher := newFlush(entries)

	for _, h := range handles {
		handle := h
		reader := bufio.NewReader(&timeoutReader{
			reader: handle,
			callback: func() {
				flusher.Flush(true)
			},
			setDeadline: func() {
				h.SetReadDeadline(time.Now().Add(time.Second))
			},
		})
		g.Go(func() error {
			for {
				buf, err := reader.ReadBytes('\n')
				if len(buf) > 0 {
					e := NewEntry()
					err = ParseAccessLogLine(f, string(buf), e, l)
					if err != nil {
						l.Warn("Failed to parse access log", "error", err)
						ReleaseEntry(e)
					} else if len(e.Fields) == 0 {
						ReleaseEntry(e)
					} else {
						e.SetString("fifo", handle.Name())
						if hostname != "" {
							e.SetString("local_hostname", hostname)
						}
						Metrics.IncomingMessages.WithLabelValues("", "stdin").Inc()
						flusher.Add(e)
					}
				}
				if err == io.EOF {
					select {
					case <-lctx.Done():
						return nil
					case <-time.After(time.Second):
					}
				} else if err != nil {
					l.Info("Read from fido ended", "fifo", handle.Name(), "error", err)
					return nil
				}
			}
		})
	}
	return g.Wait()
}

func listenStdin(ctx context.Context, f Format, entries chan<- []*Entry, l Logger) error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	infos, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("impossible to stat() stdin: %s", err)
	}
	mode := infos.Mode()
	if mode&os.ModeCharDevice != 0 && infos.Size() != 0 {
		return errors.New("--stdin does not work for such input")
	}
	if mode.IsRegular() {
		return errors.New("--stdin does not work for such input")
	}

	err = syscall.SetNonblock(0, true)
	if err != nil {
		return fmt.Errorf("stdin can not be set to non-blocking: %s", "error")
	}

	stdin := os.NewFile(0, "stdin")
	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-lctx.Done()
		_ = os.Stdin.Close()
		return nil
	})

	g.Go(func() error {
		flusher := newFlush(entries)
		defer flusher.Flush(true)

		reader := bufio.NewReader(&timeoutReader{
			reader: stdin,
			callback: func() {
				flusher.Flush(true)
			},
			setDeadline: func() {
				stdin.SetReadDeadline(time.Now().Add(time.Second))
			},
		})

		var readError error
		var line []byte

		for {
			if readError != nil {
				if readError != io.EOF {
					l.Info("Error scanning stdin", "error", readError)
				}
				return nil
			}
			line, readError = reader.ReadBytes('\n')

			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			e := NewEntry()
			err = ParseAccessLogLine(f, string(line), e, l)
			if err != nil {
				l.Warn("Failed to parse access log", "error", err)
				ReleaseEntry(e)
				continue
			}
			if len(e.Fields) == 0 {
				ReleaseEntry(e)
				continue
			}
			e.Fields["stdin"] = trueValue
			if hostname != "" {
				e.SetString("local_hostname", hostname)
			}
			Metrics.IncomingMessages.WithLabelValues("", "stdin").Inc()
			flusher.Add(e)
		}
	})
	return g.Wait()
}

func listenUDP(ctx context.Context, udp []string, f Format, useRFC5424 bool, entries chan<- []*Entry, l Logger) error {
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

func listenTCP(ctx context.Context, tcp []string, f Format, useRFC5424 bool, entries chan<- []*Entry, l Logger) error {
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
				Metrics.IncomingConnections.WithLabelValues("tcp", conn.RemoteAddr().String()).Inc()
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

func handleTCP(ctx context.Context, conn net.Conn, f Format, useRFC5424 bool, entries chan<- []*Entry, l Logger) error {
	flusher := newFlush(entries)
	defer flusher.Flush(true)

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
				msg := strings.TrimSpace(*res.Message.Message())
				if msg == "" {
					return
				}
				entry := NewEntry()
				err := ParseAccessLogLine(f, msg, entry, l)
				if err != nil {
					ReleaseEntry(entry)
					l.Info("Fail to parse TCP/RFC5424 message", "error", err)
					return
				}
				if len(entry.Fields) == 0 {
					ReleaseEntry(entry)
					return
				}
				if res.Message.Hostname() != nil {
					entry.SetString("syslog_hostname", *(res.Message.Hostname()))
				}
				if res.Message.Timestamp() != nil {
					entry.SetString("syslog_timestamp", (*(res.Message.Timestamp())).Format(time.RFC3339))
				}
				Metrics.IncomingMessages.WithLabelValues(conn.RemoteAddr().String(), "tcp").Inc()
				flusher.Add(entry)
			}),
		)
		p.Parse(&timeoutReader{
			reader: conn,
			callback: func() {
				flusher.Flush(true)
			},
			setDeadline: func() {
				conn.SetReadDeadline(time.Now().Add(time.Second))
			},
		})

		return nil
	}

	reader := bufio.NewReader(conn)
	fullLine := make([]byte, 0, 1024)

	for {
		err := conn.SetReadDeadline(time.Now().Add(time.Second))
		if err != nil {
			return err
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if isTimeout(err) {
				fullLine = append(fullLine, line...)
				flusher.Flush(true)
				continue
			}
			return err
		}

		fullLine = append(fullLine, line...)
		entry, err := parseRFC3164(fullLine, f, l)
		fullLine = fullLine[0:0]
		if err != nil {
			if _, ok := err.(ErrProtocolSyslog); ok {
				return fmt.Errorf("Failed to parse TCP/RFC3164: %s", err)
			}
			l.Info("Failed to parse access log entry from TCP/RFC3164", "error", err)
			continue
		}
		Metrics.IncomingMessages.WithLabelValues(conn.RemoteAddr().String(), "tcp").Inc()
		flusher.Add(entry)
	}
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
	err = ParseAccessLogLine(f, *m.Message(), entry, l)
	if err != nil {
		ReleaseEntry(entry)
		return nil, err
	}
	if len(entry.Fields) == 0 {
		ReleaseEntry(entry)
		return nil, ErrEmptyMessage
	}
	if m.Hostname() != nil {
		entry.SetString("syslog_hostname", *m.Hostname())
	}
	if m.Timestamp() != nil {
		entry.SetString("syslog_timestamp", (*m.Timestamp()).Format(time.RFC3339))
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
	err = ParseAccessLogLine(f, m.Message, entry, l)
	if err != nil {
		ReleaseEntry(entry)
		return nil, err
	}
	if len(entry.Fields) == 0 {
		ReleaseEntry(entry)
		return nil, ErrEmptyMessage
	}
	if m.HostName != "" {
		entry.SetString("syslog_hostname", m.HostName)
	}
	if m.Time != nil {
		entry.SetString("syslog_timestamp", m.Time.Format(time.RFC3339))
	}
	return entry, nil
}

func handleUDP(ctx context.Context, conn net.PacketConn, f Format, useRFC5424 bool, entries chan<- []*Entry, l Logger) error {
	flusher := newFlush(entries)
	defer flusher.Flush(true)

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
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, addr, err = conn.ReadFrom(buf)
		if n > 0 {
			entry, pErr = parse(buf[:n], f, l)
			if pErr != nil {
				if _, ok := pErr.(ErrProtocolSyslog); ok {
					l.Warn("Failed to parse UDP message", "error", pErr)
				} else {
					l.Info("Failed to parse UDP message", "error", pErr)
				}
				continue
			}
			entry.SetString("syslog_remote_addr", addr.String())
			Metrics.IncomingMessages.WithLabelValues(addr.String(), "udp").Inc()
			flusher.Add(entry)
		}
		if err != nil {
			if isTimeout(err) {
				flusher.Flush(true)
			} else {
				return err
			}
		}
	}
}
