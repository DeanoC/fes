package hostapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestSessionErrorSafeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *protocol.APIError
		want string
	}{
		{"retained admission", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Target retains an earlier error; use Stop to clear it, then retry.", Phase: "admission"}, "Target retains an earlier error; use Stop"},
		{"recovery", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime recovery is required", Phase: "recovery"}, "Target recovery is required; use Stop"},
		{"transport", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Cannot reach the target; check its address, network, and agent service.", Phase: "connection"}, "Cannot reach the target"},
		{"unknown", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "secret /private/token", Phase: "secret"}, "MiSTer is unavailable"},
		{"api", &protocol.APIError{Code: protocol.CodeVersionMismatch, Message: "secret"}, "target API version is missing or unsupported; expected v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeSessionError(w, tc.err)
			body := w.Body.String()
			if !strings.Contains(body, tc.want) || strings.Contains(body, "secret") || strings.Contains(body, "/private") {
				t.Fatalf("unsafe or unhelpful response: %s", body)
			}
		})
	}
}
