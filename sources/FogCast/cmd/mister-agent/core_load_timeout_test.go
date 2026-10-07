package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/agentconfig"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

type timeoutPackageRuntime struct {
	compositionRuntime
	calls     int
	remaining time.Duration
	wait      bool
	ended     error
}

func (r *timeoutPackageRuntime) LoadCoreOwned(admission, observation, owner context.Context, _ int64, body io.Reader) (misterruntime.CoreActivation, bool, *protocol.APIError) {
	r.calls++
	deadline, ok := observation.Deadline()
	if !ok {
		panic("core load has no deadline")
	}
	r.remaining = time.Until(deadline)
	if _, err := io.ReadAll(body); err != nil {
		panic(err)
	}
	if r.wait {
		select {
		case <-observation.Done():
			r.ended = observation.Err()
		case <-admission.Done():
			r.ended = admission.Err()
		case <-owner.Done():
			r.ended = owner.Err()
		}
	}
	// Model pre-programming admission completion/failure. The coordinator must
	// retain idle ownership and never retry an uncertain or cancelled request.
	return misterruntime.CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "request"}
}

func TestCoreLoadTimeoutRejectsInvalidBeforeStartup(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, timeout := range []time.Duration{-1, maxCoreLoadTimeout + 1} {
		if err := runWithDependencies(context.Background(), "not-read", logger, runDependencies{coreLoadTimeout: timeout}); err == nil || !strings.Contains(err.Error(), "core-load timeout") {
			t.Fatalf("invalid timeout %s: %v", timeout, err)
		}
	}
	for _, timeout := range []time.Duration{0, -1, maxCoreLoadTimeout + 1} {
		if err := runWithCoreLoadTimeout(context.Background(), "not-read", logger, "", timeout); err == nil || !strings.Contains(err.Error(), "core-load timeout") {
			t.Fatalf("explicit invalid timeout %s: %v", timeout, err)
		}
	}
}

func TestCoreLoadTimeoutActualTargetRoute(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		wait    bool
		cancel  bool
	}{
		{name: "default"},
		{name: "explicit", timeout: 30 * time.Millisecond, wait: true},
		{name: "request-cancelled", timeout: time.Minute, wait: true, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := writeCompositionConfig(t, "")
			runtime := &timeoutPackageRuntime{wait: tc.wait}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deps := runDependencies{
				coreLoadTimeout:      tc.timeout,
				prepareDataPartition: func() error { return nil },
				openCache: func(targetcache.Config, ...targetcache.Option) (agent.ContentStore, error) {
					return &compositionStore{}, nil
				},
				newRuntime: func(agentconfig.Config) agent.Runtime { return runtime },
				serve: func(server *http.Server) error {
					var token string
					end := time.Now().Add(time.Second)
					for token == "" {
						req := httptest.NewRequest(http.MethodPost, "/v1/kit/claim", strings.NewReader(`{"request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","owner":"test","purpose":"development"}`))
						req.Header.Set("Authorization", "Bearer test-token")
						reply := httptest.NewRecorder()
						server.Handler.ServeHTTP(reply, req)
						var grant struct{ Token string }
						_ = json.Unmarshal(reply.Body.Bytes(), &grant)
						token = grant.Token
						if token == "" {
							if time.Now().After(end) {
								t.Fatalf("claim: %d %s", reply.Code, reply.Body.String())
							}
							time.Sleep(time.Millisecond)
						}
					}
					req := httptest.NewRequest(http.MethodPost, "/v1/development/core", strings.NewReader("package"))
					req.Header.Set("Authorization", "Bearer test-token")
					req.Header.Set("Content-Type", "application/octet-stream")
					req.Header.Set(httpapi.KitLeaseHeader, token)
					if tc.cancel {
						admission, release := context.WithCancel(req.Context())
						defer release()
						req = req.WithContext(admission)
						time.AfterFunc(10*time.Millisecond, release)
					}
					reply := httptest.NewRecorder()
					server.Handler.ServeHTTP(reply, req)
					if runtime.calls != 1 || reply.Code != http.StatusServiceUnavailable {
						t.Fatalf("core request: calls=%d HTTP%d %s", runtime.calls, reply.Code, reply.Body.String())
					}
					cancel()
					return http.ErrServerClosed
				},
			}
			if err := runWithDependencies(ctx, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps); err != nil {
				t.Fatal(err)
			}
			if tc.timeout == 0 {
				if runtime.remaining < 170*time.Second || runtime.remaining > 180*time.Second {
					t.Fatalf("actual target core budget = %s", runtime.remaining)
				}
			} else if runtime.remaining <= 0 || runtime.remaining > tc.timeout {
				t.Fatalf("actual target override = %s, configured %s", runtime.remaining, tc.timeout)
			}
			if tc.wait && runtime.ended == nil {
				t.Fatal("load did not end on its bounded/cancelled context")
			}
		})
	}
}
