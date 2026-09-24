package hostapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

func TestMeshLaunchErrorsUseTheirOwnStatuses(t *testing.T) {
	cart := meshcontent.SumSHA256([]byte("cart"))
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"checking", &meshcontent.ExecuteBlockedError{Block: meshcontent.BlockEnsureProgress}, http.StatusConflict, "CONTENT_CHECKING"},
		{"missing", &meshcontent.ContentMissingError{Kind: meshcontent.SlotPrimaryMedia, ID: cart}, http.StatusUnprocessableEntity, "CONTENT_MISSING"},
		{"missing after check", &meshcontent.ExecuteBlockedError{Block: meshcontent.BlockContentMissing}, http.StatusUnprocessableEntity, "CONTENT_MISSING"},
		{"pull failed", meshcontent.ErrContentPullFailed, http.StatusUnprocessableEntity, "CONTENT_PULL_FAILED"},
		{"abi skew", &meshcontent.ExecuteBlockedError{Block: meshcontent.BlockVersionSkew}, http.StatusConflict, "ABI_INELIGIBLE"},
		{"abi missing", &meshcontent.ExecuteBlockedError{Block: meshcontent.BlockNoExecutor}, http.StatusConflict, "ABI_INELIGIBLE"},
		{"lease", meshcontent.ErrLeaseNotFree, http.StatusForbidden, "KIT_LEASE_DENIED"},
		{"timeout", meshcontent.ErrCheckingTimeout, http.StatusGatewayTimeout, "CONTENT_CHECKING_TIMEOUT"},
		{"snapshot", &fogcast.LaunchSnapshotError{Reason: "target disabled"}, http.StatusConflict, "LAUNCH_CHANGED"},
		{"other block", &meshcontent.ExecuteBlockedError{Block: meshcontent.BlockBrowseOnly}, http.StatusServiceUnavailable, "TARGET_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeSessionError(w, tc.err)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tc.code != "TARGET_UNAVAILABLE" && strings.Contains(w.Body.String(), "TARGET_UNAVAILABLE") {
				t.Fatalf("collapsed to target unavailable: %s", w.Body.String())
			}
		})
	}
}

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
