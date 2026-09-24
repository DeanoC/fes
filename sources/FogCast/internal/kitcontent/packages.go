package kitcontent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/pelletier/go-toml/v2"
)

// describedPackageID is a described core-package id: 64 lowercase hex
// digits. An empty list of these is not eligibility.
var describedPackageID = regexp.MustCompile(`^[0-9a-f]{64}$`)

const packageManifestLimit = 1 << 20

type packageABIFile struct {
	ABI struct {
		ID    string `toml:"id"`
		Major int64  `toml:"major"`
	} `toml:"abi"`
}

// ReadInstalledPackages reads [abi] from each described package directory.
// Unreadable roots, symlinks, and manifests without an ABI major of at
// least 1 are skipped. Package ids and ABI id+major pairs are deduplicated.
// Empty or missing roots return nil, nil.
func ReadInstalledPackages(roots []string) ([]meshcontent.EligibleABI, []string) {
	seenPkg := map[string]struct{}{}
	seenABI := map[string]struct{}{}
	var packages []string
	var abis []meshcontent.EligibleABI
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !describedPackageID.MatchString(name) {
				continue
			}
			dir := filepath.Join(root, name)
			info, err := os.Lstat(dir)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			abi, ok := readPackageABI(filepath.Join(dir, "manifest.toml"))
			if !ok {
				continue
			}
			if _, ok := seenPkg[name]; !ok {
				seenPkg[name] = struct{}{}
				packages = append(packages, name)
			}
			key := abi.ID + "\x00" + strconv.Itoa(abi.Major)
			if _, ok := seenABI[key]; !ok {
				seenABI[key] = struct{}{}
				abis = append(abis, abi)
			}
		}
	}
	sort.Strings(packages)
	sort.Slice(abis, func(i, j int) bool {
		if abis[i].ID == abis[j].ID {
			return abis[i].Major < abis[j].Major
		}
		return abis[i].ID < abis[j].ID
	})
	return abis, packages
}

func readPackageABI(path string) (meshcontent.EligibleABI, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > packageManifestLimit {
		return meshcontent.EligibleABI{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return meshcontent.EligibleABI{}, false
	}
	defer file.Close()
	var raw packageABIFile
	decoder := toml.NewDecoder(file)
	if err := decoder.Decode(&raw); err != nil {
		return meshcontent.EligibleABI{}, false
	}
	if raw.ABI.ID == "" || len(raw.ABI.ID) > 128 || raw.ABI.Major < 1 || raw.ABI.Major > 65535 {
		return meshcontent.EligibleABI{}, false
	}
	for _, c := range raw.ABI.ID {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return meshcontent.EligibleABI{}, false
		}
	}
	if raw.ABI.ID[0] < 'a' || raw.ABI.ID[0] > 'z' {
		return meshcontent.EligibleABI{}, false
	}
	return meshcontent.EligibleABI{ID: raw.ABI.ID, Major: int(raw.ABI.Major)}, true
}

func normalizePackageIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !describedPackageID.MatchString(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
