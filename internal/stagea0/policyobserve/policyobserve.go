// Package policyobserve turns immutable local Git observations into
// non-promotable Stage A0 policy candidates.  It deliberately does not infer
// compiler commands, generated files, or dependency closure; those require a
// separately captured build observation.
package policyobserve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

type Code string

const (
	CodeInputInvalid          Code = "POLICY_OBSERVE_INPUT_INVALID"
	CodeGitObservationInvalid Code = "POLICY_OBSERVE_GIT_INVALID"
	CodeCommandFailed         Code = "POLICY_OBSERVE_COMMAND_FAILED"
)

type Failure struct {
	Code   Code
	Detail string
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Detail }

// ObserveSourceSet records the tracked source tree at authority.ForkCommit.
// The result is candidate-direct-inputs: it is evidence from Git only and is
// never sufficient to authorize a final lock.
func ObserveSourceSet(repository string, authority policy.Authority, materialID string) (policy.Document, error) {
	root, err := validateRepository(repository)
	if err != nil {
		return policy.Document{}, err
	}
	if !safeMaterialID(materialID) {
		return policy.Document{}, invalidInput("material ID is invalid")
	}
	if err := verifyAuthority(root, authority); err != nil {
		return policy.Document{}, err
	}
	entries, err := lsTree(root, authority.ForkCommit)
	if err != nil {
		return policy.Document{}, err
	}
	var makefile *treeEntry
	records := make([]policy.SourceRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.Path == "Makefile" {
			copy := entry
			makefile = &copy
			continue
		}
		if entry.Mode != "100644" {
			return policy.Document{}, gitInvalid("tracked source has unsupported mode: " + entry.Path)
		}
		records = append(records, policy.SourceRecord{
			Path:            entry.Path,
			GitMode:         entry.Mode,
			BlobOID:         entry.OID,
			SHA256:          entry.SHA256,
			MaterialID:      materialID,
			InclusionReason: inclusionReason(entry.Path),
		})
	}
	if makefile == nil {
		return policy.Document{}, gitInvalid("tracked Makefile is missing")
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	document := policy.Document{
		Format:               policy.FormatV1,
		Schema:               policy.SchemaFor(policy.KindSourceSet),
		Kind:                 policy.KindSourceSet,
		Authority:            authority,
		NormalizationVersion: policy.NormalizationV1,
		SourceSet: &policy.SourceSetPolicy{
			Completeness:        policy.CompletenessDirect,
			BuildProfile:        policy.BuildProfile{Debug: 0, Profiling: 0, Verbose: 1},
			Makefile:            sourceRecord(*makefile, materialID, "build-config"),
			ForkSourceSetChange: "none",
			Records:             records,
		},
	}
	if err := policy.Validate(document); err != nil {
		return policy.Document{}, gitInvalid("generated source-set candidate is invalid: " + err.Error())
	}
	return document, nil
}

// ObserveForkDelta records the isolated parent-to-HEAD Git delta.  It only
// emits an observed candidate; promotion separately proves the delta is the
// single approved Makefile VDATE patch.
func ObserveForkDelta(repository string, authority policy.Authority) (policy.Document, error) {
	root, err := validateRepository(repository)
	if err != nil {
		return policy.Document{}, err
	}
	if err := verifyAuthority(root, authority); err != nil {
		return policy.Document{}, err
	}
	if authority.ForkParentCommit != authority.UpstreamCommit {
		return policy.Document{}, gitInvalid("fork parent does not equal upstream commit")
	}
	parent, err := treeEntryAt(root, authority.UpstreamCommit, "Makefile")
	if err != nil {
		return policy.Document{}, err
	}
	fork, err := treeEntryAt(root, authority.ForkCommit, "Makefile")
	if err != nil {
		return policy.Document{}, err
	}
	changed, err := runGitOutput(root, "diff", "--name-status", "--no-renames", authority.UpstreamCommit, authority.ForkCommit, "--")
	if err != nil {
		return policy.Document{}, err
	}
	if changed != "M\tMakefile\n" {
		return policy.Document{}, gitInvalid("fork delta contains a path other than the approved Makefile patch")
	}
	patch, err := runGitOutput(root, "diff", "--no-ext-diff", "--binary", authority.UpstreamCommit, authority.ForkCommit, "--", "Makefile")
	if err != nil {
		return policy.Document{}, err
	}
	document := policy.Document{
		Format:               policy.FormatV1,
		Schema:               policy.SchemaFor(policy.KindForkDelta),
		Kind:                 policy.KindForkDelta,
		Authority:            authority,
		NormalizationVersion: policy.NormalizationV1,
		ForkDelta: &policy.ForkDeltaPolicy{
			Completeness:        policy.CompletenessObserved,
			Purpose:             "deterministic-vdate-input",
			PatchDiffSHA256:     digest([]byte(patch)),
			ForkSourceSetChange: "none",
			ChangedPaths: []policy.ChangedPath{{
				Path:      "Makefile",
				Status:    "modified",
				OldMode:   parent.Mode,
				NewMode:   fork.Mode,
				OldBlob:   parent.OID,
				NewBlob:   fork.OID,
				OldSHA256: parent.SHA256,
				NewSHA256: fork.SHA256,
			}},
		},
	}
	if err := policy.Validate(document); err != nil {
		return policy.Document{}, gitInvalid("generated fork-delta candidate is invalid: " + err.Error())
	}
	return document, nil
}

type treeEntry struct {
	Mode, OID, Path, SHA256 string
}

func validateRepository(repository string) (string, error) {
	if repository == "" {
		return "", invalidInput("repository is empty")
	}
	absolute, err := filepath.Abs(repository)
	if err != nil {
		return "", invalidInput("repository path is invalid")
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", invalidInput("repository must be a non-symlink directory")
	}
	if _, err := os.Stat(filepath.Join(absolute, ".git")); err != nil {
		return "", invalidInput("repository is not a Git worktree")
	}
	return absolute, nil
}

func verifyAuthority(root string, authority policy.Authority) error {
	commit, err := runGitOutput(root, "rev-parse", "HEAD^{commit}")
	if err != nil || strings.TrimSpace(commit) != authority.ForkCommit {
		return gitInvalid("repository HEAD does not match fork authority")
	}
	tree, err := runGitOutput(root, "rev-parse", authority.ForkCommit+"^{tree}")
	if err != nil || strings.TrimSpace(tree) != authority.ForkTree {
		return gitInvalid("repository tree does not match fork authority")
	}
	parent, err := runGitOutput(root, "rev-parse", authority.ForkCommit+"^1")
	if err != nil || strings.TrimSpace(parent) != authority.ForkParentCommit {
		return gitInvalid("repository parent does not match fork authority")
	}
	return nil
}

func lsTree(root, revision string) ([]treeEntry, error) {
	raw, err := runGitOutputBytes(root, "ls-tree", "-r", "-z", revision, "--")
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(raw, []byte{0})
	entries := make([]treeEntry, 0, len(parts)-1)
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		fields := bytes.SplitN(part, []byte{'\t'}, 2)
		if len(fields) != 2 {
			return nil, gitInvalid("Git tree entry is malformed")
		}
		meta := strings.Fields(string(fields[0]))
		if len(meta) != 3 || meta[1] != "blob" || !validHex(meta[2], 40) {
			return nil, gitInvalid("Git tree entry has invalid metadata")
		}
		path := string(fields[1])
		if path == "" || strings.ContainsRune(path, '\x00') {
			return nil, gitInvalid("Git tree entry has invalid path")
		}
		content, err := runGitOutputBytes(root, "cat-file", "blob", meta[2])
		if err != nil {
			return nil, err
		}
		entries = append(entries, treeEntry{Mode: meta[0], OID: meta[2], Path: path, SHA256: digest(content)})
	}
	return entries, nil
}

func treeEntryAt(root, revision, path string) (treeEntry, error) {
	entries, err := lsTree(root, revision)
	if err != nil {
		return treeEntry{}, err
	}
	for _, entry := range entries {
		if entry.Path == path {
			return entry, nil
		}
	}
	return treeEntry{}, gitInvalid("required tracked path is missing: " + path)
}

func sourceRecord(entry treeEntry, materialID, reason string) policy.SourceRecord {
	return policy.SourceRecord{Path: entry.Path, GitMode: entry.Mode, BlobOID: entry.OID, SHA256: entry.SHA256, MaterialID: materialID, InclusionReason: reason}
}

func inclusionReason(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".h":
		return "c-translation-unit"
	case ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		return "cpp-translation-unit"
	case ".png", ".bmp", ".jpg", ".jpeg":
		return "binary-image"
	default:
		return "link-input"
	}
}

func runGitOutput(root string, args ...string) (string, error) {
	output, err := runGitOutputBytes(root, args...)
	return string(output), err
}

func runGitOutputBytes(root string, args ...string) ([]byte, error) {
	ctx := context.Background()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ATTR_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "LANG=C", "TZ=UTC")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, &Failure{Code: CodeCommandFailed, Detail: "Git command was cancelled"}
		}
		return nil, &Failure{Code: CodeCommandFailed, Detail: fmt.Sprintf("Git command failed: %s", strings.TrimSpace(stderr.String()))}
	}
	return stdout.Bytes(), nil
}

func safeMaterialID(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if !(r == '-' || r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) || (i == 0 && (r < 'a' || r > 'z') && (r < '0' || r > '9')) {
			return false
		}
	}
	return true
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func invalidInput(detail string) error { return &Failure{Code: CodeInputInvalid, Detail: detail} }
func gitInvalid(detail string) error {
	return &Failure{Code: CodeGitObservationInvalid, Detail: detail}
}
