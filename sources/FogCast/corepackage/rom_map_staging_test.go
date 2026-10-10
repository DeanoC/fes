package corepackage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type romStageCancelContext struct {
	context.Context
	bodyDone *bool
	checks   int
}

func (c *romStageCancelContext) Err() error {
	if *c.bodyDone {
		c.checks++
		if c.checks >= 100 {
			return context.Canceled
		}
	}
	return nil
}

func TestROMStageCancelsDuringMapDecode(t *testing.T) {
	_, _, _, archive := romPackageFixture(t)
	root := t.TempDir()
	bodyDone := false
	ctx := &romStageCancelContext{Context: context.Background(), bodyDone: &bodyDone}
	reader := &markAtEOFReader{data: archive, done: &bodyDone}
	staged, err := Stage(ctx, root, int64(len(archive)), reader)
	if err == nil {
		_ = staged.Cleanup()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("map decoding did not propagate staging cancellation: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled map inspection retained a publication: %v %v", entries, err)
	}
}

func TestROMStageOwnsPublishedMembers(t *testing.T) {
	manifest, payload, mapping, archive := romPackageFixture(t)
	staged, err := Stage(context.Background(), t.TempDir(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = staged.Cleanup() })
	clear(archive)
	for name, want := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload, "rom-map.json": mapping} {
		got, err := os.ReadFile(filepath.Join(staged.Directory, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("published %s changed with caller-owned archive: %v", name, err)
		}
	}
}

func TestStageArchiveManifestOwnsItsBackingBytes(t *testing.T) {
	manifest, _, _, archive := romPackageFixture(t)
	borrowedManifest, payload, mapping, err := readArchiveMembers(archive, false)
	if err != nil || len(payload) == 0 || len(mapping) == 0 {
		t.Fatalf("staging archive reader: %v", err)
	}
	// TOML descriptors may outlive inspection. Their small manifest must not
	// share the multi-megabyte payload/map allocation that inspection releases.
	clear(archive)
	if !bytes.Equal(borrowedManifest, manifest) {
		t.Fatal("staging manifest retains caller's archive backing allocation")
	}
}
