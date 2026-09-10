package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type CoreDataResult struct {
	protocol.CoreDataInspection
	Target   string `json:"target"`
	TargetID string `json:"target_id"`
}
type coreDataClient interface {
	InspectCoreData(context.Context, int64, io.Reader, string) (protocol.CoreDataInspection, error)
	UpdateCoreSettings(context.Context, int64, io.Reader, protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, error)
}
type libraryCoreClient interface {
	LoadLibraryCore(context.Context, int64, io.Reader, string) (protocol.Status, error)
}

func (s *Service) CoreSettings(ctx context.Context, gameID string) (CoreDataResult, error) {
	return s.entryCoreData(ctx, gameID, nil)
}
func (s *Service) CoreProgress(ctx context.Context, gameID string) (CoreDataResult, error) {
	return s.entryCoreData(ctx, gameID, nil)
}
func (s *Service) SetCoreSettings(ctx context.Context, gameID string, u protocol.CoreSettingsUpdate) (CoreDataResult, error) {
	if !u.Valid() {
		return CoreDataResult{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.entryCoreData(ctx, gameID, &u)
}
func (s *Service) entryCoreData(parent context.Context, gameID string, u *protocol.CoreSettingsUpdate) (CoreDataResult, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return CoreDataResult{}, err
	}
	defer release()
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return CoreDataResult{}, err
	}
	if u != nil && u.ExpectedPackageID != entry.PackageID {
		return CoreDataResult{}, canonicalError(protocol.CodeStaleRevision, nil)
	}
	if u != nil {
		s.executionMu.Lock()
		pending := s.packageRejection != nil
		s.executionMu.Unlock()
		if pending {
			return CoreDataResult{}, canonicalError(protocol.CodeBusy, nil)
		}
	}
	result, err := s.inspectInstalledCoreData(ctx, entry.PackageID, u)
	if err == nil && result.CoreID != entry.CoreID {
		return CoreDataResult{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	if err == nil && result.Mode != "persistent" {
		return CoreDataResult{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	return result, err
}

// Caller holds lifecycle admission, keeping selection and the target fixed.
func (s *Service) inspectInstalledCoreData(ctx context.Context, id string, u *protocol.CoreSettingsUpdate) (CoreDataResult, error) {
	value, data, err := s.readInstalledCore(ctx, id)
	if err != nil {
		return CoreDataResult{}, err
	}
	if s.discoveryEnabled() {
		if _, err = s.refreshTargetConnection(ctx); err != nil {
			return CoreDataResult{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return CoreDataResult{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	native, ok := client.(coreDataClient)
	if !ok {
		return CoreDataResult{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	health, err := client.Health(ctx)
	if err != nil {
		return CoreDataResult{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	var result protocol.CoreDataInspection
	if u == nil {
		result, err = native.InspectCoreData(ctx, int64(len(data)), bytes.NewReader(data), id)
	} else {
		result, err = native.UpdateCoreSettings(ctx, int64(len(data)), bytes.NewReader(data), *u)
	}
	if err != nil {
		return CoreDataResult{}, canonicalCoreDataError(err)
	}
	if !result.Valid() || result.PackageID != value.PackageID || !reflect.DeepEqual(result.Descriptor, value.Descriptor) || (result.Mode == "persistent" && health.TargetID == "") {
		return CoreDataResult{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return CoreDataResult{CoreDataInspection: result, Target: s.selectedTarget, TargetID: health.TargetID}, nil
}
func validPersistenceLayout(descriptor corepackage.Descriptor, layout *protocol.RuntimeContract) bool {
	if layout == nil {
		return true
	}
	for _, i := range descriptor.Interfaces {
		if i.Required && i.ID == layout.ID && i.Major == int64(layout.Major) && i.Minor == int64(layout.Minor) {
			return true
		}
	}
	return false
}

func canonicalCoreDataError(err error) error {
	result := canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	var source, mapped *protocol.APIError
	if errors.As(err, &source) && errors.As(result, &mapped) {
		switch source.Phase {
		case "request", "admission", "compatibility", "core_data", "save", "recovery":
			mapped.Phase = source.Phase
		}
	}
	return result
}
