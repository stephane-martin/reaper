package main

import (
	"bufio"
	"context"
	"sync"
)

type Scanner interface {
	Scan() bool
	Err() error
	Bytes() []byte
	Text() string
	Buffer([]byte, int)
	Split(bufio.SplitFunc)
}

type CtxScanner struct {
	scanner    Scanner
	resultChan chan bool
	sync.Mutex
	ctx context.Context
	sync.Once
}

func WithContext(ctx context.Context, scanner Scanner) Scanner {
	if ctx == nil {
		return scanner
	}
	return &CtxScanner{
		scanner:    scanner,
		ctx:        ctx,
		resultChan: make(chan bool),
	}
}

func (s *CtxScanner) init() {
	if s.ctx == nil {
		return
	}
	s.Lock()
	go func() {
		for {
			s.Lock()
			select {
			case <-s.ctx.Done():
				close(s.resultChan)
				return
			case s.resultChan <- s.scanner.Scan():
			}
		}
	}()
}

func (s *CtxScanner) Scan() bool {
	if s == nil || s.scanner == nil {
		return false
	}
	if s.ctx == nil {
		return s.scanner.Scan()
	}
	s.Do(s.init)

	s.Unlock()
	select {
	case res := <-s.resultChan:
		return res
	case <-s.ctx.Done():
		return false
	}
}

func (s *CtxScanner) Err() error {
	if s.ctx == nil {
		if s.scanner == nil {
			return nil
		}
		return s.scanner.Err()
	}
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return s.scanner.Err()
	}
}

func (s *CtxScanner) Bytes() []byte {
	return s.scanner.Bytes()
}

func (s *CtxScanner) Buffer(buf []byte, max int) {
	s.scanner.Buffer(buf, max)
}

func (s *CtxScanner) Text() string {
	return s.scanner.Text()
}

func (s *CtxScanner) Split(f bufio.SplitFunc) {
	s.scanner.Split(f)
}
