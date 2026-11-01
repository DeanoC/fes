//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

// Supplied by the cross-builder from an independently selected package ID.
// No untrusted file on the card grants its own identity or programming rights.
var expectedPackageID string

func payload(archive *corepackage.Archive) (string, int64, error) {
	digest := sha256.New()
	n, err := io.Copy(digest, archive.Payload())
	return hex.EncodeToString(digest.Sum(nil)), n, err
}
func run() error {
	if len(expectedPackageID) != 64 {
		return errors.New("SD package probe requires its source-bound expected package ID")
	}
	fmt.Printf("FES_SD_PACKAGE_PROBE board=%s EL=%d counter=%dHz writes=disabled activation=unavailable\n", board.Name, board.ExceptionLevel(), board.CounterFrequency())
	if _, err := board.InitDevices(); err != nil {
		return err
	}
	disk, err := board.OpenStorage()
	if err != nil {
		return err
	}
	lease, ok := disk.(*sdcontrol.Session)
	if !ok {
		return errors.New("SD package probe lacks firmware storage ownership")
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		return err
	}
	store, err := packagestore.New(volume, lease)
	if err != nil {
		return err
	}
	request := packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: expectedPackageID}
	selection, err := store.Retain(context.Background(), request)
	if err != nil {
		return err
	}
	inspection := selection.Inspection()
	d := inspection.Descriptor
	if d.Target.Platform != "de25_nano" || d.Target.Device != "A5EB013BB23BE4SCS" || d.Format != 2 {
		return errors.New("SD package probe requires a recognized format-2 Nano package")
	}
	digest, size, err := payload(selection.Archive())
	if err != nil {
		return err
	}
	if digest != d.Payload.SHA256 || size != d.Payload.Size {
		return errors.New("retained package payload differs")
	}
	before, valid := lease.LastReceipt()
	if !valid {
		return errors.New("SD package receipt invalid")
	}
	fmt.Printf("FES_NANO_PACKAGE RETAINED package_id=%s payload_sha256=%s payload_bytes=%d build_id=%s owner=%016x generation=%d\n",
		inspection.PackageID, digest, size, d.Build.ID, before.Owner, before.Generation)
	handoff, err := store.Contain(selection)
	if err != nil {
		return err
	}
	retained, err := handoff.Selection()
	if err != nil {
		return err
	}
	got, n, err := payload(retained.Archive())
	if err != nil || got != digest || n != size {
		return errors.New("contained payload changed")
	}
	closed := handoff.Receipt()
	fmt.Printf("FES_NANO_PACKAGE CONTAINED package_id=%s payload_sha256=%s owner=%016x generation=%d\n", inspection.PackageID, got, closed.Owner, closed.Generation)
	if err = store.Resume(); err != nil {
		return err
	}
	if _, err = handoff.Selection(); !errors.Is(err, packagestore.ErrState) {
		return errors.New("old storage handoff survived resume")
	}
	again, err := store.Retain(context.Background(), request)
	if err != nil {
		return err
	}
	got, n, err = payload(again.Archive())
	if err != nil || got != digest || n != size || again.Inspection().PackageID != inspection.PackageID {
		return errors.New("reread package changed")
	}
	resumed, valid := lease.LastReceipt()
	if !valid {
		return errors.New("resumed SD receipt invalid")
	}
	fmt.Printf("FES_NANO_PACKAGE RESUMED package_id=%s payload_sha256=%s owner=%016x generation=%d\n", inspection.PackageID, got, resumed.Owner, resumed.Generation)
	handoff, err = store.Contain(again)
	if err != nil {
		return err
	}
	retained, err = handoff.Selection()
	if err != nil {
		return err
	}
	got, n, err = payload(retained.Archive())
	if err != nil || got != digest || n != size {
		return errors.New("final retained package changed")
	}
	closed = handoff.Receipt()
	fmt.Printf("FES_NANO_PACKAGE PASS read-only package_id=%s payload_sha256=%s payload_bytes=%d owner=%016x generation=%d\n", inspection.PackageID, got, n, closed.Owner, closed.Generation)
	return nil
}
func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
