// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultDialTimeout           = 10 * time.Second
	defaultIdleConnTimeout       = 30 * time.Second
	defaultResponseHeaderTimeout = 30 * time.Second
	defaultMaxIdleConns          = 8

	defaultServerReadHeaderTimeout = 5 * time.Second
	defaultServerReadTimeout       = 30 * time.Second
	defaultServerWriteTimeout      = 30 * time.Second
	defaultServerIdleTimeout       = 60 * time.Second
	defaultServerMaxHeaderBytes    = 64 << 10
	defaultServerMaxConns          = 8
)

var (
	// ErrInvalidHTTPConfig is returned when TransportConfig or ServerConfig is invalid.
	ErrInvalidHTTPConfig = errors.New("network: invalid HTTP configuration")
	// ErrServerStarted is returned when HTTPServer.Start is called more than once.
	ErrServerStarted = errors.New("network: HTTP server already started")
	// ErrServerStopped is returned when HTTPServer.Start is called after Close or Shutdown.
	ErrServerStopped = errors.New("network: HTTP server stopped")
)

// TransportConfig configures a bare-metal http.Transport or http.Client bound to
// a *Link, with optional DNS resolution and TLS support.
type TransportConfig struct {
	DialTimeout           time.Duration    // Dial timeout per attempt (default 10s; <0 invalid)
	TLSConfig             *tls.Config      // Optional TLS client config (cloned before use)
	Resolver              *DNSResolver     // Optional DNSv4 resolver for non-IP hostnames
	WallClock             func() time.Time // Optional UTC clock for certificate verification when TLSConfig.Time is nil
	MaxIdleConns          int              // Max idle pooled connections (default 8; <0 invalid)
	IdleConnTimeout       time.Duration    // Idle connection timeout (default 30s; <0 invalid)
	ResponseHeaderTimeout time.Duration    // Response header deadline (default 30s; <0 invalid)
	ClientTimeout         time.Duration    // Total http.Client request timeout (used by NewHTTPClient; <0 invalid)
}

func cloneTLSConfig(src *tls.Config, wallClock func() time.Time) *tls.Config {
	if src == nil && wallClock == nil {
		return nil
	}
	var dst *tls.Config
	if src != nil {
		dst = src.Clone()
	} else {
		dst = &tls.Config{}
	}
	if dst.MinVersion == 0 {
		dst.MinVersion = tls.VersionTLS12
	}
	if dst.Time == nil && wallClock != nil {
		dst.Time = wallClock
	}
	return dst
}

// NewHTTPTransport returns an *http.Transport whose TCP connections dial over
// link, supporting optional DNSResolver hostname lookup, pause-gate enforcement,
// and opt-in TLS configuration.
func NewHTTPTransport(link *Link, cfg TransportConfig) (*http.Transport, error) {
	if link == nil || link.state == nil || link.stack == nil {
		return nil, fmt.Errorf("%w: nil network link", ErrInvalidHTTPConfig)
	}
	if cfg.DialTimeout < 0 || cfg.MaxIdleConns < 0 || cfg.IdleConnTimeout < 0 || cfg.ResponseHeaderTimeout < 0 {
		return nil, fmt.Errorf("%w: negative transport duration or limit", ErrInvalidHTTPConfig)
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = defaultDialTimeout
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = defaultMaxIdleConns
	}
	idleTimeout := cfg.IdleConnTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleConnTimeout
	}
	respTimeout := cfg.ResponseHeaderTimeout
	if respTimeout == 0 {
		respTimeout = defaultResponseHeaderTimeout
	}

	tlsCfg := cloneTLSConfig(cfg.TLSConfig, cfg.WallClock)
	resolver := cfg.Resolver

	tr := &http.Transport{
		TLSClientConfig:       tlsCfg,
		MaxIdleConns:          maxIdle,
		MaxIdleConnsPerHost:   maxIdle,
		IdleConnTimeout:       idleTimeout,
		ResponseHeaderTimeout: respTimeout,
		DisableKeepAlives:     false,
		DialContext: func(ctx context.Context, networkName, addr string) (net.Conn, error) {
			if ctx == nil {
				ctx = context.Background()
			}
			host, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid target address %q: %v", ErrInvalidHTTPConfig, addr, err)
			}
			portU64, err := strconv.ParseUint(portStr, 10, 16)
			if err != nil || portU64 == 0 {
				return nil, fmt.Errorf("%w: invalid TCP port %q", ErrInvalidHTTPConfig, portStr)
			}
			dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()

			var addrs []netip.Addr
			if parsed, parseErr := netip.ParseAddr(host); parseErr == nil {
				if !parsed.Is4() {
					return nil, fmt.Errorf("%w: IPv6 address %q unsupported", ErrInvalidHTTPConfig, host)
				}
				addrs = []netip.Addr{parsed}
			} else {
				if resolver == nil {
					return nil, fmt.Errorf("%w: hostname %q requires DNSResolver", ErrInvalidHTTPConfig, host)
				}
				addrs, err = resolver.ResolveA(dialCtx, host)
				if err != nil {
					return nil, err
				}
			}

			var lastErr error
			for _, ip := range addrs {
				if !ip.Is4() {
					continue
				}
				conn, err := link.DialTCP(dialCtx, netip.AddrPortFrom(ip, uint16(portU64)))
				if err == nil {
					return conn, nil
				}
				lastErr = err
				if errors.Is(err, ErrPaused) {
					return nil, err
				}
			}
			if lastErr == nil {
				return nil, ErrDNSNoRecords
			}
			return nil, lastErr
		},
	}
	return tr, nil
}

// NewHTTPClient constructs an *http.Client using NewHTTPTransport(link, cfg).
func NewHTTPClient(link *Link, cfg TransportConfig) (*http.Client, error) {
	if cfg.ClientTimeout < 0 {
		return nil, fmt.Errorf("%w: negative client timeout", ErrInvalidHTTPConfig)
	}
	tr, err := NewHTTPTransport(link, cfg)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: tr,
		Timeout:   cfg.ClientTimeout,
	}, nil
}

// ServerConfig configures an HTTP or HTTPS server backed by a *Link listener.
type ServerConfig struct {
	Port              uint16           // Required non-zero TCP listen port
	Handler           http.Handler     // Required HTTP handler
	TLSConfig         *tls.Config      // Optional TLS server config (cloned; enables HTTPS when non-nil)
	WallClock         func() time.Time // Optional UTC clock for TLS certificate verification when TLSConfig.Time is nil
	ReadHeaderTimeout time.Duration    // Default 5s
	ReadTimeout       time.Duration    // Default 30s
	WriteTimeout      time.Duration    // Default 30s
	IdleTimeout       time.Duration    // Default 60s
	MaxHeaderBytes    int              // Default 64 KiB
	MaxConns          int              // Concurrent accepted connection limit (default 8; <0 invalid)
}

// ServerStats summarizes live HTTPServer state and connection telemetry.
type ServerStats struct {
	Listening     bool
	Addr          string
	TLS           bool
	ActiveConns   int64
	TotalConns    uint64
	RejectedConns uint64
	PausedRejects uint64
}

// HTTPServer hosts an http.Server over *Link.ListenTCP with bounded concurrency,
// pause-gate enforcement, optional TLS termination, and clean Shutdown/Close.
type HTTPServer struct {
	link *Link
	cfg  ServerConfig

	mu        sync.Mutex
	started   bool
	closed    bool
	srv       *http.Server
	ln        net.Listener
	addr      string
	hasTLS    bool
	doneCh    chan struct{}
	stopWatch context.CancelFunc
	serveErr  error

	listening     atomic.Bool
	activeConns   atomic.Int64
	totalConns    atomic.Uint64
	rejectedConns atomic.Uint64
	pausedRejects atomic.Uint64
}

// NewHTTPServer validates cfg and constructs an idle HTTPServer bound to link.
func NewHTTPServer(link *Link, cfg ServerConfig) (*HTTPServer, error) {
	if link == nil || link.state == nil || link.stack == nil {
		return nil, fmt.Errorf("%w: nil network link", ErrInvalidHTTPConfig)
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("%w: TCP listen port must be non-zero", ErrInvalidHTTPConfig)
	}
	if cfg.Handler == nil {
		return nil, fmt.Errorf("%w: nil HTTP handler", ErrInvalidHTTPConfig)
	}
	if cfg.ReadHeaderTimeout < 0 || cfg.ReadTimeout < 0 || cfg.WriteTimeout < 0 || cfg.IdleTimeout < 0 || cfg.MaxHeaderBytes < 0 || cfg.MaxConns < 0 {
		return nil, fmt.Errorf("%w: negative server duration or limit", ErrInvalidHTTPConfig)
	}
	if cfg.ReadHeaderTimeout == 0 {
		cfg.ReadHeaderTimeout = defaultServerReadHeaderTimeout
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = defaultServerReadTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = defaultServerWriteTimeout
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = defaultServerIdleTimeout
	}
	if cfg.MaxHeaderBytes == 0 {
		cfg.MaxHeaderBytes = defaultServerMaxHeaderBytes
	}
	if cfg.MaxConns == 0 {
		cfg.MaxConns = defaultServerMaxConns
	}
	if cfg.TLSConfig != nil {
		cfg.TLSConfig = cloneTLSConfig(cfg.TLSConfig, cfg.WallClock)
	}
	return &HTTPServer{
		link:   link,
		cfg:    cfg,
		hasTLS: cfg.TLSConfig != nil,
		doneCh: make(chan struct{}),
	}, nil
}

// Start opens the TCP listener on the configured port and begins serving HTTP
// (or HTTPS when TLSConfig is non-nil) in a background goroutine.
func (s *HTTPServer) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("%w: nil server", ErrInvalidHTTPConfig)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrServerStopped
	}
	if s.started {
		return ErrServerStarted
	}

	rawLn, err := s.link.ListenTCP(ctx, s.cfg.Port)
	if err != nil {
		return err
	}

	var ln net.Listener = &serverListener{
		inner:  rawLn,
		server: s,
	}
	if s.cfg.TLSConfig != nil {
		ln = tls.NewListener(ln, s.cfg.TLSConfig)
	}

	srv := &http.Server{
		Handler:           s.cfg.Handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
		MaxHeaderBytes:    s.cfg.MaxHeaderBytes,
	}

	watchCtx, stopWatch := context.WithCancel(context.Background())
	s.started = true
	s.srv = srv
	s.ln = ln
	s.addr = rawLn.Addr().String()
	s.stopWatch = stopWatch
	s.listening.Store(true)

	go func() {
		select {
		case <-ctx.Done():
			_ = srv.Close()
		case <-watchCtx.Done():
		}
	}()

	go func() {
		defer func() {
			s.listening.Store(false)
			stopWatch()
			close(s.doneCh)
		}()
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			s.mu.Lock()
			s.serveErr = err
			s.mu.Unlock()
		}
	}()

	return nil
}

// Addr returns the bound IP:port string once Start has succeeded, or "" otherwise.
func (s *HTTPServer) Addr() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Stats returns a snapshot of the server's state and connection telemetry.
func (s *HTTPServer) Stats() ServerStats {
	if s == nil {
		return ServerStats{}
	}
	return ServerStats{
		Listening:     s.listening.Load(),
		Addr:          s.Addr(),
		TLS:           s.hasTLS,
		ActiveConns:   s.activeConns.Load(),
		TotalConns:    s.totalConns.Load(),
		RejectedConns: s.rejectedConns.Load(),
		PausedRejects: s.pausedRejects.Load(),
	}
}

// Shutdown gracefully drains active connections within ctx and stops listening.
// Safe to call multiple times or before Start.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed && !s.started {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	srv := s.srv
	stopWatch := s.stopWatch
	started := s.started
	s.mu.Unlock()

	if !started {
		return nil
	}
	err := srv.Shutdown(ctx)
	if stopWatch != nil {
		stopWatch()
	}
	<-s.doneCh
	return err
}

// Close immediately closes the listener and active connections and waits for the
// server loop to exit. Safe to call multiple times or before Start.
func (s *HTTPServer) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed && !s.started {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	srv := s.srv
	stopWatch := s.stopWatch
	started := s.started
	s.mu.Unlock()

	if !started {
		return nil
	}
	err := srv.Close()
	if stopWatch != nil {
		stopWatch()
	}
	<-s.doneCh
	return err
}

type serverListener struct {
	inner  net.Listener
	server *HTTPServer
}

func (l *serverListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.inner.Accept()
		if err != nil {
			return nil, err
		}
		if availErr := l.server.link.Available(); availErr != nil {
			_ = conn.Close()
			if errors.Is(availErr, ErrPaused) {
				l.server.pausedRejects.Add(1)
				continue
			}
			l.server.rejectedConns.Add(1)
			return nil, availErr
		}
		for {
			cur := l.server.activeConns.Load()
			if cur >= int64(l.server.cfg.MaxConns) {
				_ = conn.Close()
				l.server.rejectedConns.Add(1)
				conn = nil
				break
			}
			if l.server.activeConns.CompareAndSwap(cur, cur+1) {
				l.server.totalConns.Add(1)
				return &trackedConn{Conn: conn, active: &l.server.activeConns}, nil
			}
		}
	}
}

func (l *serverListener) Close() error   { return l.inner.Close() }
func (l *serverListener) Addr() net.Addr { return l.inner.Addr() }

type trackedConn struct {
	net.Conn
	active *atomic.Int64
	once   sync.Once
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		c.active.Add(-1)
	})
	return err
}
