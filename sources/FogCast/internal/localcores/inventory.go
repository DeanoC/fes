package localcores

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/pelletier/go-toml/v2"
)

const maxSelectionBytes = 16 << 10

const (
	unavailableManifestName = "unavailable-cores.json"
	hardwareUnavailable     = "Hardware unavailable"
	maxUnavailableBytes     = 64 << 10
)

// Core is one installed package the kit can show. Name is the manifest
// [core] name, copied verbatim. Block is empty when Launchable is true.
type Core struct {
	CoreID      string   `json:"core_id"`
	PackageID   string   `json:"package_id"`
	Name        string   `json:"name"`
	ABI         string   `json:"abi"`
	Needs       string   `json:"needs"`
	Launchable  bool     `json:"launchable"`
	Block       string   `json:"block,omitempty"`
	Inputs      []string `json:"inputs,omitempty"`
	installPath string
}

type selectionFile struct {
	CoreID        string `toml:"core_id"`
	PackageID     string `toml:"package_id"`
	PayloadSHA256 string `toml:"payload_sha256"`
	InstallPath   string `toml:"install_path"`
}

// ReadInstalledCores lists factory selections whose complete installed package
// identity and payload digest match the selection. fes.menu is idle firmware.
// Metadata describes what the local launch path needs; runtime inspection remains
// the authority for supported ABI and interface versions. Payload and ROM-map
// bytes are streamed into those digests and are not retained.
func ReadInstalledCores(selectionDir, packageRoot string) []Core {
	cores, err := readInstalledCores(context.Background(), selectionDir, packageRoot)
	if err != nil || cores == nil {
		return []Core{}
	}
	return cores
}

func readInstalledCores(ctx context.Context, selectionDir, packageRoot string) ([]Core, error) {
	cores := []Core{}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !realDir(selectionDir) || !realDir(packageRoot) {
		return cores, nil
	}
	entries, err := os.ReadDir(selectionDir)
	if err != nil {
		return cores, nil
	}
	seen := map[string]struct{}{}
	packageRoot = filepath.Clean(packageRoot)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		coreID, ok := selectionCoreID(entry.Name())
		if !ok || coreID == "fes.menu" {
			continue
		}
		selection, ok := readSelection(filepath.Join(selectionDir, entry.Name()))
		if !ok || selection.CoreID != coreID || !SHA256Hex(selection.PackageID) || !SHA256Hex(selection.PayloadSHA256) {
			continue
		}
		if _, duplicate := seen[selection.PackageID]; duplicate {
			continue
		}
		install := filepath.Join(packageRoot, selection.PackageID)
		if filepath.Clean(selection.InstallPath) != install || !realDir(install) {
			continue
		}
		inspection, err := corepackage.InspectListing(ctx, install)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if inspection.PackageID != selection.PackageID || inspection.Descriptor.Core.ID != coreID || inspection.Descriptor.Payload.SHA256 != selection.PayloadSHA256 {
			continue
		}
		seen[selection.PackageID] = struct{}{}
		cores = append(cores, FromInspection(inspection, install))
	}
	installedIDs := map[string]struct{}{}
	for _, core := range cores {
		installedIDs[core.CoreID] = struct{}{}
	}
	for _, core := range readUnavailableCores(selectionDir) {
		if _, present := installedIDs[core.CoreID]; present {
			continue
		}
		cores = append(cores, core)
	}
	sort.Slice(cores, func(i, j int) bool {
		if cores[i].CoreID == cores[j].CoreID {
			return cores[i].PackageID < cores[j].PackageID
		}
		return cores[i].CoreID < cores[j].CoreID
	})
	return cores, nil
}

// factoryFingerprint is the selection and package metadata that invalidates a
// cached catalog. Every installed directory entry is part of the key, so an
// unexpected member changes the verdict InspectListing would return. The key
// is size, mtime, inode, mode, and directory entry names. It does not read
// core.rbf or rom-map.json. Opening a selection file to resolve its package
// does not change the key, so a durable selection read cannot force a rescan.
// unavailable-cores.json, beside the selections directory, contributes its
// size, mtime, and inode, or the line "unavailable absent".
func factoryFingerprint(selectionDir, packageRoot string) string {
	if !realDir(selectionDir) || !realDir(packageRoot) {
		return "absent"
	}
	entries, err := os.ReadDir(selectionDir)
	if err != nil {
		return "unreadable"
	}
	var b strings.Builder
	packageRoot = filepath.Clean(packageRoot)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			fmt.Fprintf(&b, "sel %s missing\n", entry.Name())
			continue
		}
		fmt.Fprintf(&b, "sel %s %d %d %d\n", entry.Name(), info.Size(), info.ModTime().UnixNano(), statInode(info))
		selection, ok := readSelection(filepath.Join(selectionDir, entry.Name()))
		if !ok {
			continue
		}
		install := filepath.Join(packageRoot, selection.PackageID)
		fmt.Fprintf(&b, "path %s\n", selection.InstallPath)
		dirInfo, err := os.Lstat(install)
		if err != nil {
			fmt.Fprintf(&b, "dir %s missing\n", selection.PackageID)
			continue
		}
		fmt.Fprintf(&b, "dir %s %d %d %d\n", selection.PackageID, dirInfo.ModTime().UnixNano(), dirInfo.Mode(), statInode(dirInfo))
		if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		members, err := os.ReadDir(install)
		if err != nil {
			fmt.Fprintf(&b, "dir %s unreadable\n", selection.PackageID)
			continue
		}
		for _, member := range members {
			meta, err := member.Info()
			if err != nil {
				fmt.Fprintf(&b, "ent %s %s missing\n", selection.PackageID, member.Name())
				continue
			}
			fmt.Fprintf(&b, "ent %s %s %d %d %d %d\n", selection.PackageID, member.Name(), meta.Size(), meta.ModTime().UnixNano(), meta.Mode(), statInode(meta))
		}
	}
	appendUnavailableFingerprint(&b, selectionDir)
	return b.String()
}

func unavailableManifestPath(selectionDir string) string {
	return filepath.Join(filepath.Dir(selectionDir), unavailableManifestName)
}

func appendUnavailableFingerprint(b *strings.Builder, selectionDir string) {
	info, err := os.Lstat(unavailableManifestPath(selectionDir))
	if err != nil {
		fmt.Fprintf(b, "unavailable absent\n")
		return
	}
	fmt.Fprintf(b, "unavailable %d %d %d\n", info.Size(), info.ModTime().UnixNano(), statInode(info))
}

// unavailablePackageID is a stable package id for a core with no installed
// package. The menu can address the tile; launch stays blocked.
func unavailablePackageID(coreID string) string {
	sum := sha256.Sum256(append([]byte("fes-unavailable\x00"), coreID...))
	return hex.EncodeToString(sum[:])
}

func validUnavailableCoreID(coreID string) bool {
	if !strings.HasPrefix(coreID, "fes.") {
		return false
	}
	stem := strings.TrimPrefix(coreID, "fes.")
	if stem == "" || strings.Contains(stem, ".") {
		return false
	}
	for _, c := range stem {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func unavailableDisplayName(name, coreID string) string {
	if name == "" || len(name) > 80 {
		return coreID
	}
	for _, r := range name {
		if r < 32 || r == 127 || !unicode.IsPrint(r) {
			return coreID
		}
	}
	return name
}

type unavailableDocument struct {
	Version int `json:"version"`
	Cores   []struct {
		CoreID string `json:"core_id"`
		Name   string `json:"name"`
		Reason string `json:"reason"`
	} `json:"cores"`
}

// unavailableReasons are the parent omission reasons. A row without one is
// ignored. Launchability is not read from the file.
var unavailableReasons = map[string]struct{}{
	"timing-not-met": {},
	"build-failed":   {},
	"skipped":        {},
}

// readUnavailableCores lists omitted optional cores from the parent manifest.
// A missing, symlink, oversized, or malformed file is ignored so a bad
// manifest cannot blank the installed catalog.
func readUnavailableCores(selectionDir string) []Core {
	path := unavailableManifestPath(selectionDir)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxUnavailableBytes {
		return nil
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxUnavailableBytes+1))
	if err != nil || int64(len(data)) != info.Size() {
		return nil
	}
	var document unavailableDocument
	if json.Unmarshal(data, &document) != nil || document.Version != 1 {
		return nil
	}
	cores := []Core{}
	seen := map[string]struct{}{}
	for _, row := range document.Cores {
		if !validUnavailableCoreID(row.CoreID) {
			continue
		}
		if _, known := unavailableReasons[row.Reason]; !known {
			continue
		}
		if _, duplicate := seen[row.CoreID]; duplicate {
			continue
		}
		seen[row.CoreID] = struct{}{}
		cores = append(cores, Core{
			CoreID:     row.CoreID,
			PackageID:  unavailablePackageID(row.CoreID),
			Name:       unavailableDisplayName(row.Name, row.CoreID),
			Needs:      "unavailable",
			Launchable: false,
			Block:      hardwareUnavailable,
		})
	}
	return cores
}

func statInode(info os.FileInfo) uint64 {
	switch stat := info.Sys().(type) {
	case *syscall.Stat_t:
		if stat != nil {
			return uint64(stat.Ino)
		}
	case syscall.Stat_t:
		return uint64(stat.Ino)
	}
	return 0
}

// FromInspection describes the local launch requirements of a verified package.
// Launchable describes satisfied local metadata requirements, not live runtime
// compatibility. Durable selection and activation use runtime inspection.
func FromInspection(inspection corepackage.Inspection, path string) Core {
	d := inspection.Descriptor
	needs, block, launchable := descriptorNeeds(d)
	return Core{CoreID: d.Core.ID, PackageID: inspection.PackageID, Name: d.Core.Name,
		ABI: d.ABI.ID, Needs: needs, Block: block, Launchable: launchable,
		Inputs: descriptorInputs(d), installPath: path}
}

// descriptorInputs names the input devices a package requires: "keyboard"
// when a physical keyboard is required and "mouse" for a required fes.mouse
// interface. fes.keyboard.hid is a USB keyboard. Exact fes.keyboard 1.0 is
// the simple-computer matrix. That matrix is a typing keyboard when the
// package also requires fes.video.session-display 1.0, the computer plane
// the ZX81 bring-up package declares with it, and the package is not a
// cartridge. Master System, SG-1000 and the earlier ColecoVision matrix
// reuse fes.keyboard as a joystick and do not declare that plane. Pads are
// not listed; a connected pad does not establish how a core maps it.
func descriptorInputs(d corepackage.Descriptor) []string {
	var keyboard, mouse bool
	typing := plainKeyboardTypes(d)
	for _, iface := range d.Interfaces {
		if !iface.Required {
			continue
		}
		switch {
		case iface.ID == "fes.keyboard.hid":
			keyboard = true
		case typing && iface.ID == "fes.keyboard" && iface.Major == 1 && iface.Minor == 0:
			keyboard = true
		case iface.ID == "fes.mouse" || strings.HasPrefix(iface.ID, "fes.mouse."):
			mouse = true
		}
	}
	var inputs []string
	if keyboard {
		inputs = append(inputs, "keyboard")
	}
	if mouse {
		inputs = append(inputs, "mouse")
	}
	return inputs
}

// plainKeyboardTypes reports that exact fes.keyboard 1.0 is the machine's
// typing keyboard rather than a joystick matrix. The ZX81 bring-up package
// (build_fes_zx81.py) requires the in-session computer plane with that
// matrix and has no cartridge. A cartridge ROM keeps the matrix a pad even
// if a later package also declares the plane.
func plainKeyboardTypes(d corepackage.Descriptor) bool {
	if d.ROM != nil && d.ROM.Role == "cartridge" {
		return false
	}
	for _, rom := range d.ROMs {
		if rom.Role == "cartridge" {
			return false
		}
	}
	for _, iface := range d.Interfaces {
		if iface.Required && iface.ID == "fes.video.session-display" && iface.Major == 1 && iface.Minor == 0 {
			return true
		}
	}
	return false
}

func descriptorNeeds(d corepackage.Descriptor) (needs, block string, launchable bool) {
	// Native pixel shells have no built-in output even though the fabric socket
	// itself is an optional interface. Local launches do not compose video parts.
	for _, iface := range d.Interfaces {
		if iface.ID == "fes.fabric.video.native-pixels" {
			return "parts", "Needs a video part", false
		}
	}
	// The format-2 ZX81 package leaves its machine-ROM lanes empty. The host
	// library path splices the machine ROM after package identity is fixed, but
	// the local path only loads the installed archive.
	if d.Format == 2 && d.Core.ID == "fes.zx81" {
		return "firmware", "Needs machine ROM", false
	}
	if d.Format == 4 || len(d.ROMs) != 0 || (d.ROM != nil && d.ROM.Role == "firmware") {
		return "firmware", "Needs firmware", false
	}
	if protocol.RequiresCoreMedia(d) {
		return "unsupported", "Needs host media delivery", false
	}
	for _, iface := range d.Interfaces {
		if !iface.Required {
			continue
		}
		if strings.HasPrefix(iface.ID, "fes.firmware") {
			return "firmware", "Needs firmware", false
		}
		// Removable computer units, and the ST extensions that only widen that
		// empty drive, boot with no image. The local launcher sends the package
		// archive alone, so every other required media interface stays blocked.
		if strings.HasPrefix(iface.ID, "fes.media.") && !computerEmptyStartup(d, iface) {
			return "unsupported", "Needs host media delivery", false
		}
	}
	if d.Format == 3 && d.ROM != nil && d.ROM.Role == "cartridge" {
		return "media", "Needs a cartridge", false
	}
	if d.Format != 2 {
		return "unsupported", "Unsupported local launch", false
	}
	return "none", "", true
}

// computerEmptyStartup reports a supported fes.computer removable-media interface
// whose contract permits an empty unit at startup. Blob and other media
// endpoints are startup inputs this launcher does not deliver.
func computerEmptyStartup(d corepackage.Descriptor, iface corepackage.Interface) bool {
	if !protocol.ComputerMediaABI(d.ABI.ID, d.ABI.Major, d.ABI.Minor) {
		return false
	}
	pcxt := protocol.PcxtFloppyInterface()
	if iface.ID == pcxt.ID && iface.Major == int64(pcxt.Major) && iface.Minor == int64(pcxt.Minor) {
		return d.ABI.Minor >= protocol.ComputerExtendedMinor
	}
	for _, contract := range []protocol.RuntimeContract{
		protocol.Apple2FloppyInterface(),
		protocol.SpectrumTapeInterface(),
		protocol.Zx81TapeInterface(),
		protocol.C64DiskInterface(),
		protocol.AtariStFloppyInterface(),
		protocol.AtariStFloppyWriteInterface(),
		protocol.AtariStFloppyGeometryInterface(),
	} {
		if iface.ID == contract.ID && iface.Major == int64(contract.Major) && iface.Minor == int64(contract.Minor) {
			return true
		}
	}
	return false
}

func selectionCoreID(name string) (string, bool) {
	if !strings.HasPrefix(name, "fes-") || !strings.HasSuffix(name, ".package.toml") {
		return "", false
	}
	stem := strings.TrimSuffix(strings.TrimPrefix(name, "fes-"), ".package.toml")
	if stem == "" || strings.Contains(stem, ".") {
		return "", false
	}
	for _, c := range stem {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return "", false
		}
	}
	return "fes." + stem, true
}

// SHA256Hex reports whether value is a lowercase SHA-256 hex digest.
// Package ids and payload digests share this rule. rooms.ValidPackageID
// calls it so the socket and the room API cannot drift.
func SHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func sha256Hex(value string) bool {
	return SHA256Hex(value)
}

func realDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func readSelection(path string) (selectionFile, bool) {
	var selection selectionFile
	data, err := readLimited(path, maxSelectionBytes)
	if err != nil {
		return selection, false
	}
	if err := toml.Unmarshal(data, &selection); err != nil {
		return selection, false
	}
	return selection, true
}

func readLimited(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > max {
		return nil, errors.New("invalid selection file")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("selection changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(data)) != info.Size() {
		return nil, errors.New("invalid selection size")
	}
	return data, nil
}
