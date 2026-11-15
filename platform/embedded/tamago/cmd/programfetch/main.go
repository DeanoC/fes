//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

func run() error {
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
	raw, err := volume.ReadFile("FES/BOOT.JSON")
	if err != nil {
		return err
	}
	var cfg struct {
		Source string `json:"program_source"`
		ID     string `json:"program_id"`
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	iface, err := board.OpenNetwork(ctx)
	if err != nil {
		return err
	}
	source, err := nativefabric.NewProgramSource(&http.Client{Timeout: 100 * time.Second}, cfg.Source, "")
	if err != nil {
		return err
	}
	entry, content, err := source.Fetch(ctx, cfg.ID)
	if err != nil {
		return err
	}
	if err = iface.Err(); err != nil {
		return err
	}
	inspection := content.Selection().Inspection()
	result := struct {
		ID               string `json:"id"`
		PackageID        string `json:"package_id"`
		BuildID          string `json:"build_id"`
		MetadataSHA256   string `json:"metadata_sha256"`
		ROMReceiptSHA256 string `json:"rom_receipt_sha256"`
		TapeSHA256       string `json:"tape_sha256"`
	}{entry.ID, inspection.PackageID, inspection.Descriptor.Build.ID, entry.Request.MetadataSHA256, entry.Request.ReceiptSHA256, entry.Request.Tape.SHA256}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Printf("FES_PROGRAM_DOWNLOAD PASS %s\n", data)
	fmt.Printf("FES_PROGRAM_DOWNLOAD_SCOPE content_only=1 platform_admission=0 storage_writes=0 hardware=0 stats=%s\n", iface.Stats())
	return nil
}
func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
