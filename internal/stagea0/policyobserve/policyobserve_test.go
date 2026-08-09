package policyobserve

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

func TestObserveSourceSetEmitsSortedDirectInputCandidate(t *testing.T) {
	repo, authority := fixtureRepository(t)
	document, err := ObserveSourceSet(repo, authority, "main-fork")
	if err != nil {
		t.Fatalf("ObserveSourceSet() = %v", err)
	}
	if document.Kind != policy.KindSourceSet {
		t.Fatalf("kind = %q, want %q", document.Kind, policy.KindSourceSet)
	}
	if document.SourceSet == nil || document.SourceSet.Completeness != policy.CompletenessDirect {
		t.Fatalf("source-set candidate = %#v, want candidate-direct-inputs", document.SourceSet)
	}
	if document.SourceSet.Completeness == policy.CompletenessComplete {
		t.Fatal("source-set observation must never claim complete")
	}
	if document.SourceSet.Makefile.Path != "Makefile" || document.SourceSet.Makefile.InclusionReason != "build-config" {
		t.Fatalf("Makefile record = %#v", document.SourceSet.Makefile)
	}
	if len(document.SourceSet.Records) != 2 || document.SourceSet.Records[0].Path != "include/main.h" || document.SourceSet.Records[1].Path != "main.c" {
		t.Fatalf("records = %#v, want sorted non-Makefile paths", document.SourceSet.Records)
	}
	if document.SourceSet.Records[0].SHA256 == "" || document.SourceSet.Records[1].BlobOID == "" {
		t.Fatal("source observations must include blob and content hashes")
	}
	raw, err := policy.Encode(document)
	if err != nil {
		t.Fatalf("policy.Encode() = %v", err)
	}
	decoded, err := policy.Decode(raw)
	if err != nil {
		t.Fatalf("policy.Decode() = %v", err)
	}
	if !bytes.Equal(raw, mustEncodePolicy(t, decoded)) {
		t.Fatal("policy candidate changed after decode/re-encode")
	}
}

func TestObserveForkDeltaEmitsObservedCandidateAndPatchDigest(t *testing.T) {
	repo, authority := fixtureRepository(t)
	document, err := ObserveForkDelta(repo, authority)
	if err != nil {
		t.Fatalf("ObserveForkDelta() = %v", err)
	}
	if document.Kind != policy.KindForkDelta || document.ForkDelta == nil {
		t.Fatalf("document = %#v, want upstream-fork-delta", document)
	}
	delta := document.ForkDelta
	if delta.Completeness != policy.CompletenessObserved {
		t.Fatalf("completeness = %q, want %q", delta.Completeness, policy.CompletenessObserved)
	}
	if delta.Completeness == policy.CompletenessComplete {
		t.Fatal("fork-delta observation must never claim complete")
	}
	if delta.Purpose != "deterministic-vdate-input" || len(delta.ChangedPaths) != 1 || delta.ChangedPaths[0].Path != "Makefile" || delta.ChangedPaths[0].Status != "modified" {
		t.Fatalf("delta = %#v", delta)
	}
	if !isSHA256(delta.PatchDiffSHA256) || delta.ChangedPaths[0].OldSHA256 == delta.ChangedPaths[0].NewSHA256 {
		t.Fatalf("delta hashes = %#v", delta)
	}
	if _, err := policy.Encode(document); err != nil {
		t.Fatalf("policy.Encode() = %v", err)
	}
}

func TestObserveRejectsSymlinkRepositoryAndAuthorityMismatch(t *testing.T) {
	repo, authority := fixtureRepository(t)
	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ObserveSourceSet(link, authority, "main-fork"); !hasCode(err, CodeInputInvalid) {
		t.Fatalf("symlink repository error = %v, want %s", err, CodeInputInvalid)
	}
	authority.ForkTree = strings.Repeat("f", 40)
	if _, err := ObserveForkDelta(repo, authority); !hasCode(err, CodeGitObservationInvalid) {
		t.Fatalf("tree mismatch error = %v, want %s", err, CodeGitObservationInvalid)
	}
	authority = policy.Authority{
		UpstreamCommit:   authority.UpstreamCommit,
		UpstreamTree:     strings.Repeat("e", 40),
		ForkCommit:       authority.ForkCommit,
		ForkTree:         gitOutput(t, repo, "rev-parse", "HEAD^{tree}"),
		ForkParentCommit: authority.UpstreamCommit,
		PatchCommits:     []string{authority.ForkCommit},
		SourceDateEpoch:  0,
		VDate:            "700101",
	}
	if _, err := ObserveSourceSet(repo, authority, "main-fork"); !hasCode(err, CodeGitObservationInvalid) {
		t.Fatalf("upstream tree mismatch error = %v, want %s", err, CodeGitObservationInvalid)
	}
}

func TestObserveRejectsUnsupportedGitMode(t *testing.T) {
	repo, authority := fixtureRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "tool.sh"), []byte("#!/bin/sh\necho test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "tool.sh")
	gitRun(t, repo, "commit", "-m", "add executable")
	authority.ForkCommit = gitOutput(t, repo, "rev-parse", "HEAD")
	authority.ForkTree = gitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	authority.ForkParentCommit = authority.UpstreamCommit
	authority.PatchCommits = []string{authority.ForkCommit}
	if _, err := ObserveSourceSet(repo, authority, "main-fork"); !hasCode(err, CodeGitObservationInvalid) {
		t.Fatalf("unsupported mode error = %v, want %s", err, CodeGitObservationInvalid)
	}
}

func fixtureRepository(t *testing.T) (string, policy.Authority) {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "config", "user.name", "FogCast Test")
	gitRun(t, repo, "config", "user.email", "fogcast-test@example.invalid")
	writeFixture(t, repo, "Makefile", "all:\n\t@echo v1\n")
	writeFixture(t, repo, "main.c", "int main(void) { return 0; }\n")
	writeFixture(t, repo, "include/main.h", "#define MAIN 1\n")
	gitRun(t, repo, "add", "Makefile", "main.c", "include/main.h")
	gitRunWithDate(t, repo, "commit", "-m", "upstream")
	upstream := gitOutput(t, repo, "rev-parse", "HEAD")
	upstreamTree := gitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	writeFixture(t, repo, "Makefile", "all:\n\t@echo v2\n")
	gitRun(t, repo, "add", "Makefile")
	gitRunWithDate(t, repo, "commit", "-m", "deterministic vdate")
	fork := gitOutput(t, repo, "rev-parse", "HEAD")
	forkTree := gitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	return repo, policy.Authority{
		UpstreamCommit:   upstream,
		UpstreamTree:     upstreamTree,
		ForkCommit:       fork,
		ForkTree:         forkTree,
		ForkParentCommit: upstream,
		PatchCommits:     []string{fork},
		SourceDateEpoch:  0,
		VDate:            "700101",
	}
}

func writeFixture(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=1970-01-01T00:00:00Z", "GIT_COMMITTER_DATE=1970-01-01T00:00:00Z")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, output)
	}
}

func gitRunWithDate(t *testing.T, repo string, args ...string) { gitRun(t, repo, args...) }

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "LC_ALL=C")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(output))
}

func mustEncodePolicy(t *testing.T, document policy.Document) []byte {
	t.Helper()
	raw, err := policy.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func isSHA256(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil && len(value) == sha256.Size*2
}

func hasCode(err error, want Code) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Code == want
}
