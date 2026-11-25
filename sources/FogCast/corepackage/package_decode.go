package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/DeanoC/misteross/expansion"
	"github.com/pelletier/go-toml/v2"
)

func decode(manifest, payload []byte, maps ...[]byte) (Descriptor, error) {
	return decodeContext(context.Background(), manifest, payload, maps...)
}

// decodeContext validates without keeping the parsed ROM map. A sealed map
// decodes to several times its size, so callers that only admit a package
// stream it through the same checks instead (#863).
func decodeContext(ctx context.Context, manifest, payload []byte, maps ...[]byte) (Descriptor, error) {
	d, _, err := decodeROMMapContext(ctx, false, manifest, payload, maps...)
	return d, err
}

func decodeWithROMMap(manifest, payload []byte, maps ...[]byte) (Descriptor, *expansion.ROMMap, error) {
	return decodeWithROMMapContext(context.Background(), manifest, payload, maps...)
}

func decodeManifest(manifest []byte) (Descriptor, error) {
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
	return descriptor, nil
}

func writeDigestLen(w io.Writer, n int64) {
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(n))
	_, _ = w.Write(length[:])
}

// streamMemberDigest hashes one package member without retaining it. The
// length prefix and the bytes are written to identity in packageIdentity order.
// examine, when set, reads the same stream. It is how a ROM map is structurally
// validated without a second copy of the member.
func streamMemberDigest(ctx context.Context, root *os.Root, name string, declared int64, declaredSum string, identity io.Writer, examine func(io.Reader) error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if declared < 1 {
		return "", fmt.Errorf("core package: %s size does not match manifest", name)
	}
	before, err := root.Lstat(name)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return "", fmt.Errorf("core package: %s must be a regular non-symlink file", name)
	}
	if before.Size() != declared {
		return "", fmt.Errorf("core package: %s size does not match manifest", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return "", fmt.Errorf("core package: open %s: %w", name, err)
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		_ = file.Close()
		return "", fmt.Errorf("core package: %s changed while opening", name)
	}
	writeDigestLen(identity, declared)
	hash := sha256.New()
	dest := io.MultiWriter(hash, identity)
	if examine != nil {
		reader := &digestReader{ctx: ctx, r: io.LimitReader(file, declared), w: dest}
		examErr := examine(reader)
		closeErr := file.Close()
		if examErr != nil {
			return "", examErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if reader.n != declared {
			return "", fmt.Errorf("core package: %s changed while reading", name)
		}
		after, err := root.Lstat(name)
		if err != nil || !os.SameFile(opened, after) || after.Size() != declared {
			return "", fmt.Errorf("core package: %s changed while reading", name)
		}
		sum := hex.EncodeToString(hash.Sum(nil))
		if sum != declaredSum {
			return "", fmt.Errorf("core package: %s digest does not match manifest", name)
		}
		return sum, nil
	}
	buf := make([]byte, 32<<10)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return "", err
		}
		n, readErr := file.Read(buf)
		if n > 0 {
			if _, err := dest.Write(buf[:n]); err != nil {
				_ = file.Close()
				return "", err
			}
			read += int64(n)
			if read > declared {
				_ = file.Close()
				return "", fmt.Errorf("core package: %s changed while reading", name)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = file.Close()
			return "", fmt.Errorf("core package: %s was truncated: %w", name, readErr)
		}
	}
	closeErr := file.Close()
	if closeErr != nil {
		return "", closeErr
	}
	if read != declared {
		return "", fmt.Errorf("core package: %s changed while reading", name)
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(opened, after) || after.Size() != declared {
		return "", fmt.Errorf("core package: %s changed while reading", name)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if sum != declaredSum {
		return "", fmt.Errorf("core package: %s digest does not match manifest", name)
	}
	return sum, nil
}

func decodeWithROMMapContext(ctx context.Context, manifest, payload []byte, maps ...[]byte) (Descriptor, *expansion.ROMMap, error) {
	return decodeROMMapContext(ctx, true, manifest, payload, maps...)
}

func decodeROMMapContext(ctx context.Context, retain bool, manifest, payload []byte, maps ...[]byte) (Descriptor, *expansion.ROMMap, error) {
	if err := ctx.Err(); err != nil {
		return Descriptor{}, nil, err
	}
	descriptor, err := decodeManifest(manifest)
	if err != nil {
		return Descriptor{}, nil, err
	}
	if err := validateDescriptor(descriptor, payload); err != nil {
		return Descriptor{}, nil, err
	}
	if len(maps) > 1 {
		return Descriptor{}, nil, errors.New("multiple ROM maps")
	}
	var parsed *expansion.ROMMap
	var mapping []byte
	if len(maps) == 1 {
		mapping = maps[0]
	}
	if descriptor.Format == 5 {
		selected := descriptor.StaticExpansion
		sum := sha256.Sum256(mapping)
		if int64(len(mapping)) != selected.Size || hex.EncodeToString(sum[:]) != selected.SHA256 {
			return Descriptor{}, nil, errors.New("default persona size or digest mismatch")
		}
		return descriptor, nil, nil
	}
	if descriptor.Format == 2 {
		if mapping != nil {
			return Descriptor{}, nil, errors.New("format 2 cannot contain ROM map")
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
			return Descriptor{}, nil, errors.New("ROM map size or digest mismatch")
		}
		if nanoROM(descriptor) {
			if err := validateNanoROMMap(ctx, bytes.NewReader(mapping), size, descriptor); err != nil {
				return Descriptor{}, nil, err
			}
			return descriptor, nil, nil
		}
		if !retain {
			if err := expansion.ValidateROMMapStream(ctx, bytes.NewReader(mapping), size, descriptor.Payload.SHA256, int(sourceSize)); err != nil {
				return Descriptor{}, nil, fmt.Errorf("ROM map: %w", err)
			}
			return descriptor, nil, nil
		}
		linkedMap, err := expansion.ParseROMMap(ctx, mapping, descriptor.Payload.SHA256, int(sourceSize))
		if err != nil {
			return Descriptor{}, nil, fmt.Errorf("ROM map: %w", err)
		}
		parsed = &linkedMap
	}
	return descriptor, parsed, nil
}

func validateShape(root map[string]any) error {
	required := []string{"format", "core", "target", "payload", "abi", "interfaces", "build"}
	if root["format"] == int64(3) {
		required = append(required, "rom")
	} else if root["format"] == int64(4) {
		required = append(required, "roms", "rom_map")
	} else if root["format"] == int64(5) {
		required = append(required, "static_expansion")
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
	if root["format"] == int64(5) {
		tables = append(tables, struct {
			name               string
			required, optional []string
		}{
			"static_expansion", staticExpansionKeys, nil,
		})
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
