// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/soypat/lneto"
	gnet "github.com/usbarmory/go-net"
)

type memWireDev struct {
	mac [6]byte
	in  <-chan []byte
	out chan<- []byte
}

func (d memWireDev) Receive(buf []byte) (int, error) {
	select {
	case frame := <-d.in:
		return copy(buf, frame), nil
	default:
		return 0, nil
	}
}

func (d memWireDev) Transmit(frame []byte) error {
	select {
	case d.out <- bytes.Clone(frame):
		return nil
	default:
		return fmt.Errorf("wire queue full")
	}
}

type noopDMADev struct {
	memWireDev
}

func (d noopDMADev) Err() error { return nil }

func setupLnetoPair(t *testing.T) (linkA *Link, linkB *Link, ctrlA *Controller, cleanup func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	aToB := make(chan []byte, 256)
	bToA := make(chan []byte, 256)

	devA := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x01}, in: bToA, out: aToB}}
	devB := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x02}, in: aToB, out: bToA}}

	var err error
	linkA, err = NewLink(nil, devA, "192.0.2.10/24", "02:11:22:33:44:01", "")
	if err != nil {
		t.Fatal(err)
	}
	linkB, err = NewLink(nil, devB, "192.0.2.20/24", "02:11:22:33:44:02", "")
	if err != nil {
		t.Fatal(err)
	}
	ctrlA, err = NewController(linkA, DMAControl{
		Stop:  func(context.Context) error { return nil },
		Start: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = linkA.Start(ctx)
	}()
	go func() {
		defer wg.Done()
		_ = linkB.Start(ctx)
	}()

	return linkA, linkB, ctrlA, func() {
		cancel()
		wg.Wait()
	}
}

func makeTestCertificate(t *testing.T, dnsName string, notBefore, notAfter time.Time) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{dnsName},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        leaf,
	}, pool
}

func TestHTTPServerAndClientOverEthernetFramesAndPauseResume(t *testing.T) {
	linkServer, linkClient, ctrlServer, cleanup := setupLnetoPair(t)
	defer cleanup()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("POST /v1/echo", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = w.Write(body)
	})

	srv, err := NewHTTPServer(linkServer, ServerConfig{
		Port:     8080,
		Handler:  mux,
		MaxConns: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()

	if err := srv.Start(ctx); !errors.Is(err, ErrServerStarted) {
		t.Fatalf("second Start = %v, want ErrServerStarted", err)
	}
	if srv.Addr() != "192.0.2.10:8080" {
		t.Fatalf("srv.Addr() = %q, want 192.0.2.10:8080", srv.Addr())
	}
	if got := linkServer.Prefix().String(); got != "192.0.2.10/24" {
		t.Fatalf("linkServer.Prefix() = %q, want 192.0.2.10/24", got)
	}

	client, err := NewHTTPClient(linkClient, TransportConfig{
		DialTimeout:   5 * time.Second,
		ClientTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get("http://192.0.2.10:8080/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || string(body) != `{"ok":true}` {
		t.Fatalf("GET /v1/health status=%d body=%q err=%v", resp.StatusCode, string(body), err)
	}

	payload := bytes.Repeat([]byte("tamago-http-stream-"), 128)
	postResp, err := client.Post("http://192.0.2.10:8080/v1/echo", "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(postResp.Body)
	_ = postResp.Body.Close()
	if err != nil || postResp.StatusCode != http.StatusOK || !bytes.Equal(got, payload) {
		t.Fatalf("POST /v1/echo status=%d len=%d err=%v", postResp.StatusCode, len(got), err)
	}

	// Pause the server link: HTTP server stays alive while paused, then resumes serving.
	if err := ctrlServer.PauseNetwork(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := linkServer.DialTCP(ctx, netip.MustParseAddrPort("192.0.2.20:8080")); !errors.Is(err, ErrPaused) {
		t.Fatalf("DialTCP while paused = %v, want ErrPaused", err)
	}
	if err := ctrlServer.ResumeNetwork(ctx); err != nil {
		t.Fatal(err)
	}

	resp2, err := client.Get("http://192.0.2.10:8080/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("post-resume GET status=%d, want 200", resp2.StatusCode)
	}

	stats := srv.Stats()
	if !stats.Listening || stats.TLS || stats.TotalConns == 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(ctx); !errors.Is(err, ErrServerStopped) {
		t.Fatalf("Start after Close = %v, want ErrServerStopped", err)
	}
}

func TestHTTPSEndToEndWithDNSAndWallClock(t *testing.T) {
	linkServer, linkClient, _, cleanup := setupLnetoPair(t)
	defer cleanup()

	validTime := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	cert, pool := makeTestCertificate(t, "kit.fes.lan", validTime.Add(-24*time.Hour), validTime.Add(24*time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/secure", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tls-ok")
	})

	srv, err := NewHTTPServer(linkServer, ServerConfig{
		Port:      8443,
		Handler:   mux,
		WallClock: func() time.Time { return validTime },
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()

	if stats := srv.Stats(); !stats.TLS {
		t.Fatalf("expected stats.TLS=true, got %+v", stats)
	}

	resolver, err := NewDNSResolverFunc(func(ctx context.Context, name string) (DNSAnswer, error) {
		if name == "kit.fes.lan" {
			return DNSAnswer{Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.10")}, TTL: time.Minute}, nil
		}
		return DNSAnswer{}, ErrDNSNotFound
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewHTTPClient(linkClient, TransportConfig{
		Resolver:      resolver,
		WallClock:     func() time.Time { return validTime },
		DialTimeout:   5 * time.Second,
		ClientTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			RootCAs: pool,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get("https://kit.fes.lan:8443/v1/secure")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || string(body) != "tls-ok" {
		t.Fatalf("HTTPS GET failed: status=%d body=%q err=%v", resp.StatusCode, string(body), err)
	}

	// Verify expired WallClock rejects certificate verification.
	expiredClient, err := NewHTTPClient(linkClient, TransportConfig{
		Resolver:      resolver,
		WallClock:     func() time.Time { return validTime.Add(72 * time.Hour) },
		DialTimeout:   5 * time.Second,
		ClientTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			RootCAs: pool,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expiredClient.Get("https://kit.fes.lan:8443/v1/secure"); err == nil {
		t.Fatal("expected certificate expiry failure with shifted WallClock")
	}
}

func TestHTTPConfigValidation(t *testing.T) {
	link, _, _, cleanup := setupLnetoPair(t)
	defer cleanup()

	if _, err := NewHTTPTransport(nil, TransportConfig{}); !errors.Is(err, ErrInvalidHTTPConfig) {
		t.Fatalf("nil link transport = %v, want ErrInvalidHTTPConfig", err)
	}
	if _, err := NewHTTPTransport(link, TransportConfig{DialTimeout: -time.Second}); !errors.Is(err, ErrInvalidHTTPConfig) {
		t.Fatalf("negative dial timeout = %v, want ErrInvalidHTTPConfig", err)
	}
	if _, err := NewHTTPClient(link, TransportConfig{ClientTimeout: -time.Second}); !errors.Is(err, ErrInvalidHTTPConfig) {
		t.Fatalf("negative client timeout = %v, want ErrInvalidHTTPConfig", err)
	}
	if _, err := NewHTTPServer(nil, ServerConfig{Port: 80, Handler: http.NotFoundHandler()}); !errors.Is(err, ErrInvalidHTTPConfig) {
		t.Fatalf("nil link server = %v, want ErrInvalidHTTPConfig", err)
	}
	if _, err := NewHTTPServer(link, ServerConfig{Port: 0, Handler: http.NotFoundHandler()}); !errors.Is(err, ErrInvalidHTTPConfig) {
		t.Fatalf("zero port server = %v, want ErrInvalidHTTPConfig", err)
	}
	if _, err := NewHTTPServer(link, ServerConfig{Port: 80, Handler: nil}); !errors.Is(err, ErrInvalidHTTPConfig) {
		t.Fatalf("nil handler server = %v, want ErrInvalidHTTPConfig", err)
	}
	if _, err := NewHTTPServer(link, ServerConfig{Port: 80, Handler: http.NotFoundHandler(), MaxConns: -1}); !errors.Is(err, ErrInvalidHTTPConfig) {
		t.Fatalf("negative MaxConns = %v, want ErrInvalidHTTPConfig", err)
	}
}

func startAcceptDrain(ln net.Listener) (stop func()) {
	var mu sync.Mutex
	stopped := false
	conns := make(map[net.Conn]struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if stopped {
				mu.Unlock()
				_ = conn.Close()
				return
			}
			conns[conn] = struct{}{}
			mu.Unlock()
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer func() {
					mu.Lock()
					delete(conns, c)
					mu.Unlock()
				}()
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close()
			}(conn)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = ln.Close()
			mu.Lock()
			stopped = true
			active := make([]net.Conn, 0, len(conns))
			for c := range conns {
				active = append(active, c)
			}
			mu.Unlock()
			for _, c := range active {
				_ = c.SetReadDeadline(time.Now())
				_ = c.Close()
			}
			wg.Wait()
		})
	}
}

func TestListenerCloseConcurrentWithDialTCP(t *testing.T) {
	linkA, linkB, _, cleanup := setupLnetoPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lnB, err := linkB.ListenTCP(ctx, 9090)
	if err != nil {
		t.Fatalf("linkB.ListenTCP: %v", err)
	}
	stopAccept := startAcceptDrain(lnB)
	defer stopAccept()

	const iterations = 8
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := range iterations {
			ln, err := linkA.ListenTCP(ctx, uint16(10000+i))
			if err != nil {
				errCh <- fmt.Errorf("ListenTCP(%d): %w", 10000+i, err)
				return
			}
			if err := ln.Close(); err != nil {
				errCh <- fmt.Errorf("Close(%d): %w", 10000+i, err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		target := netip.MustParseAddrPort("192.0.2.20:9090")
		for i := range iterations {
			conn, err := linkA.DialTCP(ctx, target)
			if err != nil {
				errCh <- fmt.Errorf("DialTCP(%d): %w (linkA=%v linkB=%v)", i, err, linkA.Err(), linkB.Err())
				return
			}
			_ = conn.Close()
		}
	}()

	wg.Wait()
	if err := linkA.Err(); err != nil {
		t.Fatalf("linkA.Err() after concurrent dial/close = %v", err)
	}
	if err := linkB.Err(); err != nil {
		t.Fatalf("linkB.Err() after concurrent dial/close = %v", err)
	}
	stopAccept()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestListenerCloseDoesNotStallDuringSlowDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	aToB := make(chan []byte, 256)
	bToARaw := make(chan []byte, 256)
	bToA := make(chan []byte, 256)

	devA := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x01}, in: bToA, out: aToB}}
	devB := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x02}, in: aToB, out: bToARaw}}

	linkA, err := NewLink(nil, devA, "192.0.2.10/24", "02:11:22:33:44:01", "")
	if err != nil {
		t.Fatal(err)
	}
	linkB, err := NewLink(nil, devB, "192.0.2.20/24", "02:11:22:33:44:02", "")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		_ = linkA.Start(ctx)
	}()
	go func() {
		defer wg.Done()
		_ = linkB.Start(ctx)
	}()
	go func() {
		defer wg.Done()
		delayed := false
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-bToARaw:
				if !delayed {
					delayed = true
					timer := time.NewTimer(300 * time.Millisecond)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
				select {
				case <-ctx.Done():
					return
				case bToA <- frame:
				}
			}
		}
	}()
	defer func() {
		cancel()
		wg.Wait()
	}()

	lnB, err := linkB.ListenTCP(ctx, 9091)
	if err != nil {
		t.Fatalf("linkB.ListenTCP: %v", err)
	}
	stopAccept := startAcceptDrain(lnB)
	defer stopAccept()

	lnA, err := linkA.ListenTCP(ctx, 10099)
	if err != nil {
		t.Fatalf("linkA.ListenTCP: %v", err)
	}

	dialDone := make(chan error, 1)
	go func() {
		conn, err := linkA.DialTCP(ctx, netip.MustParseAddrPort("192.0.2.20:9091"))
		if conn != nil {
			_ = conn.Close()
		}
		dialDone <- err
	}()

	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	if err := lnA.Close(); err != nil {
		t.Fatalf("Close during slow DialTCP: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 150*time.Millisecond {
		t.Fatalf("Close stalled for %v while DialTCP was in progress", elapsed)
	}

	// Rebinding immediately must wait for the queued close rather than trying to
	// register a second listener on the still-owned port.
	rebindCtx, cancelRebind := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRebind()
	rebindStarted := time.Now()
	lnA2, err := linkA.ListenTCP(rebindCtx, 10099)
	if err != nil {
		t.Fatalf("immediate ListenTCP after deferred Close: %v", err)
	}
	if elapsed := time.Since(rebindStarted); elapsed >= 2*time.Second {
		t.Fatalf("immediate rebind exceeded its bound: %v", elapsed)
	}
	if err := <-dialDone; err != nil {
		t.Fatalf("slow DialTCP failed: %v", err)
	}
	_ = lnA2.Close()
	stopAccept()
}

func TestConcurrentListenerClosesDrainAndRebind(t *testing.T) {
	linkA, _, _, cleanup := setupLnetoPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := range 10 {
		ln1, err := linkA.ListenTCP(ctx, 10100)
		if err != nil {
			t.Fatalf("iter %d ListenTCP(10100): %v", i, err)
		}
		ln2, err := linkA.ListenTCP(ctx, 10101)
		if err != nil {
			t.Fatalf("iter %d ListenTCP(10101): %v", i, err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		errCh := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			errCh <- ln1.Close()
		}()
		go func() {
			defer wg.Done()
			<-start
			errCh <- ln2.Close()
		}()
		close(start)
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				t.Fatalf("iter %d concurrent Close: %v", i, err)
			}
		}
	}

	ln1, err := linkA.ListenTCP(ctx, 10100)
	if err != nil {
		t.Fatalf("final rebind ListenTCP(10100): %v", err)
	}
	_ = ln1.Close()
	ln2, err := linkA.ListenTCP(ctx, 10101)
	if err != nil {
		t.Fatalf("final rebind ListenTCP(10101): %v", err)
	}
	_ = ln2.Close()
}

func TestListenerCloseNotStarvedByOverlappingDials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	aToB := make(chan []byte, 256)
	bToARaw := make(chan []byte, 256)
	bToA := make(chan []byte, 256)

	devA := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x01}, in: bToA, out: aToB}}
	devB := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x02}, in: aToB, out: bToARaw}}

	cfgA := gnet.DefaultLnetoStackConfig()
	cfgA.MaxActiveTCPPorts = 64
	cfgB := gnet.DefaultLnetoStackConfig()
	cfgB.MaxActiveTCPPorts = 64
	cfgB.MaxListenerConns = 64

	linkA, err := NewLink(cfgA, devA, "192.0.2.10/24", "02:11:22:33:44:01", "")
	if err != nil {
		t.Fatal(err)
	}
	linkB, err := NewLink(cfgB, devB, "192.0.2.20/24", "02:11:22:33:44:02", "")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		_ = linkA.Start(ctx)
	}()
	go func() {
		defer wg.Done()
		_ = linkB.Start(ctx)
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-bToARaw:
				// Pace responses slightly so two dial workers overlap continuously without exhausting ports.
				timer := time.NewTimer(15 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				select {
				case <-ctx.Done():
					return
				case bToA <- frame:
				}
			}
		}
	}()
	defer func() {
		cancel()
		wg.Wait()
	}()

	lnB, err := linkB.ListenTCP(ctx, 9092)
	if err != nil {
		t.Fatalf("linkB.ListenTCP: %v", err)
	}
	stopAccept := startAcceptDrain(lnB)
	defer stopAccept()

	lnA, err := linkA.ListenTCP(ctx, 10102)
	if err != nil {
		t.Fatalf("linkA.ListenTCP(10102): %v", err)
	}

	dialCtx, stopDials := context.WithCancel(ctx)
	var dialWG sync.WaitGroup
	defer func() {
		stopDials()
		dialWG.Wait()
	}()

	target := netip.MustParseAddrPort("192.0.2.20:9092")
	for worker := range 2 {
		dialWG.Add(1)
		go func(delay time.Duration) {
			defer dialWG.Done()
			if delay > 0 {
				time.Sleep(delay)
			}
			for dialCtx.Err() == nil {
				conn, err := linkA.DialTCP(dialCtx, target)
				if err == nil {
					_ = conn.Close()
				}
			}
		}(time.Duration(worker) * 10 * time.Millisecond)
	}

	time.Sleep(20 * time.Millisecond)
	if err := lnA.Close(); err != nil {
		t.Fatalf("lnA.Close: %v", err)
	}

	// Port 10102 must be rebindable promptly even while overlapping dials continue.
	lnA2, err := linkA.ListenTCP(ctx, 10102)
	if err != nil {
		t.Fatalf("rebind ListenTCP(10102) while dials ongoing: %v", err)
	}
	_ = lnA2.Close()

	stopDials()
	dialWG.Wait()
	stopAccept()
}

func TestDialTCPAbortOnUnreachablePeerHasNoRace(t *testing.T) {
	linkA, _, _, cleanup := setupLnetoPair(t)
	defer cleanup()

	lnA, err := linkA.ListenTCP(context.Background(), 10103)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	dialErr := make(chan error, 1)
	go func() {
		conn, err := linkA.DialTCP(dialCtx, netip.MustParseAddrPort("192.0.2.99:9090"))
		if conn != nil {
			_ = conn.Close()
		}
		dialErr <- err
	}()

	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	if err := lnA.Close(); err != nil {
		t.Fatalf("Close during unreachable DialTCP: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("Close blocked for %v while DialTCP was active", elapsed)
	}

	if err := <-dialErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DialTCP error = %v, want context.DeadlineExceeded", err)
	}
}

func TestDialTCPToConfiguredGatewayIP(t *testing.T) {
	configs := []struct {
		name string
		cfg  func() *gnet.LnetoConfig
	}{
		{
			name: "default-backoff",
			cfg:  func() *gnet.LnetoConfig { return nil },
		},
		{
			name: "yield-backoff",
			cfg: func() *gnet.LnetoConfig {
				cfg := gnet.DefaultLnetoStackConfig()
				cfg.BackoffStack = lnetoYieldBackoff
				cfg.NewBackoffTCP = func() lneto.BackoffStrategy { return lnetoYieldBackoff }
				return cfg
			},
		},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			for iter := 0; iter < 3; iter++ {
				ctx, cancel := context.WithCancel(context.Background())

				guestToGW := make(chan []byte, 256)
				gwToGuest := make(chan []byte, 256)
				devGuest := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x55, 0x01}, in: gwToGuest, out: guestToGW}}
				devGW := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x55, 0x02}, in: guestToGW, out: gwToGuest}}

				guestLink, err := NewLink(tc.cfg(), devGuest, "10.0.2.15/24", "02:11:22:33:55:01", "10.0.2.2")
				if err != nil {
					cancel()
					t.Fatalf("NewLink(guest): %v", err)
				}
				gwLink, err := NewLink(tc.cfg(), devGW, "10.0.2.2/24", "02:11:22:33:55:02", "")
				if err != nil {
					cancel()
					t.Fatalf("NewLink(gw): %v", err)
				}

				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					_ = guestLink.Start(ctx)
				}()
				go func() {
					defer wg.Done()
					_ = gwLink.Start(ctx)
				}()

				ln, err := gwLink.ListenTCP(ctx, 18080)
				if err != nil {
					cancel()
					wg.Wait()
					t.Fatalf("gwLink.ListenTCP: %v", err)
				}
				mux := http.NewServeMux()
				mux.HandleFunc("/api/v1/games", func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(`{"games":[{"id":"aster"}]}`))
				})
				srv := &http.Server{Handler: mux}
				srvDone := make(chan error, 1)
				go func() {
					srvDone <- srv.Serve(ln)
				}()

				client, err := NewHTTPClient(guestLink, TransportConfig{
					DialTimeout:           3 * time.Second,
					ResponseHeaderTimeout: 3 * time.Second,
				})
				if err != nil {
					_ = ln.Close()
					cancel()
					wg.Wait()
					t.Fatalf("NewHTTPClient: %v", err)
				}

				reqCtx, reqCancel := context.WithTimeout(ctx, 3*time.Second)
				req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://10.0.2.2:18080/api/v1/games", nil)
				if err != nil {
					reqCancel()
					_ = ln.Close()
					cancel()
					wg.Wait()
					t.Fatalf("NewRequest: %v", err)
				}
				resp, err := client.Do(req)
				reqCancel()
				if err != nil {
					client.CloseIdleConnections()
					_ = srv.Close()
					<-srvDone
					cancel()
					wg.Wait()
					t.Fatalf("iter %d client.Do(10.0.2.2:18080): %v", iter, err)
				}
				body, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				client.CloseIdleConnections()
				_ = srv.Close()
				<-srvDone
				cancel()
				wg.Wait()
				if readErr != nil || resp.StatusCode != http.StatusOK || string(body) != `{"games":[{"id":"aster"}]}` {
					t.Fatalf("iter %d unexpected HTTP response: status=%d body=%q err=%v", iter, resp.StatusCode, string(body), readErr)
				}
			}
		})
	}
}

func TestAcceptedConnPinPreventsReuseWhileWrapperOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	aToB := make(chan []byte, 256)
	bToA := make(chan []byte, 256)
	devA := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x66, 0x01}, in: bToA, out: aToB}}
	devB := noopDMADev{memWireDev{mac: [6]byte{0x02, 0x11, 0x22, 0x33, 0x66, 0x02}, in: aToB, out: bToA}}

	cfgB := gnet.DefaultLnetoStackConfig()
	cfgB.MaxListenerConns = 1

	linkA, err := NewLink(nil, devA, "192.0.2.10/24", "02:11:22:33:66:01", "")
	if err != nil {
		t.Fatalf("NewLink(A): %v", err)
	}
	linkB, err := NewLink(cfgB, devB, "192.0.2.20/24", "02:11:22:33:66:02", "")
	if err != nil {
		t.Fatalf("NewLink(B): %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = linkA.Start(ctx) }()
	go func() { defer wg.Done(); _ = linkB.Start(ctx) }()
	defer func() {
		cancel()
		wg.Wait()
	}()

	lnB, err := linkB.ListenTCP(ctx, 10104)
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer func() { _ = lnB.Close() }()

	acceptCh := make(chan net.Conn, 2)
	acceptErrCh := make(chan error, 1)
	go func() {
		for {
			c, err := lnB.Accept()
			if err != nil {
				acceptErrCh <- err
				return
			}
			acceptCh <- c
		}
	}()

	target := netip.MustParseAddrPort("192.0.2.20:10104")
	cli1, err := linkA.DialTCP(ctx, target)
	if err != nil {
		t.Fatalf("DialTCP(1): %v", err)
	}
	srv1 := <-acceptCh
	if _, err := cli1.Write([]byte("ping1")); err != nil {
		t.Fatalf("cli1.Write: %v", err)
	}
	buf := make([]byte, 16)
	n, err := srv1.Read(buf)
	if err != nil || string(buf[:n]) != "ping1" {
		t.Fatalf("srv1.Read = (%q, %v), want (%q, nil)", string(buf[:n]), err, "ping1")
	}
	_ = cli1.Close()

	wrapped := lnB.(*lnetoListener)
	raw1 := srv1.(*lnetoConn).LnetoConn()

	// Force raw1 closed (as lneto TCPPool timeout does) so listener.Encapsulate ->
	// returnAccepted invokes wrapped.pool.PutTCP(raw1) while srv1 is still open.
	linkB.engine.mu.Lock()
	raw1.Abort()
	linkB.engine.mu.Unlock()
	linkB.engine.interruptBackoff()

	deadline := time.Now().Add(2 * time.Second)
	for {
		wrapped.pool.mu.Lock()
		lease := wrapped.pool.leases[raw1]
		wrapped.pool.mu.Unlock()
		returned := false
		if lease != nil {
			lease.mu.Lock()
			returned = lease.poolReturned
			lease.mu.Unlock()
		}
		if returned {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for listener to return aborted conn to pinned pool")
		}
		time.Sleep(time.Millisecond)
	}

	if got := wrapped.pool.inner.NumberOfAcquired(); got != 1 {
		t.Fatalf("NumberOfAcquired while srv1 open = %d, want 1", got)
	}
	if _, err := srv1.Read(buf); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("stale srv1.Read = %v, want net.ErrClosed", err)
	}
	if _, err := srv1.Write([]byte("x")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("stale srv1.Write = %v, want net.ErrClosed", err)
	}
	if err := srv1.SetReadDeadline(time.Now().Add(time.Second)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("stale srv1.SetReadDeadline = %v, want net.ErrClosed", err)
	}

	// Closing srv1 releases the pinned slot so Client 2 can reuse it cleanly.
	if err := srv1.Close(); err != nil {
		t.Fatalf("srv1.Close: %v", err)
	}
	if got := wrapped.pool.inner.NumberOfAcquired(); got != 0 {
		t.Fatalf("NumberOfAcquired after srv1.Close = %d, want 0", got)
	}

	cli2, err := linkA.DialTCP(ctx, target)
	if err != nil {
		t.Fatalf("DialTCP(2): %v", err)
	}
	defer func() { _ = cli2.Close() }()
	srv2 := <-acceptCh
	defer func() { _ = srv2.Close() }()

	// Hammer stale srv1 concurrently while srv2 exchanges data on the reused slot.
	hammerCtx, stopHammer := context.WithCancel(ctx)
	var hammerWG sync.WaitGroup
	hammerWG.Add(1)
	go func() {
		defer hammerWG.Done()
		tmp := make([]byte, 8)
		for hammerCtx.Err() == nil {
			_, _ = srv1.Read(tmp)
			_, _ = srv1.Write([]byte("stale"))
			_ = srv1.SetDeadline(time.Now().Add(time.Millisecond))
			_ = srv1.Close()
		}
	}()

	if _, err := cli2.Write([]byte("ping2")); err != nil {
		stopHammer()
		hammerWG.Wait()
		t.Fatalf("cli2.Write: %v", err)
	}
	n, err = srv2.Read(buf)
	if err != nil || string(buf[:n]) != "ping2" {
		stopHammer()
		hammerWG.Wait()
		t.Fatalf("srv2.Read = (%q, %v), want (%q, nil)", string(buf[:n]), err, "ping2")
	}
	if _, err := srv2.Write([]byte("pong2")); err != nil {
		stopHammer()
		hammerWG.Wait()
		t.Fatalf("srv2.Write: %v", err)
	}
	n, err = cli2.Read(buf)
	stopHammer()
	hammerWG.Wait()
	if err != nil || string(buf[:n]) != "pong2" {
		t.Fatalf("cli2.Read = (%q, %v), want (%q, nil)", string(buf[:n]), err, "pong2")
	}
}
