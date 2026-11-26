package corepackage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

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
	// The validated length bounds one allocation. ReadAll's geometric growth
	// otherwise repeatedly copies a large map before it can be inspected.
	data := make([]byte, int(length)+1)
	n, err := io.ReadFull(&contextReader{ctx: ctx, reader: reader}, data)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Staged{}, err
	}
	if int64(n) != length {
		return Staged{}, errors.New("core package: upload length does not match the declared length")
	}
	return StageBytes(ctx, root, data[:n])
}

// StageBytes stages an archive the caller already holds, without copying it.
// The caller keeps data immutable until return; the published files own
// independent bytes afterward.
func StageBytes(ctx context.Context, root string, data []byte) (Staged, error) {
	return stageArchive(ctx, root, data, nil)
}

// stageArchive publishes data. verified, when set, is this package's
// inspection, already decoded from the same bytes by the caller, so the
// archive is not decoded a second time.
func stageArchive(ctx context.Context, root string, data []byte, verified *Inspection) (Staged, error) {
	if ctx == nil || len(data) < 1 || len(data) > MaxArchiveSize {
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
	manifest, payload, romMap, err := readArchiveMembers(data, false)
	if err != nil {
		return Staged{}, err
	}
	var descriptor Descriptor
	var id string
	if verified != nil {
		descriptor, id = verified.Descriptor, verified.PackageID
	} else {
		descriptor, err = decodeContext(ctx, manifest, payload, romMap)
		if err != nil {
			return Staged{}, err
		}
		id = packageIdentity(manifest, payload, romMap)
	}
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
		if err := writePrivate(rootHandle, filepath.Join(temporary, companionName(manifest)), romMap); err != nil {
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

type digestReader struct {
	ctx context.Context
	r   io.Reader
	w   io.Writer
	n   int64
}

func (d *digestReader) Read(p []byte) (int, error) {
	if err := d.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := d.r.Read(p)
	if n > 0 {
		if _, werr := d.w.Write(p[:n]); werr != nil {
			return n, werr
		}
		d.n += int64(n)
	}
	return n, err
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

func manifestHasCompanion(manifest []byte) (bool, error) {
	if len(manifest) < 1 || len(manifest) > MaxManifestSize || !utf8.Valid(manifest) {
		return false, errors.New("invalid manifest size or UTF-8")
	}
	var fields struct {
		Format int64 `toml:"format"`
	}
	if err := toml.Unmarshal(manifest, &fields); err != nil {
		return false, err
	}
	if fields.Format != 2 && fields.Format != 3 && fields.Format != 4 && fields.Format != 5 {
		return false, errors.New("unsupported core package format")
	}
	return fields.Format == 3 || fields.Format == 4 || fields.Format == 5, nil
}

func readRootContents(root *os.Root) ([]byte, []byte, []byte, error) {
	manifest, err := readRootMember(root, "manifest.toml", MaxManifestSize)
	if err != nil {
		return nil, nil, nil, err
	}
	withCompanion, err := manifestHasCompanion(manifest)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := exactDirectoryEntries(root, withCompanion, manifest); err != nil {
		return nil, nil, nil, err
	}
	payload, err := readRootMember(root, "core.rbf", MaxPayloadSize)
	if err != nil {
		return nil, nil, nil, err
	}
	var romMap []byte
	if withCompanion {
		romMap, err = readRootMember(root, companionName(manifest), companionLimit(manifest))
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if err := exactDirectoryEntries(root, withCompanion, manifest); err != nil {
		return nil, nil, nil, err
	}
	return manifest, payload, romMap, nil
}

func exactDirectoryEntries(root *os.Root, withCompanion bool, manifest []byte) error {
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
	if withCompanion {
		expected = 3
	}
	if len(entries) != expected {
		return errors.New("wrong entry count")
	}
	found := map[string]bool{}
	for _, entry := range entries {
		found[entry.Name()] = true
	}
	if !found["manifest.toml"] || !found["core.rbf"] || (withCompanion && !found[companionName(manifest)]) {
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
	return readArchiveMembers(data, true)
}

// Stage owns the uploaded buffer through validation and private publication;
// borrowing its payload/map avoids a second complete package allocation.
// Clone the small manifest even here: descriptor strings must never pin the
// complete upload if a TOML decoder borrows its input. Other callers retain
// the original independent-member ownership for every member.
func readArchiveMembers(data []byte, copyMembers bool) ([]byte, []byte, []byte, error) {
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
		value := data[offset : offset+int(size)]
		if copyMembers || index == 0 {
			value = bytes.Clone(value)
		}
		for _, padding := range data[offset+int(size) : offset+int(padded)] {
			if padding != 0 {
				return nil, nil, nil, fmt.Errorf("core package: archive member %s has nonzero padding", member.name)
			}
		}
		if index == 0 {
			withCompanion, err := manifestHasCompanion(value)
			if err != nil {
				return nil, nil, nil, err
			}
			if withCompanion {
				members = append(members, struct {
					name    string
					maximum int64
				}{companionName(value), companionLimit(value)})
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
