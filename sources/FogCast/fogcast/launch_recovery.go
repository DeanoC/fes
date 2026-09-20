package fogcast

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/protocol"
)

func retainedIdleLaunchRecoveryCandidate(status protocol.Status) bool {
	if status.LastError == nil || status.LastError.Code != protocol.CodeMiSTerUnavailable || status.LastError.Phase != "" || status.CorePackage != nil {
		return false
	}
	clean := status
	clean.LastError = nil
	return validRecoveredDevelopmentStatus(clean)
}

func (s *Service) validateLibraryCoreBeforeRecovery(ctx context.Context, id string, client serviceClient) error {
	inspector, ok := client.(coreInspectionClient)
	if !ok {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	inspection, err := s.inspectInstalledPackageForClient(ctx, id, inspector)
	if err != nil {
		return err
	}
	if !inspection.Compatible {
		return preserveCorePackageError(inspection.CompatibilityError)
	}
	return nil
}

// Called only for a validated library selection, under lifecycle admission and
// the retained target lock, after API/identity/ownership admission and Status.
// Use the existing leased Stop transport once; never replay activation or reboot.
func (s *Service) recoverIdleLaunchError(ctx context.Context, client serviceClient, prior protocol.Status) (protocol.Status, error) {
	if err := ctx.Err(); err != nil {
		return prior, errors.Join(corePackageRequestFailure(err), err)
	}
	if prior.LastError == nil && prior.Recovery == "" {
		return prior, nil
	}
	clean := prior
	clean.LastError = nil
	if !validRecoveredDevelopmentStatus(clean) || prior.CorePackage != nil || prior.LastError == nil ||
		prior.LastError.Code != protocol.CodeMiSTerUnavailable || prior.LastError.Phase != "" {
		return prior, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target requires explicit recovery before launch", Phase: "admission"}
	}
	// The caller deadline bounds cleanup and the following single activation.
	recovered, err := client.Stop(ctx)
	if err != nil {
		return prior, WithStopStage(canonicalRemoteError(err, protocol.CodeMiSTerUnavailable), "target_stop")
	}
	if err := ctx.Err(); err != nil {
		return recovered, errors.Join(corePackageRequestFailure(err), err)
	}
	if !validRecoveredDevelopmentStatus(recovered) || recovered.CorePackage != nil {
		return recovered, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "pre-launch cleanup did not confirm clean idle", Phase: "recovery"}
	}
	return recovered, nil
}
