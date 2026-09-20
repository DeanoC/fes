package fogcast

import (
	"errors"
	"github.com/DeanoC/FogCast/protocol"
)

// Stop stages are diagnostic context, never protocol phases or retry advice.
type stopStageError struct {
	stage string
	err   error
}

func (e *stopStageError) Error() string { return e.err.Error() }
func (e *stopStageError) Unwrap() error { return e.err }

// WithStopStage preserves error identity and admits only host-owned labels.
func WithStopStage(err error, stage string) error {
	if err == nil {
		return nil
	}
	if StopStage(err) != "" {
		return err
	}
	switch stage {
	case "lifecycle", "development_probe", "admission", "admission_health", "admission_ownership", "admission_status", "admission_backoff", "target_stop", "reconcile_stop", "development_recovery", "host_stop", "input_detach", "media_stop", "lease_release":
		return &stopStageError{stage: stage, err: err}
	}
	return err
}

// StopStage returns bounded context separately from the target protocol phase.
// target_stop means the client call began, not proof a remote mutation ran.
func StopStage(err error) string {
	var staged *stopStageError
	if errors.As(err, &staged) {
		return staged.stage
	}
	return ""
}

type targetObservationError struct {
	stage string
	err   error
}

func (e *targetObservationError) Error() string { return e.err.Error() }
func (e *targetObservationError) Unwrap() error { return e.err }

func stopAdmissionError(err error) error {
	stage := "admission"
	var observation *targetObservationError
	if errors.As(err, &observation) {
		stage = observation.stage
	}
	return WithStopStage(canonicalRemoteError(err, protocol.CodeMiSTerUnavailable), stage)
}
