package httpapi_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

func TestIdlePollingDoesNotGrowDefaultTargetLog(t *testing.T) {
	var logs bytes.Buffer
	manager := kitlease.New(time.Minute, nil)
	defer manager.Close()
	handler := httpapi.New(&fakeController{health: protocol.Health{Ready: true}}, "bearer", "test", slog.New(slog.NewJSONHandler(&logs, nil)), httpapi.WithKitLease(manager))
	for range 1000 {
		for _, path := range []string{"/v1/health", "/v1/status", "/v1/kit/lease"} {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer bearer")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("%s returned %d", path, response.Code)
			}
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("idle polling retained %d log bytes", logs.Len())
	}
}

func TestTargetReadLoggingRetainsDebugDetailAndOperationalFailures(t *testing.T) {
	for _, check := range []struct {
		name, method, path, token string
		level                     slog.Level
		status                    int
		wantLevel                 string
	}{
		{"debug read", http.MethodGet, "/v1/status?private=secret", "bearer", slog.LevelDebug, 200, "DEBUG"},
		{"unauthorized read", http.MethodGet, "/v1/status", "invalid", slog.LevelInfo, 401, "INFO"},
		{"unknown read", http.MethodGet, "/v1/absent", "bearer", slog.LevelInfo, 404, "INFO"},
		{"successful mutation", http.MethodPost, "/v1/stop", "bearer", slog.LevelInfo, 200, "INFO"},
	} {
		t.Run(check.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler := httpapi.New(&fakeController{}, "bearer", "test", slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: check.level})))
			request := httptest.NewRequest(check.method, check.path, nil)
			request.Header.Set("Authorization", "Bearer "+check.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != check.status || !strings.Contains(logs.String(), `"level":"`+check.wantLevel+`"`) || !strings.Contains(logs.String(), `"msg":"request"`) {
				t.Fatalf("status=%d logs=%s", response.Code, logs.String())
			}
			if strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "bearer") || strings.Contains(logs.String(), "invalid") {
				t.Fatalf("request details leaked: %s", logs.String())
			}
		})
	}
}
