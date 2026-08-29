package fpgadev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Designation is the private, target-local qualification used by the
// development loader.  The representation intentionally contains no public
// target identity or address.  Production values are made by
// NewDesignation; package tests use newFixtureDesignation so they never open
// a host or target path.
type Designation struct {
	implementation *designationImplementation
}

type designationImplementation struct {
	designationPath string
	identityPath    string
	expectedUID     uint32
	verify          func(context.Context) error
}

var (
	ErrDesignation        = errors.New("private target designation is not valid")
	ErrDesignationMissing = errors.New("private target designation is unavailable")
)

// NewDesignation constructs the production designation resolver.  Both paths
// are private configuration paths; their contents are never included in an
// error returned to a caller.
func NewDesignation(designationPath, identityPath string) Designation {
	return Designation{implementation: &designationImplementation{
		designationPath: designationPath,
		identityPath:    identityPath,
	}}
}

// Verify resolves the operator record and verifies that the current target
// presents the exact same private identity.  It is deliberately a small
// capability: callers learn only success/failure, never the identity itself.
func (d Designation) Verify(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.implementation == nil {
		return ErrDesignationMissing
	}
	if d.implementation.verify != nil {
		if err := d.implementation.verify(ctx); err != nil {
			return err
		}
		return ctx.Err()
	}
	if err := validatePrivatePath(d.implementation.designationPath); err != nil {
		return fmt.Errorf("%w: designation record is unavailable", ErrDesignation)
	}
	if err := validatePrivatePath(d.implementation.identityPath); err != nil {
		return fmt.Errorf("%w: identity record is unavailable", ErrDesignation)
	}
	designation, err := readPrivateDesignation(ctx, d.implementation.designationPath, true, d.implementation.expectedUID)
	if err != nil {
		return fmt.Errorf("%w: designation record is invalid", ErrDesignation)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	identity, err := readPrivateDesignation(ctx, d.implementation.identityPath, false, d.implementation.expectedUID)
	if err != nil {
		return fmt.Errorf("%w: target identity is invalid", ErrDesignation)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !designation.Designated || designation.TargetID == "" || identity.TargetID == "" || designation.TargetID != identity.TargetID {
		return fmt.Errorf("%w: target identity did not match", ErrDesignation)
	}
	return nil
}

// newFixtureDesignation is package-private by design.  It supplies an
// anonymous verification seam to hostile tests without making production
// callers able to inject a claimed target identity.
func newFixtureDesignation(err error) Designation {
	return Designation{implementation: &designationImplementation{verify: func(context.Context) error { return err }}}
}

// newFixtureFileDesignation is the package-local temporary-root seam. The
// exported constructor remains root-owned; tests may explicitly name their
// process UID without making that choice available to production callers.
func newFixtureFileDesignation(designationPath, identityPath string, expectedUID uint32) Designation {
	return Designation{implementation: &designationImplementation{
		designationPath: designationPath,
		identityPath:    identityPath,
		expectedUID:     expectedUID,
	}}
}

type privateDesignationRecord struct {
	Schema     uint64 `json:"schema"`
	TargetID   string `json:"target_id"`
	Designated bool   `json:"designated"`
}

var privateDesignationFields = [...]string{"schema", "target_id", "designated"}

func readPrivateDesignation(ctx context.Context, path string, requireDesignation bool, expectedUID ...uint32) (privateDesignationRecord, error) {
	if err := ctx.Err(); err != nil {
		return privateDesignationRecord{}, err
	}
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	info, err := os.Lstat(path)
	if err != nil {
		return privateDesignationRecord{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !exactFileMode(info.Mode(), 0o600) {
		return privateDesignationRecord{}, errors.New("private record metadata is invalid")
	}
	fileUID, ok := privateFileUID(info)
	if !ok || fileUID != uid {
		return privateDesignationRecord{}, errors.New("private record owner is invalid")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return privateDesignationRecord{}, err
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || openedInfo == nil || !os.SameFile(info, openedInfo) {
		closeErr := file.Close()
		return privateDesignationRecord{}, errors.Join(errors.New("private record changed during open"), closeErr)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return privateDesignationRecord{}, errors.Join(readErr, closeErr)
	}
	if len(raw) > 4096 {
		return privateDesignationRecord{}, errors.New("private record exceeds bounded size")
	}
	var record privateDesignationRecord
	if err := decodeStrictObject(raw, privateDesignationFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &record.Schema)
		case "target_id":
			return decodeJSONString(value, &record.TargetID)
		case "designated":
			return decodeJSONBool(value, &record.Designated)
		default:
			return errors.New("unknown private record field")
		}
	}); err != nil {
		return privateDesignationRecord{}, err
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(append(canonical, '\n'), raw) {
		return privateDesignationRecord{}, errors.New("private record is not canonical")
	}
	if record.Schema != 1 || strings.TrimSpace(record.TargetID) == "" || strings.TrimSpace(record.TargetID) != record.TargetID || strings.ContainsAny(record.TargetID, "\r\n\x00") {
		return privateDesignationRecord{}, errors.New("private record fields are invalid")
	}
	if requireDesignation && !record.Designated {
		return privateDesignationRecord{}, errors.New("designation is not enabled")
	}
	if !requireDesignation && record.Designated {
		return privateDesignationRecord{}, errors.New("identity record has designation field")
	}
	return record, nil
}

func privateFileUID(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

func decodeJSONBool(raw []byte, target *bool) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed != "true" && trimmed != "false" {
		return errors.New("value must be a JSON boolean")
	}
	return json.Unmarshal([]byte(trimmed), target)
}

func validatePrivatePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return errors.New("private path is not canonical")
	}
	return nil
}

func exactFileMode(mode os.FileMode, permissions os.FileMode) bool {
	return mode.Perm() == permissions && mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
}
