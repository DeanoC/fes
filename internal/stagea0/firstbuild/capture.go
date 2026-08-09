package firstbuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Request contains operational locations only. The locations are deliberately
// not part of Evidence and are never serialized into the deterministic report.
type Request struct {
	SourceDir        string
	ToolchainArchive string
	OutputDir        string
	ImageRef         string
}

const dockerBinary = "docker"

func DefaultRequest() Request {
	return Request{ImageRef: ExpectedContainerReference}
}

// Capture materializes the locked source and toolchain, executes one real
// network-disabled Docker build, observes its complete bin inventory, and
// returns the preliminary Software-tested receipt. It refuses to emit a
// receipt for any input or output drift.
func Capture(ctx context.Context, request Request) (Evidence, error) {
	req := request
	defaults := DefaultRequest()
	if req.ImageRef == "" {
		req.ImageRef = defaults.ImageRef
	}
	if err := validateRequest(req); err != nil {
		return Evidence{}, err
	}
	if err := ensureOutputAbsent(req.OutputDir); err != nil {
		return Evidence{}, err
	}
	if err := os.MkdirAll(filepath.Dir(req.OutputDir), 0o700); err != nil {
		return Evidence{}, &Failure{Code: CodeOutputInvalid, Detail: "evidence output parent cannot be created"}
	}
	staging, err := os.MkdirTemp(filepath.Dir(req.OutputDir), ".stage-a0-firstbuild-*")
	if err != nil {
		return Evidence{}, &Failure{Code: CodeOutputInvalid, Detail: "temporary capture root cannot be created"}
	}
	defer os.RemoveAll(staging)
	workRoot := filepath.Join(staging, "work")
	if err := os.Mkdir(workRoot, 0o700); err != nil {
		return Evidence{}, &Failure{Code: CodeOutputInvalid, Detail: "temporary capture root cannot be created"}
	}

	source, err := observeSource(ctx, req.SourceDir)
	if err != nil {
		return Evidence{}, err
	}
	toolchain, toolchainRoot, err := prepareToolchain(ctx, req, workRoot)
	if err != nil {
		return Evidence{}, err
	}
	materialized, err := materializeSource(ctx, req.SourceDir, source.Commit, workRoot)
	if err != nil {
		return Evidence{}, err
	}
	confirmedSource, err := observeSource(ctx, req.SourceDir)
	if err != nil || confirmedSource != source {
		return Evidence{}, sourceFailure()
	}
	beforeTree, err := snapshotTree(materialized)
	if err != nil {
		return Evidence{}, err
	}
	container, err := inspectContainer(ctx, req)
	if err != nil {
		return Evidence{}, err
	}
	logBytes, compilerVersion, exitCode, err := runBuild(ctx, req, materialized, toolchainRoot, container.ImageID)
	if err != nil {
		return Evidence{}, err
	}
	if err := ValidateBuildAdapterLog(logBytes); err != nil {
		return Evidence{}, err
	}
	if compilerVersion != ExpectedCompilerVersion {
		return Evidence{}, &Failure{Code: CodeToolchainDrift, Detail: "compiler version differs from reviewed baseline"}
	}
	afterTree, err := snapshotTree(materialized)
	if err != nil {
		return Evidence{}, err
	}
	if err := validateUnchangedOutsideBin(beforeTree, afterTree); err != nil {
		return Evidence{}, err
	}

	binRoot := filepath.Join(materialized, "bin")
	inventory, err := collectInventory(binRoot, materialized)
	if err != nil {
		return Evidence{}, err
	}
	inventoryDigest, err := InventoryDigest(inventory)
	if err != nil {
		return Evidence{}, err
	}
	artifacts, err := observeArtifacts(ctx, binRoot)
	if err != nil {
		return Evidence{}, err
	}
	evidence := Evidence{
		Format:                 FormatV1,
		Schema:                 SchemaV1,
		Status:                 EvidenceStatusSoftwareTested,
		CanonicalStageA0Report: false,
		TwoBuildsByteIdentical: false,
		Source:                 source,
		Toolchain:              toolchain,
		Container:              container,
		Build: BuildEvidence{
			VDate:           ExpectedVDate,
			SourceDateEpoch: ExpectedSourceDateEpoch,
			Command:         []string{"make", "V=1", "VDATE=260808"},
			NetworkMode:     "none",
			ExitCode:        exitCode,
		},
		Output: OutputEvidence{
			InventoryCount:  len(inventory),
			InventorySHA256: inventoryDigest,
			Inventory:       inventory,
			Artifacts:       artifacts,
		},
	}
	if err := ValidateCapturedEvidence(evidence); err != nil {
		return Evidence{}, err
	}
	if err := writePayload(staging, logBytes, evidence, binRoot); err != nil {
		return Evidence{}, err
	}
	if err := os.RemoveAll(workRoot); err != nil {
		return Evidence{}, &Failure{Code: CodeOutputInvalid, Detail: "temporary capture root cannot be removed"}
	}
	if err := os.Rename(staging, req.OutputDir); err != nil {
		return Evidence{}, &Failure{Code: CodeOutputInvalid, Detail: "evidence output cannot be published atomically"}
	}
	return evidence, nil
}

func validateRequest(req Request) error {
	if req.SourceDir == "" || req.ToolchainArchive == "" || req.OutputDir == "" {
		return &Failure{Code: CodeSchemaInvalid, Detail: "source, toolchain, and output locations are required"}
	}
	if req.ImageRef != ExpectedContainerReference {
		return &Failure{Code: CodeContainerDrift, Detail: "container reference differs from reviewed baseline"}
	}
	if !filepath.IsAbs(req.SourceDir) || !filepath.IsAbs(req.ToolchainArchive) || !filepath.IsAbs(req.OutputDir) {
		return &Failure{Code: CodeSchemaInvalid, Detail: "operational locations must be absolute"}
	}
	return nil
}

func ensureOutputAbsent(output string) error {
	_, err := os.Lstat(output)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return &Failure{Code: CodeOutputInvalid, Detail: "evidence output cannot be inspected"}
	}
	return &Failure{Code: CodeOutputInvalid, Detail: "evidence output already exists"}
}

func observeSource(ctx context.Context, source string) (SourceEvidence, error) {
	branch, err := gitValue(ctx, source, "branch", "--show-current")
	if err != nil {
		return SourceEvidence{}, sourceFailure()
	}
	commit, err := gitValue(ctx, source, "rev-parse", "HEAD")
	if err != nil {
		return SourceEvidence{}, sourceFailure()
	}
	tree, err := gitValue(ctx, source, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return SourceEvidence{}, sourceFailure()
	}
	parent, err := gitValue(ctx, source, "rev-parse", "HEAD^")
	if err != nil {
		return SourceEvidence{}, sourceFailure()
	}
	subject, err := gitValue(ctx, source, "show", "-s", "--format=%s", "HEAD")
	if err != nil {
		return SourceEvidence{}, sourceFailure()
	}
	status, err := gitValue(ctx, source, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return SourceEvidence{}, sourceFailure()
	}
	files, err := gitValue(ctx, source, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return SourceEvidence{}, sourceFailure()
	}
	tracked := 0
	if strings.TrimSpace(files) != "" {
		tracked = len(strings.Split(strings.TrimSuffix(files, "\n"), "\n"))
	}
	return SourceEvidence{
		Branch:       branch,
		Commit:       commit,
		Tree:         tree,
		Parent:       parent,
		Subject:      subject,
		TrackedFiles: tracked,
		Clean:        status == "",
	}, nil
}

func gitValue(ctx context.Context, source string, args ...string) (string, error) {
	full := append([]string{"-C", source}, args...)
	result, err := runCommand(ctx, "git", full...)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(result), "\n"), nil
}

func prepareToolchain(ctx context.Context, req Request, workRoot string) (ToolchainEvidence, string, error) {
	info, err := os.Stat(req.ToolchainArchive)
	if err != nil || !info.Mode().IsRegular() {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive is unavailable"}
	}
	stagedArchive := filepath.Join(workRoot, "toolchain.tar.xz")
	if err := copyFile(req.ToolchainArchive, stagedArchive); err != nil {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive cannot be staged"}
	}
	hash, size, err := hashFile(stagedArchive)
	if err != nil || size != ExpectedToolchainArchiveSize || hash != ExpectedToolchainArchiveSHA256 {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive differs from reviewed baseline"}
	}
	membersRaw, err := runCommand(ctx, "tar", "-tJf", stagedArchive)
	if err != nil {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive cannot be listed"}
	}
	members := splitLines(string(membersRaw))
	root, err := archiveRoot(members)
	if err != nil {
		return ToolchainEvidence{}, "", err
	}
	if err := validateArchiveLinks(ctx, stagedArchive, members, root); err != nil {
		return ToolchainEvidence{}, "", err
	}
	toolchainDir := filepath.Join(workRoot, "toolchain")
	if err := os.Mkdir(toolchainDir, 0o700); err != nil {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain extraction root cannot be created"}
	}
	if _, err := runCommand(ctx, "tar", "-xJf", stagedArchive, "-C", toolchainDir); err != nil {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive cannot be extracted"}
	}
	postHash, postSize, err := hashFile(stagedArchive)
	if err != nil || postSize != size || postHash != hash {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "staged toolchain archive changed during capture"}
	}
	rootDir := filepath.Join(toolchainDir, root)
	compilerPath := filepath.Join(rootDir, "bin", ExpectedCompiler)
	compilerInfo, err := os.Stat(compilerPath)
	if err != nil || !compilerInfo.Mode().IsRegular() || compilerInfo.Mode().Perm()&0o111 == 0 {
		return ToolchainEvidence{}, "", &Failure{Code: CodeToolchainDrift, Detail: "locked compiler is absent from archive"}
	}
	return ToolchainEvidence{
		ArchiveSize:     size,
		ArchiveSHA256:   hash,
		ArchiveRoot:     root,
		Compiler:        ExpectedCompiler,
		CompilerVersion: ExpectedCompilerVersion,
	}, rootDir, nil
}

func archiveRoot(members []string) (string, error) {
	if len(members) == 0 {
		return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive is empty"}
	}
	root := ""
	for _, member := range members {
		member = filepath.ToSlash(strings.TrimSpace(member))
		if member == "" || path.IsAbs(member) || strings.HasPrefix(member, "../") || strings.Contains(member, "/../") {
			return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive contains an unsafe member"}
		}
		trimmed := strings.TrimSuffix(member, "/")
		if trimmed == "" || path.Clean(trimmed) != trimmed {
			return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive contains an unsafe member"}
		}
		parts := strings.Split(trimmed, "/")
		if len(parts) == 0 || parts[0] == "" {
			return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive root is invalid"}
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive has multiple roots"}
		}
	}
	if root != ExpectedToolchainArchiveRoot {
		return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive root differs from reviewed baseline"}
	}
	return root, nil
}

func validateArchiveLinks(ctx context.Context, archive string, members []string, root string) error {
	memberSet := make(map[string]struct{}, len(members))
	for _, member := range members {
		member = filepath.ToSlash(strings.TrimSuffix(strings.TrimSpace(member), "/"))
		if member != "" {
			memberSet[member] = struct{}{}
		}
	}
	verbose, err := runCommand(ctx, "tar", "-tvJf", archive)
	if err != nil {
		return &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive links cannot be inspected"}
	}
	for _, line := range splitLines(string(verbose)) {
		if line == "" {
			continue
		}
		if line[0] != '-' && line[0] != 'd' && line[0] != 'l' && line[0] != 'h' {
			return &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive contains a special member"}
		}
		if line[0] != 'l' && line[0] != 'h' {
			continue
		}
		delimiter := " -> "
		if line[0] == 'h' {
			delimiter = " link to "
		}
		parts := strings.SplitN(line, delimiter, 2)
		if len(parts) != 2 {
			return &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive link metadata is malformed"}
		}
		leftFields := strings.Fields(parts[0])
		if len(leftFields) == 0 {
			return &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive link name is missing"}
		}
		name := filepath.ToSlash(leftFields[len(leftFields)-1])
		target := filepath.ToSlash(strings.TrimSpace(parts[1]))
		resolved, err := resolveArchiveLink(name, target, root, line[0] == 'h')
		if err != nil {
			return err
		}
		if _, ok := memberSet[resolved]; !ok {
			return &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive link target is absent"}
		}
	}
	return nil
}

func resolveArchiveLink(name, target, root string, hard bool) (string, error) {
	if name == "" || target == "" || path.IsAbs(name) || path.IsAbs(target) || path.Clean(name) != name || path.Clean(target) != target {
		return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive link target is unsafe"}
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	if hard {
		// GNU tar prints hard-link targets as archive member names, while
		// symbolic-link targets are relative to the link's directory.
		resolved = path.Clean(target)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+"/") {
		return "", &Failure{Code: CodeToolchainDrift, Detail: "toolchain archive link escapes its root"}
	}
	return resolved, nil
}

func materializeSource(ctx context.Context, source, commit, workRoot string) (string, error) {
	archive := filepath.Join(workRoot, "source.tar")
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", &Failure{Code: CodeSourceDrift, Detail: "source archive cannot be created"}
	}
	cmd := exec.CommandContext(ctx, "git", "-C", source, "archive", "--format=tar", commit)
	cmd.Stdout = f
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return "", sourceFailure()
	}
	materialized := filepath.Join(workRoot, "main")
	if err := os.Mkdir(materialized, 0o700); err != nil {
		return "", &Failure{Code: CodeSourceDrift, Detail: "source materialization root cannot be created"}
	}
	if _, err := runCommand(ctx, "tar", "-xf", archive, "-C", materialized); err != nil {
		return "", sourceFailure()
	}
	return materialized, nil
}

func inspectContainer(ctx context.Context, req Request) (ContainerEvidence, error) {
	args := []string{"image", "inspect", "--platform", "linux/amd64", req.ImageRef, "--format", "{{.Id}}|{{.Os}}|{{.Architecture}}"}
	raw, err := runCommandWithBinary(ctx, dockerBinary, args...)
	if err != nil {
		return ContainerEvidence{}, &Failure{Code: CodeContainerDrift, Detail: "container image cannot be inspected"}
	}
	fields := strings.Split(strings.TrimSpace(string(raw)), "|")
	if len(fields) != 3 {
		return ContainerEvidence{}, &Failure{Code: CodeContainerDrift, Detail: "container inspection is malformed"}
	}
	evidence := ContainerEvidence{Reference: req.ImageRef, ImageID: fields[0], OS: fields[1], Architecture: fields[2]}
	if err := validateContainer(evidence); err != nil {
		return ContainerEvidence{}, err
	}
	return evidence, nil
}

func runBuild(ctx context.Context, req Request, sourceRoot, toolchainRoot, imageID string) ([]byte, string, int, error) {
	adapterRoot, err := prepareBuildAdapter(sourceRoot)
	if err != nil {
		return nil, "", -1, err
	}
	args := []string{
		"run", "--rm", "--pull=never", "--platform", "linux/amd64", "--network", "none", "--user", "0:0",
		"--volume", sourceRoot + ":/work/main",
		"--volume", toolchainRoot + ":/opt/toolchain:ro",
		"--volume", adapterRoot + ":/stage-a0/build-utils:ro",
		"--workdir", "/work/main",
		imageID,
		"bash", "-lc",
		"set -euxo pipefail; umask 022; export LC_ALL=C TZ=UTC SOURCE_DATE_EPOCH=1786215171 PATH=/stage-a0/build-utils/bin:/opt/toolchain/bin:$PATH; export MAKEFLAGS=; STAGE_A0_JOB_COUNT=$(nproc); printf 'STAGE_A0_JOB_COUNT=%s\\n' \"$STAGE_A0_JOB_COUNT\"; test \"$STAGE_A0_JOB_COUNT\" = 1; arm-none-linux-gnueabihf-gcc --version | sed -n '1p'; make clean VDATE=260808 'SHELL=/bin/bash -o pipefail' BUILDDIR=bin; make V=1 VDATE=260808 'SHELL=/bin/bash -o pipefail' BUILDDIR=bin",
	}
	raw, exitCode, err := runCommandWithBinaryExit(ctx, dockerBinary, args...)
	if err != nil {
		return raw, "", exitCode, &Failure{Code: CodeBuildFailed, Detail: "Docker build failed"}
	}
	compilerVersion := ""
	for _, line := range splitLines(string(raw)) {
		if strings.HasPrefix(line, ExpectedCompiler+" (") {
			compilerVersion = line
			break
		}
	}
	if compilerVersion == "" {
		return raw, "", exitCode, &Failure{Code: CodeToolchainDrift, Detail: "compiler version was not observed"}
	}
	return raw, compilerVersion, exitCode, nil
}

func prepareBuildAdapter(sourceRoot string) (string, error) {
	adapterRoot := filepath.Join(filepath.Dir(sourceRoot), "build-utils")
	binRoot := filepath.Join(adapterRoot, "bin")
	if err := os.MkdirAll(binRoot, 0o755); err != nil {
		return "", &Failure{Code: CodeBuildFailed, Detail: "build adapter root cannot be created"}
	}
	shim := filepath.Join(binRoot, "nproc")
	if err := os.WriteFile(shim, []byte(ExpectedNprocShimContents), 0o755); err != nil {
		return "", &Failure{Code: CodeBuildFailed, Detail: "nproc shim cannot be written"}
	}
	if err := os.Chmod(shim, 0o755); err != nil {
		return "", &Failure{Code: CodeBuildFailed, Detail: "nproc shim mode cannot be set"}
	}
	hash, _, err := hashFile(shim)
	if err != nil || hash != ExpectedNprocShimSHA256 {
		return "", &Failure{Code: CodeBuildFailed, Detail: "nproc shim hash differs from the adapter contract"}
	}
	return adapterRoot, nil
}

// ValidateBuildAdapterLog checks the fixed job-count observation emitted by
// the Stage A0 build adapter. It is shared by capture and comparison tools.
func ValidateBuildAdapterLog(raw []byte) error {
	want := fmt.Sprintf("STAGE_A0_JOB_COUNT=%d", ExpectedJobCount)
	seen := false
	for _, line := range splitLines(string(raw)) {
		if strings.HasPrefix(line, "STAGE_A0_JOB_COUNT=") {
			if line != want || seen {
				return &Failure{Code: CodeBuildFailed, Detail: "build adapter job count differs from the locked contract"}
			}
			seen = true
		}
	}
	if !seen {
		return &Failure{Code: CodeBuildFailed, Detail: "build adapter job count was not observed"}
	}
	return nil
}

func collectInventory(binRoot, sourceRoot string) ([]InventoryEntry, error) {
	var entries []InventoryEntry
	err := filepath.WalkDir(binRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return &Failure{Code: CodeOutputInvalid, Detail: "build inventory contains a symlink"}
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return &Failure{Code: CodeOutputInvalid, Detail: "build inventory contains a non-regular file"}
		}
		relative, err := filepath.Rel(sourceRoot, filePath)
		if err != nil {
			return err
		}
		hash, size, err := hashFile(filePath)
		if err != nil {
			return err
		}
		entries = append(entries, InventoryEntry{
			Path:   filepath.ToSlash(relative),
			Mode:   fmt.Sprintf("%04o", info.Mode().Perm()),
			Size:   size,
			SHA256: hash,
		})
		return nil
	})
	if err != nil {
		if failure, ok := err.(*Failure); ok {
			return nil, failure
		}
		return nil, &Failure{Code: CodeOutputInvalid, Detail: "build inventory cannot be collected"}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func snapshotTree(root string) (map[string]string, error) {
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == root {
			return nil
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		logical := filepath.ToSlash(relative)
		if logical == "." || path.IsAbs(logical) || path.Clean(logical) != logical || strings.HasPrefix(logical, "../") {
			return &Failure{Code: CodeOutputInvalid, Detail: "materialized tree contains an unsafe path"}
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			snapshot[logical] = fmt.Sprintf("dir:%04o", info.Mode().Perm())
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filePath)
			targetSlash := filepath.ToSlash(target)
			resolved := path.Clean(path.Join(path.Dir(logical), targetSlash))
			if err != nil || filepath.IsAbs(target) || path.Clean(targetSlash) != targetSlash || resolved == ".." || strings.HasPrefix(resolved, "../") {
				return &Failure{Code: CodeOutputInvalid, Detail: "materialized tree contains an unsafe symlink"}
			}
			snapshot[logical] = "link:" + targetSlash
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return &Failure{Code: CodeOutputInvalid, Detail: "materialized tree contains a special file"}
		}
		hash, size, err := hashFile(filePath)
		if err != nil {
			return err
		}
		snapshot[logical] = fmt.Sprintf("file:%04o:%d:%s", info.Mode().Perm(), size, hash)
		return nil
	})
	if err != nil {
		if failure, ok := err.(*Failure); ok {
			return nil, failure
		}
		return nil, &Failure{Code: CodeOutputInvalid, Detail: "materialized tree cannot be observed"}
	}
	return snapshot, nil
}

func validateUnchangedOutsideBin(before, after map[string]string) error {
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	for key := range keys {
		if key == "bin" || strings.HasPrefix(key, "bin/") {
			continue
		}
		if before[key] != after[key] {
			return &Failure{Code: CodeOutputInvalid, Detail: "build modified materialized files outside bin"}
		}
	}
	return nil
}

func observeArtifacts(ctx context.Context, binRoot string) ([]ArtifactEvidence, error) {
	paths := []struct {
		logical  string
		filename string
		stripped bool
	}{
		{logical: "bin/MiSTer", filename: "MiSTer", stripped: true},
		{logical: "bin/MiSTer.elf", filename: "MiSTer.elf", stripped: false},
	}
	artifacts := make([]ArtifactEvidence, 0, len(paths))
	for _, item := range paths {
		physical := filepath.Join(binRoot, item.filename)
		info, err := os.Stat(physical)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 {
			return nil, &Failure{Code: CodeOutputInvalid, Detail: "final artifact is missing or has unsafe mode"}
		}
		hash, size, err := hashFile(physical)
		if err != nil {
			return nil, &Failure{Code: CodeOutputInvalid, Detail: "final artifact cannot be hashed"}
		}
		descriptionRaw, err := runCommand(ctx, "file", "-b", physical)
		if err != nil {
			return nil, &Failure{Code: CodeOutputInvalid, Detail: "final artifact type cannot be observed"}
		}
		description := strings.TrimSpace(string(descriptionRaw))
		artifacts = append(artifacts, ArtifactEvidence{Path: item.logical, Mode: "0755", Size: size, SHA256: hash, ELF: description, Stripped: item.stripped})
	}
	return artifacts, nil
}

func writePayload(output string, logBytes []byte, evidence Evidence, binRoot string) error {
	report, err := EncodeEvidence(evidence)
	if err != nil {
		return err
	}
	for _, item := range []struct{ name, source string }{{"MiSTer", "MiSTer"}, {"MiSTer.elf", "MiSTer.elf"}} {
		destination := filepath.Join(output, item.name)
		if err := copyFile(filepath.Join(binRoot, item.source), destination); err != nil {
			return &Failure{Code: CodeOutputInvalid, Detail: "final artifact cannot be copied"}
		}
		gotHash, gotSize, err := hashFile(destination)
		if err != nil {
			return &Failure{Code: CodeOutputInvalid, Detail: "copied artifact cannot be hashed"}
		}
		info, err := os.Stat(destination)
		if err != nil || info.Mode().Perm() != 0o755 {
			return &Failure{Code: CodeOutputInvalid, Detail: "copied artifact mode differs from observed artifact"}
		}
		var want ArtifactEvidence
		for _, artifact := range evidence.Output.Artifacts {
			if artifact.Path == "bin/"+item.name {
				want = artifact
				break
			}
		}
		if want.Path == "" || gotHash != want.SHA256 || gotSize != want.Size {
			return &Failure{Code: CodeOutputInvalid, Detail: "copied artifact differs from observed artifact"}
		}
	}
	if err := os.WriteFile(filepath.Join(output, "build.log"), logBytes, 0o600); err != nil {
		return &Failure{Code: CodeOutputInvalid, Detail: "build log cannot be retained"}
	}
	if err := os.WriteFile(filepath.Join(output, "first-build.json"), report, 0o600); err != nil {
		return &Failure{Code: CodeOutputInvalid, Detail: "evidence report cannot be written"}
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, 0o755)
}

func hashFile(filename string) (string, int64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runCommandWithBinary(ctx, name, args...)
}

func runCommandWithBinary(ctx context.Context, name string, args ...string) ([]byte, error) {
	result, _, err := runCommandWithBinaryExit(ctx, name, args...)
	return result, err
}

func runCommandWithBinaryExit(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if err == nil {
		return output.Bytes(), 0, nil
	}
	exitCode := -1
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		exitCode = exitError.ExitCode()
	}
	return output.Bytes(), exitCode, err
}

func splitLines(value string) []string {
	trimmed := strings.TrimSuffix(value, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func sourceFailure() error {
	return &Failure{Code: CodeSourceDrift, Detail: "source identity cannot be observed"}
}
