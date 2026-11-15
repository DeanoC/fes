// SPDX-License-Identifier: GPL-3.0-or-later
package admissioncheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

// The native runner constructs this corpus through the actual C++ checker.
// Ordinary unit tests do not manufacture compatibility decisions for this test.
func TestRuntimeOracleCorpus(t *testing.T) {
	dir := os.Getenv("FES_ADMISSION_FIXTURES")
	if dir == "" {
		t.Skip("run with FES_ADMISSION_FIXTURES=<native run>/package-fixtures")
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var policy runtimeadmission.Policy
	if err := json.Unmarshal(read("RUNTIME.JSON"), &policy); err != nil {
		t.Fatal(err)
	}
	var cases []Case
	if err := json.Unmarshal(read("ADMIT.JSON"), &cases); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := Run(policy, cases, func(path string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, filepath.Base(path))) }, func(result Result) { count++ }); err != nil {
		t.Fatal(err)
	}
	if count != len(cases) {
		t.Fatal("missing results")
	}
}

func TestRetainedROMOracleCorpus(t *testing.T) {
	dir := os.Getenv("FES_ADMISSION_FIXTURES")
	if dir == "" {
		t.Skip("run with FES_ADMISSION_FIXTURES=<native run>/package-fixtures")
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var policy runtimeadmission.Policy
	if err := json.Unmarshal(read("RUNTIME.JSON"), &policy); err != nil {
		t.Fatal(err)
	}
	var cases []LinkedCase
	if err := json.Unmarshal(read("LINKS.JSON"), &cases); err != nil {
		t.Fatal(err)
	}
	var native struct {
		LinkedResults []LinkedResult `json:"linked_results"`
	}
	if err := json.Unmarshal(read("../result.json"), &native); err != nil {
		t.Fatal(err)
	}
	var observed []LinkedResult
	err := RunLinked(policy, cases, func(path string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, filepath.Base(path))) }, func(path string, data []byte) error {
		return os.WriteFile(filepath.Join(dir, filepath.Base(path)), data, 0600)
	}, func(result LinkedResult) { observed = append(observed, result) })
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(observed)
	expected, _ := json.Marshal(native.LinkedResults)
	if string(actual) != string(expected) {
		t.Fatalf("host and native receipts differ: %s != %s", actual, expected)
	}
}
