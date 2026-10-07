package targetclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type stopDeadlineTransport func(*http.Request) (*http.Response, error)

func (f stopDeadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMediaStopHTTPBudgetIsSelectiveBoundedAndNeverReplayed(t *testing.T) {
	var budget time.Duration
	calls := 0
	fail := false
	original := &http.Client{Timeout: 5 * time.Second, Transport: stopDeadlineTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		d, _ := r.Context().Deadline()
		budget = time.Until(d)
		if r.Method != "POST" || r.URL.Path != "/v1/stop" || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatal(r)
		}
		if fail {
			return nil, errors.New("lost response")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"state":"idle"}`)), Header: make(http.Header)}, nil
	})}
	base, _ := url.Parse("http://kit")
	c := NewClient(base, "token", original)
	if _, err := c.StopWithMediaSave(context.Background()); err != nil || budget < 149*time.Second || budget > 150*time.Second {
		t.Fatal(err, budget)
	}
	if _, err := c.Stop(context.Background()); err != nil || budget > 5*time.Second || budget < 4*time.Second {
		t.Fatal(err, budget)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.StopWithMediaSave(ctx); err != nil || budget > time.Second {
		t.Fatal(err, budget)
	}
	fail = true
	if _, err := c.StopWithMediaSave(context.Background()); err == nil || calls != 4 {
		t.Fatal(err, calls)
	}
	if c.httpClient.Timeout != 5*time.Second || original.Timeout != 5*time.Second {
		t.Fatal("shared default modified")
	}
}
