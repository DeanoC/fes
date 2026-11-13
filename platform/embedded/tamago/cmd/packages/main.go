//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"runtime"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

type fixture struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Valid         bool   `json:"valid"`
	PackageID     string `json:"package_id"`
	PayloadSHA256 string `json:"payload_sha256"`
	PayloadOffset int    `json:"payload_offset"`
	Format        int64  `json:"format"`
}

func retain(volume *fat.Volume, path string) (*corepackage.Archive, error) {
	file, err := volume.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return corepackage.RetainArchive(context.Background(), info.Size(), file)
}

func payloadDigest(archive *corepackage.Archive) (string, error) {
	digest := sha256.New()
	if _, err := io.Copy(digest, archive.Payload()); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func sourceMutation(volume *fat.Volume, test fixture) (err error) {
	original, err := volume.ReadFile(test.Path)
	if err != nil {
		return err
	}
	retained, err := retain(volume, test.Path)
	if err != nil {
		return err
	}
	if test.PayloadOffset < 0 || test.PayloadOffset >= len(original) {
		return fmt.Errorf("invalid mutation offset")
	}
	changed := bytes.Clone(original)
	changed[test.PayloadOffset] ^= 1
	defer func() {
		if restore := volume.WriteFile(test.Path, original); restore != nil {
			err = fmt.Errorf("restore package source: %w", restore)
		}
	}()
	if err = volume.WriteFile(test.Path, changed); err != nil {
		return err
	}
	if _, err = retain(volume, test.Path); err == nil {
		return fmt.Errorf("changed source unexpectedly verified")
	}
	digest, err := payloadDigest(retained)
	if err != nil {
		return err
	}
	if digest != test.PayloadSHA256 {
		return fmt.Errorf("retained payload changed after FAT source mutation")
	}
	metadata := retained.Inspection()
	metadata.Descriptor.Core.ID = "changed"
	if retained.Inspection().PackageID != test.PackageID || retained.Inspection().Descriptor.Core.ID == "changed" {
		return fmt.Errorf("returned metadata changed retained identity")
	}
	fmt.Println("FES_PACKAGE_SOURCE_MUTATION PASS retained_payload=unchanged reread=rejected")
	return nil
}

func run() error {
	fmt.Printf("FES native package inspection EL=%d activation=unavailable\n", board.ExceptionLevel())
	if _, err := board.InitDevices(); err != nil {
		return err
	}
	disk, err := board.OpenStorage()
	if err != nil {
		return err
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		return err
	}
	data, err := volume.ReadFile("FES/CASES.JSON")
	if err != nil {
		return err
	}
	var cases []fixture
	if err = json.Unmarshal(data, &cases); err != nil {
		return err
	}
	if len(cases) == 0 || len(cases) > 1000 {
		return fmt.Errorf("invalid package case count")
	}
	valid := 0
	for _, test := range cases {
		archive, err := retain(volume, test.Path)
		accepted := err == nil
		if accepted != test.Valid {
			return fmt.Errorf("case %s accepted=%t expected=%t error=%v", test.Name, accepted, test.Valid, err)
		}
		result := map[string]any{"name": test.Name, "accepted": accepted, "package_id": ""}
		if accepted {
			inspection := archive.Inspection()
			digest, err := payloadDigest(archive)
			if err != nil {
				return err
			}
			if inspection.PackageID != test.PackageID || inspection.Descriptor.Format != test.Format || digest != test.PayloadSHA256 {
				return fmt.Errorf("case %s identity/payload mismatch", test.Name)
			}
			result["package_id"] = inspection.PackageID
			result["target"] = inspection.Descriptor.Target.Platform
			result["format"] = inspection.Descriptor.Format
			valid++
		}
		encoded, _ := json.Marshal(result)
		fmt.Printf("FES_PACKAGE_CASE %s\n", encoded)
		runtime.GC()
		runtime.Gosched()
	}
	if !cases[0].Valid {
		return fmt.Errorf("mutation test requires first valid package")
	}
	if err = sourceMutation(volume, cases[0]); err != nil {
		return err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	fmt.Printf("FES_NATIVE_PACKAGES PASS cases=%d verified=%d rejected=%d heap_sys=%d activation=unavailable\n", len(cases), valid, len(cases)-valid, memory.HeapSys)
	return nil
}
func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
