package fogcast

import (
	"context"
	"errors"
	"net"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	targetRecoveryMessage      = "Target recovery is required; use Stop to recover before launching again."
	targetConnectionMessage    = "Cannot reach the target; check its address, network, and agent service."
	targetTimeoutMessage       = "Target request timed out; inspect session status before retrying."
	targetRetainedErrorMessage = "Target retains an earlier error; use Stop to clear it, then retry."
)

// SafeTargetDiagnostic returns fixed host-owned wording and an allowlisted phase.
// Empty wording asks the caller to use its existing code-based fallback. It never
// returns arbitrary upstream messages, addresses, filesystem paths, or tokens.
func SafeTargetDiagnostic(remote *protocol.APIError) (message, phase string) {
	if remote == nil {
		return "", ""
	}
	switch remote.Phase {
	case "request", "admission", "compatibility", "identity", "transfer", "transport", "program", "programming", "quiesce", "load", "save", "core_data", "recovery", "connection", "input", "lifecycle", "video":
		phase = remote.Phase
	}
	switch remote.Code {
	case protocol.CodeUnsupportedOperation:
		if remote.Phase == "admission" && (remote.Message == "legacy native core artifact is unavailable on this target" || remote.Message == "legacy native core is unavailable on the selected target; choose an installed FPGA package") {
			return "Legacy core is unavailable on this target; choose an installed FPGA package.", "admission"
		}
	case protocol.CodeMiSTerUnavailable:
		if phase == "admission" && remote.Message == targetRetainedErrorMessage {
			return targetRetainedErrorMessage, phase
		}
		if phase == "recovery" || remote.Message == "target runtime recovery is required" || remote.Message == "core data recovery is required" || remote.Message == targetRecoveryMessage {
			return targetRecoveryMessage, "recovery"
		}
		fallthrough
	case protocol.CodeTransferFailed:
		if phase == "connection" || phase == "request" {
			switch remote.Message {
			case targetConnectionMessage, targetTimeoutMessage:
				return remote.Message, phase
			}
		}
	case protocol.CodeVersionMismatch:
		return "target API version is missing or unsupported; expected v1", phase
	case protocol.CodeKitLeaseDenied:
		return "Another session owns the target; release it from that session before retrying.", phase
	}
	return "", phase
}

// Preserve pre-mutation admission semantics. The observed error belongs to the
// same client immediately before this request, not a cached connection summary.
func retainedAdmissionDiagnostic(prior protocol.Status, err error) error {
	var remote *protocol.APIError
	if (prior.LastError != nil || prior.Recovery != "") && errors.As(err, &remote) &&
		remote.Code == protocol.CodeMiSTerUnavailable && remote.Phase == "admission" {
		copy := *remote
		copy.Message = targetRetainedErrorMessage
		return &copy
	}
	return err
}

func applyRemoteDiagnostic(result *protocol.APIError, source *protocol.APIError, err error) {
	if message, phase := SafeTargetDiagnostic(source); message != "" || phase != "" {
		result.Phase = phase
		if message != "" {
			result.Message = message
		}
	}
	if result.Code != protocol.CodeMiSTerUnavailable && result.Code != protocol.CodeTransferFailed {
		return
	}
	if result.Phase == "recovery" {
		return
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		result.Message, result.Phase = targetTimeoutMessage, "connection"
	} else if errors.As(err, &network) {
		result.Message, result.Phase = targetConnectionMessage, "connection"
	}
}
