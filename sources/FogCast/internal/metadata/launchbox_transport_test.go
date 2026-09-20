package metadata

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLaunchBoxProductionTransportPolicyIsFixedAndSealed(t *testing.T) {
	policy := newProductionLaunchBoxTransportPolicy()
	if policy == nil || policy.client == nil || policy.transport == nil {
		t.Fatal("production LaunchBox policy is incomplete")
	}
	if got := policy.archiveURL.String(); got != "https://gamesdb.launchbox-app.com/Metadata.zip" {
		t.Fatalf("archive origin = %q", got)
	}
	if got := policy.imageOrigin.String(); got != "https://images.launchbox-app.com/" {
		t.Fatalf("image origin = %q", got)
	}
	if policy.client.Transport != policy.transport {
		t.Fatal("production client does not own the sealed transport")
	}
	if policy.transport.Proxy != nil {
		t.Fatal("production LaunchBox transport inherited an ambient proxy")
	}
	if policy.client.Jar != nil {
		t.Fatal("production LaunchBox client has a cookie jar")
	}
	if policy.client.CheckRedirect == nil {
		t.Fatal("production LaunchBox client permits implicit redirects")
	}
	if policy.transport.TLSClientConfig == nil || policy.transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("production LaunchBox TLS minimum is not TLS 1.2")
	}
	if policy.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("production LaunchBox TLS verification is disabled")
	}
}

func TestLaunchBoxTransportBuildsOnlyFixedArchiveAndEscapedImageRequests(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	policy := loopbackLaunchBoxPolicy(t, server)

	archive, err := policy.newArchiveRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if archive.Method != http.MethodGet || archive.URL.Path != "/Metadata.zip" || archive.URL.RawQuery != "" || archive.URL.User != nil {
		t.Fatalf("archive request = %#v", archive.URL)
	}
	if archive.Header.Get("Authorization") != "" || archive.Header.Get("Cookie") != "" {
		t.Fatal("archive request carries credentials")
	}

	image, err := policy.newImageRequest(context.Background(), "cover_01.jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if image.Method != http.MethodGet || image.URL.Path != "/cover_01.jpeg" || image.URL.RawQuery != "" || image.URL.Fragment != "" || image.URL.User != nil {
		t.Fatalf("image request = %#v", image.URL)
	}
	if got, want := image.URL.EscapedPath(), "/cover_01.jpeg"; got != want {
		t.Fatalf("escaped image path = %q, want %q", got, want)
	}
}

func TestLaunchBoxTransportRejectsInvalidImageFilenamesBeforeRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid filename reached the HTTP handler")
	}))
	defer server.Close()
	policy := loopbackLaunchBoxPolicy(t, server)
	for _, filename := range []string{
		"",
		"a",
		"A" + strings.Repeat("x", 39) + ".jpg",
		"cover.JPG",
		"cover.bmp",
		"../cover.jpg",
		"cover/part.jpg",
		"cover%2fpart.jpg",
		"cover%2Fpart.jpg",
		"cover?.jpg",
		"cover#.jpg",
		"cover file.jpg",
		"é.jpg",
		string([]byte{'c', 0xff, '.', 'j', 'p', 'g'}),
	} {
		if _, err := policy.newImageRequest(context.Background(), filename); err == nil {
			t.Fatalf("invalid image filename accepted: %q", filename)
		}
	}
}

func TestLaunchBoxTransportRefusesRedirects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/Metadata.zip" {
			http.Redirect(w, r, "/redirected", http.StatusFound)
			return
		}
		t.Fatal("redirect target was requested")
	}))
	defer server.Close()
	policy := loopbackLaunchBoxPolicy(t, server)
	request, err := policy.newArchiveRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	response, err := policy.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound || calls.Load() != 1 {
		t.Fatalf("redirect response status=%d calls=%d", response.StatusCode, calls.Load())
	}
}

func TestLaunchBoxTransportRejectsPrivateAndNonGlobalDNSAnswers(t *testing.T) {
	for _, value := range []string{
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"169.254.1.1",
		"100.64.0.1",
		"192.0.2.1",
		"2001:db8::1",
		"::1",
	} {
		if isAllowedLaunchBoxIP(net.ParseIP(value)) {
			t.Fatalf("unsafe LaunchBox DNS answer accepted: %s", value)
		}
	}
	if !isAllowedLaunchBoxIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("ordinary global DNS answer rejected")
	}
}

func TestLaunchBoxTransportUsesExactHostAndPortAdmission(t *testing.T) {
	resolver := launchBoxFixedResolver{answers: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}
	var dials atomic.Int32
	policy := newLaunchBoxTransportPolicyWithNetwork(resolver, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("fixture dial")
	})
	for _, address := range []string{
		"gamesdb.launchbox-app.com.evil:443",
		"gamesdb.launchbox-app.com:80",
		"images.launchbox-app.com:444",
		"images.launchbox-app.com:0443",
		"images.launchbox-app.com%2eexample:443",
	} {
		if _, err := policy.transport.DialContext(context.Background(), "tcp", address); err == nil {
			t.Fatalf("unsafe LaunchBox dial address accepted: %s", address)
		}
	}
	if dials.Load() != 0 {
		t.Fatalf("unsafe dial reached fixture dialer %d times", dials.Load())
	}
}

func TestLaunchBoxProductionTransportRejectsUnsafeOriginMutationAndNonTCP(t *testing.T) {
	policy := newProductionLaunchBoxTransportPolicy()
	base := policy.archiveURL
	for name, candidate := range map[string]url.URL{
		"explicit https port": func() url.URL {
			value := base
			value.Host = launchBoxArchiveHost + ":443"
			return value
		}(),
		"wrong scheme": func() url.URL {
			value := base
			value.Scheme = "http"
			return value
		}(),
		"wrong path": func() url.URL {
			value := base
			value.Path = "/other"
			return value
		}(),
		"query": func() url.URL {
			value := base
			value.RawQuery = "x=1"
			return value
		}(),
		"userinfo": func() url.URL {
			value := base
			value.User = url.User("user")
			return value
		}(),
	} {
		if err := policy.validateOrigin(candidate, launchBoxArchiveHost, "/Metadata.zip", policy.archivePort); err == nil {
			t.Errorf("unsafe %s origin accepted", name)
		}
	}

	resolver := launchBoxFixedResolver{answers: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}
	var dials atomic.Int32
	transport := newLaunchBoxTransport(resolver, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("fixture dial")
	}, false)
	if _, err := transport.DialContext(context.Background(), "udp", launchBoxArchiveHost+":443"); err == nil {
		t.Fatal("non-TCP network reached the LaunchBox dialer")
	}
	if dials.Load() != 0 {
		t.Fatalf("non-TCP network reached fixture dialer %d times", dials.Load())
	}
}

type launchBoxFixedResolver struct {
	answers []net.IPAddr
}

func (r launchBoxFixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return append([]net.IPAddr(nil), r.answers...), nil
}

func loopbackLaunchBoxPolicy(t *testing.T, server *httptest.Server) *launchBoxTransportPolicy {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Scheme = "https"
	archive := *parsed
	archive.Path = "/Metadata.zip"
	archive.RawQuery = ""
	archive.Fragment = ""
	image := *parsed
	image.Path = "/"
	image.RawQuery = ""
	image.Fragment = ""
	client := server.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client.Jar = nil
	client.Timeout = 0
	return &launchBoxTransportPolicy{
		client:        client,
		archiveURL:    archive,
		imageOrigin:   image,
		transport:     client.Transport.(*http.Transport),
		archiveHost:   archive.Hostname(),
		imageHost:     image.Hostname(),
		archivePort:   archive.Port(),
		imagePort:     image.Port(),
		allowLoopback: true,
	}
}

func TestLaunchBoxTransportIPv6PublicUnicastTableAndMixedAnswers(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "lower public boundary", ip: "2000::", want: true},
		{name: "just below public range", ip: "1fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		{name: "upper public boundary", ip: "3fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", want: true},
		{name: "just above public range", ip: "4000::"},
		{name: "ordinary public unicast", ip: "2001:4860:4860::8888", want: true},
		{name: "mapped public IPv4", ip: "::ffff:8.8.8.8", want: true},
		{name: "mapped private IPv4", ip: "::ffff:10.0.0.1"},
		{name: "AS112 IPv4 service", ip: "192.31.196.1"},
		{name: "AMT IPv4 service", ip: "192.52.193.1"},
		{name: "AS112 IPv4 delegation", ip: "192.175.48.1"},
		{name: "AS112 IPv4 lower boundary", ip: "192.31.195.255", want: true},
		{name: "AS112 IPv4 exact lower boundary", ip: "192.31.196.0"},
		{name: "AS112 IPv4 exact upper boundary", ip: "192.31.196.255"},
		{name: "AS112 IPv4 upper boundary", ip: "192.31.197.0", want: true},
		{name: "unique local", ip: "fc00::1"},
		{name: "deprecated site local lower boundary", ip: "fec0::1"},
		{name: "deprecated site local upper boundary", ip: "feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		{name: "NAT64 well-known", ip: "64:ff9b::1"},
		{name: "NAT64 local-use lower boundary", ip: "64:ff9b:1::"},
		{name: "NAT64 local-use upper boundary", ip: "64:ff9b:1:ffff:ffff:ffff:ffff:ffff"},
		{name: "discard-only", ip: "100::1"},
		{name: "dummy prefix", ip: "100:0:0:1::1"},
		{name: "IETF assignments", ip: "2001::1"},
		{name: "P.C.P. anycast", ip: "2001:1::1"},
		{name: "NAT traversal anycast", ip: "2001:1::2"},
		{name: "DNS-SD anycast", ip: "2001:1::3"},
		{name: "benchmarking", ip: "2001:2::1"},
		{name: "AMT", ip: "2001:3::1"},
		{name: "AS112", ip: "2001:4:112::1"},
		{name: "deprecated ORCHID", ip: "2001:10::1"},
		{name: "ORCHIDv2 lower boundary", ip: "2001:20::"},
		{name: "ORCHIDv2 upper boundary", ip: "2001:2f:ffff:ffff:ffff:ffff:ffff:ffff"},
		{name: "drone remote ID", ip: "2001:30::1"},
		{name: "documentation", ip: "2001:db8::1"},
		{name: "6to4", ip: "2002::1"},
		{name: "AS112 service", ip: "2620:4f:8000::1"},
		{name: "documentation v2", ip: "3fff::1"},
		{name: "segment routing", ip: "5f00::1"},
		{name: "multicast", ip: "ff02::1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isAllowedLaunchBoxIP(net.ParseIP(test.ip)); got != test.want {
				t.Fatalf("isAllowedLaunchBoxIP(%q) = %v, want %v", test.ip, got, test.want)
			}
		})
	}

	resolver := launchBoxFixedResolver{answers: []net.IPAddr{
		{IP: net.ParseIP("8.8.8.8")},
		{IP: net.ParseIP("2001:20::1")},
	}}
	var dials atomic.Int32
	policy := newLaunchBoxTransportPolicyWithNetwork(resolver, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("fixture dial")
	})
	if _, err := policy.transport.DialContext(context.Background(), "tcp", launchBoxArchiveHost+":443"); err == nil {
		t.Fatal("mixed safe/unsafe DNS answers were admitted")
	}
	if got := dials.Load(); got != 0 {
		t.Fatalf("dialer called %d times after unsafe DNS answer", got)
	}
}
