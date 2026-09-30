package localcores

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/pelletier/go-toml/v2"
)

const (
	maxSelectionBytes = 16 << 10
	maxManifestBytes  = 64 << 10
	maxPayloadBytes   = 32 << 20
)

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

type manifestFile struct {
	Core struct {
		ID   string `toml:"id"`
		Name string `toml:"name"`
	} `toml:"core"`
	Payload struct {
		File   string `toml:"file"`
		Size   int64  `toml:"size"`
		SHA256 string `toml:"sha256"`
	} `toml:"payload"`
	ABI struct {
		ID    string `toml:"id"`
		Major int64  `toml:"major"`
	} `toml:"abi"`
	Interfaces []struct {
		ID       string `toml:"id"`
		Required bool   `toml:"required"`
	} `toml:"interfaces"`
	ROM *struct {
		Role   string `toml:"role"`
		File   string `toml:"file"`
		Size   int64  `toml:"size"`
		SHA256 string `toml:"sha256"`
	} `toml:"rom"`
	ROMs []struct {
		Role string `toml:"role"`
	} `toml:"roms"`
	ROMMap *struct {
		File   string `toml:"file"`
		Size   int64  `toml:"size"`
		SHA256 string `toml:"sha256"`
	} `toml:"rom_map"`
}

type selectionFile struct {
	CoreID        string `toml:"core_id"`
	PackageID     string `toml:"package_id"`
	PayloadSHA256 string `toml:"payload_sha256"`
	InstallPath   string `toml:"install_path"`
}

// ReadInstalledCores lists selections under selectionDir whose packages live
// in packageRoot. fes.menu is excluded. A payload SHA that does not match
// the installed core.rbf and the manifest is dropped. Needs comes from the
// core id plus manifest metadata: Pong needs nothing; ZX81 needs firmware
// because this socket has no ROM link and does not consult rom-map.json;
// Coleco, SMS and SG-1000 need a cartridge; C64 and Spectrum need firmware
// unless the manifest does not declare any. Anything else is firmware and
// not launchable.
func ReadInstalledCores(selectionDir, packageRoot string) []Core {
	cores := []Core{}
	if !realDir(selectionDir) || !realDir(packageRoot) {
		return cores
	}
	_, installed := kitcontent.ReadInstalledPackages([]string{packageRoot})
	known := map[string]struct{}{}
	for _, id := range installed {
		known[id] = struct{}{}
	}
	entries, err := os.ReadDir(selectionDir)
	if err != nil {
		return cores
	}
	seen := map[string]struct{}{}
	packageRoot = filepath.Clean(packageRoot)
	for _, entry := range entries {
		name := entry.Name()
		coreID, ok := selectionCoreID(name)
		if !ok || coreID == "fes.menu" {
			continue
		}
		path := filepath.Join(selectionDir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		selection, ok := readSelection(path)
		if !ok || selection.CoreID != coreID || !packageID(selection.PackageID) || !sha256Hex(selection.PayloadSHA256) {
			continue
		}
		if _, dup := seen[selection.PackageID]; dup {
			continue
		}
		install := filepath.Join(packageRoot, selection.PackageID)
		if filepath.Clean(selection.InstallPath) != install {
			continue
		}
		if _, ok := known[selection.PackageID]; !ok {
			continue
		}
		dirInfo, err := os.Lstat(install)
		if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		manifest, ok := readManifest(filepath.Join(install, "manifest.toml"))
		if !ok || manifest.Core.ID != coreID || manifest.Core.Name == "" || manifest.ABI.ID == "" {
			continue
		}
		if manifest.Payload.File != "core.rbf" || !sha256Hex(manifest.Payload.SHA256) {
			continue
		}
		sum, size, err := sha256File(filepath.Join(install, "core.rbf"), maxPayloadBytes)
		if err != nil || sum != selection.PayloadSHA256 || sum != manifest.Payload.SHA256 {
			continue
		}
		if manifest.Payload.Size != 0 && manifest.Payload.Size != size {
			continue
		}
		needs, block, launchable := deriveNeeds(coreID, !firmwareDeclared(manifest))
		seen[selection.PackageID] = struct{}{}
		cores = append(cores, Core{
			CoreID:      coreID,
			PackageID:   selection.PackageID,
			Name:        manifest.Core.Name,
			ABI:         manifest.ABI.ID,
			Needs:       needs,
			Launchable:  launchable,
			Block:       block,
			installPath: install,
		})
	}
	sort.Slice(cores, func(i, j int) bool {
		if cores[i].CoreID == cores[j].CoreID {
			return cores[i].PackageID < cores[j].PackageID
		}
		return cores[i].CoreID < cores[j].CoreID
	})
	return cores
}

func deriveNeeds(coreID string, bootsWithoutFirmware bool) (needs, block string, launchable bool) {
	switch coreID {
	case "fes.pong":
		return "none", "", true
	case "fes.zx81":
		// No kit-local ROM link, so ZX81 is not launchable.
		return "firmware", "Needs firmware", false
	case "fes.coleco", "fes.sms", "fes.sg1000":
		return "media", "Needs a cartridge", false
	case "fes.c64", "fes.spectrum":
		if bootsWithoutFirmware {
			return "none", "", true
		}
		return "firmware", "Needs firmware", false
	default:
		return "firmware", "Needs firmware", false
	}
}

func firmwareDeclared(m manifestFile) bool {
	if m.ROM != nil && m.ROM.Role == "firmware" {
		return true
	}
	for _, rom := range m.ROMs {
		if rom.Role == "firmware" {
			return true
		}
	}
	for _, iface := range m.Interfaces {
		if iface.Required && strings.HasPrefix(iface.ID, "fes.firmware") {
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
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		default:
			return "", false
		}
	}
	return "fes." + stem, true
}

func packageID(id string) bool {
	return sha256Hex(id)
}

func sha256Hex(value string) bool {
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

func readManifest(path string) (manifestFile, bool) {
	var manifest manifestFile
	data, err := readLimited(path, maxManifestBytes)
	if err != nil {
		return manifest, false
	}
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return manifest, false
	}
	return manifest, true
}

func readLimited(path string, max int64) ([]byte, error) {
	sum, data, err := readRegular(path, max)
	if err != nil {
		return nil, err
	}
	_ = sum
	return data, nil
}

func readRegular(path string, max int64) (string, []byte, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil, errors.New("not a regular file")
	}
	if info.Size() < 1 || info.Size() > max {
		return "", nil, errors.New("size")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(data)) != info.Size() {
		return "", nil, errors.New("read")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), data, nil
}

func sha256File(path string, max int64) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, errors.New("not a regular file")
	}
	if info.Size() < 1 || info.Size() > max {
		return "", 0, errors.New("size")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, max+1))
	if err != nil || n != info.Size() {
		return "", 0, errors.New("read")
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}
