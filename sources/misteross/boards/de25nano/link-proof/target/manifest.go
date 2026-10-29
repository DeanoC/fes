package main

// This standalone diagnostic handles producer receipts and ordinary files. It
// does not decode an Agilex bitstream or prove compatibility from its hash.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

const maxManifestBytes = 64 << 10
const maxArtifactBytes = 256 << 20

type blob struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type toolIdentity struct {
	Version          string `json:"version"`
	Edition          string `json:"edition"`
	QuartusShSHA256  string `json:"quartus_sh_sha256"`
	QuartusAsmSHA256 string `json:"quartus_asm_sha256"`
	QuartusPfgSHA256 string `json:"quartus_pfg_sha256"`
}

// These are PFG -i fields preserved by the producer. Matching them is a
// specific vendor-metadata check, not independently decoded compatibility.
type vendorPOF struct {
	RegionID   string `json:"region_id"`
	Hash       string `json:"hash"`
	InfoSHA256 string `json:"info_sha256"`
}

type shellIdentity struct {
	ID                  string    `json:"id"`
	ControlBuildID      string    `json:"control_build_id"`
	SourceRevision      string    `json:"source_revision"`
	SourceClosureSHA256 string    `json:"source_closure_sha256"`
	RootQDBSHA256       string    `json:"root_qdb_sha256"`
	InterfaceSHA256     string    `json:"interface_sha256"`
	ClockResetSHA256    string    `json:"clock_reset_sha256"`
	Base                blob      `json:"base"`
	VendorPOF           vendorPOF `json:"vendor_pof"`
}

type regionIdentity struct {
	ID        string `json:"id"`
	Instance  string `json:"instance"`
	Placement string `json:"placement"`
	Routing   string `json:"routing"`
}

type persona struct {
	ID               string    `json:"id"`
	HardwareID       uint32    `json:"hardware_id"`
	Entity           string    `json:"entity"`
	Revision         string    `json:"revision"`
	Path             string    `json:"path"`
	Size             int64     `json:"size"`
	SHA256           string    `json:"sha256"`
	RootQDBSHA256    string    `json:"root_qdb_sha256"`
	InterfaceSHA256  string    `json:"interface_sha256"`
	ClockResetSHA256 string    `json:"clock_reset_sha256"`
	RegionID         string    `json:"region_id"`
	VendorPOF        vendorPOF `json:"vendor_pof"`
}

type acceptance struct {
	Physical                 string `json:"physical"`
	Timing                   string `json:"timing"`
	BasePersonaCompatibility string `json:"base_persona_compatibility"`
}

type artifacts struct {
	Format         int            `json:"format"`
	Kind           string         `json:"kind"`
	DiagnosticOnly bool           `json:"diagnostic_only"`
	Device         string         `json:"device"`
	Tool           toolIdentity   `json:"tool"`
	Shell          shellIdentity  `json:"shell"`
	Region         regionIdentity `json:"region"`
	Personas       []persona      `json:"personas"`
	Acceptance     acceptance     `json:"acceptance"`
}

var hash64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var revision40 = regexp.MustCompile(`^[0-9a-f]{40}$`)
var build32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var rectangle = regexp.MustCompile(`^X([0-9]+) Y([0-9]+) X([0-9]+) Y([0-9]+)$`)

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// canonical uses sorted object keys and ASCII-only descriptors, shared with
// Python json.dumps(sort_keys=True,separators=(',',':'),ensure_ascii=True).
func canonical(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var tree any
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	if err = d.Decode(&tree); err != nil {
		return nil, err
	}
	return json.Marshal(tree)
}

// jsonObject rejects duplicates independently of canonical ordering. Its bounded
// walk also rejects deeply nested or trailing data before decoding typed fields.
func jsonObject(d *json.Decoder, depth int) error {
	if depth > 12 {
		return errors.New("descriptor JSON exceeds nesting bound")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				token, err = d.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || seen[key] {
					return errors.New("duplicate or invalid descriptor key")
				}
				seen[key] = true
				if err = jsonObject(d, depth+1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case '[':
			for d.More() {
				if err = jsonObject(d, depth+1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		default:
			return errors.New("unexpected descriptor delimiter")
		}
	}
	return nil
}

func parseArtifacts(data []byte) (artifacts, error) {
	var result artifacts
	if len(data) == 0 || len(data) > maxManifestBytes {
		return result, errors.New("descriptor size is outside proof bounds")
	}
	for _, b := range data {
		if b > 127 {
			return result, errors.New("descriptor must be ASCII JSON")
		}
	}
	walk := json.NewDecoder(bytes.NewReader(data))
	walk.UseNumber()
	if err := jsonObject(walk, 0); err != nil {
		return result, err
	}
	if _, err := walk.Token(); err != io.EOF {
		return result, errors.New("descriptor has trailing JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return result, err
	}
	want, err := canonical(result)
	if err != nil || !bytes.Equal(data, append(want, '\n')) {
		return result, errors.New("descriptor must use canonical closed JSON plus one newline")
	}
	return result, nil
}

func safePath(name string) bool {
	if name == "" || len(name) > 180 || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || part == "" {
			return false
		}
		for _, b := range []byte(part) {
			if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("._-", rune(b))) {
				return false
			}
		}
	}
	return true
}

func validBlob(b blob) bool {
	return safePath(b.Path) && b.Size > 0 && b.Size <= maxArtifactBytes && hash64.MatchString(b.SHA256)
}

func validVendorPOF(p vendorPOF) bool {
	return p.RegionID == "0x01000000" && hash64.MatchString(p.Hash) && p.Hash != strings.Repeat("0", 64) &&
		hash64.MatchString(p.InfoSHA256) && p.InfoSHA256 != strings.Repeat("0", 64)
}

func shellID(a artifacts) (string, error) {
	// Omit only id; all shell/tool/region/base inputs enter the identity.
	encoded, err := canonical(a.Shell)
	if err != nil {
		return "", err
	}
	var shell map[string]any
	if err = json.Unmarshal(encoded, &shell); err != nil {
		return "", err
	}
	delete(shell, "id")
	material, err := canonical(map[string]any{"device": a.Device, "tool": a.Tool, "shell": shell, "region": a.Region})
	if err != nil {
		return "", err
	}
	return digest(append([]byte("fes-de25-pr-shell-v2\x00"), material...)), nil
}

func validRectangle(value string) bool {
	if !rectangle.MatchString(value) {
		return false
	}
	var x0, y0, x1, y1 uint64
	if _, err := fmt.Sscanf(value, "X%d Y%d X%d Y%d", &x0, &y0, &x1, &y1); err != nil {
		return false
	}
	return x0 <= x1 && y0 <= y1 && x1 < 65536 && y1 < 65536
}

func validate(a artifacts, expectedShell, expectedBase string, selections []string) (persona, error) {
	var selected persona
	if !hash64.MatchString(expectedShell) || !hash64.MatchString(expectedBase) {
		return selected, errors.New("external expected shell ID and base SHA256 are required")
	}
	if a.Format != 2 || a.Kind != "fes.de25-pr-proof-artifacts" || !a.DiagnosticOnly || a.Device != "A5EB013BB23BE4SCS" {
		return selected, errors.New("unsupported proof format, kind, scope or device")
	}
	if a.Tool.Version != "26.3.0 Build114" || a.Tool.Edition != "SC Pro Edition" ||
		!hash64.MatchString(a.Tool.QuartusShSHA256) || !hash64.MatchString(a.Tool.QuartusAsmSHA256) || !hash64.MatchString(a.Tool.QuartusPfgSHA256) {
		return selected, errors.New("unsupported or incomplete Quartus tool identity")
	}
	if a.Acceptance.Physical != "not-run" || a.Acceptance.Timing != "passed" || a.Acceptance.BasePersonaCompatibility != "pfg-info-pof0-match" {
		return selected, errors.New("proof requires retained compiler/timing checks and physical not-run boundary")
	}
	s := a.Shell
	if !build32.MatchString(s.ControlBuildID) || s.ControlBuildID == strings.Repeat("0", 32) {
		return selected, errors.New("missing controlled-base identity")
	}
	if !revision40.MatchString(s.SourceRevision) || !hash64.MatchString(s.SourceClosureSHA256) || !hash64.MatchString(s.RootQDBSHA256) ||
		!hash64.MatchString(s.InterfaceSHA256) || !hash64.MatchString(s.ClockResetSHA256) || !validBlob(s.Base) || !validVendorPOF(s.VendorPOF) {
		return selected, errors.New("invalid shell or base identity")
	}
	id, err := shellID(a)
	if err != nil || s.ID != id || s.ID != expectedShell || s.Base.SHA256 != expectedBase {
		return selected, errors.New("shell or base differs from external selected descriptor")
	}
	if a.Region.ID != "slot0" || a.Region.Instance != "slot_inst" || !validRectangle(a.Region.Placement) || !validRectangle(a.Region.Routing) {
		return selected, errors.New("unsupported physical slot or bounded QSF region")
	}
	if len(a.Personas) != 2 || len(selections) != 1 {
		return selected, errors.New("one physical slot requires exactly one selected persona from two alternatives; selections overlap")
	}
	seenID, seenPath, seenRevision := map[string]bool{}, map[string]bool{s.Base.Path: true}, map[string]bool{}
	seenHardware := map[uint32]bool{}
	for i, p := range a.Personas {
		if p.HardwareID == 0 || seenHardware[p.HardwareID] {
			return selected, errors.New("invalid or duplicate persona hardware identity")
		}
		seenHardware[p.HardwareID] = true
		if !identifier.MatchString(p.ID) || !identifier.MatchString(p.Entity) || !identifier.MatchString(p.Revision) ||
			!validBlob(blob{p.Path, p.Size, p.SHA256}) || seenID[p.ID] || seenPath[p.Path] || seenRevision[p.Revision] ||
			(i > 0 && a.Personas[i-1].ID >= p.ID) {
			return selected, errors.New("invalid, duplicate or unordered persona identity")
		}
		seenID[p.ID], seenPath[p.Path], seenRevision[p.Revision] = true, true, true
		if p.RegionID != a.Region.ID || p.RootQDBSHA256 != s.RootQDBSHA256 || p.InterfaceSHA256 != s.InterfaceSHA256 || p.ClockResetSHA256 != s.ClockResetSHA256 {
			return selected, errors.New("persona does not match frozen shell, region, interface or clock/reset contract")
		}
		if !validVendorPOF(p.VendorPOF) || p.VendorPOF.RegionID != s.VendorPOF.RegionID || p.VendorPOF.Hash != s.VendorPOF.Hash {
			return selected, errors.New("persona PFG POF0 metadata does not match the selected base")
		}
		if p.ID == selections[0] {
			selected = p
		}
	}
	if selected.ID == "" {
		return selected, errors.New("selected persona is not in producer descriptor")
	}
	return selected, nil
}
