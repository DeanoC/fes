package corepackage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchDirectoryToValidatedArchive(t *testing.T) {
	for _, version := range []int{2, 3, 4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			fixture := filepath.Join("testdata", fmt.Sprintf("core-bundle-v%d", version))
			files := map[string]string{"manifest.toml": "manifests/valid-basic.toml", "core.rbf": "payloads/fes-fixture.rbf"}
			if version == 3 || version == 4 {
				files["rom-map.json"] = "maps/valid-basic.json"
			}
			if version == 5 {
				files["core.rbf"] = "payloads/base.rbf"
				files["persona.rbf"] = "payloads/persona.rbf"
			}
			dir := t.TempDir()
			for name, source := range files {
				data, err := os.ReadFile(filepath.Join(fixture, source))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// This is the validation the durable store performs before matching.
			expected, err := InspectPackage(dir)
			if err != nil {
				t.Fatal(err)
			}
			archive, err := CanonicalArchive(dir)
			if err != nil {
				t.Fatal(err)
			}
			archivePath := filepath.Join(t.TempDir(), "package.fcore")
			if err := os.WriteFile(archivePath, archive, 0600); err != nil {
				t.Fatal(err)
			}
			verified, err := InspectPackage(archivePath)
			if err != nil || verified.PackageID != expected.PackageID {
				t.Fatalf("archive inspection=%+v, %v", verified, err)
			}
			if err := MatchDirectoryToArchive(context.Background(), dir, archive); err != nil {
				t.Fatal(err)
			}
			for name := range files {
				t.Run("changed-"+name, func(t *testing.T) {
					path := filepath.Join(dir, name)
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					changed := bytes.Clone(data)
					changed[len(changed)/2] ^= 1
					if err := os.WriteFile(path, changed, 0600); err != nil {
						t.Fatal(err)
					}
					if err := MatchDirectoryToArchive(context.Background(), dir, archive); err == nil {
						t.Fatal("changed member accepted")
					}
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				})
			}
			extra := filepath.Join(dir, "unexpected")
			if err := os.WriteFile(extra, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := MatchDirectoryToArchive(context.Background(), dir, archive); err == nil {
				t.Fatal("extra entry accepted")
			}
			if err := os.Remove(extra); err != nil {
				t.Fatal(err)
			}
			member := filepath.Join(dir, "core.rbf")
			original, err := os.ReadFile(member)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(member); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(fixture, files["core.rbf"]), member); err != nil {
				t.Fatal(err)
			}
			if err := MatchDirectoryToArchive(context.Background(), dir, archive); err == nil {
				t.Fatal("symlink member accepted")
			}
			if err := os.Remove(member); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(member, original, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := MatchDirectoryToArchive(ctx, dir, archive); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled comparison: %v", err)
			}
			if err := MatchDirectoryToArchive(context.Background(), dir, archive[:len(archive)-1]); err == nil {
				t.Fatal("truncated archive accepted")
			}
		})
	}
}

type cancelDuringComparison struct {
	context.Context
	cancel context.CancelFunc
	calls  int
}

func (c *cancelDuringComparison) Err() error {
	c.calls++
	if c.calls == 5 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestMatchDirectoryStopsDuringMemberRead(t *testing.T) {
	manifest, err := os.ReadFile("testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0x5a}, 256<<10)
	dir := t.TempDir()
	for name, data := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := writeCanonicalMembers([]canonicalMember{{name: "manifest.toml", data: manifest}, {name: "core.rbf", data: payload}}, MaxArchiveSize)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &cancelDuringComparison{Context: ctx, cancel: cancel}
	if err := MatchDirectoryToArchive(probe, dir, archive); !errors.Is(err, context.Canceled) || probe.calls != 5 {
		t.Fatalf("in-flight cancellation: calls=%d, err=%v", probe.calls, err)
	}
}
