package metadata

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	providerRequestTimeout = 5 * time.Second
	tokenRequestTimeout    = 3 * time.Second
	lookupTimeout          = 8 * time.Second
	maxTokenBody           = 16 << 10
	maxProviderBody        = 512 << 10
	maxArtworkCompressed   = 8 << 20
)

// HTTPDoer is deliberately small so protocol tests can use a fixture transport
// without changing production origins or routing policy.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

// NewHardenedHTTPClient returns the production client used for all provider
// requests. It has no ambient proxy, cookie jar, redirect following, or TLS
// verification bypass.
func NewHardenedHTTPClient() *http.Client {
	return &http.Client{
		Transport: newHardenedTransport(net.DefaultResolver, (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Jar:     nil,
		Timeout: 0,
	}
}

func newHardenedTransport(resolver ipResolver, dialer dialContextFunc) *http.Transport {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || host == "" || port == "" {
				return nil, errors.New("provider dial address is invalid")
			}
			if !isAllowedProviderHost(host) {
				return nil, errors.New("provider host is not allowlisted")
			}
			portNumber, err := strconv.Atoi(port)
			if err != nil || portNumber != 443 {
				return nil, errors.New("provider dial port is not allowed")
			}
			answers, err := resolver.LookupIPAddr(ctx, host)
			if err != nil || len(answers) == 0 {
				return nil, errors.New("provider DNS resolution failed")
			}
			for _, answer := range answers {
				if !isAllowedProviderIP(answer.IP) {
					return nil, errors.New("provider DNS answer is not allowed")
				}
			}
			// Dial the first validated answer. The resolver is called for every
			// new connection; the HTTP transport retains the original hostname
			// for Host and TLS ServerName.
			return dialer(ctx, network, net.JoinHostPort(answers[0].IP.String(), port))
		},
	}
}

func isAllowedProviderIP(ip net.IP) bool {
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	if addr.Is4() {
		value := addr.As4()
		return !inIPv4Range(value, [4]byte{0, 0, 0, 0}, 8) &&
			!inIPv4Range(value, [4]byte{100, 64, 0, 0}, 10) &&
			!inIPv4Range(value, [4]byte{127, 0, 0, 0}, 8) &&
			!inIPv4Range(value, [4]byte{169, 254, 0, 0}, 16) &&
			!inIPv4Range(value, [4]byte{192, 0, 0, 0}, 24) &&
			!inIPv4Range(value, [4]byte{192, 0, 2, 0}, 24) &&
			!inIPv4Range(value, [4]byte{192, 88, 99, 0}, 24) &&
			!inIPv4Range(value, [4]byte{198, 18, 0, 0}, 15) &&
			!inIPv4Range(value, [4]byte{198, 51, 100, 0}, 24) &&
			!inIPv4Range(value, [4]byte{203, 0, 113, 0}, 24) &&
			!inIPv4Range(value, [4]byte{224, 0, 0, 0}, 4) &&
			!inIPv4Range(value, [4]byte{240, 0, 0, 0}, 4)
	}
	if addr == netip.MustParseAddr("::") || addr == netip.MustParseAddr("::1") {
		return false
	}
	for _, prefix := range []string{"2001:db8::/32", "2001:10::/28", "2001:2::/48", "3fff::/20"} {
		if netip.MustParsePrefix(prefix).Contains(addr) {
			return false
		}
	}
	if strings.HasPrefix(addr.String(), "2001:db8:") {
		return false
	}
	return true
}

func isAllowedProviderHost(host string) bool {
	host = strings.ToLower(host)
	switch host {
	case "id.twitch.tv", "api.igdb.com", "images.igdb.com":
		return true
	default:
		return false
	}
}

func inIPv4Range(value [4]byte, network [4]byte, bits int) bool {
	mask := uint32(0xffffffff) << (32 - bits)
	toUint32 := func(input [4]byte) uint32 {
		return uint32(input[0])<<24 | uint32(input[1])<<16 | uint32(input[2])<<8 | uint32(input[3])
	}
	return toUint32(value)&mask == toUint32(network)&mask
}

type requestLimiter struct {
	sem      chan struct{}
	mu       sync.Mutex
	last     time.Time
	interval time.Duration
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{sem: make(chan struct{}, 4), interval: 250 * time.Millisecond}
}

func (l *requestLimiter) acquire(ctx context.Context) error {
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		l.mu.Lock()
		wait := time.Duration(0)
		if !l.last.IsZero() {
			wait = l.interval - time.Since(l.last)
			if wait < 0 {
				wait = 0
			}
		}
		if wait == 0 {
			l.last = time.Now()
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			l.release()
			return ctx.Err()
		}
	}
}

func (l *requestLimiter) release() {
	select {
	case <-l.sem:
	default:
	}
}

func mapContextError(err error) *OpError {
	switch {
	case errors.Is(err, context.Canceled):
		return newOpError(ErrCanceled, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return newOpError(ErrDeadline, context.DeadlineExceeded)
	default:
		return newOpError(ErrUpstreamUnavailable, nil)
	}
}

func validateFixedOrigin(raw string, host string, path string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.Opaque != "" {
		return errors.New("provider origin is invalid")
	}
	if !strings.EqualFold(parsed.Hostname(), host) || (parsed.Port() != "" && parsed.Port() != "443") || parsed.Path != path || parsed.RawPath != "" {
		return fmt.Errorf("provider origin is not allowlisted")
	}
	return nil
}
