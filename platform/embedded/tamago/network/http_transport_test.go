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
	"net/http"
	"net/netip"
	"sync"
	"testing"
	"time"

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

	stackA := gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig())
	stackB := gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig())

	var err error
	linkA, err = NewLink(stackA, devA, "192.0.2.10/24", "02:11:22:33:44:01", "")
	if err != nil {
		t.Fatal(err)
	}
	linkB, err = NewLink(stackB, devB, "192.0.2.20/24", "02:11:22:33:44:02", "")
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

func TestListenerCloseConcurrentWithDialTCP(t *testing.T) {
	linkA, linkB, _, cleanup := setupLnetoPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lnB, err := linkB.ListenTCP(ctx, 9090)
	if err != nil {
		t.Fatalf("linkB.ListenTCP: %v", err)
	}
	var acceptWG sync.WaitGroup
	defer acceptWG.Wait()
	defer func() { _ = lnB.Close() }()

	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		for {
			conn, err := lnB.Accept()
			if err != nil {
				return
			}
			acceptWG.Add(1)
			go func() {
				c := conn
				defer acceptWG.Done()
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close()
			}()
		}
	}()

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
				errCh <- fmt.Errorf("DialTCP(%d): %w", i, err)
				return
			}
			_ = conn.Close()
		}
	}()

	wg.Wait()
	_ = lnB.Close()
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

	stackA := gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig())
	stackB := gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig())

	linkA, err := NewLink(stackA, devA, "192.0.2.10/24", "02:11:22:33:44:01", "")
	if err != nil {
		t.Fatal(err)
	}
	linkB, err := NewLink(stackB, devB, "192.0.2.20/24", "02:11:22:33:44:02", "")
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
	var acceptWG sync.WaitGroup
	defer acceptWG.Wait()
	defer func() { _ = lnB.Close() }()

	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		for {
			conn, err := lnB.Accept()
			if err != nil {
				return
			}
			acceptWG.Add(1)
			go func() {
				c := conn
				defer acceptWG.Done()
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close()
			}()
		}
	}()

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

	if err := <-dialDone; err != nil {
		t.Fatalf("slow DialTCP failed: %v", err)
	}

	// Deferred listener close should have drained when DialTCP returned, freeing port 10099.
	lnA2, err := linkA.ListenTCP(ctx, 10099)
	if err != nil {
		t.Fatalf("ListenTCP after deferred Close drain: %v", err)
	}
	_ = lnA2.Close()
	_ = lnB.Close()
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
