//go:build !tamago

// SPDX-License-Identifier: GPL-3.0-or-later
// This host adapter derives content receipts with the shared retained linker.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast/corepackage"
)

func run() error {
	if len(os.Args) != 4 {
		return errors.New("usage: nano-rom-selection PACKAGE_ARCHIVE FIRMWARE_ROM EXPECTED_PACKAGE_ID")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 8<<20 {
		f.Close()
		return errors.New("bounded regular Nano archive required")
	}
	a, err := corepackage.RetainArchive(context.Background(), info.Size(), f)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	i := a.Inspection()
	d := i.Descriptor
	if i.PackageID != os.Args[3] || d.Format != 3 || d.Target.Platform != "de25_nano" ||
		d.Target.Device != "A5EB013BB23BE4SCS" || d.ROM == nil || d.ROM.Role != "firmware" ||
		(d.ROM.SourceSize != 4096 && d.ROM.SourceSize != 8192) {
		return errors.New("expected Nano held-ROM package required")
	}
	f, err = os.Open(os.Args[2])
	if err != nil {
		return err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != d.ROM.SourceSize {
		return errors.New("exact bounded firmware image required")
	}
	data, err := io.ReadAll(io.LimitReader(f, d.ROM.SourceSize+1))
	if err != nil || int64(len(data)) != d.ROM.SourceSize {
		return errors.New("firmware image changed while reading")
	}
	linked, err := a.LinkROMs(context.Background(), []corepackage.ROMSource{{ID: d.ROM.ID, Role: "firmware", Data: data}})
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	return json.NewEncoder(os.Stdout).Encode(struct {
		PackageID        string `json:"package_id"`
		SourceID         string `json:"source_id"`
		SourceSize       int    `json:"source_size"`
		SourceSHA256     string `json:"source_sha256"`
		ROMReceiptSHA256 string `json:"rom_receipt_sha256"`
	}{i.PackageID, d.ROM.ID, len(data), hex.EncodeToString(sum[:]), linked.ReceiptSHA256()})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
