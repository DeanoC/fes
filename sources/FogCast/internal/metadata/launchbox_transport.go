package metadata

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	launchBoxArchiveOrigin = "https://gamesdb.launchbox-app.com/Metadata.zip"
	launchBoxImageOrigin   = "https://images.launchbox-app.com/"
	launchBoxArchiveHost   = "gamesdb.launchbox-app.com"
	launchBoxImageHost     = "images.launchbox-app.com"
	launchBoxHTTPSPort     = 443
)

// launchBoxTransportPolicy is deliberately private. Production callers cannot
// replace its origins, resolver, dialer, trust roots, or admission rules.
type launchBoxTransportPolicy struct {
	client      *http.Client
	transport   *http.Transport
	archiveURL  url.URL
	imageOrigin url.URL

	archiveHost string
	imageHost   string
	archivePort string
	imagePort   string

	// allowLoopback exists only on same-package loopback fixtures. Production
	// construction always leaves it false.
	allowLoopback bool
	closeOnce     sync.Once
}

func (p *launchBoxTransportPolicy) Close() error {
	if p == nil || p.transport == nil {
		return nil
	}
	p.closeOnce.Do(p.transport.CloseIdleConnections)
	return nil
}

func newProductionLaunchBoxTransportPolicy() *launchBoxTransportPolicy {
	return newLaunchBoxTransportPolicy(net.DefaultResolver, (&net.Dialer{
		Timeout:   providerRequestTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext, false)
}

// newLaunchBoxTransportPolicyWithNetwork is package-private and intentionally
// narrow. It exists for deterministic same-package transport tests; production
// construction uses newProductionLaunchBoxTransportPolicy.
func newLaunchBoxTransportPolicyWithNetwork(resolver ipResolver, dialer dialContextFunc) *launchBoxTransportPolicy {
	return newLaunchBoxTransportPolicy(resolver, dialer, false)
}

func newLaunchBoxTransportPolicy(resolver ipResolver, dialer dialContextFunc, allowLoopback bool) *launchBoxTransportPolicy {
	archive, _ := url.Parse(launchBoxArchiveOrigin)
	image, _ := url.Parse(launchBoxImageOrigin)
	transport := newLaunchBoxTransport(resolver, dialer, allowLoopback)
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("LaunchBox redirects are not allowed")
		},
		Jar:     nil,
		Timeout: 0,
	}
	return &launchBoxTransportPolicy{
		client:        client,
		transport:     transport,
		archiveURL:    *archive,
		imageOrigin:   *image,
		archiveHost:   launchBoxArchiveHost,
		imageHost:     launchBoxImageHost,
		archivePort:   archive.Port(),
		imagePort:     image.Port(),
		allowLoopback: allowLoopback,
	}
}

func newLaunchBoxTransport(resolver ipResolver, dialer dialContextFunc, allowLoopback bool) *http.Transport {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = (&net.Dialer{Timeout: providerRequestTimeout, KeepAlive: 30 * time.Second}).DialContext
	}
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   providerRequestTimeout,
		ResponseHeaderTimeout: providerRequestTimeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" && network != "tcp4" && network != "tcp6" {
				return nil, errors.New("LaunchBox network is not allowed")
			}
			host, port, err := net.SplitHostPort(address)
			if err != nil || host == "" || port == "" {
				return nil, errors.New("LaunchBox dial address is invalid")
			}
			if !isAllowedLaunchBoxHost(host) && !allowLoopback {
				return nil, errors.New("LaunchBox host is not allowlisted")
			}
			portNumber, err := strconv.Atoi(port)
			if err != nil || portNumber <= 0 || portNumber > 65535 || (!allowLoopback && port != strconv.Itoa(launchBoxHTTPSPort)) {
				return nil, errors.New("LaunchBox dial port is not allowed")
			}
			answers, err := resolver.LookupIPAddr(ctx, host)
			if err != nil || len(answers) == 0 {
				return nil, errors.New("LaunchBox DNS resolution failed")
			}
			for _, answer := range answers {
				if allowLoopback {
					if !isAllowedLaunchBoxLoopbackIP(answer.IP) {
						return nil, errors.New("LaunchBox DNS answer is not allowed")
					}
					continue
				}
				if !isAllowedLaunchBoxIP(answer.IP) {
					return nil, errors.New("LaunchBox DNS answer is not allowed")
				}
			}
			return dialer(ctx, network, net.JoinHostPort(answers[0].IP.String(), port))
		},
	}
}

func isAllowedLaunchBoxHost(host string) bool {
	if strings.ContainsAny(host, "[]%") {
		return false
	}
	switch strings.ToLower(host) {
	case launchBoxArchiveHost, launchBoxImageHost:
		return true
	default:
		return false
	}
}

var launchBoxPublicIPv6 = netip.MustParsePrefix("2000::/3")

// These are the current IANA special-purpose blocks that must not be
// admitted as public destinations. Positive admission is intentional: an
// address is usable only when it is in 2000::/3 and outside every reviewed
// special block. The broader blocks cover the more-specific IANA allocations
// within them, while the out-of-range entries document the complete registry
// boundary used by this policy.
var launchBoxIPv6SpecialPurpose = [...]netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	// IANA IPv6 Special-Purpose Address Space entries that fall within
	// 2000::/3. Keep the positive 2000::/3 admission below in addition to
	// this registry-derived exclusion so newly assigned non-public ranges
	// cannot escape the public-unicast boundary.
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:3::/32"),
	netip.MustParsePrefix("2001:4:112::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:30::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

func isAllowedLaunchBoxIP(ip net.IP) bool {
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	if addr.Is4() {
		value := addr.As4()
		return !launchBoxInIPv4Range(value, [4]byte{0, 0, 0, 0}, 8) &&
			!launchBoxInIPv4Range(value, [4]byte{100, 64, 0, 0}, 10) &&
			!launchBoxInIPv4Range(value, [4]byte{127, 0, 0, 0}, 8) &&
			!launchBoxInIPv4Range(value, [4]byte{169, 254, 0, 0}, 16) &&
			!launchBoxInIPv4Range(value, [4]byte{192, 0, 0, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{192, 31, 196, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{192, 52, 193, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{192, 0, 2, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{192, 88, 99, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{198, 18, 0, 0}, 15) &&
			!launchBoxInIPv4Range(value, [4]byte{198, 51, 100, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{192, 175, 48, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{203, 0, 113, 0}, 24) &&
			!launchBoxInIPv4Range(value, [4]byte{224, 0, 0, 0}, 4) &&
			!launchBoxInIPv4Range(value, [4]byte{240, 0, 0, 0}, 4)
	}
	if !launchBoxPublicIPv6.Contains(addr) {
		return false
	}
	for _, prefix := range launchBoxIPv6SpecialPurpose {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func isAllowedLaunchBoxLoopbackIP(ip net.IP) bool {
	return ip != nil && ip.IsLoopback()
}

func launchBoxInIPv4Range(value [4]byte, network [4]byte, bits int) bool {
	mask := uint32(0xffffffff) << (32 - bits)
	toUint32 := func(input [4]byte) uint32 {
		return uint32(input[0])<<24 | uint32(input[1])<<16 | uint32(input[2])<<8 | uint32(input[3])
	}
	return toUint32(value)&mask == toUint32(network)&mask
}

func (p *launchBoxTransportPolicy) newArchiveRequest(ctx context.Context) (*http.Request, error) {
	if p == nil || p.client == nil || p.transport == nil || p.archiveURL.Scheme == "" {
		return nil, errors.New("LaunchBox transport policy is unavailable")
	}
	if err := p.validateOrigin(p.archiveURL, p.archiveHost, "/Metadata.zip", p.archivePort); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, p.archiveURL.String(), nil)
}

func (p *launchBoxTransportPolicy) newImageRequest(ctx context.Context, filename string) (*http.Request, error) {
	if p == nil || p.client == nil || p.transport == nil || p.imageOrigin.Scheme == "" {
		return nil, errors.New("LaunchBox transport policy is unavailable")
	}
	if !validLaunchBoxImageFilename(filename) {
		return nil, newOpError(ErrInvalidResponse, nil)
	}
	imageURL := p.imageOrigin
	imageURL.Path = "/" + filename
	imageURL.RawPath = ""
	if err := p.validateOrigin(imageURL, p.imageHost, "/"+filename, p.imagePort); err != nil {
		return nil, err
	}
	if imageURL.EscapedPath() != "/"+url.PathEscape(filename) || imageURL.RawQuery != "" || imageURL.ForceQuery || imageURL.Fragment != "" || imageURL.User != nil {
		return nil, newOpError(ErrPolicyBlocked, nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, imageURL.String(), nil)
}

func (p *launchBoxTransportPolicy) validateOrigin(value url.URL, host, path, explicitPort string) error {
	if value.Scheme != "https" || value.Opaque != "" || value.User != nil || value.Fragment != "" || value.RawQuery != "" || value.ForceQuery || value.RawPath != "" {
		return newOpError(ErrPolicyBlocked, nil)
	}
	if !strings.EqualFold(value.Hostname(), host) || value.Path != path || (!isAllowedLaunchBoxHost(value.Hostname()) && !p.allowLoopback) {
		return newOpError(ErrPolicyBlocked, nil)
	}
	if p.allowLoopback {
		if value.Port() == "" {
			return newOpError(ErrPolicyBlocked, nil)
		}
		return nil
	}
	if value.Port() != explicitPort || (explicitPort != "" && value.Port() != strconv.Itoa(launchBoxHTTPSPort)) {
		return newOpError(ErrPolicyBlocked, nil)
	}
	return nil
}

func (p *launchBoxTransportPolicy) validateRequest(request *http.Request) error {
	if p == nil || request == nil || request.URL == nil || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.Fragment != "" || request.URL.Opaque != "" {
		return newOpError(ErrPolicyBlocked, nil)
	}
	host := request.URL.Hostname()
	switch {
	case strings.EqualFold(host, p.archiveHost):
		return p.validateOrigin(*request.URL, p.archiveHost, "/Metadata.zip", p.archivePort)
	case strings.EqualFold(host, p.imageHost):
		filename := strings.TrimPrefix(request.URL.Path, "/")
		if strings.Count(request.URL.Path, "/") != 1 || !validLaunchBoxImageFilename(filename) {
			return newOpError(ErrPolicyBlocked, nil)
		}
		return p.validateOrigin(*request.URL, p.imageHost, request.URL.Path, p.imagePort)
	default:
		return newOpError(ErrPolicyBlocked, nil)
	}
}

func validLaunchBoxImageFilename(filename string) bool {
	if filename == "" || !utf8.ValidString(filename) || len(filename) > 44 {
		return false
	}
	if !isLaunchBoxFilenameByte(filename[0]) || !strings.Contains(filename, ".") {
		return false
	}
	dot := strings.LastIndexByte(filename, '.')
	if dot < 1 || dot > 39 || dot == len(filename)-1 {
		return false
	}
	extension := filename[dot+1:]
	switch extension {
	case "jpg", "jpeg", "png", "gif", "webp":
	default:
		return false
	}
	for index := 0; index < dot; index++ {
		value := filename[index]
		if !isLaunchBoxFilenameByte(value) {
			return false
		}
	}
	return true
}

func isLaunchBoxFilenameByte(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_' || value == '-'
}

// launchBoxRequestGate is the only request capability a worker may receive.
// It starts revoked, is attached only after activation commits, and rejects
// every request that is not one of the fixed LaunchBox origins/shapes.
type launchBoxRequestGate struct {
	mu     sync.RWMutex
	policy *launchBoxTransportPolicy
}

func newLaunchBoxRequestGate() *launchBoxRequestGate {
	return &launchBoxRequestGate{}
}

func (g *launchBoxRequestGate) attach(policy *launchBoxTransportPolicy) {
	g.mu.Lock()
	g.policy = policy
	g.mu.Unlock()
}

func (g *launchBoxRequestGate) revoke() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.policy = nil
	g.mu.Unlock()
}

func (g *launchBoxRequestGate) Do(request *http.Request) (*http.Response, error) {
	if g == nil {
		return nil, errors.New("LaunchBox request gate is revoked")
	}
	g.mu.RLock()
	policy := g.policy
	if policy == nil {
		g.mu.RUnlock()
		return nil, errors.New("LaunchBox request gate is not committed")
	}
	if err := policy.validateRequest(request); err != nil {
		g.mu.RUnlock()
		return nil, err
	}
	client := policy.client
	g.mu.RUnlock()
	if client == nil {
		return nil, errors.New("LaunchBox request gate is unavailable")
	}
	return client.Do(request)
}

func (g *launchBoxRequestGate) newArchiveRequest(ctx context.Context) (*http.Request, error) {
	g.mu.RLock()
	policy := g.policy
	g.mu.RUnlock()
	if policy == nil {
		return nil, errors.New("LaunchBox request gate is not committed")
	}
	return policy.newArchiveRequest(ctx)
}

func (g *launchBoxRequestGate) newImageRequest(ctx context.Context, filename string) (*http.Request, error) {
	g.mu.RLock()
	policy := g.policy
	g.mu.RUnlock()
	if policy == nil {
		return nil, errors.New("LaunchBox request gate is not committed")
	}
	return policy.newImageRequest(ctx, filename)
}
