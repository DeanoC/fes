//go:build !tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/modelbackend"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
	"github.com/DeanoC/misteross/expansion"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func syntheticMap(base []byte, points []uint32) expansion.ROMMap {
	blocks := make([]expansion.ROMBlock, len(points)/(256*40))
	for i := range blocks {
		words := make([][]uint32, 256)
		for w := range words {
			words[w] = append([]uint32(nil), points[(i*256+w)*40:(i*256+w+1)*40]...)
		}
		blocks[i] = expansion.ROMBlock{BEL: fmt.Sprintf("synthetic-%d", i), SourceOffset: i * 1024, WordBits: words}
	}
	return expansion.ROMMap{Format: 1, Device: expansion.Device, Encoding: "m10k-1024x10-v1", BaseSHA256: digest(base), SourceSize: len(blocks) * 1024, Blocks: blocks}
}
func addComposition(dir, oracle string, body []byte, original *corepackage.Archive, config *modelbackend.Config) error {
	ctx := context.Background()
	base, err := io.ReadAll(original.Payload())
	if err != nil {
		return err
	}
	reader := tar.NewReader(bytes.NewReader(body))
	var manifest []byte
	for {
		header, e := reader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if header.Name == "manifest.toml" {
			manifest, e = io.ReadAll(reader)
			if e != nil {
				return e
			}
			break
		}
	}
	cut := strings.Index(string(manifest), "\n[rom]")
	if cut < 0 {
		return fmt.Errorf("missing source ROM fixture declaration")
	}
	text := strings.ReplaceAll(string(manifest[:cut]), "format = 3", "format = 4")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "fes.pong", "fes.coleco"), "fes.simple-game", "fes.application")
	destinations, err := expansion.BlankROMDestinations(base, 24*256*40, 124, 32, 3906, 3442)
	if err != nil {
		return err
	}
	mapping := syntheticMap(base, destinations)
	mapBytes, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	text += fmt.Sprintf("\n[[roms]]\nid = \"bios\"\nrole = \"firmware\"\nsource_size = 8192\nsource_offset = 0\n\n[[roms]]\nid = \"cart\"\nrole = \"cartridge\"\nsource_size = 16384\nsource_offset = 8192\n\n[rom_map]\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%s\"\n\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n", len(mapBytes), digest(mapBytes))
	private := filepath.Join(filepath.Dir(dir), "composition-package")
	if err = os.Mkdir(private, 0700); err != nil {
		return err
	}
	for name, data := range map[string][]byte{"manifest.toml": []byte(text), "core.rbf": base, "rom-map.json": mapBytes} {
		if err = os.WriteFile(filepath.Join(private, name), data, 0600); err != nil {
			return err
		}
	}
	pkg, err := corepackage.CanonicalArchive(private)
	if err != nil {
		return err
	}
	archive, err := corepackage.RetainArchive(ctx, int64(len(pkg)), bytes.NewReader(pkg))
	if err != nil {
		return err
	}
	inspection := archive.Inspection()
	result, err := exec.Command(oracle, private, inspection.PackageID).Output()
	if err != nil {
		return err
	}
	var decision runtimeadmission.Decision
	if err = json.Unmarshal(result, &decision); err != nil {
		return err
	}
	if decision.Code != "none" || decision.PackageID != inspection.PackageID {
		return fmt.Errorf("composition fixture rejected by runtime oracle: %s", result)
	}
	policyPath := filepath.Join(dir, "RUNTIME.JSON")
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		return err
	}
	var policy runtimeadmission.Policy
	if err = json.Unmarshal(raw, &policy); err != nil {
		return err
	}
	policy.Decisions = append(policy.Decisions, decision)
	raw, err = json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(policyPath, append(raw, '\n'), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "PARTS.FCP"), pkg, 0600); err != nil {
		return err
	}
	// These coordinates describe only the frozen Cyclone V synthetic fixture.
	// Existing admission remains authoritative for both physical socket fences.
	const width = 7605
	blanks, err := expansion.BlankROMDestinations(base, 400000, 0, 0, 0, 0)
	if err != nil {
		return err
	}
	cpuBits, videoBits := []uint32{}, []uint32{}
	for _, point := range blanks {
		x, y := int(point)%width, int(point)/width
		if x < 1769 || x >= 2806 {
			continue
		}
		if y >= 32 && y < 1800 && len(cpuBits) < 256*40 {
			cpuBits = append(cpuBits, point)
		}
		if y >= 1800 && y < 3442 && len(videoBits) < 256*40 {
			videoBits = append(videoBits, point)
		}
		if len(cpuBits) == 256*40 && len(videoBits) == 256*40 {
			break
		}
	}
	if len(cpuBits) != 256*40 || len(videoBits) != 256*40 {
		return fmt.Errorf("synthetic part lanes missing")
	}
	makePart := func(name, slot, socket string, major int, bits []uint32, pattern byte) (expansion.Asset, modelbackend.Part, []byte, error) {
		cart, e := expansion.LinkROM(ctx, base, syntheticMap(base, bits), bytes.Repeat([]byte{pattern}, 1024))
		if e != nil {
			return expansion.Asset{}, modelbackend.Part{}, nil, e
		}
		asset, e := expansion.NewAsset(expansion.Manifest{Format: 1, Device: expansion.Device, Map: socket, Slot: slot, SlotMajor: major, ShellPackageID: inspection.PackageID, ShellBuildID: inspection.Descriptor.Build.ID, ShellSHA256: digest(base), CartSHA256: digest(cart), CartSize: int64(len(cart)), RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40)}, cart)
		if e != nil {
			return asset, modelbackend.Part{}, nil, e
		}
		var output bytes.Buffer
		if e = asset.Write(&output); e != nil {
			return asset, modelbackend.Part{}, nil, e
		}
		data := output.Bytes()
		if e = os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
			return asset, modelbackend.Part{}, nil, e
		}
		return asset, modelbackend.Part{ID: asset.ID, Path: "FES/" + name, SHA256: digest(data)}, data, nil
	}
	video, videoPart, videoRaw, err := makePart("VIDEO.FEX", expansion.VideoSlot, expansion.ColecoVideoMap, 1, videoBits, 0xaa)
	if err != nil {
		return err
	}
	cpu, cpuPart, cpuRaw, err := makePart("CPU.FEX", expansion.ColecoSlot, expansion.ColecoMapV2, 2, cpuBits, 0x55)
	if err != nil {
		return err
	}
	bios, cart := bytes.Repeat([]byte{0x31}, 8192), bytes.Repeat([]byte{0x55, 0xaa}, 8192)
	for name, data := range map[string][]byte{"BIOS.BIN": bios, "CART.BIN": cart} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	sources := []corepackage.ROMSource{{ID: "bios", Role: "firmware", Data: bios}, {ID: "cart", Role: "cartridge", Data: cart}}
	linked, err := archive.LinkROMs(ctx, sources, corepackage.PartInput{ID: cpu.ID, Archive: cpuRaw}, corepackage.PartInput{ID: video.ID, Archive: videoRaw})
	if err != nil {
		return err
	}
	receipt := linked.Receipt()
	overlay, err := io.ReadAll(linked.Overlay())
	if err != nil {
		return err
	}
	programmed, err := io.ReadAll(linked.Programmed())
	if err != nil {
		return err
	}
	if receipt.PartsComposition == nil || len(receipt.PartsComposition.Parts) != 2 || digest(overlay) == digest(base) || digest(programmed) == digest(overlay) {
		return fmt.Errorf("synthetic parts did not independently change overlay and programmed bytes")
	}
	entry := modelbackend.Entry{CompositionID: receipt.PartsComposition.ID, ID: "model-composed", Title: "H - Composed CPU and video", Check: "return", Path: "FES/PARTS.FCP", PackageID: inspection.PackageID, ReceiptSHA256: linked.ReceiptSHA256(), Parts: []modelbackend.Part{cpuPart, videoPart}, Sources: []modelbackend.Source{{ID: "bios", Role: "firmware", Path: "FES/BIOS.BIN", SHA256: digest(bios)}, {ID: "cart", Role: "cartridge", Path: "FES/CART.BIN", SHA256: digest(cart)}}}
	config.Entries = append(config.Entries, entry)
	changedManifest := video.Manifest
	changedManifest.RecipeSHA256 = strings.Repeat("f", 64)
	changedAsset, err := expansion.NewAsset(changedManifest, video.Cart)
	if err != nil {
		return err
	}
	var changedBytes bytes.Buffer
	if err = changedAsset.Write(&changedBytes); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "CHANGED.FEX"), changedBytes.Bytes(), 0600); err != nil {
		return err
	}
	bad := entry
	bad.ID = "model-part-changed"
	bad.Title = "I - Changed video part"
	bad.Check = "preserve"
	bad.Parts = append([]modelbackend.Part(nil), entry.Parts...)
	bad.Parts[1] = modelbackend.Part{ID: changedAsset.ID, Path: "FES/CHANGED.FEX", SHA256: digest(changedBytes.Bytes())}
	config.Entries = append(config.Entries, bad)
	bad = entry
	bad.ID = "model-part-missing"
	bad.Title = "J - Missing video part"
	bad.Check = "preserve"
	bad.Parts = append([]modelbackend.Part(nil), entry.Parts[:1]...)
	config.Entries = append(config.Entries, bad)
	incompatible := video.Manifest
	incompatible.ShellPackageID = strings.Repeat("e", 64)
	other, err := expansion.NewAsset(incompatible, video.Cart)
	if err != nil {
		return err
	}
	var otherBytes bytes.Buffer
	if err = other.Write(&otherBytes); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "OTHER.FEX"), otherBytes.Bytes(), 0600); err != nil {
		return err
	}
	bad = entry
	bad.ID = "model-part-incompatible"
	bad.Title = "K - Incompatible video part"
	bad.Check = "preserve"
	bad.Parts = append([]modelbackend.Part(nil), entry.Parts...)
	bad.Parts[1] = modelbackend.Part{ID: other.ID, Path: "FES/OTHER.FEX", SHA256: digest(otherBytes.Bytes())}
	config.Entries = append(config.Entries, bad)
	report := map[string]any{"receipt": receipt, "receipt_sha256": linked.ReceiptSHA256(), "package_id": inspection.PackageID, "overlay_sha256": digest(overlay), "programmed_sha256": digest(programmed), "parts": entry.Parts}
	raw, err = json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "COMPOSE.JSON"), append(raw, '\n'), 0600)
}
