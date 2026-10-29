//go:build !tamago

// SPDX-License-Identifier: GPL-3.0-or-later
// This host-only fixture producer uses the same retained linker as the guest.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/admissioncheck"
	"github.com/DeanoC/fes/platform/embedded/tamago/modelbackend"
)

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: session-fixtures FIXTURE_DIRECTORY ORACLE")
	}
	dir := os.Args[1]
	raw, err := os.ReadFile(filepath.Join(dir, "LINKS.JSON"))
	if err != nil {
		return err
	}
	var cases []admissioncheck.LinkedCase
	if err = json.Unmarshal(raw, &cases); err != nil || len(cases) == 0 {
		return fmt.Errorf("missing link fixture: %v", err)
	}
	selected := cases[0]
	read := func(path string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, filepath.Base(path))) }
	body, err := read(selected.Path)
	if err != nil {
		return err
	}
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		return err
	}
	base := modelbackend.Entry{Check: "return", ID: "model-normal", Title: "A - Model launch", Path: selected.Path, PackageID: selected.PackageID}
	sources := []corepackage.ROMSource{}
	for _, source := range selected.Sources {
		data, err := read(source.Path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		base.Sources = append(base.Sources, modelbackend.Source{ID: source.ID, Role: source.Role, Path: source.Path, SHA256: hex.EncodeToString(sum[:])})
		sources = append(sources, corepackage.ROMSource{ID: source.ID, Role: source.Role, Data: data})
	}
	linked, err := archive.LinkROMs(context.Background(), sources)
	if err != nil {
		return err
	}
	base.ReceiptSHA256 = linked.ReceiptSHA256()
	config := modelbackend.Config{Scope: "qemu-model-only", Entries: []modelbackend.Entry{base}}
	for _, test := range []struct{ id, title, fault string }{
		{"model-wrong-rom", "B - Wrong ROM", "source"},
		{"model-wrong-receipt", "C - Wrong receipt", "receipt"},
		{"model-interrupted", "D - Interrupted preparation", "cancel"},
		{"model-identity", "E - Identity recovery", "identity"},
		{"model-program", "F - Programming recovery", "program"},
		{"model-save", "G - Return retry", "save"},
	} {
		entry := base
		entry.ID, entry.Title = test.id, test.title
		entry.Sources = append([]modelbackend.Source(nil), base.Sources...)
		entry.Check = "preserve"
		if test.fault == "identity" || test.fault == "program" {
			entry.Check = "recover"
		}
		if test.fault == "save" {
			entry.Check = "retry"
		}
		switch test.fault {
		case "source":
			changed := bytes.Clone(sources[0].Data)
			changed[0] ^= 1
			if err = os.WriteFile(filepath.Join(dir, "WRONGROM.BIN"), changed, 0600); err != nil {
				return err
			}
			entry.Sources[0].Path = "FES/WRONGROM.BIN"
		case "receipt":
			entry.ReceiptSHA256 = strings.Repeat("0", 64)
		default:
			entry.Fault = test.fault
		}
		config.Entries = append(config.Entries, entry)
	}
	if err = addComposition(dir, os.Args[2], body, archive, &config); err != nil {
		return err
	}
	raw, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "PLAY.JSON"), append(raw, '\n'), 0600)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
