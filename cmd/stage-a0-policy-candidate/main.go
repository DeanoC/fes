// Command stage-a0-policy-candidate emits non-promotable Stage A0 policy
// candidates from a pinned local fork and one preliminary build capture.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/materialobserve"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policyobserve"
)

const usage = "usage: stage-a0-policy-candidate --repository DIR --source-material ID --authority FILE --build-log FILE --receipt FILE --artifact-dir DIR --toolchain-archive FILE --toolchain-root DIR --output-dir DIR\n"

type request struct {
	repository       string
	sourceMaterial   string
	authority        string
	buildLog         string
	receipt          string
	artifactDir      string
	toolchainArchive string
	toolchainRoot    string
	outputDir        string
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	req, ok := parseArgs(args)
	if !ok {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	authority, err := readAuthority(req.authority)
	if err == nil {
		err = generate(req, authority)
	}
	if err != nil {
		var failure *policyobserve.Failure
		if errors.As(err, &failure) {
			_, _ = fmt.Fprintf(stderr, "%s\n", failure)
		} else {
			var materialFailure *materialobserve.Failure
			if errors.As(err, &materialFailure) {
				_, _ = fmt.Fprintf(stderr, "%s\n", materialFailure)
			} else {
				_, _ = fmt.Fprintf(stderr, "POLICY_OBSERVE_COMMAND_FAILED: %v\n", err)
			}
		}
		return 1
	}
	_, _ = io.WriteString(stdout, "candidate-observed policies written; promotion remains blocked\n")
	return 0
}

func parseArgs(args []string) (request, bool) {
	var req request
	seen := map[string]bool{}
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) || args[i+1] == "" || seen[args[i]] {
			return request{}, false
		}
		key, value := args[i], args[i+1]
		seen[key] = true
		switch key {
		case "--repository":
			req.repository = value
		case "--source-material":
			req.sourceMaterial = value
		case "--authority":
			req.authority = value
		case "--build-log":
			req.buildLog = value
		case "--receipt":
			req.receipt = value
		case "--artifact-dir":
			req.artifactDir = value
		case "--toolchain-archive":
			req.toolchainArchive = value
		case "--toolchain-root":
			req.toolchainRoot = value
		case "--output-dir":
			req.outputDir = value
		default:
			return request{}, false
		}
	}
	return req, seen["--repository"] && seen["--source-material"] && seen["--authority"] && seen["--build-log"] && seen["--receipt"] && seen["--artifact-dir"] && seen["--toolchain-archive"] && seen["--toolchain-root"] && seen["--output-dir"]
}

func readAuthority(filename string) (policy.Authority, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return policy.Authority{}, fmt.Errorf("authority cannot be read: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var authority policy.Authority
	if err := decoder.Decode(&authority); err != nil {
		return policy.Authority{}, fmt.Errorf("authority JSON cannot be decoded: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return policy.Authority{}, fmt.Errorf("authority JSON has trailing data")
	}
	return authority, nil
}

func generate(req request, authority policy.Authority) error {
	log, err := os.ReadFile(req.buildLog)
	if err != nil {
		return fmt.Errorf("build log cannot be read: %w", err)
	}
	receiptRaw, err := os.ReadFile(req.receipt)
	if err != nil {
		return fmt.Errorf("receipt cannot be read: %w", err)
	}
	evidence, err := firstbuild.DecodeEvidence(receiptRaw)
	if err != nil {
		return err
	}
	if err := firstbuild.ValidateCapturedEvidence(evidence); err != nil {
		return err
	}
	if err := bindEvidenceAuthority(evidence, authority); err != nil {
		return err
	}
	if err := bindBuildLog(string(log), authority); err != nil {
		return err
	}
	sourceSet, err := policyobserve.ObserveSourceSet(req.repository, authority, req.sourceMaterial)
	if err != nil {
		return err
	}
	forkDelta, err := policyobserve.ObserveForkDelta(req.repository, authority)
	if err != nil {
		return err
	}
	compileLink, err := policyobserve.ObserveCompileLink(log, authority)
	if err != nil {
		return err
	}
	generated, err := policyobserve.ObserveGeneratedInput(log, evidence.Output.Inventory, authority)
	if err != nil {
		return err
	}
	intermediate, err := policyobserve.ObserveIntermediate(evidence, authority)
	if err != nil {
		return err
	}
	elfDependency, err := policyobserve.ObserveELFDependency(evidence, req.artifactDir, req.repository, req.toolchainRoot, authority)
	if err != nil {
		return err
	}
	documents := map[string]policy.Document{
		"source-set.json":          sourceSet,
		"upstream-fork-delta.json": forkDelta,
		"compile-link.json":        compileLink,
		"elf-dependency.json":      elfDependency,
		"generated-input.json":     generated,
		"intermediate-path.json":   intermediate,
	}
	policyFiles := make(map[string][]byte, len(documents))
	for name, document := range documents {
		raw, err := policy.Encode(document)
		if err != nil {
			return err
		}
		policyFiles[name] = raw
	}
	materials, err := materialobserve.Observe(materialobserve.Request{
		Evidence: evidence, Authority: authority, Receipt: receiptRaw, BuildLog: log,
		SourceMaterialID: req.sourceMaterial, Repository: req.repository,
		ToolchainArchive: req.toolchainArchive, ToolchainRoot: req.toolchainRoot,
		PolicyFiles: policyFiles,
	})
	if err != nil {
		return err
	}
	materialRaw, err := materialobserve.Encode(materials)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(req.outputDir); err == nil {
		return fmt.Errorf("output directory already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("output directory cannot be inspected: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(req.outputDir), 0o700); err != nil {
		return fmt.Errorf("output parent cannot be created: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(req.outputDir), ".stage-a0-policy-candidate-*")
	if err != nil {
		return fmt.Errorf("output staging directory cannot be created: %w", err)
	}
	defer os.RemoveAll(staging)
	for name, raw := range policyFiles {
		if err := os.WriteFile(filepath.Join(staging, name), raw, 0o600); err != nil {
			return fmt.Errorf("candidate policy cannot be written: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "materials.json"), materialRaw, 0o600); err != nil {
		return fmt.Errorf("material catalog cannot be written: %w", err)
	}
	if err := os.Rename(staging, req.outputDir); err != nil {
		return fmt.Errorf("candidate output cannot be published: %w", err)
	}
	return nil
}

func bindEvidenceAuthority(evidence firstbuild.Evidence, authority policy.Authority) error {
	if evidence.Source.Commit != authority.ForkCommit || evidence.Source.Tree != authority.ForkTree || evidence.Source.Parent != authority.ForkParentCommit {
		return fmt.Errorf("receipt source identity does not match authority")
	}
	if evidence.Build.SourceDateEpoch != authority.SourceDateEpoch || evidence.Build.VDate != authority.VDate {
		return fmt.Errorf("receipt date identity does not match authority")
	}
	return nil
}

func bindBuildLog(log string, authority policy.Authority) error {
	if !strings.Contains(log, "SOURCE_DATE_EPOCH="+strconv.FormatInt(authority.SourceDateEpoch, 10)) || !strings.Contains(log, "make clean VDATE="+authority.VDate) || !strings.Contains(log, "make V=1 VDATE="+authority.VDate) {
		return fmt.Errorf("build log does not contain the reviewed build recipe")
	}
	jobMarker := "STAGE_A0_JOB_COUNT=" + strconv.Itoa(firstbuild.ExpectedJobCount)
	jobSeen := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "STAGE_A0_JOB_COUNT=") {
			if line != jobMarker {
				return fmt.Errorf("build log job count does not match the adapter contract")
			}
			jobSeen++
		}
	}
	if jobSeen != 1 {
		return fmt.Errorf("build log does not contain one adapter job-count observation")
	}
	seen := false
	remaining := log
	for {
		index := strings.Index(remaining, "VDATE=")
		if index < 0 {
			break
		}
		remaining = remaining[index+len("VDATE="):]
		remaining = strings.TrimLeft(remaining, `\"`)
		end := 0
		for end < len(remaining) && remaining[end] >= '0' && remaining[end] <= '9' {
			end++
		}
		if end != len(authority.VDate) || remaining[:end] != authority.VDate {
			return fmt.Errorf("build log VDATE does not match authority")
		}
		seen = true
		remaining = remaining[end:]
	}
	if !seen {
		return fmt.Errorf("build log has no VDATE binding")
	}
	return nil
}
