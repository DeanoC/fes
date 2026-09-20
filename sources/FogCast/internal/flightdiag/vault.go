package flightdiag

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultVaultRoot = "/media/fat/fogcast/evidence"
	maxEvidenceBytes = 1 << 20
)

// KeyPath identifies one diagnostic path to record before an intentional
// reboot. Multiple entries may share a Name when a platform has alternate
// journal or owner locations.
type KeyPath struct {
	Name string
	Path string
}

// DefaultKeyPaths describe the existing target surfaces without asserting
// that every image exposes every path. Missing paths remain explicit in the
// manifest instead of being silently invented or omitted.
var DefaultKeyPaths = []KeyPath{
	{Name: "journal", Path: "/var/log/journal"},
	{Name: "journal", Path: "/var/log/messages"},
	{Name: "journal", Path: "/var/log/mister-main.log"},
	{Name: "owner", Path: "/run/fogcast/owner.json"},
	{Name: "owner", Path: "/media/fat/fogcast/owner.json"},
	{Name: "CORENAME", Path: "/tmp/CORENAME"},
	{Name: "fpga_manager", Path: "/sys/class/fpga_manager/fpga0/state"},
	{Name: "runtime_events", Path: "/run/mister-runtime.events.json"},
	{Name: "FAT note", Path: "/media/fat/fogcast/fat-note"},
}

type SnapshotRequest struct {
	RunID    string `json:"run_id"`
	LeaseGen string `json:"lease_gen"`
	FlightID string `json:"flight_id,omitempty"`
}

type PathEvidence struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	State     string `json:"state"`
	Bytes     int64  `json:"bytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Artifact  string `json:"artifact,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Error     string `json:"error,omitempty"`
}

type SnapshotManifest struct {
	EvidenceClass string         `json:"evidence_class"`
	CreatedUTC    string         `json:"created_utc"`
	RunID         string         `json:"run_id"`
	LeaseGen      string         `json:"lease_gen"`
	FlightID      string         `json:"flight_id,omitempty"`
	Events        []Event        `json:"events"`
	Paths         []PathEvidence `json:"paths"`
}

type SnapshotResult struct {
	EvidenceClass string `json:"evidence_class"`
	CreatedUTC    string `json:"created_utc"`
	RunID         string `json:"run_id"`
	LeaseGen      string `json:"lease_gen"`
	FlightID      string `json:"flight_id,omitempty"`
	Directory     string `json:"directory"`
	Manifest      string `json:"manifest"`
	EventCount    int    `json:"event_count"`
	PathCount     int    `json:"path_count"`
}

type Vault struct {
	Root     string
	Ring     *Ring
	KeyPaths []KeyPath
}

func NewVault(root string, ring *Ring) *Vault {
	if strings.TrimSpace(root) == "" {
		root = DefaultVaultRoot
	}
	paths := append([]KeyPath(nil), DefaultKeyPaths...)
	return &Vault{Root: root, Ring: ring, KeyPaths: paths}
}

func (v *Vault) SnapshotBeforeReboot(request SnapshotRequest) (SnapshotResult, error) {
	if v == nil {
		return SnapshotResult{}, errors.New("diagnostic vault is unavailable")
	}
	if err := validateSnapshotRequest(request); err != nil {
		return SnapshotResult{}, err
	}
	created := time.Now().UTC()
	var events []Event
	if v.Ring != nil {
		events = v.Ring.Snapshot(0)
	}
	if err := os.MkdirAll(v.Root, 0o700); err != nil {
		return SnapshotResult{}, fmt.Errorf("create diagnostic vault: %w", err)
	}
	temporary, err := os.MkdirTemp(v.Root, ".snapshot-")
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("create diagnostic snapshot: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return SnapshotResult{}, fmt.Errorf("protect diagnostic snapshot: %w", err)
	}
	pathEvidence, err := v.copyKeyPaths(temporary)
	if err != nil {
		return SnapshotResult{}, err
	}
	manifest := SnapshotManifest{
		EvidenceClass: DiagnosticEvidence,
		CreatedUTC:    created.Format(time.RFC3339Nano),
		RunID:         request.RunID,
		LeaseGen:      request.LeaseGen,
		FlightID:      request.FlightID,
		Events:        events,
		Paths:         pathEvidence,
	}
	manifestPath := filepath.Join(temporary, "manifest.json")
	if err := writeJSON(manifestPath, manifest); err != nil {
		return SnapshotResult{}, fmt.Errorf("write diagnostic manifest: %w", err)
	}
	if err := syncDirectory(temporary); err != nil {
		return SnapshotResult{}, fmt.Errorf("sync diagnostic snapshot: %w", err)
	}
	directoryName := fmt.Sprintf("%s-%s", created.Format("20060102T150405.000000000Z"), safeComponent(request.RunID))
	directory := filepath.Join(v.Root, directoryName)
	if err := os.Rename(temporary, directory); err != nil {
		return SnapshotResult{}, fmt.Errorf("publish diagnostic snapshot: %w", err)
	}
	removeTemporary = false
	if err := syncDirectory(v.Root); err != nil {
		return SnapshotResult{}, fmt.Errorf("sync diagnostic vault: %w", err)
	}
	return SnapshotResult{
		EvidenceClass: DiagnosticEvidence,
		CreatedUTC:    manifest.CreatedUTC,
		RunID:         request.RunID,
		LeaseGen:      request.LeaseGen,
		FlightID:      request.FlightID,
		Directory:     directory,
		Manifest:      filepath.Join(directory, "manifest.json"),
		EventCount:    len(events),
		PathCount:     len(pathEvidence),
	}, nil
}

func validateSnapshotRequest(request SnapshotRequest) error {
	if !validComponent(request.RunID) {
		return errors.New("run_id is required and must be a safe diagnostic identifier")
	}
	if !validComponent(request.LeaseGen) {
		return errors.New("lease_gen is required and must be a safe diagnostic identifier")
	}
	if request.FlightID != "" && !validFlightID(request.FlightID) {
		return errors.New("flight_id must be the canonical host UUID v4")
	}
	return nil
}

func validFlightID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == uuid.Version(4) && parsed.String() == value
}

func validComponent(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}

func safeComponent(value string) string {
	if len(value) > 64 {
		value = value[:64]
	}
	if value == "" {
		return "run"
	}
	return value
}

func (v *Vault) copyKeyPaths(directory string) ([]PathEvidence, error) {
	filesDirectory := filepath.Join(directory, "paths")
	if err := os.MkdirAll(filesDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create diagnostic path store: %w", err)
	}
	result := make([]PathEvidence, 0, len(v.KeyPaths))
	for index, keyPath := range v.KeyPaths {
		evidence := PathEvidence{Name: keyPath.Name, Path: keyPath.Path}
		info, err := os.Stat(keyPath.Path)
		if errors.Is(err, os.ErrNotExist) {
			evidence.State = "missing"
			result = append(result, evidence)
			continue
		}
		if err != nil {
			evidence.State = "unreadable"
			evidence.Error = err.Error()
			result = append(result, evidence)
			continue
		}
		evidence.Mode = info.Mode().String()
		if !info.Mode().IsRegular() {
			evidence.State = "present"
			result = append(result, evidence)
			continue
		}
		content, readErr := readBounded(keyPath.Path, maxEvidenceBytes)
		if readErr != nil {
			evidence.State = "unreadable"
			evidence.Error = readErr.Error()
			result = append(result, evidence)
			continue
		}
		artifact := fmt.Sprintf("%03d-%s", index, safeFileName(keyPath.Name))
		artifactPath := filepath.Join(filesDirectory, artifact)
		if err := os.WriteFile(artifactPath, content.Bytes, 0o600); err != nil {
			return nil, fmt.Errorf("copy diagnostic path %s: %w", keyPath.Path, err)
		}
		if err := syncFile(artifactPath); err != nil {
			return nil, fmt.Errorf("sync diagnostic path %s: %w", keyPath.Path, err)
		}
		digest := sha256.Sum256(content.Bytes)
		evidence.State = "copied"
		evidence.Bytes = int64(len(content.Bytes))
		evidence.SHA256 = hex.EncodeToString(digest[:])
		evidence.Artifact = filepath.Join("paths", artifact)
		evidence.Truncated = content.Truncated
		result = append(result, evidence)
	}
	if err := syncDirectory(filesDirectory); err != nil {
		return nil, fmt.Errorf("sync diagnostic path store: %w", err)
	}
	return result, nil
}

type boundedContent struct {
	Bytes     []byte
	Truncated bool
}

func readBounded(path string, limit int64) (boundedContent, error) {
	file, err := os.Open(path)
	if err != nil {
		return boundedContent{}, err
	}
	defer file.Close()
	reader := io.LimitReader(file, limit+1)
	content, err := io.ReadAll(reader)
	if err != nil {
		return boundedContent{}, err
	}
	truncated := int64(len(content)) > limit
	if truncated {
		content = content[:limit]
	}
	return boundedContent{Bytes: content, Truncated: truncated}, nil
}

func safeFileName(value string) string {
	if value == "" {
		return "path"
	}
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func writeJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return syncFile(path)
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
