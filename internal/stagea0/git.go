package stagea0

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Run executes exactly the supplied command. In particular, Env is a complete
// environment rather than an addition to the caller's environment.
func (ExecRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Env, cmd.Dir = command.Env, command.Dir
	cmd.Stdin = bytes.NewReader(command.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		result.ExitCode = exited.ExitCode()
		return result, nil
	}
	return CommandResult{}, failure(CodeCommandFailed, "git", "Git command could not start")
}

func VerifyFogCastC0(ctx context.Context, runner Runner, bootstrap Bootstrap, fogcastRoot string) error {
	if err := ValidateBootstrap(bootstrap); err != nil {
		return err
	}
	gitPath, err := gitExecutable()
	if err != nil {
		return err
	}
	root, err := filepath.Abs(fogcastRoot)
	if err != nil {
		return failure(CodeRepositoryPolicyMismatch, "repository", "FogCast repository is invalid")
	}
	if err := rejectUnsafeRepositoryConfig(root); err != nil {
		return err
	}
	env := readOnlyGitEnv()
	head, err := gitOutput(ctx, runner, Command{Path: gitPath, Args: []string{"rev-parse", "HEAD^{commit}"}, Env: env, Dir: root})
	if err != nil || strings.TrimSpace(head) != bootstrap.FogCastBaseRevision {
		return failure(CodeRepositoryPolicyMismatch, "repository", "FogCast C0 revision does not match")
	}
	symbolic, err := runner.Run(ctx, Command{Path: gitPath, Args: []string{"symbolic-ref", "-q", "HEAD"}, Env: env, Dir: root})
	if err != nil || symbolic.ExitCode == 0 {
		return failure(CodeRepositoryPolicyMismatch, "repository", "FogCast HEAD is not detached")
	}
	status, err := gitOutput(ctx, runner, Command{Path: gitPath, Args: []string{"status", "--porcelain=v1", "--untracked-files=all"}, Env: env, Dir: root})
	if err != nil || status != "" {
		return failure(CodeRepositoryPolicyMismatch, "repository", "FogCast worktree is not clean")
	}
	return nil
}

func InitializeFork(ctx context.Context, runner Runner, request InitRequest) (ForkIdentity, error) {
	if err := ValidateBootstrap(request.Bootstrap); err != nil {
		return ForkIdentity{}, err
	}
	if !safeSourcePath(request.Bootstrap.VDate.SourcePath) {
		return ForkIdentity{}, failure(CodeBootstrapSchemaInvalid, "bootstrap", "VDATE source path is invalid")
	}
	gitPath, err := gitExecutable()
	if err != nil {
		return ForkIdentity{}, err
	}
	destination, err := filepath.Abs(request.Destination)
	if err != nil || filepath.Base(destination) == "." {
		return ForkIdentity{}, failure(CodeRepositoryPolicyMismatch, "repository", "destination is invalid")
	}
	if _, err := os.Lstat(destination); err == nil {
		return verifyExistingFork(ctx, runner, gitPath, destination, request.Bootstrap)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ForkIdentity{}, failure(CodeRepositoryPolicyMismatch, "repository", "destination cannot be inspected")
	}
	parent := filepath.Dir(destination)
	lock, err := acquirePublicationLock(destination + ".stage-a0.publish.lock")
	if err != nil {
		return ForkIdentity{}, err
	}
	defer lock.release()
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return ForkIdentity{}, failure(CodeRepositoryPolicyMismatch, "repository", "destination appeared before initialization")
	}
	temporary, err := os.MkdirTemp(parent, ".Main_MiSTer.stage-a0-")
	if err != nil {
		return ForkIdentity{}, failure(CodeCommandFailed, "repository", "temporary repository could not be created")
	}
	owned, err := ownDirectory(temporary)
	if err != nil {
		return ForkIdentity{}, failure(CodeCommandFailed, "repository", "temporary repository could not be verified")
	}
	defer owned.remove()
	identity, err := initializeTemporaryFork(ctx, runner, gitPath, temporary, request.Bootstrap)
	if err != nil {
		return ForkIdentity{}, err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return ForkIdentity{}, failure(CodeRepositoryPolicyMismatch, "repository", "destination appeared before publication")
	}
	if err := os.Rename(temporary, destination); err != nil {
		return ForkIdentity{}, failure(CodeCommandFailed, "repository", "repository publication failed")
	}
	owned.disarm()
	if _, err := verifyExistingFork(ctx, runner, gitPath, destination, request.Bootstrap); err != nil {
		return ForkIdentity{}, err
	}
	return identity, nil
}

func initializeTemporaryFork(ctx context.Context, runner Runner, gitPath, root string, bootstrap Bootstrap) (ForkIdentity, error) {
	index := filepath.Join(root, ".git", "index")
	env := gitEnv(bootstrap.InitialCommit, index)
	run := func(args ...string) (string, error) {
		return gitOutput(ctx, runner, Command{Path: gitPath, Args: args, Env: env, Dir: root})
	}
	if _, err := run("init"); err != nil {
		return ForkIdentity{}, err
	}
	if _, err := run("remote", "add", "stage-a0-fetch", bootstrap.MainUpstream.FetchURL); err != nil {
		return ForkIdentity{}, err
	}
	if _, err := run("fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "stage-a0-fetch", bootstrap.MainUpstream.Commit); err != nil {
		return ForkIdentity{}, err
	}
	tree, err := run("rev-parse", bootstrap.MainUpstream.Commit+"^{tree}")
	if err != nil || strings.TrimSpace(tree) != bootstrap.MainUpstream.Tree {
		return ForkIdentity{}, repositoryMismatch("locked upstream tree does not match")
	}
	if _, err := run("remote", "remove", "stage-a0-fetch"); err != nil {
		return ForkIdentity{}, err
	}
	refs, err := run("for-each-ref", "--format=%(refname)")
	if err != nil || refs != "" {
		return ForkIdentity{}, repositoryMismatch("temporary fetch ref remains")
	}
	if _, err := os.Lstat(filepath.Join(root, ".git", "FETCH_HEAD")); !errors.Is(err, os.ErrNotExist) {
		return ForkIdentity{}, repositoryMismatch("temporary fetch state remains")
	}
	mode, blob, err := lockedSourceBlob(ctx, runner, gitPath, root, env, bootstrap)
	if err != nil {
		return ForkIdentity{}, err
	}
	source, err := run("cat-file", "blob", blob)
	if err != nil {
		return ForkIdentity{}, err
	}
	patched, err := ApplyVDateRecipeV1([]byte(source), bootstrap.VDate)
	if err != nil {
		return ForkIdentity{}, err
	}
	if _, err := run("remote", "add", "upstream", bootstrap.MainUpstream.FetchURL); err != nil {
		return ForkIdentity{}, err
	}
	for _, pair := range [][2]string{{"core.autocrlf", "false"}, {"core.eol", "lf"}, {"core.attributesfile", "/dev/null"}, {"remote.upstream.url", bootstrap.MainUpstream.FetchURL}, {"remote.upstream.fetch", "+refs/heads/*:refs/remotes/upstream/*"}, {"remote.upstream.pushurl", DisabledPushURL}} {
		if _, err := run("config", "--local", pair[0], pair[1]); err != nil {
			return ForkIdentity{}, err
		}
	}
	if err := verifyClosedConfig(root, bootstrap); err != nil {
		return ForkIdentity{}, err
	}
	if _, err := run("read-tree", bootstrap.Branch.ParentCommit); err != nil {
		return ForkIdentity{}, err
	}
	patchedBlob, err := runWithInput(ctx, runner, Command{Path: gitPath, Args: []string{"hash-object", "-w", "--stdin"}, Env: env, Dir: root, Stdin: patched})
	if err != nil {
		return ForkIdentity{}, err
	}
	patchedBlob = strings.TrimSpace(patchedBlob)
	if _, err := run("update-index", "--add", "--cacheinfo", mode+","+patchedBlob+","+bootstrap.VDate.SourcePath); err != nil {
		return ForkIdentity{}, err
	}
	patchTree, err := run("write-tree")
	if err != nil {
		return ForkIdentity{}, err
	}
	patchTree = strings.TrimSpace(patchTree)
	diff, err := run("diff-tree", "--no-commit-id", "--name-status", "-r", bootstrap.Branch.ParentCommit, patchTree)
	if err != nil || diff != "M\t"+bootstrap.VDate.SourcePath+"\n" {
		return ForkIdentity{}, repositoryMismatch("patch changes an unexpected path")
	}
	patchCommit, err := runWithInput(ctx, runner, Command{Path: gitPath, Args: []string{"-c", "commit.gpgSign=false", "commit-tree", patchTree, "-p", bootstrap.Branch.ParentCommit, "-F", "-"}, Env: env, Dir: root, Stdin: []byte(bootstrap.InitialCommit.CommitMessage)})
	if err != nil {
		return ForkIdentity{}, err
	}
	patchCommit = strings.TrimSpace(patchCommit)
	zero := strings.Repeat("0", 40)
	if _, err := run("update-ref", "refs/heads/"+bootstrap.Branch.Name, patchCommit, zero); err != nil {
		return ForkIdentity{}, err
	}
	if _, err := run("symbolic-ref", "HEAD", "refs/heads/"+bootstrap.Branch.Name); err != nil {
		return ForkIdentity{}, err
	}
	if _, err := run("read-tree", patchTree); err != nil {
		return ForkIdentity{}, err
	}
	attrs, err := run("check-attr", "--cached", "--all", "--", bootstrap.VDate.SourcePath)
	if err != nil || strings.TrimSpace(attrs) != "" {
		return ForkIdentity{}, repositoryMismatch("source has checkout-altering attributes")
	}
	if _, err := run("checkout-index", "--all"); err != nil {
		return ForkIdentity{}, err
	}
	materialized, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(bootstrap.VDate.SourcePath)))
	if err != nil {
		return ForkIdentity{}, failure(CodeCommandFailed, "repository", "patched source could not be verified")
	}
	digest := sha256.Sum256(materialized)
	committed, err := run("cat-file", "blob", patchTree+":"+bootstrap.VDate.SourcePath)
	if err != nil {
		return ForkIdentity{}, err
	}
	committedDigest := sha256.Sum256([]byte(committed))
	if digest != committedDigest {
		return ForkIdentity{}, repositoryMismatch("materialized source differs from committed blob")
	}
	identity := ForkIdentity{UpstreamCommit: bootstrap.MainUpstream.Commit, UpstreamTree: bootstrap.MainUpstream.Tree, Branch: bootstrap.Branch.Name, PatchCommit: patchCommit, PatchTree: patchTree}
	if _, err := verifyFork(ctx, runner, gitPath, root, bootstrap, identity, env); err != nil {
		return ForkIdentity{}, err
	}
	return identity, nil
}

func verifyExistingFork(ctx context.Context, runner Runner, gitPath, root string, bootstrap Bootstrap) (ForkIdentity, error) {
	if err := verifyClosedConfig(root, bootstrap); err != nil {
		return ForkIdentity{}, err
	}
	identity := ForkIdentity{UpstreamCommit: bootstrap.MainUpstream.Commit, UpstreamTree: bootstrap.MainUpstream.Tree, Branch: bootstrap.Branch.Name}
	if _, err := verifyFork(ctx, runner, gitPath, root, bootstrap, identity, readOnlyGitEnv()); err != nil {
		return ForkIdentity{}, err
	}
	commit, err := gitOutput(ctx, runner, Command{Path: gitPath, Args: []string{"rev-parse", "HEAD^{commit}"}, Env: readOnlyGitEnv(), Dir: root})
	if err != nil {
		return ForkIdentity{}, repositoryMismatch("existing fork has no HEAD")
	}
	identity.PatchCommit = strings.TrimSpace(commit)
	tree, err := gitOutput(ctx, runner, Command{Path: gitPath, Args: []string{"rev-parse", "HEAD^{tree}"}, Env: readOnlyGitEnv(), Dir: root})
	if err != nil {
		return ForkIdentity{}, repositoryMismatch("existing fork has no tree")
	}
	identity.PatchTree = strings.TrimSpace(tree)
	return identity, nil
}

func verifyFork(ctx context.Context, runner Runner, gitPath, root string, bootstrap Bootstrap, identity ForkIdentity, env []string) (ForkIdentity, error) {
	run := func(args ...string) (string, error) {
		return gitOutput(ctx, runner, Command{Path: gitPath, Args: args, Env: env, Dir: root})
	}
	status, err := run("status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "" {
		return ForkIdentity{}, repositoryMismatch("fork worktree is not clean")
	}
	head, err := run("symbolic-ref", "-q", "HEAD")
	if err != nil || strings.TrimSpace(head) != "refs/heads/"+bootstrap.Branch.Name {
		return ForkIdentity{}, repositoryMismatch("fork branch is invalid")
	}
	parent, err := run("rev-parse", "HEAD^")
	if err != nil || strings.TrimSpace(parent) != bootstrap.Branch.ParentCommit {
		return ForkIdentity{}, repositoryMismatch("fork parent is invalid")
	}
	parentTree, err := run("rev-parse", bootstrap.Branch.ParentCommit+"^{tree}")
	if err != nil || strings.TrimSpace(parentTree) != bootstrap.MainUpstream.Tree {
		return ForkIdentity{}, repositoryMismatch("fork parent tree is invalid")
	}
	headTree, err := run("rev-parse", "HEAD^{tree}")
	if err != nil {
		return ForkIdentity{}, repositoryMismatch("fork tree is invalid")
	}
	headTree = strings.TrimSpace(headTree)
	rawCommit, err := run("cat-file", "commit", "HEAD")
	if err != nil || !bytes.Equal([]byte(rawCommit), expectedCommitObject(headTree, bootstrap)) {
		return ForkIdentity{}, repositoryMismatch("fork commit identity is invalid")
	}
	diff, err := run("diff-tree", "--no-commit-id", "--name-status", "-r", bootstrap.Branch.ParentCommit, headTree)
	if err != nil || diff != "M\t"+bootstrap.VDate.SourcePath+"\n" {
		return ForkIdentity{}, repositoryMismatch("fork patch changes an unexpected path")
	}
	_, blob, err := lockedSourceBlob(ctx, runner, gitPath, root, env, bootstrap)
	if err != nil {
		return ForkIdentity{}, err
	}
	source, err := run("cat-file", "blob", blob)
	if err != nil {
		return ForkIdentity{}, repositoryMismatch("fork parent source is invalid")
	}
	expected, err := ApplyVDateRecipeV1([]byte(source), bootstrap.VDate)
	if err != nil {
		return ForkIdentity{}, err
	}
	actual, err := run("cat-file", "blob", "HEAD:"+bootstrap.VDate.SourcePath)
	if err != nil || !bytes.Equal([]byte(actual), expected) {
		return ForkIdentity{}, repositoryMismatch("fork patched source differs")
	}
	remote, err := run("remote")
	if err != nil || strings.TrimSpace(remote) != "upstream" {
		return ForkIdentity{}, repositoryMismatch("fork remotes are invalid")
	}
	fetchURL, err := run("remote", "get-url", "upstream")
	if err != nil || strings.TrimSpace(fetchURL) != bootstrap.MainUpstream.FetchURL {
		return ForkIdentity{}, repositoryMismatch("fork fetch URL is invalid")
	}
	pushURL, err := run("remote", "get-url", "--push", "upstream")
	if err != nil || strings.TrimSpace(pushURL) != DisabledPushURL {
		return ForkIdentity{}, repositoryMismatch("fork push URL is invalid")
	}
	return identity, nil
}

func expectedCommitObject(tree string, bootstrap Bootstrap) []byte {
	spec := bootstrap.InitialCommit
	return []byte(fmt.Sprintf("tree %s\nparent %s\nauthor %s <%s> %d +0000\ncommitter %s <%s> %d +0000\n\n%s", tree, bootstrap.Branch.ParentCommit, spec.AuthorName, spec.AuthorEmail, spec.AuthorTimestamp, spec.CommitterName, spec.CommitterEmail, spec.CommitterTimestamp, spec.CommitMessage))
}

func lockedSourceBlob(ctx context.Context, runner Runner, gitPath, root string, env []string, bootstrap Bootstrap) (string, string, error) {
	components := strings.Split(bootstrap.VDate.SourcePath, "/")
	for i := range components {
		path := strings.Join(components[:i+1], "/")
		out, err := gitOutput(ctx, runner, Command{Path: gitPath, Args: []string{"ls-tree", bootstrap.MainUpstream.Tree, "--", path}, Env: env, Dir: root})
		if err != nil {
			return "", "", repositoryMismatch("locked source cannot be inspected")
		}
		fields := strings.Fields(strings.TrimSpace(out))
		if len(fields) < 3 || fields[2] == "" {
			return "", "", repositoryMismatch("locked source path is invalid")
		}
		if i < len(components)-1 {
			if fields[0] != "040000" || fields[1] != "tree" {
				return "", "", repositoryMismatch("locked source has a non-directory parent")
			}
			continue
		}
		if (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
			return "", "", repositoryMismatch("locked source is not a regular blob")
		}
		return fields[0], fields[2], nil
	}
	return "", "", repositoryMismatch("locked source path is invalid")
}

func gitExecutable() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil || !filepath.IsAbs(path) {
		return "", failure(CodeCommandFailed, "git", "Git executable is unavailable")
	}
	return path, nil
}

func gitOutput(ctx context.Context, runner Runner, command Command) (string, error) {
	result, err := runner.Run(ctx, command)
	if err != nil {
		return "", commandFailure(err)
	}
	if result.ExitCode != 0 {
		return "", failure(CodeCommandFailed, "git", "Git command failed")
	}
	return string(result.Stdout), nil
}

func runWithInput(ctx context.Context, runner Runner, command Command) (string, error) {
	return gitOutput(ctx, runner, command)
}

func commandFailure(err error) error {
	var value *Failure
	if errors.As(err, &value) {
		return err
	}
	return failure(CodeCommandFailed, "git", "Git command failed")
}

func repositoryMismatch(detail string) error {
	return failure(CodeRepositoryPolicyMismatch, "repository", detail)
}

func gitEnv(spec CommitSpec, canonicalIndexPath string) []string {
	return []string{
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ATTR_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1", "LC_ALL=C", "LANG=C", "TZ=UTC",
		"GIT_INDEX_FILE=" + canonicalIndexPath,
		"GIT_AUTHOR_NAME=" + spec.AuthorName, "GIT_AUTHOR_EMAIL=" + spec.AuthorEmail, fmt.Sprintf("GIT_AUTHOR_DATE=@%d +0000", spec.AuthorTimestamp),
		"GIT_COMMITTER_NAME=" + spec.CommitterName, "GIT_COMMITTER_EMAIL=" + spec.CommitterEmail, fmt.Sprintf("GIT_COMMITTER_DATE=@%d +0000", spec.CommitterTimestamp),
	}
}

func readOnlyGitEnv() []string {
	return []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ATTR_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1", "LC_ALL=C", "LANG=C", "TZ=UTC", "GIT_OPTIONAL_LOCKS=0"}
}

func verifyClosedConfig(root string, bootstrap Bootstrap) error {
	values, err := rawGitConfig(root)
	if err != nil {
		return repositoryMismatch("local Git configuration cannot be verified")
	}
	allowed := map[string]string{
		"core.autocrlf": "false", "core.eol": "lf", "core.attributesfile": "/dev/null",
		"remote.upstream.url": bootstrap.MainUpstream.FetchURL, "remote.upstream.fetch": "+refs/heads/*:refs/remotes/upstream/*", "remote.upstream.pushurl": DisabledPushURL,
	}
	for key, value := range values {
		if want, managed := allowed[key]; managed {
			if value != want {
				return repositoryMismatch("local Git configuration differs")
			}
			delete(allowed, key)
			continue
		}
		if !allowedRepositoryConfig(key) {
			return repositoryMismatch("local Git configuration has an unexpected key")
		}
	}
	if len(allowed) != 0 {
		return repositoryMismatch("local Git configuration is incomplete")
	}
	return nil
}

// rawGitConfig reads config without asking Git to interpret includes.
func resolvedGitConfig(root string) (string, error) {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe gitdir")
	}
	if info.IsDir() {
		return filepath.Join(dotGit, "config"), nil
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("unsafe gitdir")
	}
	raw, err := os.ReadFile(dotGit)
	if err != nil || bytes.IndexByte(raw, 0) >= 0 {
		return "", errors.New("unsafe gitdir")
	}
	line := strings.TrimSuffix(string(raw), "\n")
	path, ok := strings.CutPrefix(line, "gitdir: ")
	if !ok || path == "" || strings.ContainsAny(path, "\r\n") {
		return "", errors.New("unsafe gitdir")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	info, err = os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe gitdir")
	}
	commonFile := filepath.Join(path, "commondir")
	info, err = os.Lstat(commonFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe commondir")
	}
	raw, err = os.ReadFile(commonFile)
	if err != nil || bytes.IndexByte(raw, 0) >= 0 {
		return "", errors.New("unsafe commondir")
	}
	common := strings.TrimSuffix(string(raw), "\n")
	if common == "" || strings.ContainsAny(common, "\r\n") {
		return "", errors.New("unsafe commondir")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	common = filepath.Clean(common)
	info, err = os.Lstat(common)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe commondir")
	}
	return filepath.Join(common, "config"), nil
}

// rawGitConfig reads only a resolved regular config file and never asks Git to
// interpret includes.
func rawGitConfig(root string) (map[string]string, error) {
	config, err := resolvedGitConfig(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(config)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe config")
	}
	raw, err := os.ReadFile(config)
	if err != nil || bytes.IndexByte(raw, 0) >= 0 {
		return nil, errors.New("unreadable config")
	}
	values := make(map[string]string)
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			parts := strings.Fields(section)
			if len(parts) == 2 {
				section = parts[0] + "." + strings.Trim(parts[1], `"`)
			}
			section = strings.ToLower(section)
			if section == "" {
				return nil, errors.New("bad section")
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			return nil, errors.New("bad config")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		full := section + "." + key
		if _, duplicate := values[full]; duplicate {
			return nil, errors.New("duplicate config")
		}
		values[full] = value
	}
	return values, nil
}

func rejectUnsafeRepositoryConfig(root string) error {
	values, err := rawGitConfig(root)
	if err != nil {
		return repositoryMismatch("repository configuration cannot be safely inspected")
	}
	for key := range values {
		if key == "extensions.worktreeconfig" || key == "core.fsmonitor" || key == "core.hookspath" || key == "core.attributesfile" || strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") || strings.HasPrefix(key, "filter.") || strings.HasPrefix(key, "diff.") {
			return repositoryMismatch("repository configuration is unsafe")
		}
	}
	return nil
}

func allowedRepositoryConfig(key string) bool {
	switch key {
	case "core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.symlinks", "core.ignorecase", "core.precomposeunicode", "extensions.objectformat":
		return true
	default:
		return false
	}
}

type publicationLock struct {
	path     string
	file     *os.File
	dev, ino uint64
}

func acquirePublicationLock(path string) (*publicationLock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, failure(CodeRepositoryPolicyMismatch, "repository", "publication is already in progress")
	}
	var token [32]byte
	lock := &publicationLock{path: path, file: file}
	var opened syscall.Stat_t
	info, err := file.Stat()
	if err != nil || !fillStat(info, &opened) {
		_ = file.Close()
		return nil, failure(CodeCommandFailed, "repository", "publication lock could not be verified")
	}
	lock.dev, lock.ino = uint64(opened.Dev), uint64(opened.Ino)
	success := false
	defer func() {
		if !success {
			lock.release()
		}
	}()
	if _, err := io.ReadFull(rand.Reader, token[:]); err != nil {
		return nil, failure(CodeCommandFailed, "repository", "publication lock could not be initialized")
	}
	want := []byte(hex.EncodeToString(token[:]))
	if written, writeErr := file.Write(want); writeErr != nil || written != len(want) || file.Sync() != nil {
		return nil, failure(CodeCommandFailed, "repository", "publication lock could not be initialized")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, failure(CodeCommandFailed, "repository", "publication lock could not be verified")
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(file, got); err != nil || !bytes.Equal(got, want) {
		return nil, failure(CodeCommandFailed, "repository", "publication lock could not be verified")
	}
	var named syscall.Stat_t
	info, err = os.Lstat(path)
	if err != nil || !fillStat(info, &named) {
		return nil, failure(CodeCommandFailed, "repository", "publication lock could not be verified")
	}
	if opened.Dev != named.Dev || opened.Ino != named.Ino {
		return nil, failure(CodeCommandFailed, "repository", "publication lock could not be verified")
	}
	success = true
	return lock, nil
}

type ownedDirectory struct {
	path     string
	dev, ino uint64
	active   bool
}

func ownDirectory(path string) (*ownedDirectory, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe temporary directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.New("unsafe temporary directory")
	}
	return &ownedDirectory{path: path, dev: uint64(stat.Dev), ino: uint64(stat.Ino), active: true}, nil
}

func (d *ownedDirectory) disarm() {
	if d != nil {
		d.active = false
	}
}
func (d *ownedDirectory) remove() {
	if d == nil || !d.active {
		return
	}
	info, err := os.Lstat(d.path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Dev) != d.dev || uint64(stat.Ino) != d.ino {
		return
	}
	_ = os.RemoveAll(d.path)
}

func fillStat(info os.FileInfo, target *syscall.Stat_t) bool {
	value, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return false
	}
	*target = *value
	return true
}

func (l *publicationLock) release() {
	if l == nil || l.file == nil {
		return
	}
	var named syscall.Stat_t
	if info, err := os.Lstat(l.path); err == nil && fillStat(info, &named) && uint64(named.Dev) == l.dev && uint64(named.Ino) == l.ino {
		_ = os.Remove(l.path)
	}
	_ = l.file.Close()
}
