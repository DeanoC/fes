package kitsoftware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivationHostHealthDuringMaintenance(t *testing.T) {
	for _, body := range []string{
		`{"host":{"revision":"head"},"ready":false,"target":{"ready":false}}`,
		`{"host":{"revision":"head"},"ready":true,"target":{"ready":true}}`,
	} {
		if err := validateActivationHostHealth(strings.NewReader(body), "head"); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{}`, `{"host":{"revision":"other"}}`, `{"host":{"revision":"head"}} {}`, `broken`} {
		if err := validateActivationHostHealth(strings.NewReader(body), "head"); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestActivationHostProbeRefusesNonServingHostAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/failed" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"host":{"revision":"head"},"ready":false}`)
	}))
	defer server.Close()
	if err := probeActivationHost(context.Background(), server.Client(), server.URL, "head"); err != nil {
		t.Fatal(err)
	}
	if err := probeActivationHost(context.Background(), server.Client(), server.URL+"/failed", "head"); err == nil {
		t.Fatal("accepted non-serving host")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := probeActivationHost(ctx, server.Client(), server.URL, "head"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled probe: %v", err)
	}
}

func TestActivationHostRefusesMissingPreparedChild(t *testing.T) {
	c := &ProcessController{}
	if err := c.ActivationReady(context.Background(), "head"); err == nil {
		t.Fatal("accepted absent host")
	}
}
