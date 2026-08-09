// Package materialobserve emits a deterministic, non-promotable catalog of
// the material identities seen by a Stage A0 candidate build.  It records
// observations without turning local image, source, or license facts into a
// final lock entry.
package materialobserve

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

const (
	FormatV1                = 1
	SchemaV1                = "fogcast.stage-a0.materials.v1"
	StatusCandidateObserved = "candidate-observed"
	SourceAvailabilityLocal = "local-only"
	licenseUnreviewed       = "unreviewed"

	toolchainURL      = "https://developer.arm.com/-/media/Files/downloads/gnu-a/10.2-2020.11/binrel/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf.tar.xz"
	upstreamURL       = "https://github.com/MiSTer-devel/Main_MiSTer.git"
	containerRef      = "localhost/stage-a0-firstbuild@sha256:24045e0e800b0ce7df88076ccab628387b149f1bd0786fab46fffae07a859d0c"
	containerManifest = "sha256:24045e0e800b0ce7df88076ccab628387b149f1bd0786fab46fffae07a859d0c"
	containerConfig   = "sha256:6a2a0fcb598a0a890355847900575e99a1cc7801f68d616edc85d449b9ae7548"
)

type Code string

const (
	CodeInputInvalid  Code = "MATERIAL_OBSERVE_INPUT_INVALID"
	CodeSchemaInvalid Code = "MATERIAL_OBSERVE_SCHEMA_INVALID"
	CodeHashInvalid   Code = "MATERIAL_OBSERVE_HASH_INVALID"
)

type Failure struct {
	Code   Code
	Detail string
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Detail }

// Manifest is a deterministic candidate catalog. Receipt and build-log
// digests bind the catalog to the reviewed observation, while Unresolved
// explicitly keeps the material/license review boundary visible.
type Manifest struct {
	Format             int              `json:"format"`
	Schema             string           `json:"schema"`
	Status             string           `json:"status"`
	SourceAvailability string           `json:"source_availability"`
	Authority          policy.Authority `json:"authority"`
	ReceiptSHA256      string           `json:"receipt_sha256"`
	BuildLogSHA256     string           `json:"build_log_sha256"`
	Records            []Record         `json:"records"`
	Unresolved         []string         `json:"unresolved"`
}

// Record describes one observed input or policy material. Empty fields are
// intentional for variants whose immutable identity is not yet available;
// final-lock validation remains the authority for the closed material union.
type Record struct {
	ID             string   `json:"id"`
	Role           string   `json:"role"`
	Kind           string   `json:"kind"`
	ParentID       string   `json:"parent_id"`
	Path           string   `json:"path"`
	URL            string   `json:"url"`
	Reference      string   `json:"reference"`
	ManifestDigest string   `json:"manifest_digest"`
	ConfigDigest   string   `json:"config_digest"`
	Commit         string   `json:"commit"`
	Tree           string   `json:"tree"`
	Root           string   `json:"root"`
	TreeSHA256     string   `json:"tree_sha256"`
	Size           int64    `json:"size"`
	SHA256         string   `json:"sha256"`
	LicenseState   string   `json:"license_state"`
	LicenseIDs     []string `json:"license_ids"`
}

// Request contains operational paths only. They are used to verify the
// observation, never serialized into Manifest.
type Request struct {
	Evidence         firstbuild.Evidence
	Authority        policy.Authority
	Receipt          []byte
	BuildLog         []byte
	SourceMaterialID string
	Repository       string
	ToolchainArchive string
	ToolchainRoot    string
	PolicyFiles      map[string][]byte
}

// Observe binds the candidate catalog to a reviewed first-build receipt and
// its authority. It verifies the pinned archive bytes and extraction root
// identity, but does not claim that the local container or licenses are
// durably retrievable/reviewed.
func Observe(request Request) (Manifest, error) {
	if err := firstbuild.ValidateCapturedEvidence(request.Evidence); err != nil {
		return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "receipt is not a reviewed capture"}
	}
	if request.Evidence.Source.Commit != request.Authority.ForkCommit || request.Evidence.Source.Tree != request.Authority.ForkTree || request.Evidence.Source.Parent != request.Authority.ForkParentCommit || request.Evidence.Build.SourceDateEpoch != request.Authority.SourceDateEpoch || request.Evidence.Build.VDate != request.Authority.VDate {
		return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "receipt does not match authority"}
	}
	if request.SourceMaterialID == "" || request.Repository == "" || request.ToolchainArchive == "" || request.ToolchainRoot == "" {
		return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "material observation inputs are incomplete"}
	}
	if !filepath.IsAbs(request.Repository) || !filepath.IsAbs(request.ToolchainArchive) || !filepath.IsAbs(request.ToolchainRoot) || filepath.Clean(request.Repository) != request.Repository || filepath.Clean(request.ToolchainArchive) != request.ToolchainArchive || filepath.Clean(request.ToolchainRoot) != request.ToolchainRoot {
		return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "material observation paths must be clean absolute paths"}
	}
	if info, err := os.Stat(request.Repository); err != nil || !info.IsDir() {
		return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "source repository is unavailable"}
	}
	rootInfo, err := os.Stat(request.ToolchainRoot)
	if err != nil || !rootInfo.IsDir() || filepath.Base(request.ToolchainRoot) != firstbuild.ExpectedToolchainArchiveRoot {
		return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "toolchain extraction root is not the pinned archive root"}
	}
	archiveInfo, err := os.Stat(request.ToolchainArchive)
	if err != nil || !archiveInfo.Mode().IsRegular() || archiveInfo.Size() != firstbuild.ExpectedToolchainArchiveSize {
		return Manifest{}, &Failure{Code: CodeHashInvalid, Detail: "toolchain archive size differs from receipt"}
	}
	archiveHash, err := hashFile(request.ToolchainArchive)
	if err != nil || archiveHash != firstbuild.ExpectedToolchainArchiveSHA256 {
		return Manifest{}, &Failure{Code: CodeHashInvalid, Detail: "toolchain archive hash differs from receipt"}
	}
	treeHash, err := verifyExtractedArchive(request.ToolchainArchive, request.ToolchainRoot)
	if err != nil {
		return Manifest{}, err
	}
	if len(request.Receipt) == 0 {
		request.Receipt, err = firstbuild.EncodeEvidence(request.Evidence)
		if err != nil {
			return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "receipt cannot be encoded"}
		}
	}
	if len(request.BuildLog) == 0 {
		return Manifest{}, &Failure{Code: CodeInputInvalid, Detail: "build log is empty"}
	}
	if err := validateBuildLog(request.BuildLog, request.Authority); err != nil {
		return Manifest{}, err
	}
	receiptHash := digest(request.Receipt)
	logHash := digest(request.BuildLog)
	policies, err := policyRecords(request.PolicyFiles, request.SourceMaterialID)
	if err != nil {
		return Manifest{}, err
	}
	records := []Record{
		{ID: "container", Role: "consumed-build-input", Kind: "oci", Reference: containerRef, ManifestDigest: containerManifest, ConfigDigest: containerConfig, LicenseState: licenseUnreviewed, LicenseIDs: []string{}},
		{ID: request.SourceMaterialID, Role: "consumed-build-input", Kind: "git-local", Commit: request.Evidence.Source.Commit, Tree: request.Evidence.Source.Tree, LicenseState: licenseUnreviewed, LicenseIDs: []string{}},
		{ID: "main-upstream", Role: "consumed-build-input", Kind: "git-https", URL: upstreamURL, Commit: request.Authority.UpstreamCommit, Tree: request.Authority.UpstreamTree, LicenseState: licenseUnreviewed, LicenseIDs: []string{}},
		{ID: "toolchain", Role: "consumed-build-input", Kind: "archive-https", URL: toolchainURL, Root: firstbuild.ExpectedToolchainArchiveRoot, TreeSHA256: treeHash, Size: archiveInfo.Size(), SHA256: archiveHash, LicenseState: licenseUnreviewed, LicenseIDs: []string{}},
	}
	records = append(records, policies...)
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	manifest := Manifest{
		Format: FormatV1, Schema: SchemaV1, Status: StatusCandidateObserved, SourceAvailability: SourceAvailabilityLocal,
		Authority: request.Authority, ReceiptSHA256: receiptHash, BuildLogSHA256: logHash, Records: records,
		Unresolved: []string{"build-log-review-identity", "build-utility-license-review", "container-durable-provenance", "fork-durable-retrieval", "material-license-review"},
	}
	if err := Validate(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func policyRecords(files map[string][]byte, parent string) ([]Record, error) {
	want := []string{"compile-link.json", "elf-dependency.json", "generated-input.json", "intermediate-path.json", "source-set.json", "upstream-fork-delta.json"}
	if len(files) != len(want) {
		return nil, &Failure{Code: CodeInputInvalid, Detail: "policy material inventory is not exactly six files"}
	}
	records := make([]Record, 0, len(want))
	for _, name := range want {
		raw, ok := files[name]
		if !ok || len(raw) == 0 {
			return nil, &Failure{Code: CodeInputInvalid, Detail: "policy material is missing: " + name}
		}
		stem := strings.TrimSuffix(name, ".json")
		records = append(records, Record{ID: "policy-" + stem, Role: "consumed-build-input", Kind: "material-file", ParentID: parent, Path: name, Size: int64(len(raw)), SHA256: digest(raw), LicenseState: licenseUnreviewed, LicenseIDs: []string{}})
	}
	return records, nil
}

func validateBuildLog(raw []byte, authority policy.Authority) error {
	log := string(raw)
	if !strings.Contains(log, "SOURCE_DATE_EPOCH="+strconv.FormatInt(authority.SourceDateEpoch, 10)) || !strings.Contains(log, "make clean VDATE="+authority.VDate) || !strings.Contains(log, "make V=1 VDATE="+authority.VDate) {
		return &Failure{Code: CodeInputInvalid, Detail: "build log does not contain the reviewed build recipe"}
	}
	want := "STAGE_A0_JOB_COUNT=" + strconv.Itoa(firstbuild.ExpectedJobCount)
	seen := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "STAGE_A0_JOB_COUNT=") {
			if line != want {
				return &Failure{Code: CodeInputInvalid, Detail: "build log job count differs from the adapter contract"}
			}
			seen++
		}
	}
	if seen != 1 {
		return &Failure{Code: CodeInputInvalid, Detail: "build log has no unique adapter job-count observation"}
	}
	return nil
}

func Encode(manifest Manifest) ([]byte, error) {
	if err := Validate(manifest); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, &Failure{Code: CodeSchemaInvalid, Detail: "material manifest cannot be encoded"}
	}
	return append(raw, '\n'), nil
}

func Decode(raw []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, &Failure{Code: CodeSchemaInvalid, Detail: "material manifest JSON cannot be decoded"}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Manifest{}, &Failure{Code: CodeSchemaInvalid, Detail: "material manifest JSON has trailing data"}
	}
	canonical, err := Encode(manifest)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Manifest{}, &Failure{Code: CodeSchemaInvalid, Detail: "material manifest JSON is not canonical"}
	}
	return manifest, nil
}

func Validate(manifest Manifest) error {
	if manifest.Format != FormatV1 || manifest.Schema != SchemaV1 || manifest.Status != StatusCandidateObserved || manifest.SourceAvailability != SourceAvailabilityLocal || len(manifest.Records) < 4 || len(manifest.Unresolved) == 0 {
		return &Failure{Code: CodeSchemaInvalid, Detail: "material manifest envelope is invalid"}
	}
	if !lowerHex(manifest.ReceiptSHA256) || !lowerHex(manifest.BuildLogSHA256) {
		return &Failure{Code: CodeHashInvalid, Detail: "material observation digest is invalid"}
	}
	if err := validateAuthority(manifest.Authority); err != nil {
		return err
	}
	previous := ""
	seen := make(map[string]struct{}, len(manifest.Records))
	for _, record := range manifest.Records {
		if record.ID == "" || record.ID <= previous || strings.ContainsAny(record.ID, "/\\") {
			return &Failure{Code: CodeSchemaInvalid, Detail: "material records are not strictly ordered"}
		}
		previous = record.ID
		if _, ok := seen[record.ID]; ok {
			return &Failure{Code: CodeSchemaInvalid, Detail: "material record is duplicated"}
		}
		seen[record.ID] = struct{}{}
		if record.Role == "" || record.Kind == "" || record.LicenseState != licenseUnreviewed || record.Size < 0 || (record.SHA256 != "" && !lowerHex(record.SHA256)) || (record.TreeSHA256 != "" && !lowerHex(record.TreeSHA256)) {
			return &Failure{Code: CodeSchemaInvalid, Detail: "material record is invalid: " + record.ID}
		}
		if record.Kind == "material-file" && (record.ParentID == "" || record.Path == "" || record.Size == 0 || record.SHA256 == "") {
			return &Failure{Code: CodeSchemaInvalid, Detail: "material-file record is incomplete: " + record.ID}
		}
	}
	for i := 1; i < len(manifest.Unresolved); i++ {
		if manifest.Unresolved[i-1] >= manifest.Unresolved[i] {
			return &Failure{Code: CodeSchemaInvalid, Detail: "unresolved entries are not sorted"}
		}
	}
	return nil
}

func validateAuthority(authority policy.Authority) error {
	for _, value := range []string{authority.UpstreamCommit, authority.UpstreamTree, authority.ForkCommit, authority.ForkTree, authority.ForkParentCommit} {
		if !lowerHexLength(value, 40) {
			return &Failure{Code: CodeSchemaInvalid, Detail: "material authority identity is invalid"}
		}
	}
	if authority.ForkParentCommit != authority.UpstreamCommit || authority.VDate == "" || authority.SourceDateEpoch <= 0 || len(authority.PatchCommits) == 0 {
		return &Failure{Code: CodeSchemaInvalid, Detail: "material authority ancestry is invalid"}
	}
	return nil
}

func hashFile(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyExtractedArchive(archive, extractedRoot string) (string, error) {
	tarPath, err := exec.LookPath("tar")
	if err != nil {
		return "", &Failure{Code: CodeInputInvalid, Detail: "tar is unavailable for archive verification"}
	}
	staging, err := os.MkdirTemp("", ".stage-a0-material-*")
	if err != nil {
		return "", &Failure{Code: CodeInputInvalid, Detail: "archive verification root cannot be created"}
	}
	defer os.RemoveAll(staging)
	command := exec.Command(tarPath, "-xJf", archive, "-C", staging)
	command.Env = []string{"LC_ALL=C", "TZ=UTC", "PATH=" + filepath.Dir(tarPath)}
	if output, err := command.CombinedOutput(); err != nil {
		return "", &Failure{Code: CodeHashInvalid, Detail: "toolchain archive cannot be re-extracted: " + strings.TrimSpace(string(output))}
	}
	archiveRoot := filepath.Join(staging, firstbuild.ExpectedToolchainArchiveRoot)
	archiveHash, err := treeDigest(archiveRoot)
	if err != nil {
		return "", &Failure{Code: CodeHashInvalid, Detail: "toolchain archive extraction is unsafe"}
	}
	observedHash, err := treeDigest(extractedRoot)
	if err != nil {
		return "", &Failure{Code: CodeHashInvalid, Detail: "toolchain extraction tree is unsafe"}
	}
	if archiveHash != observedHash {
		return "", &Failure{Code: CodeHashInvalid, Detail: "toolchain extraction tree differs from pinned archive"}
	}
	return observedHash, nil
}

type treeEntry struct {
	Path   string
	Mode   uint32
	Kind   string
	Size   int64
	Target string
	SHA256 string
}

func treeDigest(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", os.ErrInvalid
	}
	entries := make([]treeEntry, 0)
	err = filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		record := treeEntry{Path: relative, Mode: uint32(info.Mode().Perm())}
		switch {
		case info.IsDir():
			record.Kind = "directory"
		case info.Mode()&os.ModeSymlink != 0:
			record.Kind = "symlink"
			target, err := os.Readlink(filename)
			if err != nil {
				return err
			}
			record.Target = target
			resolved := target
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(filepath.Dir(filename), resolved)
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil || !pathWithin(root, resolved) {
				return os.ErrInvalid
			}
		case info.Mode().IsRegular():
			record.Kind = "regular"
			record.Size = info.Size()
			record.SHA256, err = hashFile(filename)
			if err != nil {
				return err
			}
		default:
			return os.ErrInvalid
		}
		entries = append(entries, record)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	hash := sha256.New()
	for _, entry := range entries {
		_, _ = io.WriteString(hash, entry.Path+"\x00"+entry.Kind+"\x00"+strconv.FormatUint(uint64(entry.Mode), 8)+"\x00")
		_, _ = io.WriteString(hash, entry.Target+"\x00"+entry.SHA256+"\x00")
		_, _ = io.WriteString(hash, strconv.FormatInt(entry.Size, 10)+"\n")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func lowerHex(value string) bool {
	return lowerHexLength(value, sha256.Size*2)
}

func lowerHexLength(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
