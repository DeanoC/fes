// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestCompatibilityRetainedIdentityAndPlatformCopy(t *testing.T) {
	a := fixture(t, nil)
	id := a.Inspection().PackageID
	c, err := NewDE10Compatibility()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.CheckArchive(a, id); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []*Compatibility{nil, {}} {
		if ErrorCode(invalid.CheckArchive(a, id)) != "backend_unavailable" {
			t.Fatal("unconfigured compatibility checker accepted")
		}
	}
	for _, wrong := range []string{"", strings.Repeat("f", 64)} {
		if ErrorCode(c.CheckArchive(a, wrong)) != "invalid_package" {
			t.Fatal("wrong retained identity accepted")
		}
	}
	if ErrorCode(c.CheckArchive(nil, id)) != "invalid_package" {
		t.Fatal("missing archive accepted")
	}
	profiles := []ProgrammingProfile{{"fes-gp-v1", "fes.simple-game", 1, false}}
	copy, err := NewCompatibility(c.target, profiles)
	if err != nil {
		t.Fatal(err)
	}
	profiles[0].DiagnosticOnly = true
	profiles[0].ABI = "changed"
	metadata := a.Inspection()
	metadata.Descriptor.ABI.ID = "changed"
	if err = copy.CheckArchive(a, id); err != nil {
		t.Fatal("caller mutation changed compatibility", err)
	}
	nano := fixture(t, func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "de10_nano", "de25_nano"), "5CSEBA6U23I7", "A5EB013BB23BE4SCS")
	})
	if ErrorCode(copy.CheckArchive(nano, nano.Inspection().PackageID)) != "unsupported_target" {
		t.Fatal("DE10 registry admitted Nano")
	}
}

func TestCompatibilityRejectsAmbiguousProfileConstruction(t *testing.T) {
	target := Target{"de10_nano", "5CSEBA6U23I7"}
	for _, profiles := range [][]ProgrammingProfile{
		nil, {{"p", "", 1, false}}, {{"p", "a", 0, false}}, {{"p", "a", 1, true}},
		{{"p", "", 0, true}, {"p", "a", 1, false}}, {{"p", "a", 1, false}, {"p", "a", 1, false}},
	} {
		if _, err := NewCompatibility(target, profiles); err == nil {
			t.Fatalf("ambiguous profiles accepted: %+v", profiles)
		}
	}
}

// These exact retained archives and decisions are produced by the C++ runtime
// from valid shared vectors. No caller-supplied decision enters Compatibility.
func TestCompatibilityRuntimeParity(t *testing.T) {
	index := os.Getenv("FES_COMPATIBILITY_CASES")
	if index == "" {
		t.Skip("run runtime/verify_compatibility.py for paired C++ corpus")
	}
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		PackageID string `json:"package_id"`
		Code      string `json:"code"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 200 {
		t.Fatal("paired compatibility corpus is incomplete")
	}
	c, err := NewDE10Compatibility()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range cases {
		t.Run(row.Name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(filepath.Dir(index), row.Path))
			if err != nil {
				t.Fatal(err)
			}
			a, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if got := ErrorCode(c.CheckArchive(a, row.PackageID)); got != row.Code {
				t.Fatalf("native=%s C++=%s", got, row.Code)
			}
		})
	}
}
