// Command stage-a0-promotion-report evaluates the Stage A0 candidate boundary
// without modifying the lock or upgrading candidate evidence. Version 1 is a
// blocked-only candidate report; final promotion requires a later schema.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/promotion"
)

const (
	exitBlocked = 1
	exitUsage   = 2
	usageText   = "usage: stage-a0-promotion-report --lock FILE --policy-dir DIR --materials FILE --comparison FILE --output FILE\n"
)

var policyNames = []string{
	"compile-link.json",
	"elf-dependency.json",
	"generated-input.json",
	"intermediate-path.json",
	"source-set.json",
	"upstream-fork-delta.json",
}

type request struct {
	lock, policyDir, materials, comparison, output string
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	req, ok := parseArgs(args)
	if !ok {
		_, _ = io.WriteString(stderr, usageText)
		return exitUsage
	}
	inputs, err := readInputs(req)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "PROMOTION_REPORT_INPUT_INVALID: %s\n", err)
		return exitUsage
	}
	report := promotion.Evaluate(inputs)
	if err := writeReport(req.output, report); err != nil {
		var failure *promotion.Failure
		if errors.As(err, &failure) {
			_, _ = fmt.Fprintf(stderr, "%s\n", failure)
		} else {
			_, _ = fmt.Fprintf(stderr, "PROMOTION_REPORT_OUTPUT_INVALID: %s\n", err)
		}
		return exitUsage
	}
	_, _ = io.WriteString(stdout, "promotion decision: blocked\n")
	return exitBlocked
}

func parseArgs(args []string) (request, bool) {
	var req request
	seen := make(map[string]bool, 5)
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) || args[i+1] == "" || seen[args[i]] {
			return request{}, false
		}
		key, value := args[i], args[i+1]
		seen[key] = true
		switch key {
		case "--lock":
			req.lock = value
		case "--policy-dir":
			req.policyDir = value
		case "--materials":
			req.materials = value
		case "--comparison":
			req.comparison = value
		case "--output":
			req.output = value
		default:
			return request{}, false
		}
	}
	return req, seen["--lock"] && seen["--policy-dir"] && seen["--materials"] && seen["--comparison"] && seen["--output"]
}

func readInputs(req request) (promotion.Inputs, error) {
	lock, err := os.ReadFile(req.lock)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return promotion.Inputs{}, fmt.Errorf("lock cannot be read")
	}
	materials, err := os.ReadFile(req.materials)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return promotion.Inputs{}, fmt.Errorf("material catalog cannot be read")
	}
	comparison, err := os.ReadFile(req.comparison)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return promotion.Inputs{}, fmt.Errorf("comparison cannot be read")
	}
	policies := make(map[string][]byte, len(policyNames))
	for _, name := range policyNames {
		raw, readErr := os.ReadFile(filepath.Join(req.policyDir, name))
		if readErr == nil {
			policies[name] = raw
		}
	}
	return promotion.Inputs{Lock: lock, Policies: policies, Materials: materials, Comparison: comparison}, nil
}

func writeReport(filename string, report promotion.Report) error {
	raw, err := promotion.EncodeReport(report)
	if err != nil {
		return err
	}
	if filename == "" || !filepath.IsAbs(filename) || filepath.Clean(filename) != filename {
		return fmt.Errorf("output must be a clean absolute path")
	}
	if _, err := os.Lstat(filename); err == nil {
		return fmt.Errorf("output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("output cannot be inspected")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("output parent cannot be created")
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), ".stage-a0-promotion-*")
	if err != nil {
		return fmt.Errorf("output staging file cannot be created")
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("output staging permissions cannot be set")
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("output cannot be written")
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("output staging file cannot be closed")
	}
	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("output cannot be published")
	}
	return nil
}
