package fpgadev

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	ManifestSchemaVersion uint64 = 1
	// MaxRBFSize is the protocol-v1 upper bound for a staged RBF.
	MaxRBFSize uint64 = 16_777_216
	// MaxArtifactSize is retained as a descriptive alias for callers that
	// reason about the bundle rather than its RBF file.
	MaxArtifactSize = MaxRBFSize

	ManifestExperiment = "020_linux_mailbox"
	ManifestBoard      = "misterpi"
	ManifestArtifact   = "top.rbf"
	ManifestEvidence   = "resource_evidence.json"
	ManifestChecksums  = "bundle.sha256"
	BuildLaneOSS       = "oss"
	BuildLaneOracle    = "oracle"
)

var (
	manifestRunIDPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	manifestHashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	manifestCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Manifest is the exact schema-v1 bundle manifest exchanged with misteross.
// The fields intentionally contain no target identity, credentials, or paths
// other than the fixed artifact filename.
type Manifest struct {
	Schema           uint64 `json:"schema"`
	RunID            string `json:"run_id"`
	Experiment       string `json:"experiment"`
	Board            string `json:"board"`
	BuildLane        string `json:"build_lane"`
	ArtifactFilename string `json:"artifact_filename"`
	ArtifactSize     uint64 `json:"artifact_size"`
	ArtifactSHA256   string `json:"artifact_sha256"`
	SourceCommit     string `json:"source_commit"`
}

var manifestFields = [...]string{
	"schema",
	"run_id",
	"experiment",
	"board",
	"build_lane",
	"artifact_filename",
	"artifact_size",
	"artifact_sha256",
	"source_commit",
}

// Validate checks the immutable manifest contract without normalizing it.
func (m Manifest) Validate() error {
	if m.Schema != ManifestSchemaVersion {
		return fmt.Errorf("manifest schema must be %d", ManifestSchemaVersion)
	}
	if !manifestRunIDPattern.MatchString(m.RunID) {
		return errors.New("manifest run_id must be 32 lowercase hexadecimal characters")
	}
	if m.Experiment != ManifestExperiment {
		return fmt.Errorf("manifest experiment must be %q", ManifestExperiment)
	}
	if m.Board != ManifestBoard {
		return fmt.Errorf("manifest board must be %q", ManifestBoard)
	}
	if m.BuildLane != BuildLaneOSS && m.BuildLane != BuildLaneOracle {
		return errors.New("manifest build_lane must be oss or oracle")
	}
	if m.ArtifactFilename != ManifestArtifact {
		return fmt.Errorf("manifest artifact_filename must be %q", ManifestArtifact)
	}
	if m.ArtifactSize == 0 || m.ArtifactSize > MaxRBFSize {
		return fmt.Errorf("manifest artifact_size must be between 1 and %d", MaxRBFSize)
	}
	if !manifestHashPattern.MatchString(m.ArtifactSHA256) {
		return errors.New("manifest artifact_sha256 must be 64 lowercase hexadecimal characters")
	}
	if !manifestCommitPattern.MatchString(m.SourceCommit) {
		return errors.New("manifest source_commit must be 40 lowercase hexadecimal characters")
	}
	return nil
}

// MarshalCanonical emits the compact, ordered schema-v1 representation with
// exactly one terminal newline.
func (m Manifest) MarshalCanonical() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return append(raw, '\n'), nil
}

// ParseManifest parses a strict schema-v1 manifest. Object members must occur
// in the canonical order, with no unknown or duplicate keys. The persisted
// bundle is canonical compact JSON with one newline, so alternate whitespace
// and escaped spellings are rejected after semantic validation.
func ParseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	if err := decodeStrictObject(raw, manifestFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &manifest.Schema)
		case "run_id":
			return decodeJSONString(value, &manifest.RunID)
		case "experiment":
			return decodeJSONString(value, &manifest.Experiment)
		case "board":
			return decodeJSONString(value, &manifest.Board)
		case "build_lane":
			return decodeJSONString(value, &manifest.BuildLane)
		case "artifact_filename":
			return decodeJSONString(value, &manifest.ArtifactFilename)
		case "artifact_size":
			return decodeJSONUint(value, &manifest.ArtifactSize)
		case "artifact_sha256":
			return decodeJSONString(value, &manifest.ArtifactSHA256)
		case "source_commit":
			return decodeJSONString(value, &manifest.SourceCommit)
		default:
			return fmt.Errorf("unknown manifest field %q", key)
		}
	}); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	canonical, err := manifest.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return Manifest{}, errors.New("manifest JSON is not canonical")
	}
	return manifest, nil
}

// decodeStrictObject is shared by the manifest and result parsers. It walks
// members instead of decoding through map[string]any, preserving duplicate
// detection and canonical field-order policy.
func decodeStrictObject(raw []byte, fields []string, assign func(string, []byte) error) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON object: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return errors.New("JSON value must be an object")
	}
	seen := make([]bool, len(fields))
	member := 0
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode JSON field: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("JSON object key is not a string")
		}
		index := -1
		for i, field := range fields {
			if field == key {
				index = i
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("unknown JSON field %q", key)
		}
		if seen[index] {
			return fmt.Errorf("duplicate JSON field %q", key)
		}
		if index != member {
			return fmt.Errorf("JSON field %q is out of canonical order", key)
		}
		seen[index] = true
		member++
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("decode JSON field %q: %w", key, err)
		}
		if err := assign(key, value); err != nil {
			return fmt.Errorf("field %q: %w", key, err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("close JSON object: %w", err)
	}
	if member != len(fields) {
		return fmt.Errorf("JSON object has %d fields; want %d", member, len(fields))
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON has trailing data")
		}
		return fmt.Errorf("JSON has trailing data: %w", err)
	}
	return nil
}

func decodeJSONString(raw []byte, target *string) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed[0] != '"' {
		return errors.New("value must be a JSON string")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("value has trailing data")
	}
	return nil
}

func decodeJSONUint(raw []byte, target *uint64) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || (trimmed[0] < '0' || trimmed[0] > '9') {
		return errors.New("value must be a non-negative integer")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("value has trailing data")
	}
	return nil
}
