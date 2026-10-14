package localcores

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/pelletier/go-toml/v2"
)

const maxSelectionBytes = 16 << 10

// Core is one installed package the kit can show. Name is the manifest
// [core] name, copied verbatim. Block is empty when Launchable is true.
type Core struct {
	CoreID      string `json:"core_id"`
	PackageID   string `json:"package_id"`
	Name        string `json:"name"`
	ABI         string `json:"abi"`
	Needs       string `json:"needs"`
	Launchable  bool   `json:"launchable"`
	Block       string `json:"block,omitempty"`
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
// the authority for supported ABI and interface versions.
func ReadInstalledCores(selectionDir, packageRoot string) []Core {
	cores := []Core{}
	if !realDir(selectionDir) || !realDir(packageRoot) {
		return cores
	}
	entries, err := os.ReadDir(selectionDir)
	if err != nil {
		return cores
	}
	seen := map[string]struct{}{}
	packageRoot = filepath.Clean(packageRoot)
	for _, entry := range entries {
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
		inspection, err := corepackage.InspectPackage(install)
		if err != nil || inspection.PackageID != selection.PackageID || inspection.Descriptor.Core.ID != coreID || inspection.Descriptor.Payload.SHA256 != selection.PayloadSHA256 {
			continue
		}
		seen[selection.PackageID] = struct{}{}
		cores = append(cores, FromInspection(inspection, install))
	}
	sort.Slice(cores, func(i, j int) bool {
		if cores[i].CoreID == cores[j].CoreID {
			return cores[i].PackageID < cores[j].PackageID
		}
		return cores[i].CoreID < cores[j].CoreID
	})
	return cores
}

// FromInspection describes the local launch requirements of a verified package.
// Launchable describes satisfied local metadata requirements, not live runtime
// compatibility. Durable selection and activation use runtime inspection.
func FromInspection(inspection corepackage.Inspection, path string) Core {
	d := inspection.Descriptor
	needs, block, launchable := descriptorNeeds(d)
	return Core{CoreID: d.Core.ID, PackageID: inspection.PackageID, Name: d.Core.Name,
		ABI: d.ABI.ID, Needs: needs, Block: block, Launchable: launchable, installPath: path}
}

func descriptorNeeds(d corepackage.Descriptor) (needs, block string, launchable bool) {
	// Native pixel shells have no built-in output even though the fabric socket
	// itself is an optional interface. Local launches do not compose video parts.
	for _, iface := range d.Interfaces {
		if iface.ID == "fes.fabric.video.native-pixels" {
			return "parts", "Needs a video part", false
		}
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

// computerEmptyStartup reports a fes.computer 1.0 removable-media interface
// whose contract permits an empty unit at startup. Blob and other media
// endpoints are startup inputs this launcher does not deliver.
func computerEmptyStartup(d corepackage.Descriptor, iface corepackage.Interface) bool {
	if !protocol.ComputerABI(d.ABI.ID, d.ABI.Major, d.ABI.Minor) {
		return false
	}
	for _, contract := range []protocol.RuntimeContract{
		protocol.Apple2FloppyInterface(),
		protocol.SpectrumTapeInterface(),
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
