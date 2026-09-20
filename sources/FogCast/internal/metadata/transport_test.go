package metadata

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fixedResolver struct {
	answers []net.IPAddr
}

func (r fixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return append([]net.IPAddr(nil), r.answers...), nil
}

func TestProviderIPPolicyRejectsReservedAndDocumentationRanges(t *testing.T) {
	for _, value := range []string{
		"100.64.0.1",
		"192.0.0.1",
		"192.0.2.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"240.0.0.1",
		"255.255.255.254",
		"2001:db8::1",
		"fc00::1",
	} {
		if isAllowedProviderIP(net.ParseIP(value)) {
			t.Fatalf("reserved provider address accepted: %s", value)
		}
	}
	if !isAllowedProviderIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("ordinary global provider address rejected")
	}
}

func TestHardenedTransportRejectsNonAllowlistedHostsAndMixedDNSAnswers(t *testing.T) {
	dials := 0
	transport := newHardenedTransport(fixedResolver{answers: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}, func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("dial should not be reached")
	})
	if _, err := transport.DialContext(context.Background(), "tcp", "api.igdb.com.evil:443"); err == nil {
		t.Fatal("lookalike provider host accepted")
	}
	if dials != 0 {
		t.Fatalf("lookalike host reached dialer %d times", dials)
	}
	transport = newHardenedTransport(fixedResolver{answers: []net.IPAddr{
		{IP: net.ParseIP("8.8.8.8")},
		{IP: net.ParseIP("192.168.1.1")},
	}}, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("dial should not be reached")
	})
	if _, err := transport.DialContext(context.Background(), "tcp", "api.igdb.com:443"); err == nil {
		t.Fatal("mixed public/private DNS answers accepted")
	}
}

func TestFixedProviderOriginsRequireExactSchemeHostPortAndPath(t *testing.T) {
	for _, value := range []string{
		"https://api.igdb.com.evil/v4/games",
		"https://api.igdb.com:444/v4/games",
		"http://api.igdb.com/v4/games",
		"https://api.igdb.com/v4/games?x=1",
		"https://api.igdb.com/v4/games/extra",
	} {
		if validateFixedOrigin(value, "api.igdb.com", "/v4/games") == nil {
			t.Fatalf("unsafe provider origin accepted: %s", value)
		}
	}
	if err := validateFixedOrigin("https://api.igdb.com/v4/games", "api.igdb.com", "/v4/games"); err != nil {
		t.Fatalf("valid provider origin rejected: %v", err)
	}
}
