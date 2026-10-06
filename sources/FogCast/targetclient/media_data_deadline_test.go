package targetclient

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type diskDeadlineTransport struct {
	calls     int
	remaining time.Duration
}

func (t *diskDeadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls++
	deadline, ok := r.Context().Deadline()
	if !ok {
		return nil, errors.New("missing deadline")
	}
	t.remaining = time.Until(deadline)
	return nil, io.EOF
}
func TestMediaSaveTransportsOwnBoundedDeadlineWithoutReplay(t *testing.T) {
	b := protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 7}
	library := protocol.LibraryMediaBinding{MediaUnitBinding: b, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	for _, tc := range []struct {
		name    string
		seconds int
		call    func(*Client) error
	}{
		{"ordinary insert", 1, func(c *Client) error {
			_, e := c.InsertMedia(context.Background(), 737280, strings.NewReader("disk"), b)
			return e
		}},
		{"ordinary eject", 1, func(c *Client) error { _, e := c.EjectMedia(context.Background(), b); return e }},
		{"bound replacement", 450, func(c *Client) error {
			_, e := c.InsertMediaWithSave(context.Background(), 737280, strings.NewReader("disk"), b)
			return e
		}},
		{"bound eject", 150, func(c *Client) error { _, e := c.EjectMediaWithSave(context.Background(), b); return e }},
		{"library insertion", 450, func(c *Client) error {
			_, e := c.InsertLibraryMedia(context.Background(), 737280, strings.NewReader("disk"), library)
			return e
		}},
		{"checkpoint", 150, func(c *Client) error { _, e := c.SaveMedia(context.Background(), b); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := &diskDeadlineTransport{}
			source := &http.Client{Timeout: time.Second, Transport: rt}
			base, _ := url.Parse("http://kit")
			c := NewClient(base, "secret", source).WithKitLease(&KitLease{grant: kitlease.Grant{Token: "held"}, localExpiry: time.Now().Add(time.Hour)})
			if e := tc.call(c); e == nil || rt.calls != 1 {
				t.Fatalf("error=%v calls=%d", e, rt.calls)
			}
			want := time.Duration(tc.seconds) * time.Second
			if rt.remaining < want-time.Second/2 || rt.remaining > want {
				t.Fatalf("deadline=%v want=%v", rt.remaining, want)
			}
			if source.Timeout != time.Second || c.httpClient.Timeout != time.Second {
				t.Fatal("mutated shared transport")
			}
		})
	}
}
