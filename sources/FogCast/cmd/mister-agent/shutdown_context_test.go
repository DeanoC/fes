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
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

type shutdownOwnedRuntime struct {
	compositionRuntime
	owner              context.Context
	stopCalls          int
	stopAdmissionError error
	stopOperationError error
	stopBudget         time.Duration
}

func (r *shutdownOwnedRuntime) LoadDevelopmentRBFOwned(admission, observation, owner context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	r.owner = owner
	return r.LoadDevelopmentRBF(observation, size, content)
}
func (*shutdownOwnedRuntime) StopReady() bool { return true }
func (r *shutdownOwnedRuntime) StopOwned(admission, operation context.Context) (string, *protocol.APIError) {
	r.stopCalls++
	r.stopAdmissionError = admission.Err()
	r.stopOperationError = operation.Err()
	if deadline, ok := operation.Deadline(); ok {
		r.stopBudget = time.Until(deadline)
	}
	if r.stopAdmissionError != nil || r.stopOperationError != nil {
		return "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "cleanup context canceled"}
	}
	return "MENU", nil
}

func TestRunRetainsBoundedOwnedStopUntilLeaseShutdownFinishes(t *testing.T) {
	runtime := &shutdownOwnedRuntime{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := runDependencies{
		openCache: func(targetcache.Config, ...targetcache.Option) (agent.ContentStore, error) {
			return &compositionStore{}, nil
		},
		newRuntime: func(agentconfig.Config) agent.Runtime { return runtime },
		serve: func(server *http.Server) error {
			request := httptest.NewRequest(http.MethodPost, "/v1/kit/claim", strings.NewReader(`{"request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","owner":"shutdown-test","purpose":"owned active cleanup"}`))
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			var grant struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &grant); err != nil || response.Code != http.StatusOK || grant.Token == "" {
				t.Fatalf("claim: %d %s", response.Code, response.Body.String())
			}
			request = httptest.NewRequest(http.MethodPost, "/v1/development/rbf", strings.NewReader("owned-test-rbf"))
			request.Header.Set("Authorization", "Bearer test-token")
			request.Header.Set("Content-Type", "application/octet-stream")
			request.Header.Set(httpapi.KitLeaseHeader, grant.Token)
			response = httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || rbfStatus(response.Body.Bytes()).State != protocol.StateActive {
				t.Fatalf("load: %d %s", response.Code, response.Body.String())
			}
			cancel()
			return http.ErrServerClosed
		},
	}
	if err := runWithDependencies(ctx, writeCompositionConfig(t, ""), slog.New(slog.NewJSONHandler(io.Discard, nil)), deps); err != nil {
		t.Fatal(err)
	}
	if runtime.stopCalls != 1 {
		t.Fatalf("owned Stop calls=%d, want exactly1", runtime.stopCalls)
	}
	if runtime.stopAdmissionError != nil || runtime.stopOperationError != nil {
		t.Fatalf("shutdown canceled cleanup before Stop: admission=%v operation=%v", runtime.stopAdmissionError, runtime.stopOperationError)
	}
	if runtime.stopBudget <= 0 || runtime.stopBudget > 5*time.Second {
		t.Fatalf("cleanup Stop budget=%s, want existing positive budget <=5s", runtime.stopBudget)
	}
	if runtime.owner == nil || runtime.owner.Err() != context.Canceled {
		t.Fatal("runtime operation lifetime was not canceled after lease cleanup")
	}
}
