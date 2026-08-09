package stagea0

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This test catches an initializer that permits a potentially destructive Git
// verb. The runner is real enough to observe the command boundary, while the
// bootstrap failure keeps the filesystem untouched.
func TestInitializeForkRejectsInvalidBootstrapBeforeGit(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(t.TempDir(), "Main_MiSTer")
	runner := &recordingRunner{}
	_, err := InitializeFork(context.Background(), runner, InitRequest{Destination: destination})
	if !hasCode(err, CodeBootstrapSchemaInvalid) {
		t.Fatalf("InitializeFork() error = %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Git commands = %#v, want none", runner.commands)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination changed before validation: %v", statErr)
	}
}

func TestVerifyFogCastC0RequiresDetachedCleanMatchingHead(t *testing.T) {
	t.Parallel()
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]\nrepositoryformatversion = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{results: []CommandResult{
		{Stdout: []byte(bootstrap.FogCastBaseRevision + "\n")},
		{Stdout: []byte("refs/heads/main\n")},
	}}
	err = VerifyFogCastC0(context.Background(), runner, bootstrap, root)
	if !hasCode(err, CodeRepositoryPolicyMismatch) {
		t.Fatalf("VerifyFogCastC0() error = %v", err)
	}
	if len(runner.commands) != 2 || !strings.HasSuffix(runner.commands[0].Path, "/git") {
		t.Fatalf("commands = %#v", runner.commands)
	}
}

// A repository-local fsmonitor must be rejected by raw inspection before any
// Git invocation; otherwise even `status` can execute the configured program.
func TestExistingForkRejectsUnsafeConfigBeforeGit(t *testing.T) {
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]\nfsmonitor = /not-run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	_, err = verifyExistingFork(context.Background(), runner, mustGit(t), root, bootstrap)
	if !hasCode(err, CodeRepositoryPolicyMismatch) || len(runner.commands) != 0 {
		t.Fatalf("error=%v commands=%#v", err, runner.commands)
	}
}

// This catches a validator that broadens the one permitted official
// .gitattributes result, normalizes raw attribute output, or materializes a
// source whose bytes could change during checkout.
func TestVerifyCheckoutAttributesUsesClosedOfficialPolicy(t *testing.T) {
	source := []byte("SHELL = /bin/bash\nDFLAGS = x\n")
	exact := []byte("Makefile: text: set\nMakefile: eol: lf\n")
	for _, tc := range []struct {
		name   string
		source []byte
		output []byte
		admit  bool
	}{
		{name: "no attributes", source: source, admit: true},
		{name: "exact official attributes", source: source, output: exact, admit: true},
		{name: "reversed records", source: source, output: []byte("Makefile: eol: lf\nMakefile: text: set\n")},
		{name: "duplicate record", source: source, output: []byte("Makefile: text: set\nMakefile: eol: lf\nMakefile: eol: lf\n")},
		{name: "extra attribute", source: source, output: []byte("Makefile: text: set\nMakefile: eol: lf\nMakefile: binary: unset\n")},
		{name: "text auto", source: source, output: []byte("Makefile: text: auto\nMakefile: eol: lf\n")},
		{name: "eol crlf", source: source, output: []byte("Makefile: text: set\nMakefile: eol: crlf\n")},
		{name: "different path", source: source, output: []byte("other: text: set\nother: eol: lf\n")},
		{name: "malformed output", source: source, output: []byte("Makefile: text: set\nMakefile: eol: lf")},
		{name: "crlf source", source: []byte("SHELL = /bin/bash\r\nDFLAGS = x\r\n"), output: exact},
		{name: "cr inside line", source: []byte("SHELL = /bin/bash\rDFLAGS = x\n"), output: exact},
		{name: "source missing terminal lf", source: []byte("SHELL = /bin/bash\nDFLAGS = x"), output: exact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyCheckoutAttributes("Makefile", tc.source, tc.output)
			if tc.admit {
				if err != nil {
					t.Fatalf("verifyCheckoutAttributes() error = %v", err)
				}
				return
			}
			if !hasCode(err, CodeRepositoryPolicyMismatch) {
				t.Fatalf("verifyCheckoutAttributes() error = %v, want repository policy mismatch", err)
			}
		})
	}
}

// This catches an initializer that rejects the exact official Makefile
// attributes, or that permits checkout-altering attributes to reach
// materialization or publication.
func TestExecRunnerAdmitsOfficialMakefileAttributes(t *testing.T) {
	t.Run("admitted exact official attributes", func(t *testing.T) {
		fixture, bootstrap := checkoutAttributesFixture(t, "* text=auto eol=lf\nMakefile text eol=lf\n")
		destination := filepath.Join(t.TempDir(), "fork")
		runner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
		if _, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination}); err != nil {
			t.Fatal(err)
		}
		if attrs := runGit(t, destination, "check-attr", "--cached", "--all", "--", "Makefile"); attrs != "Makefile: text: set\nMakefile: eol: lf\n" {
			t.Fatalf("cached Makefile attributes = %q", attrs)
		}
		materialized := mustReadFile(t, filepath.Join(destination, "Makefile"))
		committed := []byte(runGit(t, destination, "cat-file", "blob", "HEAD:Makefile"))
		if sha256.Sum256(materialized) != sha256.Sum256(committed) {
			t.Fatal("materialized Makefile differs from HEAD:Makefile")
		}
	})

	t.Run("rejects crlf attributes before checkout", func(t *testing.T) {
		fixture, bootstrap := checkoutAttributesFixture(t, "Makefile text eol=crlf\n")
		destination := filepath.Join(t.TempDir(), "fork")
		runner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
		_, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination})
		if !hasCode(err, CodeRepositoryPolicyMismatch) {
			t.Fatalf("InitializeFork() error = %v, want repository policy mismatch", err)
		}
		if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("rejected attributes published destination: %v", statErr)
		}
		for _, command := range runner.commands {
			if slicesContain(command.Args, "checkout-index") {
				t.Fatalf("rejected attributes reached checkout: %#v", command)
			}
		}
	})
}

func TestExecRunnerSupportsEpochZeroAndCanonicalCommitBytes(t *testing.T) {
	fixture, bootstrap := localUpstreamFixture(t)
	for _, epoch := range []int64{0, 1722470400} {
		t.Run(fmt.Sprintf("epoch-%d", epoch), func(t *testing.T) {
			bootstrap.InitialCommit.AuthorTimestamp, bootstrap.InitialCommit.CommitterTimestamp = epoch, epoch
			destination := filepath.Join(t.TempDir(), "fork")
			runner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
			identity, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination})
			if err != nil {
				t.Fatal(err)
			}
			raw := runGit(t, destination, "cat-file", "commit", identity.PatchCommit)
			if raw != string(expectedCommitObject(identity.PatchTree, bootstrap)) {
				t.Fatalf("raw commit = %q", raw)
			}
		})
	}
}

// This catches loss of deterministic commit construction: inherited config,
// local timezone, and a second initializer invocation must not affect the
// first committed patch or mutate an already matching destination.
func TestExecRunnerDeterministicFork(t *testing.T) {
	fixture, bootstrap := localUpstreamFixture(t)
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	firstRunner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
	firstIdentity, err := InitializeFork(context.Background(), firstRunner, InitRequest{Bootstrap: bootstrap, Destination: first})
	if err != nil {
		t.Fatalf("%v; transcript=%#v", err, firstRunner.commands)
	}
	secondRunner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
	secondIdentity, err := InitializeFork(context.Background(), secondRunner, InitRequest{Bootstrap: bootstrap, Destination: second})
	if err != nil {
		t.Fatal(err)
	}
	if !firstRunner.Intercepted || !secondRunner.Intercepted || firstIdentity.PatchCommit != secondIdentity.PatchCommit || firstIdentity.PatchTree != secondIdentity.PatchTree {
		t.Fatalf("identities = %#v %#v", firstIdentity, secondIdentity)
	}
	assertClosedGitTranscript(t, firstRunner.commands, bootstrap.InitialCommit)
	before := completeSnapshot(t, first)
	runAgain := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
	got, err := InitializeFork(context.Background(), runAgain, InitRequest{Bootstrap: bootstrap, Destination: first})
	if err != nil || got.PatchCommit != firstIdentity.PatchCommit {
		t.Fatalf("rerun = %#v, %v", got, err)
	}
	if after := completeSnapshot(t, first); before != after {
		t.Fatal("matching existing destination changed")
	}
	if err := os.WriteFile(filepath.Join(second, ".git", "config"), append([]byte("[unsafe]\n\toption = true\n"), mustReadFile(t, filepath.Join(second, ".git", "config"))...), 0o600); err != nil {
		t.Fatal(err)
	}
	mismatchedBefore := completeSnapshot(t, second)
	_, err = InitializeFork(context.Background(), &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}, InitRequest{Bootstrap: bootstrap, Destination: second})
	if !hasCode(err, CodeRepositoryPolicyMismatch) || completeSnapshot(t, second) != mismatchedBefore {
		t.Fatalf("mismatched existing destination result = %v", err)
	}
	lockPath := filepath.Join(root, "locked.stage-a0.publish.lock")
	if err := os.WriteFile(lockPath, []byte("other initializer"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = InitializeFork(context.Background(), &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}, InitRequest{Bootstrap: bootstrap, Destination: filepath.Join(root, "locked")})
	if !hasCode(err, CodeRepositoryPolicyMismatch) {
		t.Fatalf("publication lock error = %v", err)
	}
}

type HybridFixtureRunner struct {
	Exec                                                      ExecRunner
	GitPath, OfficialHTTPSURL, LockedCommit, LocalBareFixture string
	Intercepted                                               bool
	TempRoot, CanonicalIndex                                  string
	commands                                                  []Command
}

func (r *HybridFixtureRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	r.commands = append(r.commands, command)
	if isOfficialObjectFetch(command, r.GitPath, r.OfficialHTTPSURL, r.LockedCommit) {
		if r.Intercepted {
			return CommandResult{}, failure(CodeCommandFailed, "fixture", "repeated official fetch")
		}
		root, index, ok := learnContainedIndexAndRoot(command.Env, command.Dir)
		if !ok {
			return CommandResult{}, failure(CodeCommandFailed, "fixture", "invalid canonical index")
		}
		r.TempRoot, r.CanonicalIndex = root, index
		r.Intercepted = true
		command.Args = []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", r.LocalBareFixture, r.LockedCommit}
		return r.Exec.Run(ctx, command)
	}
	if slicesContain(command.Args, "fetch") {
		return CommandResult{}, failure(CodeCommandFailed, "fixture", "unexpected fetch")
	}
	return r.Exec.Run(ctx, command)
}

func learnContainedIndexAndRoot(env []string, root string) (string, string, bool) {
	for _, value := range env {
		if index, ok := strings.CutPrefix(value, "GIT_INDEX_FILE="); ok && filepath.IsAbs(index) && index == filepath.Join(root, ".git", "index") {
			return root, index, true
		}
	}
	return "", "", false
}

func isOfficialObjectFetch(command Command, gitPath, officialHTTPSURL, lockedCommit string) bool {
	return command.Path == gitPath && strings.Join(command.Args, "\x00") == strings.Join([]string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "stage-a0-fetch", lockedCommit}, "\x00") && officialHTTPSURL != ""
}

func localUpstreamFixture(t *testing.T) (string, Bootstrap) {
	t.Helper()
	root := t.TempDir()
	work, bare := filepath.Join(root, "work"), filepath.Join(root, "upstream.git")
	runGit(t, root, "init", work)
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	source := validRecipeSource()
	if err := os.WriteFile(filepath.Join(work, "Makefile"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "Makefile")
	runGit(t, work, "commit", "-m", "upstream")
	commit := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	tree := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD^{tree}"))
	runGit(t, root, "clone", "--bare", work, bare)
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	bootstrap.MainUpstream.FetchURL = "https://example.invalid/Main_MiSTer.git"
	bootstrap.MainUpstream.Commit, bootstrap.MainUpstream.Tree, bootstrap.Branch.ParentCommit = commit, tree, commit
	bootstrap.VDate.SourceEvidenceSHA256 = fmt.Sprintf("%x", digest)
	return bare, bootstrap
}

func checkoutAttributesFixture(t *testing.T, attributes string) (string, Bootstrap) {
	t.Helper()
	root := t.TempDir()
	work, bare := filepath.Join(root, "work"), filepath.Join(root, "upstream.git")
	runGit(t, root, "init", work)
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	source := validRecipeSource()
	if err := os.WriteFile(filepath.Join(work, "Makefile"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".gitattributes"), []byte(attributes), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "Makefile", ".gitattributes")
	runGit(t, work, "commit", "-m", "upstream")
	commit := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	tree := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD^{tree}"))
	runGit(t, root, "clone", "--bare", work, bare)
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	bootstrap.MainUpstream.FetchURL = "https://example.invalid/Main_MiSTer.git"
	bootstrap.MainUpstream.Commit, bootstrap.MainUpstream.Tree, bootstrap.Branch.ParentCommit = commit, tree, commit
	bootstrap.VDate.SourceEvidenceSHA256 = fmt.Sprintf("%x", digest)
	return bare, bootstrap
}

func completeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, rel+"|"+info.Mode().String()+"|"+fmt.Sprint(info.ModTime().UnixNano()))
		if !entry.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entries = append(entries, fmt.Sprintf("%x", sha256.Sum256(raw)))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(entries, "\n")
}

func mustGit(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command(mustGit(t), args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "TZ=UTC")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %q: %v: %s", args, err, out)
	}
	return string(out)
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertClosedGitTranscript(t *testing.T, commands []Command, spec CommitSpec) {
	t.Helper()
	for _, command := range commands {
		if !filepath.IsAbs(command.Path) {
			t.Fatalf("non-absolute Git path: %#v", command)
		}
		for _, forbidden := range []string{"push", "reset", "rebase", "clean"} {
			if slicesContain(command.Args, forbidden) {
				t.Fatalf("forbidden Git command: %#v", command.Args)
			}
		}
		if strings.Join(command.Args, " ") == "checkout -f" || strings.Join(command.Args, " ") == "config --global" {
			t.Fatalf("forbidden Git arguments: %#v", command.Args)
		}
		if !slicesContain(command.Env, "GIT_CONFIG_NOSYSTEM=1") || !slicesContain(command.Env, "GIT_CONFIG_GLOBAL=/dev/null") || !slicesContain(command.Env, "GIT_TERMINAL_PROMPT=0") || !slicesContain(command.Env, "GIT_ATTR_NOSYSTEM=1") || !slicesContain(command.Env, "GIT_NO_REPLACE_OBJECTS=1") || !slicesContain(command.Env, "LC_ALL=C") || !slicesContain(command.Env, "LANG=C") || !slicesContain(command.Env, "TZ=UTC") {
			t.Fatalf("non-isolated Git environment: %#v", command.Env)
		}
		if len(command.Env) == 9 && slicesContain(command.Env, "GIT_OPTIONAL_LOCKS=0") {
			continue
		}
		if len(command.Env) != 15 || !slicesContain(command.Env, "GIT_AUTHOR_NAME="+spec.AuthorName) || !slicesContain(command.Env, "GIT_COMMITTER_EMAIL="+spec.CommitterEmail) {
			t.Fatalf("non-deterministic Git environment: %#v", command.Env)
		}
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type recordingRunner struct {
	commands []Command
	results  []CommandResult
}

func (r *recordingRunner) Run(_ context.Context, command Command) (CommandResult, error) {
	r.commands = append(r.commands, command)
	if len(r.results) == 0 {
		return CommandResult{}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

// This catches an initializer that accepts an unsafe tree entry and reaches
// materialization before it has rejected that entry.
func TestInitializeForkRejectsUnsafeSourceTreeEntriesBeforeMaterialization(t *testing.T) {
	for _, tc := range []struct {
		name, sourcePath, mode string
	}{
		{"symlink leaf", "Makefile", "120000"},
		{"symlink parent", "linked/Makefile", "120000"},
		{"gitlink", "Makefile", "160000"},
		{"tree leaf", "Makefile", "040000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, bootstrap := unsafeSourceFixture(t, tc.sourcePath, tc.mode)
			destination := filepath.Join(t.TempDir(), "fork")
			runner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
			_, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination})
			if !hasCode(err, CodeRepositoryPolicyMismatch) {
				t.Fatalf("InitializeFork() error = %v, want repository mismatch", err)
			}
			if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("unsafe source published destination: %v", statErr)
			}
			for _, command := range runner.commands {
				if slicesContain(command.Args, "checkout-index") || slicesContain(command.Args, "hash-object") || slicesContain(command.Args, "update-index") {
					t.Fatalf("unsafe source reached materialization/mutation: %#v", command)
				}
			}
		})
	}
}

// This catches drift in the closed fetch protocol, its canonical index, or
// the inherited process environment.  The real fixture proves no FETCH_HEAD
// or temporary remote/ref survives publication.
func TestInitializeForkUsesExactFetchClosedEnvironmentAndNoTemporaryState(t *testing.T) {
	fixture, bootstrap := localUpstreamFixture(t)
	destination := filepath.Join(t.TempDir(), "fork")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "hostile-global-config"))
	t.Setenv("TZ", "Pacific/Kiritimati")
	t.Setenv("LANG", "tr_TR.UTF-8")
	runner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
	if _, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination}); err != nil {
		t.Fatal(err)
	}
	if !runner.Intercepted || runner.TempRoot == "" || runner.CanonicalIndex != filepath.Join(runner.TempRoot, ".git", "index") || !strings.HasPrefix(filepath.Base(runner.TempRoot), ".Main_MiSTer.stage-a0-") {
		t.Fatalf("fixture did not learn a canonical contained index: root=%q index=%q", runner.TempRoot, runner.CanonicalIndex)
	}
	var fetch Command
	for _, command := range runner.commands {
		if slicesContain(command.Args, "fetch") {
			fetch = command
			break
		}
	}
	wantFetch := []string{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "stage-a0-fetch", bootstrap.MainUpstream.Commit}
	if !reflect.DeepEqual(fetch.Args, wantFetch) {
		t.Fatalf("fetch args = %#v, want %#v", fetch.Args, wantFetch)
	}
	assertExactGitEnvironments(t, runner.commands, bootstrap.InitialCommit)
	if _, err := os.Lstat(filepath.Join(destination, ".git", "FETCH_HEAD")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FETCH_HEAD survived publication: %v", err)
	}
	if refs := runGit(t, destination, "for-each-ref", "--format=%(refname)"); strings.Contains(refs, "stage-a0-fetch") {
		t.Fatalf("temporary fetch ref survived: %q", refs)
	}
	if remotes := runGit(t, destination, "remote"); remotes != "upstream\n" {
		t.Fatalf("published remotes = %q", remotes)
	}
}

// Each row names a distinct existing-destination policy break.  The snapshot
// guards the non-repair contract: verification must leave all contents, refs,
// index/config bytes, and timestamps exactly as it found them.
func TestExistingForkPolicyMismatchesLeaveCompleteSnapshotUntouched(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, destination string, bootstrap *Bootstrap)
	}{
		{"extra remote", func(t *testing.T, d string, _ *Bootstrap) {
			runGit(t, d, "remote", "add", "extra", "https://example.invalid/extra.git")
		}},
		{"effective push URL", func(t *testing.T, d string, _ *Bootstrap) {
			runGit(t, d, "remote", "set-url", "--push", "upstream", "https://example.invalid/push.git")
		}},
		{"dirty worktree", func(t *testing.T, d string, _ *Bootstrap) {
			if err := os.WriteFile(filepath.Join(d, "untracked"), []byte("dirty\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong branch", func(t *testing.T, d string, _ *Bootstrap) {
			runGit(t, d, "update-ref", "refs/heads/wrong", "HEAD")
			runGit(t, d, "symbolic-ref", "HEAD", "refs/heads/wrong")
		}},
		{"wrong parent tree", func(t *testing.T, _ string, b *Bootstrap) { b.MainUpstream.Tree = strings.Repeat("1", 40) }},
		{"wrong patch tree", func(t *testing.T, d string, _ *Bootstrap) { rewriteForkWithParentTree(t, d) }},
		{"wrong direct parent", func(t *testing.T, d string, _ *Bootstrap) {
			setCommitWithParent(t, d, runGitTrim(t, d, "rev-parse", "HEAD^{tree}"), runGitTrim(t, d, "rev-parse", "HEAD"), CommitSpec{AuthorName: "A", AuthorEmail: "a@example.invalid", CommitterName: "C", CommitterEmail: "c@example.invalid", AuthorTimestamp: 7, CommitterTimestamp: 8, CommitMessage: "wrong parent\n"})
		}},
		{"wrong author", func(t *testing.T, d string, _ *Bootstrap) {
			rewriteForkMetadata(t, d, func(s *CommitSpec) { s.AuthorName = "not locked" })
		}},
		{"wrong author email", func(t *testing.T, d string, _ *Bootstrap) {
			rewriteForkMetadata(t, d, func(s *CommitSpec) { s.AuthorEmail = "not-locked@example.invalid" })
		}},
		{"wrong author timestamp", func(t *testing.T, d string, _ *Bootstrap) {
			rewriteForkMetadata(t, d, func(s *CommitSpec) { s.AuthorTimestamp++ })
		}},
		{"wrong committer", func(t *testing.T, d string, _ *Bootstrap) {
			rewriteForkMetadata(t, d, func(s *CommitSpec) { s.CommitterName = "not locked" })
		}},
		{"wrong committer email", func(t *testing.T, d string, _ *Bootstrap) {
			rewriteForkMetadata(t, d, func(s *CommitSpec) { s.CommitterEmail = "not-locked@example.invalid" })
		}},
		{"wrong committer timestamp", func(t *testing.T, d string, _ *Bootstrap) {
			rewriteForkMetadata(t, d, func(s *CommitSpec) { s.CommitterTimestamp++ })
		}},
		{"wrong message", func(t *testing.T, d string, _ *Bootstrap) {
			rewriteForkMetadata(t, d, func(s *CommitSpec) { s.CommitMessage = "not the locked message\n" })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, bootstrap := localUpstreamFixture(t)
			destination := filepath.Join(t.TempDir(), "fork")
			initializeFixtureFork(t, fixture, bootstrap, destination)
			tc.mutate(t, destination, &bootstrap)
			before := completeSnapshot(t, destination)
			_, err := InitializeFork(context.Background(), &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}, InitRequest{Bootstrap: bootstrap, Destination: destination})
			if !hasCode(err, CodeRepositoryPolicyMismatch) {
				t.Fatalf("InitializeFork() error = %v, want policy mismatch", err)
			}
			if after := completeSnapshot(t, destination); after != before {
				t.Fatalf("existing destination changed\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// This catches a C0 verifier that cannot safely inspect a normal linked
// worktree or invokes a configured fsmonitor before rejecting its config.
func TestVerifyFogCastC0HandlesLinkedWorktreeAndRejectsUnsafeConfigWithoutGit(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	runGit(t, root, "init", repository)
	runGit(t, repository, "config", "user.name", "Fixture")
	runGit(t, repository, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "tracked"), []byte("tracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "tracked")
	runGit(t, repository, "commit", "-m", "base")
	linked := filepath.Join(root, "linked")
	runGit(t, repository, "worktree", "add", "--detach", linked, "HEAD")
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	bootstrap.FogCastBaseRevision = runGitTrim(t, linked, "rev-parse", "HEAD")
	if err := VerifyFogCastC0(context.Background(), ExecRunner{}, bootstrap, linked); err != nil {
		t.Fatalf("VerifyFogCastC0(linked worktree) = %v", err)
	}
	config, err := resolvedGitConfig(linked)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, append(mustReadFile(t, config), []byte("[extensions]\nworktreeConfig = true\n[core]\nfsmonitor = /definitely/not/run\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	before := completeSnapshot(t, linked)
	runner := &recordingRunner{}
	err = VerifyFogCastC0(context.Background(), runner, bootstrap, linked)
	if !hasCode(err, CodeRepositoryPolicyMismatch) || len(runner.commands) != 0 {
		t.Fatalf("unsafe C0 result=%v commands=%#v", err, runner.commands)
	}
	if after := completeSnapshot(t, linked); after != before {
		t.Fatal("unsafe C0 verification mutated linked worktree")
	}
}

// This catches publication code that lets os.Rename replace a destination
// which appeared after the cooperating lock was acquired.
func TestInitializeForkStopsWhenDestinationAppearsBeforePublication(t *testing.T) {
	fixture, bootstrap := localUpstreamFixture(t)
	parent := t.TempDir()
	destination := filepath.Join(parent, "fork")
	runner := &destinationAppearanceRunner{
		HybridFixtureRunner: HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture},
		destination:         destination,
	}
	_, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination})
	if !hasCode(err, CodeRepositoryPolicyMismatch) {
		t.Fatalf("InitializeFork() error = %v, want destination-appearance mismatch", err)
	}
	if got := mustReadFile(t, filepath.Join(destination, "foreign")); string(got) != "foreign\n" {
		t.Fatalf("foreign destination changed: %q", got)
	}
	matches, globErr := filepath.Glob(filepath.Join(parent, ".Main_MiSTer.stage-a0-*"))
	if globErr != nil || len(matches) != 0 {
		t.Fatalf("owned temporary roots remain: %v, %v", matches, globErr)
	}
}

// This catches clean-up that unlinks a foreign replacement for the creator's
// publication lock or removes a foreign replacement for its temporary root.
func TestInitializerCleanupPreservesForeignLockAndTemporaryReplacement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replace func(t *testing.T, command Command, destination string)
	}{
		{"lock", func(t *testing.T, _ Command, destination string) {
			lock := destination + ".stage-a0.publish.lock"
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lock, []byte("foreign lock"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"temporary root", func(t *testing.T, command Command, _ string) {
			foreign := command.Dir + ".foreign"
			if err := os.Rename(command.Dir, foreign); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(command.Dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(command.Dir, "foreign"), []byte("foreign temporary\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, bootstrap := localUpstreamFixture(t)
			parent := t.TempDir()
			destination := filepath.Join(parent, "fork")
			runner := &replaceOnInitRunner{HybridFixtureRunner: HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}, destination: destination, replace: tc.replace, t: t}
			_, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination})
			if !hasCode(err, CodeCommandFailed) {
				t.Fatalf("InitializeFork() error = %v", err)
			}
			if tc.name == "lock" {
				if got := mustReadFile(t, destination+".stage-a0.publish.lock"); string(got) != "foreign lock" {
					t.Fatalf("foreign lock changed: %q", got)
				}
			} else {
				if got := mustReadFile(t, runner.temporaryForeign()); string(got) != "foreign temporary\n" {
					t.Fatalf("foreign temp changed: %q", got)
				}
			}
		})
	}
}

type destinationAppearanceRunner struct {
	HybridFixtureRunner
	destination string
	appeared    bool
}

func (r *destinationAppearanceRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	result, err := r.HybridFixtureRunner.Run(ctx, command)
	if err == nil && !r.appeared && command.Dir != r.destination && reflect.DeepEqual(command.Args, []string{"remote", "get-url", "--push", "upstream"}) {
		if err := os.Mkdir(r.destination, 0o700); err != nil {
			return CommandResult{}, err
		}
		if err := os.WriteFile(filepath.Join(r.destination, "foreign"), []byte("foreign\n"), 0o600); err != nil {
			return CommandResult{}, err
		}
		r.appeared = true
	}
	return result, err
}

type replaceOnInitRunner struct {
	HybridFixtureRunner
	destination string
	replace     func(*testing.T, Command, string)
	t           *testing.T
	foreign     string
	didReplace  bool
}

func (r *replaceOnInitRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	if !r.didReplace && reflect.DeepEqual(command.Args, []string{"init"}) {
		r.didReplace = true
		r.replace(r.t, command, r.destination)
		r.foreign = filepath.Join(command.Dir, "foreign")
		return CommandResult{}, failure(CodeCommandFailed, "fixture", "stop after replacement")
	}
	return r.HybridFixtureRunner.Run(ctx, command)
}

func (r *replaceOnInitRunner) temporaryForeign() string {
	if r.foreign == "" {
		return ""
	}
	return r.foreign
}

func assertExactGitEnvironments(t *testing.T, commands []Command, spec CommitSpec) {
	t.Helper()
	readonly := readOnlyGitEnv()
	for _, command := range commands {
		if !filepath.IsAbs(command.Path) {
			t.Fatalf("non-absolute executable: %#v", command)
		}
		if reflect.DeepEqual(command.Env, readonly) {
			continue
		}
		write := gitEnv(spec, filepath.Join(command.Dir, ".git", "index"))
		if !reflect.DeepEqual(command.Env, write) {
			t.Fatalf("command %q env = %#v, want exact write or read-only environment", command.Args, command.Env)
		}
	}
}

func initializeFixtureFork(t *testing.T, fixture string, bootstrap Bootstrap, destination string) ForkIdentity {
	t.Helper()
	identity, err := InitializeFork(context.Background(), &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}, InitRequest{Bootstrap: bootstrap, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func runGitTrim(t *testing.T, dir string, args ...string) string {
	return strings.TrimSpace(runGit(t, dir, args...))
}

func setForkRef(t *testing.T, destination, commit string) {
	runGit(t, destination, "update-ref", "refs/heads/fogcast/stage-a-baseline", commit)
}

func rewriteForkMetadata(t *testing.T, destination string, mutate func(*CommitSpec)) {
	t.Helper()
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	// The fixture uses the same locked metadata as the valid synthetic TOML.
	spec := bootstrap.InitialCommit
	mutate(&spec)
	setCommitWithParent(t, destination, runGitTrim(t, destination, "rev-parse", "HEAD^{tree}"), runGitTrim(t, destination, "rev-parse", "HEAD^"), spec)
}

func rewriteForkWithParentTree(t *testing.T, destination string) {
	t.Helper()
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	parent := runGitTrim(t, destination, "rev-parse", "HEAD^")
	parentTree := runGitTrim(t, destination, "rev-parse", parent+"^{tree}")
	setCommitWithParent(t, destination, parentTree, parent, bootstrap.InitialCommit)
	runGit(t, destination, "read-tree", "--reset", "-u", "HEAD")
}

func setCommitWithParent(t *testing.T, destination, tree, parent string, spec CommitSpec) {
	t.Helper()
	command := exec.Command(mustGit(t), "commit-tree", tree, "-p", parent)
	command.Dir = destination
	command.Stdin = strings.NewReader(spec.CommitMessage)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+spec.AuthorName, "GIT_AUTHOR_EMAIL="+spec.AuthorEmail, fmt.Sprintf("GIT_AUTHOR_DATE=@%d +0000", spec.AuthorTimestamp),
		"GIT_COMMITTER_NAME="+spec.CommitterName, "GIT_COMMITTER_EMAIL="+spec.CommitterEmail, fmt.Sprintf("GIT_COMMITTER_DATE=@%d +0000", spec.CommitterTimestamp),
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("commit-tree: %v: %s", err, out)
	}
	setForkRef(t, destination, strings.TrimSpace(string(out)))
}

func unsafeSourceFixture(t *testing.T, sourcePath, mode string) (string, Bootstrap) {
	t.Helper()
	root := t.TempDir()
	work, bare := filepath.Join(root, "work"), filepath.Join(root, "upstream.git")
	runGit(t, root, "init", work)
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	valid := validRecipeSource()
	if err := os.WriteFile(filepath.Join(work, "ordinary"), valid, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "ordinary")
	runGit(t, work, "commit", "-m", "base")
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	bootstrap.MainUpstream.FetchURL = "https://example.invalid/Main_MiSTer.git"
	bootstrap.VDate.SourcePath = sourcePath
	digest := sha256.Sum256(valid)
	bootstrap.VDate.SourceEvidenceSHA256 = fmt.Sprintf("%x", digest)
	var tree string
	if mode == "040000" {
		subtree := strings.TrimSpace(runGit(t, work, "mktree"))
		input := "040000 tree " + subtree + "\tMakefile\n"
		command := exec.Command(mustGit(t), "mktree")
		command.Dir, command.Stdin = work, strings.NewReader(input)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("mktree: %v: %s", err, out)
		}
		tree = strings.TrimSpace(string(out))
	} else {
		blob := runGitTrim(t, work, "hash-object", "-w", "ordinary")
		entryPath := sourcePath
		if sourcePath == "linked/Makefile" {
			entryPath = "linked"
			blob = runGitTrim(t, work, "hash-object", "-w", "--stdin")
		}
		runGit(t, work, "read-tree", "HEAD")
		runGit(t, work, "update-index", "--add", "--cacheinfo", mode+","+blob+","+entryPath)
		tree = runGitTrim(t, work, "write-tree")
	}
	commit := runGitTrim(t, work, "commit-tree", tree, "-p", "HEAD")
	runGit(t, work, "update-ref", "refs/heads/main", commit)
	runGit(t, root, "clone", "--bare", work, bare)
	bootstrap.MainUpstream.Commit, bootstrap.MainUpstream.Tree, bootstrap.Branch.ParentCommit = commit, tree, commit
	return bare, bootstrap
}

// This catches a lock creator that leaves a partially initialized lock behind
// when its cryptographic token source fails, or that removes a foreign lock.
func TestAcquirePublicationLockCleansUpTokenFailureAndPreservesForeignLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fork.stage-a0.publish.lock")
	original := cryptorand.Reader
	cryptorand.Reader = failingRandomReader{}
	lock, err := acquirePublicationLock(path)
	cryptorand.Reader = original
	if lock != nil || !hasCode(err, CodeCommandFailed) {
		t.Fatalf("acquirePublicationLock(token failure) = %#v, %v", lock, err)
	}
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("creator lock remained after token failure: %v", statErr)
	}
	if err := os.WriteFile(path, []byte("foreign lock"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err = acquirePublicationLock(path)
	if lock != nil || !hasCode(err, CodeRepositoryPolicyMismatch) {
		t.Fatalf("acquirePublicationLock(foreign lock) = %#v, %v", lock, err)
	}
	if got := mustReadFile(t, path); string(got) != "foreign lock" {
		t.Fatalf("foreign lock changed: %q", got)
	}
}

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) { return 0, errors.New("injected random failure") }

// This catches a fetch-cleanup check that ignores a persisted temporary ref
// and continues to hash, index, or materialize a source after that violation.
func TestInitializeForkRejectsInjectedLeftoverRefBeforeMaterialization(t *testing.T) {
	fixture, bootstrap := localUpstreamFixture(t)
	destination := filepath.Join(t.TempDir(), "fork")
	runner := &leftoverRefRunner{HybridFixtureRunner: HybridFixtureRunner{
		GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL,
		LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture,
	}}
	_, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination})
	if !hasCode(err, CodeRepositoryPolicyMismatch) {
		t.Fatalf("InitializeFork() error = %v, want leftover-ref policy mismatch", err)
	}
	if !runner.injected {
		t.Fatal("fixture never injected temporary ref")
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination exists after leftover-ref rejection: %v", statErr)
	}
	for _, command := range runner.commands {
		if slicesContain(command.Args, "hash-object") || slicesContain(command.Args, "update-index") || slicesContain(command.Args, "checkout-index") {
			t.Fatalf("leftover ref reached materialization: %#v", command)
		}
	}
}

type leftoverRefRunner struct {
	HybridFixtureRunner
	injected bool
}

func (r *leftoverRefRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	result, err := r.HybridFixtureRunner.Run(ctx, command)
	if err != nil || r.injected || !reflect.DeepEqual(command.Args, []string{"remote", "remove", "stage-a0-fetch"}) {
		return result, err
	}
	injected := Command{Path: command.Path, Args: []string{"update-ref", "refs/stage-a0/injected", r.LockedCommit}, Env: append([]string(nil), command.Env...), Dir: command.Dir}
	if _, injectErr := r.Exec.Run(ctx, injected); injectErr != nil {
		return CommandResult{}, injectErr
	}
	r.injected = true
	return result, nil
}

// This catches any command-order drift in the absent-destination transaction,
// including missing final pre-publication verification.  Identifiers produced
// by Git are normalized to named tokens; every command and argument position
// remains fixed in the hard-coded transcript below.
func TestInitializeForkAbsentDestinationUsesExactOrderedTranscript(t *testing.T) {
	fixture, bootstrap := localUpstreamFixture(t)
	destination := filepath.Join(t.TempDir(), "fork")
	runner := &HybridFixtureRunner{GitPath: mustGit(t), OfficialHTTPSURL: bootstrap.MainUpstream.FetchURL, LockedCommit: bootstrap.MainUpstream.Commit, LocalBareFixture: fixture}
	identity, err := InitializeFork(context.Background(), runner, InitRequest{Bootstrap: bootstrap, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	got := normalizeAbsentTranscript(t, runner.commands, bootstrap, identity)
	want := [][]string{
		{"init"},
		{"remote", "add", "stage-a0-fetch", "$URL"},
		{"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "stage-a0-fetch", "$PARENT"},
		{"rev-parse", "$PARENT^{tree}"},
		{"remote", "remove", "stage-a0-fetch"},
		{"for-each-ref", "--format=%(refname)"},
		{"ls-tree", "$UPSTREAM_TREE", "--", "Makefile"},
		{"cat-file", "blob", "$SOURCE_BLOB"},
		{"remote", "add", "upstream", "$URL"},
		{"config", "--local", "core.autocrlf", "false"},
		{"config", "--local", "core.eol", "lf"},
		{"config", "--local", "core.attributesfile", "/dev/null"},
		{"config", "--local", "remote.upstream.url", "$URL"},
		{"config", "--local", "remote.upstream.fetch", "+refs/heads/*:refs/remotes/upstream/*"},
		{"config", "--local", "remote.upstream.pushurl", DisabledPushURL},
		{"read-tree", "$PARENT"},
		{"hash-object", "-w", "--stdin"},
		{"update-index", "--add", "--cacheinfo", "100644,$PATCHED_BLOB,Makefile"},
		{"write-tree"},
		{"diff-tree", "--no-commit-id", "--name-status", "-r", "$PARENT", "$PATCH_TREE"},
		{"-c", "commit.gpgSign=false", "commit-tree", "$PATCH_TREE", "-p", "$PARENT", "-F", "-"},
		{"update-ref", "refs/heads/fogcast/stage-a-baseline", "$PATCH_COMMIT", "$ZERO"},
		{"symbolic-ref", "HEAD", "refs/heads/fogcast/stage-a-baseline"},
		{"read-tree", "$PATCH_TREE"},
		{"check-attr", "--cached", "--all", "--", "Makefile"},
		{"checkout-index", "--all"},
		{"cat-file", "blob", "$PATCH_TREE:Makefile"},
		{"status", "--porcelain=v1", "--untracked-files=all"},
		{"symbolic-ref", "-q", "HEAD"},
		{"rev-parse", "HEAD^"},
		{"rev-parse", "$PARENT^{tree}"},
		{"rev-parse", "HEAD^{tree}"},
		{"cat-file", "commit", "HEAD"},
		{"diff-tree", "--no-commit-id", "--name-status", "-r", "$PARENT", "$PATCH_TREE"},
		{"ls-tree", "$UPSTREAM_TREE", "--", "Makefile"},
		{"cat-file", "blob", "$SOURCE_BLOB"},
		{"cat-file", "blob", "HEAD:Makefile"},
		{"remote"},
		{"remote", "get-url", "upstream"},
		{"remote", "get-url", "--push", "upstream"},
		{"status", "--porcelain=v1", "--untracked-files=all"},
		{"symbolic-ref", "-q", "HEAD"},
		{"rev-parse", "HEAD^"},
		{"rev-parse", "$PARENT^{tree}"},
		{"rev-parse", "HEAD^{tree}"},
		{"cat-file", "commit", "HEAD"},
		{"diff-tree", "--no-commit-id", "--name-status", "-r", "$PARENT", "$PATCH_TREE"},
		{"ls-tree", "$UPSTREAM_TREE", "--", "Makefile"},
		{"cat-file", "blob", "$SOURCE_BLOB"},
		{"cat-file", "blob", "HEAD:Makefile"},
		{"remote"},
		{"remote", "get-url", "upstream"},
		{"remote", "get-url", "--push", "upstream"},
		{"rev-parse", "HEAD^{commit}"},
		{"rev-parse", "HEAD^{tree}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered transcript = %#v\nwant %#v", got, want)
	}
	assertExactGitEnvironments(t, runner.commands, bootstrap.InitialCommit)
}

func normalizeAbsentTranscript(t *testing.T, commands []Command, bootstrap Bootstrap, identity ForkIdentity) [][]string {
	t.Helper()
	var sourceBlob, patchedBlob string
	for _, command := range commands {
		if len(command.Args) == 3 && command.Args[0] == "cat-file" && command.Args[1] == "blob" && sourceBlob == "" {
			sourceBlob = command.Args[2]
		}
		if len(command.Args) == 4 && command.Args[0] == "update-index" && command.Args[2] == "--cacheinfo" {
			parts := strings.Split(command.Args[3], ",")
			if len(parts) == 3 {
				patchedBlob = parts[1]
			}
		}
	}
	if sourceBlob == "" || patchedBlob == "" {
		t.Fatalf("cannot normalize source=%q patched=%q transcript=%#v", sourceBlob, patchedBlob, commands)
	}
	replacements := []struct{ old, new string }{
		{bootstrap.MainUpstream.FetchURL, "$URL"}, {bootstrap.MainUpstream.Commit, "$PARENT"}, {bootstrap.MainUpstream.Tree, "$UPSTREAM_TREE"},
		{sourceBlob, "$SOURCE_BLOB"}, {patchedBlob, "$PATCHED_BLOB"}, {identity.PatchTree, "$PATCH_TREE"}, {identity.PatchCommit, "$PATCH_COMMIT"}, {strings.Repeat("0", 40), "$ZERO"},
	}
	got := make([][]string, len(commands))
	for i, command := range commands {
		got[i] = make([]string, len(command.Args))
		for j, argument := range command.Args {
			for _, replacement := range replacements {
				argument = strings.ReplaceAll(argument, replacement.old, replacement.new)
			}
			got[i][j] = argument
		}
	}
	return got
}

// These cases are intentionally raw filesystem layouts instead of real Git
// worktrees: each must be rejected before a Runner can execute any command.
func TestVerifyFogCastC0RejectsHostileLinkedWorktreeIndirectionBeforeGit(t *testing.T) {
	bootstrap, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{"git symlink", func(t *testing.T, root string) {
			makeSymlink(t, filepath.Join(root, ".git"), filepath.Join(root, "target"))
		}},
		{"malformed gitdir", func(t *testing.T, root string) { writeGitFile(t, root, "not a gitdir\n") }},
		{"multiline gitdir", func(t *testing.T, root string) { writeGitFile(t, root, "gitdir: target\nextra\n") }},
		{"gitdir target symlink", func(t *testing.T, root string) {
			writeGitFile(t, root, "gitdir: target\n")
			makeSymlink(t, filepath.Join(root, "target"), filepath.Join(root, "actual"))
		}},
		{"commondir symlink", func(t *testing.T, root string) {
			makeLinkedLayout(t, root, true, false, "[core]\nrepositoryformatversion = 0\n")
		}},
		{"config symlink", func(t *testing.T, root string) {
			makeLinkedLayout(t, root, false, true, "[core]\nrepositoryformatversion = 0\n")
		}},
		{"worktreeConfig enabled", func(t *testing.T, root string) {
			makeLinkedLayout(t, root, false, false, "[extensions]\nworktreeConfig = true\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			runner := &recordingRunner{}
			err := VerifyFogCastC0(context.Background(), runner, bootstrap, root)
			if !hasCode(err, CodeRepositoryPolicyMismatch) || len(runner.commands) != 0 {
				t.Fatalf("VerifyFogCastC0() = %v commands=%#v", err, runner.commands)
			}
		})
	}
}

func writeGitFile(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeSymlink(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func makeLinkedLayout(t *testing.T, root string, commondirSymlink, configSymlink bool, config string) {
	t.Helper()
	writeGitFile(t, root, "gitdir: gitdir\n")
	gitdir, common := filepath.Join(root, "gitdir"), filepath.Join(root, "common")
	if err := os.Mkdir(gitdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if commondirSymlink {
		makeSymlink(t, filepath.Join(gitdir, "commondir"), common)
		return
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte("../common\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(common, 0o700); err != nil {
		t.Fatal(err)
	}
	if configSymlink {
		makeSymlink(t, filepath.Join(common, "config"), filepath.Join(root, "outside-config"))
		return
	}
	if err := os.WriteFile(filepath.Join(common, "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

var _ io.Reader = failingRandomReader{}
