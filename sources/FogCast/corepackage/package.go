// Package corepackage validates and privately stages FES format-2/3/4 core packages.
package corepackage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/DeanoC/misteross/expansion"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
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
	ROM        *ROM              `toml:"rom,omitempty" json:"rom,omitempty"`
	ROMs       []ROMRequirement  `toml:"roms,omitempty" json:"roms,omitempty"`
	ROMMap     *ROMMapDescriptor `toml:"rom_map,omitempty" json:"rom_map,omitempty"`
	Format     int64             `toml:"format" json:"format"`
	Core       Core              `toml:"core" json:"core"`
	Target     Target            `toml:"target" json:"target"`
	Payload    Payload           `toml:"payload" json:"payload"`
	ABI        Contract          `toml:"abi" json:"abi"`
	Interfaces []Interface       `toml:"interfaces" json:"interfaces"`
	Build      Build             `toml:"build" json:"build"`
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
	ROMLink            *ROMLinkIdentity       `json:"rom_link,omitempty"`
	ROMLinks           *ROMLinksIdentity      `json:"rom_links,omitempty"`
	ProgrammedPath     string                 `json:"programmed_path,omitempty"`
	Composition        *expansion.Composition `json:"composition,omitempty"`
	ExpansionDirectory string                 `json:"expansion_directory,omitempty"`
	PayloadPath        string                 `json:"payload_path,omitempty"`
	companions         []Staged
	Directory          string     `json:"directory"`
	PackageID          string     `json:"package_id"`
	Descriptor         Descriptor `json:"descriptor"`
	root               string
	rootInfo           os.FileInfo
	publication        string
	publicationInfo    os.FileInfo
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
		if err := adoptComposition(rootHandle, &adopted[len(adopted)-1]); err != nil {
			return nil, err
		}
		if err := adoptROMInput(rootHandle, &adopted[len(adopted)-1]); err != nil {
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

func Stage(ctx context.Context, root string, length int64, reader io.Reader) (Staged, error) {
	if ctx == nil || reader == nil {
		return Staged{}, errors.New("core package: missing staging input")
	}
	if length < 1 || length > MaxArchiveSize {
		return Staged{}, fmt.Errorf("core package: archive size must be 1 through %d bytes", MaxArchiveSize)
	}
	if !filepath.IsAbs(root) {
		return Staged{}, errors.New("core package: staging root must be absolute")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return Staged{}, errors.New("core package: staging root must be an existing non-symlink directory")
	}
	if err := ctx.Err(); err != nil {
		return Staged{}, err
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, length+1))
	if err != nil {
		return Staged{}, err
	}
	if int64(len(data)) != length {
		return Staged{}, errors.New("core package: upload length does not match the declared length")
	}
	manifest, payload, romMap, err := readArchive(data)
	if err != nil {
		return Staged{}, err
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return Staged{}, err
	}
	id := packageIdentity(manifest, payload, romMap)
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return Staged{}, fmt.Errorf("core package: open staging root: %w", err)
	}
	defer rootHandle.Close()
	openedRoot, err := rootHandle.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRoot) {
		return Staged{}, errors.New("core package: staging root changed while opening")
	}
	token, err := randomToken()
	if err != nil {
		return Staged{}, err
	}
	temporary := ".corepackage-" + token
	if err := rootHandle.Mkdir(temporary, 0o700); err != nil {
		return Staged{}, fmt.Errorf("core package: create private staging directory: %w", err)
	}
	ownedName := temporary
	handedOff := false
	defer func() {
		if !handedOff {
			_ = rootHandle.Chmod(ownedName, 0o700)
			_ = rootHandle.RemoveAll(ownedName)
		}
	}()
	if err := writePrivate(rootHandle, filepath.Join(temporary, "manifest.toml"), manifest); err != nil {
		return Staged{}, err
	}
	if err := writePrivate(rootHandle, filepath.Join(temporary, "core.rbf"), payload); err != nil {
		return Staged{}, err
	}
	if romMap != nil {
		if err := writePrivate(rootHandle, filepath.Join(temporary, "rom-map.json"), romMap); err != nil {
			return Staged{}, err
		}
	}
	if err := rootHandle.Chmod(temporary, 0o500); err != nil {
		return Staged{}, fmt.Errorf("core package: seal staging directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Staged{}, err
	}
	publication := id + "-" + token
	if _, err := rootHandle.Lstat(publication); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return Staged{}, errors.New("core package: staging publication already exists")
		}
		return Staged{}, fmt.Errorf("core package: inspect publication path: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Staged{}, err
	}
	if err := rootHandle.Rename(temporary, publication); err != nil {
		return Staged{}, fmt.Errorf("core package: publish staging directory: %w", err)
	}
	ownedName = publication
	publicationInfo, err := rootHandle.Lstat(publication)
	if err != nil {
		return Staged{}, fmt.Errorf("core package: retain staged publication: %w", err)
	}
	// Cancellation observed through this point retains no publication. Once
	// handedOff becomes true, the returned Staged value owns cleanup.
	if err := ctx.Err(); err != nil {
		return Staged{}, err
	}
	handedOff = true
	return Staged{Directory: filepath.Join(root, publication), PackageID: id,
		Descriptor: descriptor, root: root, rootInfo: openedRoot,
		publication: publication, publicationInfo: publicationInfo}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func writePrivate(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return fmt.Errorf("core package: create staged member: %w", err)
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("core package: write staged member: %w", err)
	}
	return nil
}

func randomToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("core package: generate staging identity: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func readPath(path string) ([]byte, []byte, []byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("core package: inspect path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, nil, errors.New("core package: package path must not be a symlink")
	}
	if info.IsDir() {
		return readDirectory(path, info)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, nil, errors.New("core package: path must be a directory or regular archive")
	}
	data, err := readRegular(path, info, MaxArchiveSize, "archive")
	if err != nil {
		return nil, nil, nil, err
	}
	return readArchive(data)
}

func readDirectory(path string, expected os.FileInfo) ([]byte, []byte, []byte, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("core package: open directory: %w", err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, opened) {
		return nil, nil, nil, errors.New("core package: directory changed while opening")
	}
	return readRootContents(root)
}

func manifestRequiresROM(manifest []byte) (bool, error) {
	if len(manifest) < 1 || len(manifest) > MaxManifestSize || !utf8.Valid(manifest) {
		return false, errors.New("invalid manifest size or UTF-8")
	}
	var fields struct {
		Format int64 `toml:"format"`
	}
	if err := toml.Unmarshal(manifest, &fields); err != nil {
		return false, err
	}
	if fields.Format != 2 && fields.Format != 3 && fields.Format != 4 {
		return false, errors.New("unsupported core package format")
	}
	return fields.Format == 3 || fields.Format == 4, nil
}

func readRootContents(root *os.Root) ([]byte, []byte, []byte, error) {
	manifest, err := readRootMember(root, "manifest.toml", MaxManifestSize)
	if err != nil {
		return nil, nil, nil, err
	}
	withROM, err := manifestRequiresROM(manifest)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := exactDirectoryEntries(root, withROM); err != nil {
		return nil, nil, nil, err
	}
	payload, err := readRootMember(root, "core.rbf", MaxPayloadSize)
	if err != nil {
		return nil, nil, nil, err
	}
	var romMap []byte
	if withROM {
		romMap, err = readRootMember(root, "rom-map.json", MaxROMMapSize)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if err := exactDirectoryEntries(root, withROM); err != nil {
		return nil, nil, nil, err
	}
	return manifest, payload, romMap, nil
}

func exactDirectoryEntries(root *os.Root, withROM bool) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	expected := 2
	if withROM {
		expected = 3
	}
	if len(entries) != expected {
		return errors.New("wrong entry count")
	}
	found := map[string]bool{}
	for _, entry := range entries {
		found[entry.Name()] = true
	}
	if !found["manifest.toml"] || !found["core.rbf"] || (withROM && !found["rom-map.json"]) {
		return errors.New("wrong entries")
	}
	return nil
}

func readRootMember(root *os.Root, name string, maximum int64) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("core package: %s must be a regular non-symlink file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("core package: open %s: %w", name, err)
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("core package: %s changed while opening", name)
	}
	data, err := readOpenFile(file, opened.Size(), maximum, name)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(opened, after) {
		return nil, fmt.Errorf("core package: %s changed while reading", name)
	}
	return data, nil
}

func readRegular(path string, expected os.FileInfo, maximum int64, field string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("core package: open %s: %w", field, err)
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("core package: %s changed while opening", field)
	}
	data, err := readOpenFile(file, opened.Size(), maximum, field)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return data, err
}

func readOpenFile(file *os.File, size, maximum int64, field string) ([]byte, error) {
	if size < 1 || size > maximum {
		return nil, fmt.Errorf("core package: %s size must be 1 through %d bytes", field, maximum)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, fmt.Errorf("core package: %s was truncated: %w", field, err)
	}
	var extra [1]byte
	if count, err := file.Read(extra[:]); count != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return nil, fmt.Errorf("core package: %s changed while reading", field)
	}
	return data, nil
}

func readArchive(data []byte) ([]byte, []byte, []byte, error) {
	offset := 0
	values := make([][]byte, 0, 2)
	members := []struct {
		name    string
		maximum int64
	}{{"manifest.toml", MaxManifestSize}, {"core.rbf", MaxPayloadSize}}
	for index := 0; index < len(members); index++ {
		member := members[index]
		if len(data)-offset < 512 {
			return nil, nil, nil, fmt.Errorf("core package: archive is missing %s header", member.name)
		}
		header := data[offset : offset+512]
		size, err := canonicalSize(header[124:136])
		if err != nil || size < 1 || size > member.maximum {
			return nil, nil, nil, fmt.Errorf("core package: invalid %s archive size", member.name)
		}
		if !bytes.Equal(header, canonicalHeader(member.name, size)) {
			return nil, nil, nil, fmt.Errorf("core package: %s header is not canonical restricted ustar", member.name)
		}
		offset += 512
		padded := (size + 511) &^ 511
		if padded > int64(len(data)-offset) {
			return nil, nil, nil, fmt.Errorf("core package: archive member %s is truncated", member.name)
		}
		value := append([]byte(nil), data[offset:offset+int(size)]...)
		for _, padding := range data[offset+int(size) : offset+int(padded)] {
			if padding != 0 {
				return nil, nil, nil, fmt.Errorf("core package: archive member %s has nonzero padding", member.name)
			}
		}
		if index == 0 {
			withROM, err := manifestRequiresROM(value)
			if err != nil {
				return nil, nil, nil, err
			}
			if withROM {
				members = append(members, struct {
					name    string
					maximum int64
				}{"rom-map.json", MaxROMMapSize})
			}
		}
		values = append(values, value)
		offset += int(padded)
	}
	if len(data)-offset != 1024 || !allZero(data[offset:]) {
		return nil, nil, nil, errors.New("core package: archive must end with exactly two zero blocks")
	}
	var romMap []byte
	if len(values) == 3 {
		romMap = values[2]
	}
	return values[0], values[1], romMap, nil
}

func canonicalSize(field []byte) (int64, error) {
	if len(field) != 12 || field[11] != 0 {
		return 0, errors.New("invalid size")
	}
	var value int64
	for _, digit := range field[:11] {
		if digit < '0' || digit > '7' {
			return 0, errors.New("invalid size")
		}
		value = value*8 + int64(digit-'0')
	}
	return value, nil
}

func canonicalHeader(name string, size int64) []byte {
	header := make([]byte, 512)
	copy(header[0:100], name)
	copy(header[100:108], "0000644\x00")
	copy(header[108:116], "0000000\x00")
	copy(header[116:124], "0000000\x00")
	copy(header[124:136], fmt.Sprintf("%011o\x00", size))
	copy(header[136:148], "00000000000\x00")
	for index := 148; index < 156; index++ {
		header[index] = ' '
	}
	header[156] = '0'
	copy(header[257:263], "ustar\x00")
	copy(header[263:265], "00")
	var checksum int
	for _, value := range header {
		checksum += int(value)
	}
	copy(header[148:156], fmt.Sprintf("%06o\x00 ", checksum))
	return header
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func decode(manifest, payload []byte, maps ...[]byte) (Descriptor, error) {
	if len(manifest) < 1 || len(manifest) > MaxManifestSize || !utf8.Valid(manifest) {
		return Descriptor{}, errors.New("core package: manifest must be 1 through 65536 valid UTF-8 bytes")
	}
	var descriptor Descriptor
	decoder := toml.NewDecoder(bytes.NewReader(manifest))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return Descriptor{}, fmt.Errorf("core package: invalid manifest: %w", err)
	}
	var fields map[string]any
	if err := toml.Unmarshal(manifest, &fields); err != nil {
		return Descriptor{}, fmt.Errorf("core package: invalid manifest shape: %w", err)
	}
	if err := validateShape(fields); err != nil {
		return Descriptor{}, err
	}
	if err := validateDescriptor(descriptor, payload); err != nil {
		return Descriptor{}, err
	}
	if len(maps) > 1 {
		return Descriptor{}, errors.New("multiple ROM maps")
	}
	var mapping []byte
	if len(maps) == 1 {
		mapping = maps[0]
	}
	if descriptor.Format == 2 {
		if mapping != nil {
			return Descriptor{}, errors.New("format 2 cannot contain ROM map")
		}
	} else {
		var size int64
		var sha string
		var sourceSize int64
		if descriptor.Format == 3 {
			size, sha, sourceSize = descriptor.ROM.Size, descriptor.ROM.SHA256, descriptor.ROM.SourceSize
		} else {
			size, sha = descriptor.ROMMap.Size, descriptor.ROMMap.SHA256
			for _, rom := range descriptor.ROMs {
				sourceSize += rom.SourceSize
			}
		}
		hash := sha256.Sum256(mapping)
		if int64(len(mapping)) != size || hex.EncodeToString(hash[:]) != sha {
			return Descriptor{}, errors.New("ROM map size or digest mismatch")
		}
		if _, err := expansion.ParseROMMap(context.Background(), mapping, descriptor.Payload.SHA256, int(sourceSize)); err != nil {
			return Descriptor{}, fmt.Errorf("ROM map: %w", err)
		}
	}
	return descriptor, nil
}

func validateShape(root map[string]any) error {
	required := []string{"format", "core", "target", "payload", "abi", "interfaces", "build"}
	if root["format"] == int64(3) {
		required = append(required, "rom")
	} else if root["format"] == int64(4) {
		required = append(required, "roms", "rom_map")
	}
	if err := exactKeys(root, required, nil, "manifest"); err != nil {
		return err
	}
	tables := []struct {
		name     string
		required []string
		optional []string
	}{
		{"core", []string{"id", "name", "description", "version"}, []string{"system"}},
		{"target", []string{"platform", "device", "programming_profile"}, nil},
		{"payload", []string{"file", "size", "sha256"}, nil},
		{"abi", []string{"id", "major", "minor"}, nil},
		{"build", []string{"id", "repository", "revision", "recipe_sha256", "toolchain"}, nil},
	}
	if root["format"] == int64(3) {
		table, ok := root["rom"].(map[string]any)
		if !ok {
			return errors.New("rom must be a TOML table")
		}
		if err := exactKeys(table, []string{"id", "role", "source_size", "file", "size", "sha256"}, nil, "rom"); err != nil {
			return err
		}
	}
	if root["format"] == int64(4) {
		entries, ok := root["roms"].([]any)
		if !ok || len(entries) != 2 {
			return errors.New("roms must contain two tables")
		}
		for index, value := range entries {
			table, ok := value.(map[string]any)
			if !ok {
				return errors.New("roms must contain tables")
			}
			if err := exactKeys(table, []string{"id", "role", "source_size", "source_offset"}, nil, fmt.Sprintf("roms[%d]", index)); err != nil {
				return err
			}
		}
		mapTable, ok := root["rom_map"].(map[string]any)
		if !ok {
			return errors.New("rom_map must be a table")
		}
		if err := exactKeys(mapTable, []string{"file", "size", "sha256"}, nil, "rom_map"); err != nil {
			return err
		}
	}
	for _, item := range tables {
		table, ok := root[item.name].(map[string]any)
		if !ok {
			return fmt.Errorf("core package: %s must be a TOML table", item.name)
		}
		if err := exactKeys(table, item.required, item.optional, item.name); err != nil {
			return err
		}
	}
	interfaces, ok := root["interfaces"].([]any)
	if !ok {
		return errors.New("core package: interfaces must be an array of tables")
	}
	for index, value := range interfaces {
		table, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("core package: interfaces[%d] must be a TOML table", index)
		}
		if err := exactKeys(table, []string{"id", "major", "minor", "required"}, nil,
			fmt.Sprintf("interfaces[%d]", index)); err != nil {
			return err
		}
	}
	return nil
}

func exactKeys(table map[string]any, required, optional []string, field string) error {
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
		if _, exists := table[key]; !exists {
			return fmt.Errorf("core package: %s is missing required field %s", field, key)
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range table {
		if !allowed[key] {
			return fmt.Errorf("core package: %s has unknown field %s", field, key)
		}
	}
	return nil
}

// ValidateDescriptor applies every payload-independent field rule from the
// format-2 manifest contract. Readers use it before binding the declared
// payload size and digest to the bytes they opened; protocol projections reuse
// it so they cannot drift from package admission semantics.
func ValidateDescriptor(d Descriptor) error {
	if d.Format != 2 && d.Format != 3 && d.Format != 4 {
		return errors.New("core package: unsupported format")
	}
	if (d.Format == 3) != (d.ROM != nil) {
		return errors.New("core package: ROM declaration requires format 3")
	}
	if d.Format == 4 {
		if len(d.ROMs) != 2 || d.ROMMap == nil {
			return errors.New("format 4 requires two ROM sources and a map")
		}
		total := int64(0)
		for i, r := range d.ROMs {
			if err := identifier(r.ID, "roms.id"); err != nil {
				return err
			}
			if (i == 0 && r.Role != "firmware") || (i == 1 && r.Role != "cartridge") || r.SourceSize < 1024 || r.SourceSize > 262144 || r.SourceSize%1024 != 0 || r.SourceOffset != total {
				return errors.New("invalid ordered ROM requirements")
			}
			total += r.SourceSize
		}
		if d.ROMs[0].ID == d.ROMs[1].ID || total > 262144 {
			return errors.New("duplicate ROM ID or combined source too large")
		}
		m := d.ROMMap
		if m.File != "rom-map.json" || m.Size < 1 || m.Size > MaxROMMapSize || !hex64RE.MatchString(m.SHA256) {
			return errors.New("invalid ROM map metadata")
		}
	} else if len(d.ROMs) != 0 || d.ROMMap != nil {
		return errors.New("two-source ROM fields require format 4")
	}
	if r := d.ROM; r != nil {
		if err := identifier(r.ID, "rom.id"); err != nil {
			return err
		}
		if r.Role != "firmware" && r.Role != "cartridge" {
			return errors.New("invalid ROM role")
		}
		if r.SourceSize < 1024 || r.SourceSize > 262144 || r.SourceSize%1024 != 0 || r.File != "rom-map.json" || r.Size < 1 || r.Size > MaxROMMapSize || !hex64RE.MatchString(r.SHA256) {
			return errors.New("invalid ROM metadata")
		}
	}
	if err := identifier(d.Core.ID, "core.id"); err != nil {
		return err
	}
	if err := text(d.Core.Name, "core.name", true, 128); err != nil {
		return err
	}
	if err := text(d.Core.Description, "core.description", false, 2048); err != nil {
		return err
	}
	if !semverRE.MatchString(d.Core.Version) {
		return errors.New("core package: core.version must be full SemVer")
	}
	if d.Core.System != "" {
		if err := identifier(d.Core.System, "core.system"); err != nil {
			return err
		}
	}
	if err := identifier(d.Target.Platform, "target.platform"); err != nil {
		return err
	}
	if err := text(d.Target.Device, "target.device", true, 0); err != nil {
		return err
	}
	if err := identifier(d.Target.ProgrammingProfile, "target.programming_profile"); err != nil {
		return err
	}
	if d.Target.Platform != "de10_nano" || d.Target.Device != "5CSEBA6U23I7" {
		return errors.New("core package: unsupported target")
	}
	if d.Payload.File != "core.rbf" {
		return errors.New("core package: payload.file must be core.rbf")
	}
	if d.Payload.Size < 1 || d.Payload.Size > MaxPayloadSize {
		return errors.New("core package: payload.size is out of bounds")
	}
	if !hex64RE.MatchString(d.Payload.SHA256) {
		return errors.New("core package: invalid payload.sha256")
	}
	if err := contract(d.ABI, "abi"); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(d.Interfaces))
	for index, value := range d.Interfaces {
		field := fmt.Sprintf("interfaces[%d]", index)
		if err := identifier(value.ID, field+".id"); err != nil {
			return err
		}
		if value.Major < 1 || value.Major > 65535 || value.Minor < 0 || value.Minor > 65535 {
			return fmt.Errorf("core package: %s version is out of bounds", field)
		}
		if _, exists := seen[value.ID]; exists {
			return fmt.Errorf("core package: duplicate interface id %s", value.ID)
		}
		seen[value.ID] = struct{}{}
	}
	if !hex32RE.MatchString(d.Build.ID) {
		return errors.New("core package: invalid build.id")
	}
	if !validRepository(d.Build.Repository) {
		return errors.New("core package: invalid build.repository")
	}
	if !hex40RE.MatchString(d.Build.Revision) {
		return errors.New("core package: invalid build.revision")
	}
	if !hex64RE.MatchString(d.Build.RecipeSHA256) {
		return errors.New("core package: invalid build.recipe_sha256")
	}
	if err := text(d.Build.Toolchain, "build.toolchain", true, 1024); err != nil {
		return err
	}
	return nil
}

func validateDescriptor(d Descriptor, payload []byte) error {
	if err := ValidateDescriptor(d); err != nil {
		return err
	}
	if int64(len(payload)) != d.Payload.Size {
		return errors.New("core package: payload size does not match manifest")
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != d.Payload.SHA256 {
		return errors.New("core package: payload digest does not match manifest")
	}
	return nil
}

func contract(value Contract, field string) error {
	if err := identifier(value.ID, field+".id"); err != nil {
		return err
	}
	if value.Major < 1 || value.Major > 65535 || value.Minor < 0 || value.Minor > 65535 {
		return fmt.Errorf("core package: %s version is out of bounds", field)
	}
	return nil
}

func identifier(value, field string) error {
	if !identifierRE.MatchString(value) {
		return fmt.Errorf("core package: invalid %s", field)
	}
	return nil
}

func text(value, field string, nonempty bool, maximum int) error {
	if nonempty && value == "" {
		return fmt.Errorf("core package: %s must not be empty", field)
	}
	if maximum > 0 && len([]byte(value)) > maximum {
		return fmt.Errorf("core package: %s exceeds %d UTF-8 bytes", field, maximum)
	}
	for _, character := range value {
		if character < 32 || (character >= 127 && character <= 159) {
			return fmt.Errorf("core package: %s contains a control character", field)
		}
	}
	return nil
}

func validRepository(value string) bool {
	if !strings.HasPrefix(value, "https://") {
		return false
	}
	rest := value[len("https://"):]
	end := strings.IndexAny(rest, "/?#")
	authority := rest
	suffix := ""
	if end >= 0 {
		authority, suffix = rest[:end], rest[end:]
	}
	if authority == "" || strings.Contains(authority, "@") || !validAuthority(authority) {
		return false
	}
	path, query, fragment, ok := splitSuffix(suffix)
	return ok && validURIText(path, false) && validURIText(query, true) && validURIText(fragment, true)
}

func validAuthority(authority string) bool {
	host, port := authority, ""
	literal := false
	if strings.HasPrefix(authority, "[") {
		closing := strings.IndexByte(authority, ']')
		if closing < 0 {
			return false
		}
		host = authority[:closing+1]
		remaining := authority[closing+1:]
		if remaining != "" {
			if !strings.HasPrefix(remaining, ":") {
				return false
			}
			port = remaining[1:]
		}
		inside := host[1 : len(host)-1]
		parsed := net.ParseIP(inside)
		if (parsed == nil || !strings.Contains(inside, ":")) &&
			!ipvFutureRE.MatchString(inside) {
			return false
		}
		literal = true
	} else if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		host, port = authority[:colon], authority[colon+1:]
		if strings.Contains(host, ":") {
			return false
		}
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	if literal {
		return true
	}
	return validComponent(host, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.~-!$&'()*+,;=%", true)
}

func splitSuffix(value string) (string, string, string, bool) {
	if strings.Count(value, "#") > 1 {
		return "", "", "", false
	}
	fragment := ""
	if at := strings.IndexByte(value, '#'); at >= 0 {
		fragment, value = value[at+1:], value[:at]
	}
	query := ""
	if at := strings.IndexByte(value, '?'); at >= 0 {
		query, value = value[at+1:], value[:at]
	}
	if value != "" && value[0] != '/' {
		return "", "", "", false
	}
	return value, query, fragment, true
}

func validURIText(value string, query bool) bool {
	allowed := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.~-!$&'()*+,;=:@"
	if query {
		allowed += "/?"
	} else {
		allowed += "/"
	}
	return validComponent(value, allowed, true)
}

func validComponent(value, allowed string, percent bool) bool {
	for index := 0; index < len(value); index++ {
		if value[index] == '%' && percent {
			if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
				return false
			}
			index += 2
			continue
		}
		if value[index] >= utf8.RuneSelf || !strings.ContainsRune(allowed, rune(value[index])) {
			return false
		}
	}
	return true
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func packageIdentity(manifest, payload []byte, maps ...[]byte) string {
	digest := sha256.New()
	domain := "FES-CORE-PACKAGE-2\n"
	var mapping []byte
	if len(maps) == 1 {
		mapping = maps[0]
	}
	if mapping != nil {
		domain = "FES-CORE-PACKAGE-3\n"
		var header struct {
			Format int64 `toml:"format"`
		}
		if err := toml.Unmarshal(manifest, &header); err == nil && header.Format == 4 {
			domain = "FES-CORE-PACKAGE-4\n"
		}
	}
	_, _ = digest.Write([]byte(domain))
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(manifest)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(manifest)
	binary.LittleEndian.PutUint64(length[:], uint64(len(payload)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(payload)
	if mapping != nil {
		binary.LittleEndian.PutUint64(length[:], uint64(len(mapping)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(mapping)
	}
	return hex.EncodeToString(digest.Sum(nil))
}
