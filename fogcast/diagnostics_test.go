package fogcast

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestRetainedErrorAdmissionDiagnosticDoesNotChangeLifecycle(t *testing.T) {
	for _, retained := range []bool{false, true} {
		prior := protocol.Status{State: protocol.StateIdle}
		if retained {
			prior.LastError = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "secret /private/core.rbf"}
		}
		client := &fakeServiceClient{statusResult: prior, coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable", Phase: "admission"}
		}}
		service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
		_, err := service.LoadCore(context.Background(), 3, strings.NewReader("rbf"))
		var apiErr *protocol.APIError
		if !errors.As(err, &apiErr) || apiErr.Phase != "admission" || client.coreCalls != 1 || client.stopCalls != 0 {
			t.Fatalf("err=%v calls=%d stops=%d", err, client.coreCalls, client.stopCalls)
		}
		if retained && apiErr.Message != targetRetainedErrorMessage {
			t.Fatalf("missing retained diagnostic: %v", err)
		}
		if !retained && apiErr.Message == targetRetainedErrorMessage {
			t.Fatal("invented retained failure")
		}
	}
}

func TestCanonicalRemoteDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name           string
		err            error
		message, phase string
	}{
		{"recovery", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime recovery is required"}, "Target recovery is required; use Stop to recover before launching again.", "recovery"},
		{"recovery phase", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "secret /private/token", Phase: "recovery"}, "Target recovery is required; use Stop to recover before launching again.", "recovery"},
		{"transport", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("secret endpoint")}, "Cannot reach the target; check its address, network, and agent service.", "connection"},
		{"unknown", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "missing /private/core.rbf token", Phase: "secret"}, "MiSTer is unavailable", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := canonicalRemoteError(tc.err, protocol.CodeMiSTerUnavailable)
			var got *protocol.APIError
			if !errors.As(err, &got) || got.Message != tc.message || got.Phase != tc.phase {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}

func TestSafeTargetDiagnosticRecognizedPhases(t *testing.T) {
	for _, phase := range []string{"request", "admission", "compatibility", "identity", "transfer", "transport", "program", "load", "save", "core_data", "recovery", "connection", "input", "lifecycle", "video"} {
		_, got := SafeTargetDiagnostic(&protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "secret", Phase: phase})
		if got != phase {
			t.Fatalf("lost recognized phase %q: %q", phase, got)
		}
	}
}
