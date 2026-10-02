package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func fixtureInputs(t *testing.T, archive bool) ([]string, expansion.PartsShell, expansion.Asset) {
	t.Helper()
	f, err := os.Open("../../testdata/rom/blank.rbf.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	payload, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	shell := expansion.PartsShell{PackageID: strings.Repeat("a", 64), BuildID: strings.Repeat("b", 32),
		Layout: expansion.ColecoVideoLayout, Payload: payload}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: digest, CartSize: int64(len(payload)),
		Device: expansion.Device, Format: 1, Map: expansion.ColecoVideoMap, RecipeSHA256: strings.Repeat("c", 64),
		Revision: strings.Repeat("d", 40), ShellBuildID: shell.BuildID, ShellPackageID: shell.PackageID,
		ShellSHA256: digest, Slot: expansion.VideoSlot, SlotMajor: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	shellPath := filepath.Join(dir, "shell.rbf")
	if err := os.WriteFile(shellPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	partPath := filepath.Join(dir, "video")
	if archive {
		var b bytes.Buffer
		if err := asset.Write(&b); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(partPath, b.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.Mkdir(partPath, 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"manifest.json": asset.ManifestBytes, "cart.rbf": asset.Cart} {
			if err := os.WriteFile(filepath.Join(partPath, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return []string{"-shell", shellPath, "-package-id", shell.PackageID, "-build-id", shell.BuildID,
		"-video", partPath, "-output", filepath.Join(dir, "output.rbf")}, shell, asset
}

func TestRunLinksArchiveAndDirectory(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(fmt.Sprintf("archive=%t", archive), func(t *testing.T) {
			args, shell, asset := fixtureInputs(t, archive)
			var stdout bytes.Buffer
			if err := run(context.Background(), args, &stdout); err != nil {
				t.Fatal(err)
			}
			_, expected, err := expansion.ComposePartsContext(context.Background(), shell, []expansion.Asset{asset})
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(args[9])
			if err != nil || !bytes.Equal(got, expected) {
				t.Fatal("CLI output differs from validated composition")
			}
			var evidence struct {
				Composition expansion.PartsComposition `json:"composition"`
				Programmed  string                     `json:"programmed_sha256"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &evidence); err != nil || evidence.Composition.ID == "" ||
				evidence.Programmed != evidence.Composition.PayloadSHA256 {
				t.Fatalf("bad evidence: %s (%v)", stdout.Bytes(), err)
			}
		})
	}
}

func TestRunFailuresPreserveOutput(t *testing.T) {
	args, _, _ := fixtureInputs(t, false)
	original := []byte("previous output")
	if err := os.WriteFile(args[9], original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(args[7], "unexpected"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), args, io.Discard); err == nil {
		t.Fatal("accepted extra directory member")
	}
	if err := os.Remove(filepath.Join(args[7], "unexpected")); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(args[7], "manifest.json")
	originalManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(originalManifest, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), args, io.Discard); err == nil {
		t.Fatal("accepted noncanonical directory manifest")
	}
	if err := os.WriteFile(manifestPath, originalManifest, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, args, io.Discard); err != context.Canceled {
		t.Fatalf("cancel ignored: %v", err)
	}
	got, err := os.ReadFile(args[9])
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("failure overwrote previous output")
	}
}

func TestRunRejectsMissingOrUnpairedArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"-video", "x"}, {"-shell", "x", "-map", "map"}, {"unexpected"}} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Fatalf("accepted arguments %v", args)
		}
	}
}
