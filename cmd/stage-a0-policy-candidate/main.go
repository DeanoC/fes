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

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policyobserve"
)

const usage = "usage: stage-a0-policy-candidate --repository DIR --source-material ID --authority FILE --build-log FILE --receipt FILE --output-dir DIR\n"

type request struct {
	repository     string
	sourceMaterial string
	authority      string
	buildLog       string
	receipt        string
	outputDir      string
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
			_, _ = fmt.Fprintf(stderr, "POLICY_OBSERVE_COMMAND_FAILED: %v\n", err)
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
		case "--output-dir":
			req.outputDir = value
		default:
			return request{}, false
		}
	}
	return req, seen["--repository"] && seen["--source-material"] && seen["--authority"] && seen["--build-log"] && seen["--receipt"] && seen["--output-dir"]
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
	documents := map[string]policy.Document{
		"source-set.json":          sourceSet,
		"upstream-fork-delta.json": forkDelta,
		"compile-link.json":        compileLink,
		"generated-input.json":     generated,
		"intermediate-path.json":   intermediate,
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
	for name, document := range documents {
		raw, err := policy.Encode(document)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staging, name), raw, 0o600); err != nil {
			return fmt.Errorf("candidate policy cannot be written: %w", err)
		}
	}
	if err := os.Rename(staging, req.outputDir); err != nil {
		return fmt.Errorf("candidate output cannot be published: %w", err)
	}
	return nil
}
