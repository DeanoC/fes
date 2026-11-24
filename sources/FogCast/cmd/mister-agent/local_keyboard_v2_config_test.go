package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
)

func TestLocalKeyboardV2Configuration(t *testing.T) {
	for _, tc := range []struct {
		name, value, managed   string
		present, want, invalid bool
	}{
		{name: "unmanaged absent"},
		{name: "unmanaged disabled", value: "0", present: true},
		{name: "unmanaged enabled", value: "1", present: true, want: true},
		{name: "unmanaged marker zero", managed: "0"},
		{name: "managed default", managed: "1", want: true},
		{name: "managed cannot disable", managed: "1", value: "0", present: true, want: true},
		{name: "managed explicit enabled", managed: "1", value: "1", present: true, want: true},
		{name: "empty", present: true, invalid: true},
		{name: "typo", value: "true", present: true, invalid: true},
		{name: "managed typo", managed: "1", value: "true", present: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled, err := localKeyboardV2Enabled(func(key string) (string, bool) {
				switch key {
				case localKeyboardV2Env:
					return tc.value, tc.present
				case softwareManagedEnv:
					return tc.managed, tc.managed != ""
				default:
					t.Fatalf("unexpected key %q", key)
					return "", false
				}
			})
			if enabled != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("enabled=%v err=%v", enabled, err)
			}
		})
	}
}

type orderedLocalFeed struct {
	events chan string
	err    error
}

func (f *orderedLocalFeed) SetLocalLeaseManager(*kitlease.Manager) { f.events <- "lease" }
func (f *orderedLocalFeed) EnableLocalInputRecovery() error {
	f.events <- "enable"
	return f.err
}
func (f *orderedLocalFeed) ServeLocalInput(context.Context, string) error {
	f.events <- "serve"
	return nil
}

func TestLocalKeyboardV2ConfiguredBeforeServe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name    string
		enabled bool
		err     error
		want    []string
	}{
		{name: "legacy default", want: []string{"lease", "serve"}},
		{name: "enabled", enabled: true, want: []string{"lease", "enable", "serve"}},
		{name: "enable failure", enabled: true, err: errors.New("recovery setup failed"), want: []string{"lease", "enable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			feed := &orderedLocalFeed{events: make(chan string, 3), err: tc.err}
			err := startLocalInputFeed(context.Background(), feed, nil, tc.enabled, logger)
			if !errors.Is(err, tc.err) {
				t.Fatalf("start error %v", err)
			}
			var got []string
			for range tc.want {
				select {
				case event := <-feed.events:
					got = append(got, event)
				case <-time.After(time.Second):
					t.Fatal("feed did not reach expected state")
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("events %v, want %v", got, tc.want)
			}
			select {
			case unexpected := <-feed.events:
				t.Fatalf("unexpected extra event %q", unexpected)
			default:
			}
		})
	}
}
