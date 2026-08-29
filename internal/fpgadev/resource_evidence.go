package fpgadev

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	ResourceEvidenceSchemaVersion = uint64(2)
	MaxResourceEvidenceBytes      = 2048
	maxResourceEvidenceBytes      = MaxResourceEvidenceBytes
	maxResourceEvidenceCount      = uint64(^uint32(0))
	maxBundleChecksumBytes        = 4096
)

var resourceEvidenceHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ResourceEvidenceV2 is the unsigned, schema-2 resource contract emitted by
// the accepted misteross producer.  The field order is part of the canonical
// wire format; keep this structure in producer order.
type ResourceEvidenceV2 struct {
	Schema                      uint64
	Experiment                  string
	Board                       string
	BuildLane                   string
	SourceCommit                string
	ArtifactSHA256              string
	SynthesisReportSHA256       string
	ClockInputs                 uint64
	ExternalInputPorts          uint64
	ExternalOutputPorts         uint64
	BidirectionalPorts          uint64
	HPSGeneralPurposeInterfaces uint64
	PLLBlocks                   uint64
	DSPBlocks                   uint64
	BlockMemoryBits             uint64
	LUTRAMBits                  uint64
	SDRAMInterfaces             uint64
}

type resourceEvidenceWire struct {
	Schema                      uint64 `json:"schema"`
	Experiment                  string `json:"experiment"`
	Board                       string `json:"board"`
	BuildLane                   string `json:"build_lane"`
	SourceCommit                string `json:"source_commit"`
	ArtifactSHA256              string `json:"artifact_sha256"`
	SynthesisReportSHA256       string `json:"synthesis_report_sha256"`
	ClockInputs                 uint64 `json:"clock_inputs"`
	ExternalInputPorts          uint64 `json:"external_input_ports"`
	ExternalOutputPorts         uint64 `json:"external_output_ports"`
	BidirectionalPorts          uint64 `json:"bidirectional_ports"`
	HPSGeneralPurposeInterfaces uint64 `json:"hps_general_purpose_interfaces"`
	PLLBlocks                   uint64 `json:"pll_blocks"`
	DSPBlocks                   uint64 `json:"dsp_blocks"`
	BlockMemoryBits             uint64 `json:"block_memory_bits"`
	LUTRAMBits                  uint64 `json:"lutram_bits"`
	SDRAMInterfaces             uint64 `json:"sdram_interfaces"`
}

var resourceEvidenceFields = []string{
	"schema",
	"experiment",
	"board",
	"build_lane",
	"source_commit",
	"artifact_sha256",
	"synthesis_report_sha256",
	"clock_inputs",
	"external_input_ports",
	"external_output_ports",
	"bidirectional_ports",
	"hps_general_purpose_interfaces",
	"pll_blocks",
	"dsp_blocks",
	"block_memory_bits",
	"lutram_bits",
	"sdram_interfaces",
}

func (e ResourceEvidenceV2) wire() resourceEvidenceWire {
	return resourceEvidenceWire{
		Schema:                      e.Schema,
		Experiment:                  e.Experiment,
		Board:                       e.Board,
		BuildLane:                   e.BuildLane,
		SourceCommit:                e.SourceCommit,
		ArtifactSHA256:              e.ArtifactSHA256,
		SynthesisReportSHA256:       e.SynthesisReportSHA256,
		ClockInputs:                 e.ClockInputs,
		ExternalInputPorts:          e.ExternalInputPorts,
		ExternalOutputPorts:         e.ExternalOutputPorts,
		BidirectionalPorts:          e.BidirectionalPorts,
		HPSGeneralPurposeInterfaces: e.HPSGeneralPurposeInterfaces,
		PLLBlocks:                   e.PLLBlocks,
		DSPBlocks:                   e.DSPBlocks,
		BlockMemoryBits:             e.BlockMemoryBits,
		LUTRAMBits:                  e.LUTRAMBits,
		SDRAMInterfaces:             e.SDRAMInterfaces,
	}
}

// MarshalCanonical returns the exact compact JSON representation accepted by
// ParseResourceEvidenceV2.  A final newline is required by the producer
// contract and is included here.
func (e ResourceEvidenceV2) MarshalCanonical() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(e.wire())
	if err != nil {
		return nil, fmt.Errorf("marshal resource evidence: %w", err)
	}
	return append(b, '\n'), nil
}

// ParseResourceEvidenceV2 parses only the canonical unsigned schema-2
// producer record.  Signed/schema-1 and non-canonical JSON are deliberately
// rejected rather than normalized.
func ParseResourceEvidenceV2(raw []byte) (ResourceEvidenceV2, error) {
	var zero ResourceEvidenceV2
	if len(raw) == 0 || len(raw) > maxResourceEvidenceBytes {
		return zero, fmt.Errorf("resource evidence size %d outside 1..%d", len(raw), maxResourceEvidenceBytes)
	}
	var e ResourceEvidenceV2
	err := decodeStrictObject(raw, resourceEvidenceFields, func(key string, value []byte) error {
		switch key {
		case "schema":
			n, err := decodeEvidenceUint(value)
			e.Schema = n
			return err
		case "experiment":
			return decodeJSONString(value, &e.Experiment)
		case "board":
			return decodeJSONString(value, &e.Board)
		case "build_lane":
			return decodeJSONString(value, &e.BuildLane)
		case "source_commit":
			return decodeJSONString(value, &e.SourceCommit)
		case "artifact_sha256":
			return decodeJSONString(value, &e.ArtifactSHA256)
		case "synthesis_report_sha256":
			return decodeJSONString(value, &e.SynthesisReportSHA256)
		case "clock_inputs":
			n, err := decodeEvidenceUint(value)
			e.ClockInputs = n
			return err
		case "external_input_ports":
			n, err := decodeEvidenceUint(value)
			e.ExternalInputPorts = n
			return err
		case "external_output_ports":
			n, err := decodeEvidenceUint(value)
			e.ExternalOutputPorts = n
			return err
		case "bidirectional_ports":
			n, err := decodeEvidenceUint(value)
			e.BidirectionalPorts = n
			return err
		case "hps_general_purpose_interfaces":
			n, err := decodeEvidenceUint(value)
			e.HPSGeneralPurposeInterfaces = n
			return err
		case "pll_blocks":
			n, err := decodeEvidenceUint(value)
			e.PLLBlocks = n
			return err
		case "dsp_blocks":
			n, err := decodeEvidenceUint(value)
			e.DSPBlocks = n
			return err
		case "block_memory_bits":
			n, err := decodeEvidenceUint(value)
			e.BlockMemoryBits = n
			return err
		case "lutram_bits":
			n, err := decodeEvidenceUint(value)
			e.LUTRAMBits = n
			return err
		case "sdram_interfaces":
			n, err := decodeEvidenceUint(value)
			e.SDRAMInterfaces = n
			return err
		default:
			return fmt.Errorf("unknown resource evidence field %q", key)
		}
	})
	if err != nil {
		return zero, fmt.Errorf("resource evidence object: %w", err)
	}
	if err := e.Validate(); err != nil {
		return zero, err
	}
	canonical, err := e.MarshalCanonical()
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(raw, canonical) {
		return zero, errors.New("resource evidence is not canonical")
	}
	return e, nil
}

func decodeEvidenceUint(raw json.RawMessage) (uint64, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, errors.New("must be a JSON integer")
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errors.New("must be a JSON integer")
		}
	}
	var n uint64
	for _, ch := range s {
		digit := uint64(ch - '0')
		if n > (^uint64(0)-digit)/10 {
			return 0, errors.New("integer overflows uint64")
		}
		n = n*10 + digit
	}
	return n, nil
}

func (e ResourceEvidenceV2) Validate() error {
	if e.Schema != ResourceEvidenceSchemaVersion {
		return fmt.Errorf("unsupported resource evidence schema %d", e.Schema)
	}
	if e.Experiment != ManifestExperiment {
		return fmt.Errorf("unexpected experiment %q", e.Experiment)
	}
	if e.Board != ManifestBoard {
		return fmt.Errorf("unexpected board %q", e.Board)
	}
	if e.BuildLane != BuildLaneOSS && e.BuildLane != BuildLaneOracle {
		return fmt.Errorf("unexpected build lane %q", e.BuildLane)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(e.SourceCommit) {
		return errors.New("source_commit must be 40 lowercase hexadecimal characters")
	}
	if !resourceEvidenceHashPattern.MatchString(e.ArtifactSHA256) {
		return errors.New("artifact_sha256 must be 64 lowercase hexadecimal characters")
	}
	if !resourceEvidenceHashPattern.MatchString(e.SynthesisReportSHA256) {
		return errors.New("synthesis_report_sha256 must be 64 lowercase hexadecimal characters")
	}
	for name, value := range map[string]uint64{
		"clock_inputs":                   e.ClockInputs,
		"external_input_ports":           e.ExternalInputPorts,
		"external_output_ports":          e.ExternalOutputPorts,
		"bidirectional_ports":            e.BidirectionalPorts,
		"hps_general_purpose_interfaces": e.HPSGeneralPurposeInterfaces,
		"pll_blocks":                     e.PLLBlocks,
		"dsp_blocks":                     e.DSPBlocks,
		"block_memory_bits":              e.BlockMemoryBits,
		"lutram_bits":                    e.LUTRAMBits,
		"sdram_interfaces":               e.SDRAMInterfaces,
	} {
		if value > maxResourceEvidenceCount {
			return fmt.Errorf("%s exceeds uint32 range", name)
		}
	}
	if e.ClockInputs != 1 {
		return fmt.Errorf("clock_inputs must be 1, got %d", e.ClockInputs)
	}
	if e.HPSGeneralPurposeInterfaces != 1 {
		return fmt.Errorf("hps_general_purpose_interfaces must be 1, got %d", e.HPSGeneralPurposeInterfaces)
	}
	if e.ExternalInputPorts != 0 || e.ExternalOutputPorts != 0 || e.BidirectionalPorts != 0 ||
		e.PLLBlocks != 0 || e.DSPBlocks != 0 || e.BlockMemoryBits != 0 || e.LUTRAMBits != 0 || e.SDRAMInterfaces != 0 {
		return errors.New("resource evidence contains forbidden or unsupported resources")
	}
	return nil
}

func (e ResourceEvidenceV2) MatchesManifest(m Manifest) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.Experiment != m.Experiment || e.Board != m.Board || e.BuildLane != m.BuildLane || e.SourceCommit != m.SourceCommit {
		return errors.New("resource evidence identity does not match manifest")
	}
	if e.ArtifactSHA256 != m.ArtifactSHA256 {
		return errors.New("resource evidence artifact hash does not match manifest")
	}
	return nil
}

type ArtifactBundle struct {
	Manifest     Manifest
	Evidence     ResourceEvidenceV2
	Artifact     ArtifactMetadata
	BundleSHA256 map[string]string
}

var requiredBundleMembers = []string{"manifest.json", ManifestEvidence, ManifestArtifact}

// ParseBundleSHA256 parses the exact three-line checksum contract generated by
// the accepted producer.  The entries are the three payload members.
func ParseBundleSHA256(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > maxBundleChecksumBytes || !bytes.HasSuffix(raw, []byte{'\n'}) {
		return nil, errors.New("bundle.sha256 must end with one newline")
	}
	lines := bytes.Split(raw, []byte{'\n'})
	if len(lines) != len(requiredBundleMembers)+1 || len(lines[len(lines)-1]) != 0 {
		return nil, errors.New("bundle.sha256 has unexpected line count")
	}
	got := make(map[string]string, len(requiredBundleMembers)+1)
	for i, line := range lines[:len(lines)-1] {
		parts := bytes.Split(line, []byte("  "))
		if len(parts) != 2 || string(parts[1]) == "" || strings.Contains(string(parts[1]), " ") {
			return nil, errors.New("bundle.sha256 entries must be '<hash>  <name>'")
		}
		name := string(parts[1])
		if !resourceEvidenceHashPattern.MatchString(string(parts[0])) {
			return nil, fmt.Errorf("bundle.sha256 line %d has invalid hash", i+1)
		}
		if name != requiredBundleMembers[i] {
			return nil, errors.New("bundle.sha256 members are not in canonical order")
		}
		if _, exists := got[name]; exists {
			return nil, errors.New("bundle.sha256 has duplicate member")
		}
		got[name] = string(parts[0])
	}
	return got, nil
}

// VerifyArtifactBundle verifies checksum entries before parsing any evidence,
// then binds the evidence identity to the manifest and an opened RBF
// descriptor. The platform implementation opens the protected directory
// once and reads every member relative to that descriptor.
func VerifyArtifactBundle(staging string, expectedUID ...uint32) (ArtifactBundle, error) {
	// Standalone/offline callers default to their own UID; target production
	// callers pass the root UID retained by ArtifactBinding explicitly.
	uid := uint32(os.Getuid())
	if len(expectedUID) == 1 {
		uid = expectedUID[0]
	} else if len(expectedUID) > 1 {
		return ArtifactBundle{}, errors.New("expected UID must be supplied at most once")
	}
	return verifyArtifactBundleAtPath(staging, uid)
}

// LoadArtifactBundle is a descriptive alias used by callers that load a
// verified staging directory rather than binding it to a target.
func LoadArtifactBundle(staging string, expectedUID ...uint32) (ArtifactBundle, error) {
	return VerifyArtifactBundle(staging, expectedUID...)
}
