// Package bridge implements the MiSTer-side remote input listener.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	MaxHandshakeBytes = 4096
	MaxFrameBytes     = 4096
	HeartbeatTimeout  = 750 * time.Millisecond
	MaxHandshakeConns = 16
)

type Sink interface {
	Apply(protocol.InputFrame) error
	ReleaseAll() error
	Close() error
}
type Config struct {
	Addr             string
	Token            []byte
	Session          uint64
	Core             string
	HeartbeatTimeout time.Duration
	Logger           *slog.Logger
	OnDisconnect     func()
}
type Metrics struct{ Accepted, Applied, Rejected, SequenceGaps, Releases atomic.Uint64 }

func (m *Metrics) Snapshot() map[string]uint64 {
	return map[string]uint64{"accepted": m.Accepted.Load(), "applied": m.Applied.Load(), "rejected": m.Rejected.Load(), "sequence_gaps": m.SequenceGaps.Load(), "releases": m.Releases.Load()}
}

type hello struct {
	Version uint8  `json:"version"`
	Session uint64 `json:"session"`
	Core    string `json:"core"`
	Proof   string `json:"proof"`
}
type Server struct {
	cfg        Config
	sink       Sink
	ln         net.Listener
	ready      chan struct{}
	metrics    Metrics
	stopOnce   sync.Once
	wg         sync.WaitGroup
	activeMu   sync.Mutex
	active     bool
	activeConn net.Conn
	handshakes chan struct{}
}

func New(cfg Config, sink Sink) (*Server, error) {
	if len(cfg.Token) < 16 || cfg.Session == 0 || sink == nil {
		return nil, errors.New("bridge requires token, session, and sink")
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = HeartbeatTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{cfg: cfg, sink: sink, ready: make(chan struct{}), handshakes: make(chan struct{}, MaxHandshakeConns)}, nil
}
func (s *Server) Ready() <-chan struct{} { return s.ready }
func (s *Server) Addr() net.Addr {
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}
func (s *Server) Metrics() *Metrics { return &s.metrics }
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.ln = ln
	close(s.ready)
	go func() { <-ctx.Done(); s.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		select {
		case s.handshakes <- struct{}{}:
		default:
			_ = c.Close()
			s.metrics.Rejected.Add(1)
			continue
		}
		s.metrics.Accepted.Add(1)
		s.wg.Add(1)
		go func() { defer s.wg.Done(); defer func() { <-s.handshakes }(); s.handle(ctx, c) }()
	}
}
func (s *Server) Close() error {
	var err error
	s.stopOnce.Do(func() {
		if s.ln != nil {
			err = s.ln.Close()
		}
		s.activeMu.Lock()
		if s.activeConn != nil {
			_ = s.activeConn.Close()
		}
		s.activeMu.Unlock()
		s.wg.Wait()
		_ = s.sink.ReleaseAll()
		s.metrics.Releases.Add(1)
		_ = s.sink.Close()
	})
	return err
}
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReaderSize(conn, MaxHandshakeBytes)
	line, err := readHandshake(br)
	if err != nil {
		s.metrics.Rejected.Add(1)
		return
	}
	var h hello
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if dec.Decode(&h) != nil || dec.Decode(&struct{}{}) != io.EOF || h.Version != protocol.InputVersion || h.Session != s.cfg.Session || h.Core != s.cfg.Core || !validProof(h.Proof, s.cfg.Token) {
		s.metrics.Rejected.Add(1)
		return
	}
	s.activeMu.Lock()
	if s.active {
		s.activeMu.Unlock()
		s.metrics.Rejected.Add(1)
		return
	}
	s.active = true
	s.activeConn = conn
	s.activeMu.Unlock()
	defer func() {
		s.activeMu.Lock()
		s.active = false
		s.activeConn = nil
		s.activeMu.Unlock()
		_ = s.sink.ReleaseAll()
		s.metrics.Releases.Add(1)
		if s.cfg.OnDisconnect != nil {
			s.cfg.OnDisconnect()
		}
	}()
	_ = conn.SetReadDeadline(time.Time{})
	_, _ = io.WriteString(conn, "{\"ok\":true}\n")
	tracker := protocol.SequenceTracker{}
	last := time.Now()
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if time.Since(last) > s.cfg.HeartbeatTimeout {
				return
			}
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		f, e := protocol.DecodeInputFrame(br, MaxFrameBytes)
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		last = time.Now()
		if f.Header.Type == protocol.InputTypePing {
			f.Header.Type = protocol.InputTypePong
			f.ServerMonoNS = uint64(time.Now().UnixNano())
			wire, err := protocol.EncodeInputFrame(f)
			if err != nil {
				return
			}
			if _, err := conn.Write(wire); err != nil {
				return
			}
			continue
		}
		accepted, gap := tracker.Observe(f.Seq)
		if gap {
			s.metrics.SequenceGaps.Add(1)
		}
		if f.Header.Session != s.cfg.Session || !accepted {
			s.metrics.Rejected.Add(1)
			continue
		}
		if err := s.sink.Apply(f); err != nil {
			s.metrics.Rejected.Add(1)
			continue
		}
		s.metrics.Applied.Add(1)
	}
}
func readHandshake(r *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 256)
	for {
		part, err := r.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > MaxHandshakeBytes {
			return nil, errors.New("handshake too large")
		}
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}
func validProof(encoded string, token []byte) bool {
	b, err := hex.DecodeString(encoded)
	return err == nil && len(b) == len(token) && subtle.ConstantTimeCompare(b, token) == 1
}
func WriteFrame(w io.Writer, f protocol.InputFrame) error {
	b, err := protocol.EncodeInputFrame(f)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
