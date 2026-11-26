// Package corepackage validates and privately stages FES format-2/3/4 core packages.
package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/DeanoC/misteross/expansion"
)

const (
	MaxManifestSize = 65_536
	MaxPayloadSize  = 33_554_432
	MaxArchiveSize  = 65 * 1024 * 1024
	MaxROMMapSize   = expansion.MaxROMMapBytes
)

var (
	identifierRE  = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
	hex32RE       = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex40RE       = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)
	hex64RE       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	publicationRE = regexp.MustCompile(`^([0-9a-f]{64})-([0-9a-f]{32})$`)
	semverRE      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	ipvFutureRE   = regexp.MustCompile(`^v[0-9A-Fa-f]+\.[A-Za-z0-9_.~!$&'()*+,;=:-]+$`)
)

type ROM struct {
	ID         string `toml:"id" json:"id"`
	Role       string `toml:"role" json:"role"`
	SourceSize int64  `toml:"source_size" json:"source_size"`
	File       string `toml:"file" json:"file"`
	Size       int64  `toml:"size" json:"size"`
	SHA256     string `toml:"sha256" json:"sha256"`
}

type ROMRequirement struct {
	ID           string `toml:"id" json:"id"`
	Role         string `toml:"role" json:"role"`
	SourceSize   int64  `toml:"source_size" json:"source_size"`
	SourceOffset int64  `toml:"source_offset" json:"source_offset"`
}

type ROMMapDescriptor struct {
	File   string `toml:"file" json:"file"`
	Size   int64  `toml:"size" json:"size"`
	SHA256 string `toml:"sha256" json:"sha256"`
}

type Descriptor struct {
	StaticExpansion *StaticExpansion  `toml:"static_expansion,omitempty" json:"static_expansion,omitempty"`
	ROM             *ROM              `toml:"rom,omitempty" json:"rom,omitempty"`
	ROMs            []ROMRequirement  `toml:"roms,omitempty" json:"roms,omitempty"`
	ROMMap          *ROMMapDescriptor `toml:"rom_map,omitempty" json:"rom_map,omitempty"`
	Format          int64             `toml:"format" json:"format"`
	Core            Core              `toml:"core" json:"core"`
	Target          Target            `toml:"target" json:"target"`
	Payload         Payload           `toml:"payload" json:"payload"`
	ABI             Contract          `toml:"abi" json:"abi"`
	Interfaces      []Interface       `toml:"interfaces" json:"interfaces"`
	Build           Build             `toml:"build" json:"build"`
}

type Core struct {
	ID          string `toml:"id" json:"id"`
	Name        string `toml:"name" json:"name"`
	Description string `toml:"description" json:"description"`
	Version     string `toml:"version" json:"version"`
	System      string `toml:"system" json:"system,omitempty"`
}

type Target struct {
	Platform           string `toml:"platform" json:"platform"`
	Device             string `toml:"device" json:"device"`
	ProgrammingProfile string `toml:"programming_profile" json:"programming_profile"`
}

type Payload struct {
	File   string `toml:"file" json:"file"`
	Size   int64  `toml:"size" json:"size"`
	SHA256 string `toml:"sha256" json:"sha256"`
}

type Contract struct {
	ID    string `toml:"id" json:"id"`
	Major int64  `toml:"major" json:"major"`
	Minor int64  `toml:"minor" json:"minor"`
}

type Interface struct {
	ID       string `toml:"id" json:"id"`
	Major    int64  `toml:"major" json:"major"`
	Minor    int64  `toml:"minor" json:"minor"`
	Required bool   `toml:"required" json:"required"`
}

type Build struct {
	ID           string `toml:"id" json:"id"`
	Repository   string `toml:"repository" json:"repository"`
	Revision     string `toml:"revision" json:"revision"`
	RecipeSHA256 string `toml:"recipe_sha256" json:"recipe_sha256"`
	Toolchain    string `toml:"toolchain" json:"toolchain"`
}

type Staged struct {
	StaticComposition  *expansion.StaticComposition `json:"static_composition,omitempty"`
	PersonaPath        string                       `json:"persona_path,omitempty"`
	InitialMedia       *StagedInitialMedia          `json:"-"`
	PartsComposition   *expansion.PartsComposition  `json:"parts_composition,omitempty"`
	PartDirectories    []PartDirectory              `json:"parts,omitempty"`
	ROMLink            *ROMLinkIdentity             `json:"rom_link,omitempty"`
	ROMLinks           *ROMLinksIdentity            `json:"rom_links,omitempty"`
	ProgrammedPath     string                       `json:"programmed_path,omitempty"`
	FirmwarePath       string                       `json:"firmware_path,omitempty"` // a Nano upload package's ROM source, in place of ProgrammedPath
	Composition        *expansion.Composition       `json:"composition,omitempty"`
	ExpansionDirectory string                       `json:"expansion_directory,omitempty"`
	PayloadPath        string                       `json:"payload_path,omitempty"`
	companions         []Staged
	Directory          string     `json:"directory"`
	PackageID          string     `json:"package_id"`
	Descriptor         Descriptor `json:"descriptor"`
	root               string
	rootInfo           os.FileInfo
	publication        string
	publicationInfo    os.FileInfo

	// SlotComposition and SlotDirectories describe a multi-socket shell with
	// one staged card directory per selected physical slot.
	SlotComposition *expansion.SlotComposition `json:"slot_composition,omitempty"`
	SlotDirectories []SlotDirectory            `json:"slot_directories,omitempty"`
}

// RetainProgrammedBitstream stores bytes beside this publication. Cleanup
// removes them with the package, including when an ambiguous load retains it.
func (s *Staged) RetainProgrammedBitstream(body []byte) (string, error) {
	if s.root == "" || s.rootInfo == nil || s.publication == "" || len(body) == 0 {
		return "", errors.New("core package: programmed bitstream has no staging ownership")
	}
	handle, err := os.OpenRoot(s.root)
	if err != nil {
		return "", fmt.Errorf("core package: open programmed bitstream root: %w", err)
	}
	defer handle.Close()
	info, err := handle.Stat(".")
	if err != nil || !os.SameFile(info, s.rootInfo) {
		return "", errors.New("core package: programmed bitstream root changed")
	}
	name := "machine-rom-" + s.publication
	if err = handle.Mkdir(name, 0o700); err != nil {
		return "", fmt.Errorf("core package: create programmed bitstream directory: %w", err)
	}
	retained, err := handle.Lstat(name)
	if err != nil {
		return "", fmt.Errorf("core package: inspect programmed bitstream directory: %w", err)
	}
	s.companions = append(s.companions, Staged{root: s.root, rootInfo: info, publication: name, publicationInfo: retained})
	if err = writePrivate(handle, filepath.Join(name, "programmed.rbf"), body); err != nil {
		return "", err
	}
	if err = handle.Chmod(name, 0o500); err != nil {
		return "", fmt.Errorf("core package: seal programmed bitstream directory: %w", err)
	}
	return filepath.Join(s.root, name, "programmed.rbf"), nil
}

// Cleanup removes this caller-owned private publication through its retained
// staging root. It is safe to call again after successful removal.
func (s Staged) Cleanup() error {
	var companionErrors []error
	for _, companion := range s.companions {
		companionErrors = append(companionErrors, companion.Cleanup())
	}
	if err := errors.Join(companionErrors...); err != nil {
		return err
	}
	if s.root == "" || s.rootInfo == nil || s.publication == "" ||
		s.publicationInfo == nil {
		return errors.New("core package: staged package has no cleanup ownership")
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return fmt.Errorf("core package: open cleanup root: %w", err)
	}
	defer root.Close()
	openedRoot, err := root.Stat(".")
	if err != nil || !os.SameFile(s.rootInfo, openedRoot) {
		return errors.New("core package: cleanup root changed while opening")
	}
	publication, err := root.Lstat(s.publication)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("core package: inspect staged publication: %w", err)
	}
	if publication.Mode()&os.ModeSymlink != 0 || !publication.IsDir() ||
		!os.SameFile(s.publicationInfo, publication) {
		return errors.New("core package: staged publication changed before cleanup")
	}
	if err := root.Chmod(s.publication, 0o700); err != nil {
		return fmt.Errorf("core package: unseal staged publication: %w", err)
	}
	if err := root.RemoveAll(s.publication); err != nil {
		return fmt.Errorf("core package: remove staged publication: %w", err)
	}
	return nil
}

// Inspection is the identity and closed manifest projection derived from one
// pinned read of a package directory or archive.
type Inspection struct {
	PackageID  string     `json:"package_id"`
	Descriptor Descriptor `json:"descriptor"`
}

// InspectPackage validates a package and returns its identity and descriptor
// from the same pinned manifest and payload bytes.
func InspectPackage(path string) (Inspection, error) {
	manifest, payload, romMap, err := readPath(path)
	if err != nil {
		return Inspection{}, err
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{PackageID: packageIdentity(manifest, payload, romMap),
		Descriptor: descriptor}, nil
}

// MatchDirectoryToArchive checks every installed member against the same
// bytes in a canonical archive. The caller must first validate that archive;
// equality to unvalidated bytes does not make a package safe. This avoids
// parsing an identical ROM map twice while still reading every installed byte.
func MatchDirectoryToArchive(ctx context.Context, path string, archive []byte) error {
	if ctx == nil {
		return errors.New("core package: missing comparison context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifest, payload, companion, err := readArchiveMembers(archive, false)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("core package: installed path must be a non-symlink directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return fmt.Errorf("core package: open directory: %w", err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("core package: directory changed while opening")
	}
	withCompanion := companion != nil
	if err := exactDirectoryEntries(root, withCompanion, manifest); err != nil {
		return err
	}
	members := []struct {
		name string
		want []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}}
	if withCompanion {
		members = append(members, struct {
			name string
			want []byte
		}{companionName(manifest), companion})
	}
	buf := make([]byte, 32<<10)
	for _, member := range members {
		if err := compareRootMember(ctx, root, member.name, member.want, buf); err != nil {
			return err
		}
	}
	if err := exactDirectoryEntries(root, withCompanion, manifest); err != nil {
		return err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(opened, after) {
		return errors.New("core package: directory changed while comparing")
	}
	return ctx.Err()
}

func compareRootMember(ctx context.Context, root *os.Root, name string, want, buf []byte) error {
	before, err := root.Lstat(name)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() != int64(len(want)) {
		return fmt.Errorf("core package: %s must be a matching regular non-symlink file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("core package: open %s: %w", name, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || opened.Size() != int64(len(want)) {
		return fmt.Errorf("core package: %s changed while opening", name)
	}
	for len(want) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(len(buf), len(want))
		if _, err := io.ReadFull(file, buf[:n]); err != nil || !bytes.Equal(buf[:n], want[:n]) {
			return fmt.Errorf("core package: %s differs from validated archive", name)
		}
		want = want[n:]
	}
	var extra [1]byte
	if n, err := file.Read(extra[:]); n != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return fmt.Errorf("core package: %s changed while comparing", name)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("core package: close %s: %w", name, err)
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return fmt.Errorf("core package: %s changed while comparing", name)
	}
	return nil
}

// InspectListing validates an installed package directory for inventory.
// The manifest is the only member kept in memory. core.rbf is hashed as a
// stream. rom-map.json is hashed and structurally validated as a stream,
// including its binding to the payload digest, and its decoded blocks are
// not retained. A map InspectPackage would reject is rejected here. Launch
// and staging still use InspectPackage.
func InspectListing(ctx context.Context, path string) (Inspection, error) {
	if ctx == nil {
		return Inspection{}, errors.New("core package: missing listing context")
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Inspection{}, fmt.Errorf("core package: inspect path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Inspection{}, errors.New("core package: listing requires a non-symlink package directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return Inspection{}, fmt.Errorf("core package: open directory: %w", err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return Inspection{}, errors.New("core package: directory changed while opening")
	}
	manifest, err := readRootMember(root, "manifest.toml", MaxManifestSize)
	if err != nil {
		return Inspection{}, err
	}
	withCompanion, err := manifestHasCompanion(manifest)
	if err != nil {
		return Inspection{}, err
	}
	if err := exactDirectoryEntries(root, withCompanion, manifest); err != nil {
		return Inspection{}, err
	}
	descriptor, err := decodeManifest(manifest)
	if err != nil {
		return Inspection{}, err
	}
	if err := ValidateDescriptor(descriptor); err != nil {
		return Inspection{}, err
	}
	identity := sha256.New()
	domain := "FES-CORE-PACKAGE-2\n"
	if withCompanion {
		domain = "FES-CORE-PACKAGE-3\n"
		if descriptor.Format == 4 {
			domain = "FES-CORE-PACKAGE-4\n"
		}
		if descriptor.Format == 5 {
			domain = "FES-CORE-PACKAGE-5\n"
		}
	}
	_, _ = identity.Write([]byte(domain))
	writeDigestLen(identity, int64(len(manifest)))
	_, _ = identity.Write(manifest)
	if _, err := streamMemberDigest(ctx, root, "core.rbf", descriptor.Payload.Size, descriptor.Payload.SHA256, identity, nil); err != nil {
		return Inspection{}, err
	}
	if descriptor.Format == 5 {
		selected := descriptor.StaticExpansion
		if _, err := streamMemberDigest(ctx, root, "persona.rbf", selected.Size, selected.SHA256, identity, nil); err != nil {
			return Inspection{}, err
		}
	} else if withCompanion {
		var declared int64
		var sum string
		var sourceSize int64
		if descriptor.Format == 3 {
			declared, sum, sourceSize = descriptor.ROM.Size, descriptor.ROM.SHA256, descriptor.ROM.SourceSize
		} else {
			declared, sum = descriptor.ROMMap.Size, descriptor.ROMMap.SHA256
			for _, rom := range descriptor.ROMs {
				sourceSize += rom.SourceSize
			}
		}
		if _, err := streamMemberDigest(ctx, root, "rom-map.json", declared, sum, identity, func(r io.Reader) error {
			if nanoROM(descriptor) {
				return validateNanoROMMap(ctx, r, declared, descriptor)
			}
			if err := expansion.ValidateROMMapStream(ctx, r, declared, descriptor.Payload.SHA256, int(sourceSize)); err != nil {
				return fmt.Errorf("ROM map: %w", err)
			}
			return nil
		}); err != nil {
			return Inspection{}, err
		}
	}
	if err := exactDirectoryEntries(root, withCompanion, manifest); err != nil {
		return Inspection{}, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(opened, after) {
		return Inspection{}, errors.New("core package: directory changed while listing")
	}
	return Inspection{PackageID: hex.EncodeToString(identity.Sum(nil)), Descriptor: descriptor}, nil
}

// CanonicalArchive rewrites a package directory or archive as the canonical
// restricted ustar ROM input expects. Identity hashes the member bytes, so
// the package id matches InspectPackage of the same path.
func CanonicalArchive(path string) ([]byte, error) {
	manifest, payload, romMap, err := readPath(path)
	if err != nil {
		return nil, err
	}
	members := []canonicalMember{{name: "manifest.toml", data: manifest}, {name: "core.rbf", data: payload}}
	if romMap != nil {
		members = append(members, canonicalMember{name: companionName(manifest), data: romMap})
	}
	return writeCanonicalMembers(members, MaxArchiveSize)
}

// ReadPackage is InspectPackage and CanonicalArchive from one read of path.
func ReadPackage(path string) (Inspection, []byte, error) {
	manifest, payload, romMap, err := readPath(path)
	if err != nil {
		return Inspection{}, nil, err
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return Inspection{}, nil, err
	}
	members := []canonicalMember{{name: "manifest.toml", data: manifest}, {name: "core.rbf", data: payload}}
	if romMap != nil {
		members = append(members, canonicalMember{name: companionName(manifest), data: romMap})
	}
	data, err := writeCanonicalMembers(members, MaxArchiveSize)
	if err != nil {
		return Inspection{}, nil, err
	}
	return Inspection{PackageID: packageIdentity(manifest, payload, romMap), Descriptor: descriptor}, data, nil
}

// Inspect validates a package and returns its closed manifest projection.
func Inspect(path string) (Descriptor, error) {
	inspection, err := InspectPackage(path)
	return inspection.Descriptor, err
}

// Adopt reconstructs cleanup ownership for every valid private publication
// beneath a trusted staging root. It accepts only names and filesystem objects
// that Stage itself could have published.
func Adopt(root string) ([]Staged, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("core package: invalid adoption request")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("core package: adoption root must be an existing non-symlink directory")
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("core package: open adoption root: %w", err)
	}
	defer rootHandle.Close()
	return adoptOpenedRoot(root, rootHandle, rootInfo)
}

func adoptOpenedRoot(root string, rootHandle *os.Root, rootInfo os.FileInfo) ([]Staged, error) {
	openedRoot, err := rootHandle.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRoot) {
		return nil, errors.New("core package: adoption root changed while opening")
	}
	directory, err := rootHandle.Open(".")
	if err != nil {
		return nil, fmt.Errorf("core package: read adoption root: %w", err)
	}
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		return nil, fmt.Errorf("core package: read adoption root: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var adopted []Staged
	for _, entry := range entries {
		name := entry.Name()
		match := publicationRE.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		packageID := match[1]
		publicationInfo, err := rootHandle.Lstat(name)
		if err != nil || publicationInfo.Mode()&os.ModeSymlink != 0 ||
			!publicationInfo.IsDir() || publicationInfo.Mode().Perm() != 0o500 {
			return nil, errors.New("core package: invalid matching publication")
		}
		inspection, err := inspectRootedPublication(rootHandle, name, publicationInfo)
		if err != nil || inspection.PackageID != packageID {
			return nil, errors.New("core package: matching publication content is invalid")
		}
		after, err := rootHandle.Lstat(name)
		currentRoot, rootErr := rootHandle.Stat(".")
		if err != nil || rootErr != nil || !os.SameFile(publicationInfo, after) ||
			!os.SameFile(openedRoot, currentRoot) {
			return nil, errors.New("core package: matching publication changed while adopting")
		}
		adopted = append(adopted, Staged{Directory: filepath.Join(root, name),
			PackageID: packageID, Descriptor: inspection.Descriptor,
			root: root, rootInfo: openedRoot, publication: name,
			publicationInfo: publicationInfo})
		static, err := adoptStaticComposition(rootHandle, &adopted[len(adopted)-1])
		if err != nil {
			return nil, err
		}
		if !static {
			slots, err := adoptSlotComposition(rootHandle, &adopted[len(adopted)-1])
			if err != nil {
				return nil, err
			}
			if !slots {
				if err := adoptComposition(rootHandle, &adopted[len(adopted)-1]); err != nil {
					return nil, err
				}
			}
		}
		if err := adoptParts(rootHandle, &adopted[len(adopted)-1]); err != nil {
			return nil, err
		}
		if err := adoptROMInput(rootHandle, &adopted[len(adopted)-1]); err != nil {
			return nil, err
		}
		if err := adoptPcxtInitialInput(rootHandle, &adopted[len(adopted)-1]); err != nil {
			return nil, err
		}
	}
	return adopted, nil
}

func inspectRootedPublication(parent *os.Root, name string, expected os.FileInfo) (Inspection, error) {
	root, err := parent.OpenRoot(name)
	if err != nil {
		return Inspection{}, fmt.Errorf("core package: open staged publication: %w", err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, opened) {
		return Inspection{}, errors.New("core package: staged publication changed while opening")
	}
	manifest, payload, romMap, err := readRootContents(root)
	if err != nil {
		return Inspection{}, err
	}
	after, err := parent.Lstat(name)
	if err != nil || !os.SameFile(opened, after) {
		return Inspection{}, errors.New("core package: staged publication changed while inspecting")
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{PackageID: packageIdentity(manifest, payload, romMap), Descriptor: descriptor}, nil
}
