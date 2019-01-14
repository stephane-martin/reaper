package main

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"net"
)

func listen(ctx context.Context, host string, port int) error {
	g, lctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return listenTCP(lctx, host, port)
	})
	g.Go(func() error {
		return listenUDP(lctx, host, port)
	})
	return g.Wait()
}


func listenUDP(ctx context.Context, host string, port int) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	pconn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}

	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-lctx.Done()
		_ = pconn.Close()
		return lctx.Err()
	})

	g.Go(func() error {
		return handleUDP(lctx, pconn)
	})

	return g.Wait()
}

func listenTCP(ctx context.Context, host string, port int) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-lctx.Done()
		_ = listener.Close()
		return lctx.Err()
	})

	g.Go(func() error {
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
				return handleTCP(lctx, conn)
			})
		}
	})

	return g.Wait()

}



func handleTCP(ctx context.Context, conn net.Conn) error {
	return nil
}

func handleUDP(ctx context.Context, conn net.PacketConn) error {
	buf := make([]byte, 65536)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if n > 0 {
			lbuf := buf[:n]
			fmt.Println(addr.String(), string(lbuf))
		}
		if err != nil {
			return err
		}

	}
}