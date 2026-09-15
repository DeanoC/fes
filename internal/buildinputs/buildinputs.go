// Package buildinputs reads the sealed native image identity without hashing
// live binaries on each health poll.
package buildinputs

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultBuildInputs = "/usr/share/mister-runtime/build-inputs"
	DefaultSelections  = "/usr/share/mister-runtime/selections"
	maxRecordBytes     = 64 << 10
)

// Paths names the sealed identity files on a native target. Empty fields use
// the image-installed defaults.
type Paths struct {
	BuildInputs string
	Selections  string
	BootJSON    string
	BootIDFile  string
}

type selectionFile struct {
	System string `toml:"system"`
	SHA256 string `toml:"sha256"`
}

var (
	sha256Value = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitValue = regexp.MustCompile(`^[0-9a-f]{40}$`)
	coreSHAKeys = map[string]string{
		"megadrive_sha256": "megadrive",
		"pong_sha256":      "pong",
		"snes_sha256":      "snes",
		"nes_sha256":       "nes",
	}
	fesPackageIDKeys = []string{
		"fes_pong_package_id",
		"fes_zx81_package_id",
		"fes_coleco_package_id",
	}
)

func (p Paths) withDefaults() Paths {
	if p.BuildInputs == "" {
		p.BuildInputs = DefaultBuildInputs
	}
	if p.Selections == "" {
		p.Selections = DefaultSelections
	}
	if p.BootJSON == "" {
		p.BootJSON = appliance.DefaultRoot + "/boot.json"
	}
	if p.BootIDFile == "" {
		p.BootIDFile = "/proc/sys/kernel/random/boot_id"
	}
	return p
}

// Snapshot returns the closed artifact identity available on this target.
// Missing files omit fields; the result is never nil when revision is set.
func Snapshot(paths Paths, revision string) *protocol.Artifacts {
	paths = paths.withDefaults()
	artifacts := &protocol.Artifacts{AgentRevision: strings.TrimSpace(revision)}
	fields, digest, err := readRecord(paths.BuildInputs)
	if err == nil {
		artifacts.RecordSHA256 = digest
		applyRecord(artifacts, fields)
	}
	applySelections(artifacts, paths.Selections)
	if boot, err := applianceupdate.ReadBootIdentity(paths.BootJSON, paths.BootIDFile); err == nil {
		artifacts.ImageSHA256 = boot.ImageSHA256
	}
	if artifacts.Cores != nil && len(artifacts.Cores) == 0 {
		artifacts.Cores = nil
	}
	if empty(artifacts) {
		return nil
	}
	return artifacts
}

func empty(artifacts *protocol.Artifacts) bool {
	return artifacts.RecordSHA256 == "" && artifacts.RuntimeCommit == "" &&
		artifacts.AgentSHA256 == "" && artifacts.AgentRevision == "" &&
		artifacts.KitSHA256 == "" && artifacts.ImageSHA256 == "" &&
		artifacts.IdleSHA256 == "" && artifacts.ABI == "" &&
		artifacts.PackageID == "" && len(artifacts.Cores) == 0
}

func readRecord(path string) (map[string]string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecordBytes {
		return nil, "", os.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxRecordBytes {
		return nil, "", os.ErrInvalid
	}
	sum := sha256.Sum256(data)
	fields := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, "", os.ErrInvalid
		}
		if _, exists := fields[key]; exists {
			return nil, "", os.ErrInvalid
		}
		fields[key] = value
	}
	if format, ok := fields["format"]; ok && format != "1" {
		return nil, "", os.ErrInvalid
	}
	return fields, hex.EncodeToString(sum[:]), nil
}

func applyRecord(artifacts *protocol.Artifacts, fields map[string]string) {
	artifacts.RuntimeCommit = digestIf(commitValue, fields["mister_runtime_commit"])
	artifacts.AgentSHA256 = digestIf(sha256Value, fields["mister_agent_sha256"])
	artifacts.KitSHA256 = digestIf(sha256Value, fields["fogcast_kit_sha256"])
	artifacts.IdleSHA256 = digestIf(sha256Value, fields["idle_sha256"])
	if abi := fields["megadrive_abi"]; abi != "" && !strings.ContainsAny(abi, " \t") {
		artifacts.ABI = abi
	}
	// PackageID is the legacy scalar health field. Keep Pong as the stable
	// primary identity when present. A non-Pong identity is reported only when
	// it is the sole valid FES package identity; several non-Pong records cannot
	// be represented by this scalar without misleading health consumers.
	packageIDs := make([]string, 0, len(fesPackageIDKeys))
	for _, key := range fesPackageIDKeys {
		if packageID := digestIf(sha256Value, fields[key]); packageID != "" {
			packageIDs = append(packageIDs, packageID)
		}
	}
	if pongID := digestIf(sha256Value, fields["fes_pong_package_id"]); pongID != "" {
		artifacts.PackageID = pongID
	} else if len(packageIDs) == 1 {
		artifacts.PackageID = packageIDs[0]
	}
	for key, system := range coreSHAKeys {
		if value := digestIf(sha256Value, fields[key]); value != "" {
			setCore(artifacts, system, value)
		}
	}
}

func applySelections(artifacts *protocol.Artifacts, directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil || int64(len(data)) > maxRecordBytes {
			continue
		}
		var selection selectionFile
		if toml.Unmarshal(data, &selection) != nil {
			continue
		}
		system := strings.TrimSpace(selection.System)
		if system == "" {
			system = strings.TrimSuffix(entry.Name(), ".toml")
		}
		digest := digestIf(sha256Value, selection.SHA256)
		if digest == "" || artifacts.Cores[system] != "" {
			continue
		}
		setCore(artifacts, system, digest)
	}
}

func digestIf(pattern *regexp.Regexp, value string) string {
	if pattern.MatchString(value) {
		return value
	}
	return ""
}

func setCore(artifacts *protocol.Artifacts, system, digest string) {
	if artifacts.Cores == nil {
		artifacts.Cores = map[string]string{}
	}
	artifacts.Cores[system] = digest
}
