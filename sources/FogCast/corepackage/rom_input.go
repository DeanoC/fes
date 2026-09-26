package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/misteross/expansion"
)

// MaxROMInputSize bounds the package, source ROM, optional expansion and framing.
const MaxROMInputSize = MaxArchiveSize + expansion.MaxArchiveBytes + (256 << 10) + 16384

// ROMInput transports source bytes only. The target derives all programmed bytes
// using the map sealed inside Package; callers cannot supply a map or bitstream.
type ROMInput struct {
	Package   []byte
	ROM       []byte
	Expansion *expansion.Asset
}

// ROMLinkIdentity binds an exact source ROM and sealed map to target-linked bytes.
type ROMLinkIdentity struct {
	ROMID            string `json:"rom_id"`
	MapSHA256        string `json:"map_sha256"`
	SourceSHA256     string `json:"source_sha256"`
	SourceSize       int64  `json:"source_size"`
	ProgrammedSHA256 string `json:"programmed_sha256"`
	ProgrammedSize   int64  `json:"programmed_size"`
}

// ValidFor checks identity syntax and its binding to sealed ROM metadata.
func (r ROMLinkIdentity) ValidFor(d Descriptor) bool {
	return d.Format == 3 && d.ROM != nil && identifier(r.ROMID, "rom.id") == nil && r.ROMID == d.ROM.ID && r.MapSHA256 == d.ROM.SHA256 &&
		hex64RE.MatchString(r.MapSHA256) && hex64RE.MatchString(r.SourceSHA256) && hex64RE.MatchString(r.ProgrammedSHA256) &&
		r.SourceSize == d.ROM.SourceSize && r.SourceSize >= 1024 && r.SourceSize <= 256<<10 && r.SourceSize%1024 == 0 &&
		r.ProgrammedSize > 0 && r.ProgrammedSize <= MaxPayloadSize
}

type romInputReceipt struct {
	Format       int    `json:"format"`
	PackageID    string `json:"package_id"`
	ROMID        string `json:"rom_id"`
	MapSHA256    string `json:"map_sha256"`
	SourceSHA256 string `json:"source_sha256"`
	SourceSize   int64  `json:"source_size"`
	ExpansionID  string `json:"expansion_id,omitempty"`
}

func romDigest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func inspectROMInput(in ROMInput) (Inspection, []byte, []byte, romInputReceipt, error) {
	manifest, payload, mapping, err := readArchive(in.Package)
	if err != nil {
		return Inspection{}, nil, nil, romInputReceipt{}, err
	}
	d, err := decode(manifest, payload, mapping)
	if err != nil {
		return Inspection{}, nil, nil, romInputReceipt{}, err
	}
	if d.Format != 3 || d.ROM == nil || int64(len(in.ROM)) != d.ROM.SourceSize {
		return Inspection{}, nil, nil, romInputReceipt{}, errors.New("ROM input requires format 3 and exact source size")
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload, mapping), Descriptor: d}
	receipt := romInputReceipt{Format: 1, PackageID: inspection.PackageID, ROMID: d.ROM.ID, MapSHA256: d.ROM.SHA256, SourceSHA256: romDigest(in.ROM), SourceSize: int64(len(in.ROM))}
	if in.Expansion != nil {
		shell, err := compositionShell(inspection, payload)
		if err != nil {
			return Inspection{}, nil, nil, romInputReceipt{}, err
		}
		if err = expansion.Admit(shell, *in.Expansion); err != nil {
			return Inspection{}, nil, nil, romInputReceipt{}, err
		}
		receipt.ExpansionID = in.Expansion.ID
	}
	return inspection, payload, mapping, receipt, nil
}

// IsROMInput recognizes the first transport member; it does not validate input.
func IsROMInput(data []byte) bool {
	if len(data) < 512 {
		return false
	}
	name := data[:100]
	if end := bytes.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	return string(name) == "rom-link.json"
}

// WriteROMInput validates source bindings and emits a canonical source-only archive.
func WriteROMInput(in ROMInput) ([]byte, error) {
	_, _, _, receipt, err := inspectROMInput(in)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	members := []struct {
		name string
		data []byte
	}{{"rom-link.json", encoded}, {"package.tar", in.Package}, {"rom.bin", in.ROM}}
	if in.Expansion != nil {
		var asset bytes.Buffer
		if err = in.Expansion.Write(&asset); err != nil {
			return nil, err
		}
		members = append(members, struct {
			name string
			data []byte
		}{"expansion.tar", asset.Bytes()})
	}
	var out bytes.Buffer
	for _, member := range members {
		out.Write(canonicalHeader(member.name, int64(len(member.data))))
		out.Write(member.data)
		out.Write(make([]byte, (512-len(member.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	if out.Len() > MaxROMInputSize {
		return nil, errors.New("ROM input exceeds size limit")
	}
	return out.Bytes(), nil
}

func readROMInput(data []byte) (ROMInput, error) {
	if len(data) > MaxROMInputSize || !IsROMInput(data) {
		return ROMInput{}, errors.New("invalid ROM input transport")
	}
	offset := 0
	read := func(name string, limit int64) ([]byte, error) {
		if len(data)-offset < 512 {
			return nil, errors.New("truncated ROM input")
		}
		header := data[offset : offset+512]
		size, err := canonicalSize(header[124:136])
		if err != nil || size < 1 || size > limit || !bytes.Equal(header, canonicalHeader(name, size)) {
			return nil, fmt.Errorf("invalid ROM input member %s", name)
		}
		offset += 512
		padded := int((size + 511) &^ 511)
		if padded > len(data)-offset || !allZero(data[offset+int(size):offset+padded]) {
			return nil, errors.New("invalid ROM input padding or length")
		}
		value := data[offset : offset+int(size)]
		offset += padded
		return value, nil
	}
	receipt, err := read("rom-link.json", 4096)
	if err != nil {
		return ROMInput{}, err
	}
	pkg, err := read("package.tar", MaxArchiveSize)
	if err != nil {
		return ROMInput{}, err
	}
	rom, err := read("rom.bin", 256<<10)
	if err != nil {
		return ROMInput{}, err
	}
	in := ROMInput{Package: pkg, ROM: rom}
	if len(data)-offset != 1024 {
		encoded, err := read("expansion.tar", expansion.MaxArchiveBytes)
		if err != nil {
			return ROMInput{}, err
		}
		asset, err := expansion.ReadAsset(bytes.NewReader(encoded))
		if err != nil {
			return ROMInput{}, err
		}
		in.Expansion = &asset
	}
	if len(data)-offset != 1024 || !allZero(data[offset:]) {
		return ROMInput{}, errors.New("ROM input must end with exactly two zero blocks")
	}
	_, _, _, expected, err := inspectROMInput(in)
	if err != nil {
		return ROMInput{}, err
	}
	canonical, err := json.Marshal(expected)
	if err != nil {
		return ROMInput{}, err
	}
	if !bytes.Equal(receipt, canonical) {
		return ROMInput{}, errors.New("ROM input receipt differs from components")
	}
	return in, nil
}

func linkROMInput(ctx context.Context, in ROMInput) (Inspection, ROMLinkIdentity, []byte, *CompositionBundle, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, ROMLinkIdentity{}, nil, nil, err
	}
	inspection, base, mapping, receipt, err := inspectROMInput(in)
	if err != nil {
		return Inspection{}, ROMLinkIdentity{}, nil, nil, err
	}
	m, err := expansion.ParseROMMap(ctx, mapping, inspection.Descriptor.Payload.SHA256, len(in.ROM))
	if err != nil {
		return Inspection{}, ROMLinkIdentity{}, nil, nil, err
	}
	var programmed []byte
	var bundle *CompositionBundle
	if in.Expansion == nil {
		programmed, err = expansion.LinkROM(ctx, base, m, in.ROM)
	} else {
		shell, shellErr := compositionShell(inspection, base)
		if shellErr != nil {
			return Inspection{}, ROMLinkIdentity{}, nil, nil, shellErr
		}
		identity, overlay, result, linkErr := expansion.ComposeROM(ctx, shell, *in.Expansion, m, in.ROM)
		err = linkErr
		programmed = result
		bundle = &CompositionBundle{Package: in.Package, Asset: *in.Expansion, Composition: identity, Payload: overlay}
	}
	if err != nil {
		return Inspection{}, ROMLinkIdentity{}, nil, nil, err
	}
	identity := ROMLinkIdentity{ROMID: receipt.ROMID, MapSHA256: receipt.MapSHA256, SourceSHA256: receipt.SourceSHA256, SourceSize: receipt.SourceSize, ProgrammedSHA256: romDigest(programmed), ProgrammedSize: int64(len(programmed))}
	return inspection, identity, programmed, bundle, nil
}

// StageROMInput independently links all inputs before publishing private files.
// The original package and composition remain separate from the programmed RBF.
func StageROMInput(ctx context.Context, root string, size int64, reader io.Reader) (Staged, error) {
	if ctx == nil || reader == nil || size < 1 || size > MaxROMInputSize {
		return Staged{}, errors.New("invalid ROM input staging request")
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, size+1))
	if err != nil {
		return Staged{}, err
	}
	if int64(len(data)) != size {
		return Staged{}, errors.New("ROM input size mismatch")
	}
	in, err := readROMInput(data)
	if err != nil {
		return Staged{}, err
	}
	_, identity, programmed, bundle, err := linkROMInput(ctx, in)
	if err != nil {
		return Staged{}, err
	}
	var staged Staged
	if bundle == nil {
		staged, err = Stage(ctx, root, int64(len(in.Package)), bytes.NewReader(in.Package))
	} else {
		staged, err = stageCompositionBundle(ctx, root, *bundle)
	}
	if err != nil {
		return Staged{}, err
	}
	success := false
	defer func() {
		if !success {
			_ = staged.Cleanup()
		}
	}()
	handle, err := os.OpenRoot(root)
	if err != nil {
		return Staged{}, err
	}
	defer handle.Close()
	info, err := handle.Stat(".")
	if err != nil || !os.SameFile(info, staged.rootInfo) {
		return Staged{}, errors.New("ROM staging root changed")
	}
	name := "rom-link-" + staged.publication
	if err = handle.Mkdir(name, 0700); err != nil {
		return Staged{}, err
	}
	retained, err := handle.Lstat(name)
	if err != nil {
		return Staged{}, err
	}
	staged.companions = append(staged.companions, Staged{root: root, rootInfo: info, publication: name, publicationInfo: retained})
	encoded, err := json.Marshal(identity)
	if err != nil {
		return Staged{}, err
	}
	for file, content := range map[string][]byte{"input.tar": data, "programmed.rbf": programmed, "identity.json": encoded} {
		if err = ctx.Err(); err != nil {
			return Staged{}, err
		}
		if err = writePrivate(handle, filepath.Join(name, file), content); err != nil {
			return Staged{}, err
		}
	}
	if err = handle.Chmod(name, 0500); err != nil {
		return Staged{}, err
	}
	if err = ctx.Err(); err != nil {
		return Staged{}, err
	}
	staged.ROMLink = &identity
	staged.ProgrammedPath = filepath.Join(root, name, "programmed.rbf")
	success = true
	return staged, nil
}

func adoptROMInput(handle *os.Root, staged *Staged) error {
	name := "rom-link-" + staged.publication
	info, err := handle.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0500 {
		return errors.New("invalid ROM publication")
	}
	child, err := handle.OpenRoot(name)
	if err != nil {
		return err
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("ROM publication changed")
	}
	directory, err := child.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	directory.Close()
	if err != nil || len(entries) != 3 {
		return errors.New("invalid ROM publication members")
	}
	expected := map[string]int64{"input.tar": MaxROMInputSize, "identity.json": 4096, "programmed.rbf": MaxPayloadSize}
	files := map[string][]byte{}
	for _, entry := range entries {
		limit, ok := expected[entry.Name()]
		if !ok {
			return errors.New("unexpected ROM publication member")
		}
		member, statErr := child.Lstat(entry.Name())
		if statErr != nil || member.Mode().Perm() != 0400 {
			return errors.New("ROM publication member must be immutable and private")
		}
		files[entry.Name()], err = readRootMember(child, entry.Name(), limit)
		if err != nil {
			return err
		}
	}
	var inspection Inspection
	var programmed []byte
	var bundle *CompositionBundle
	var identity any
	if IsROMInputV2(files["input.tar"]) {
		prepared, readErr := readPreparedROMInputV2(files["input.tar"])
		if readErr != nil {
			return readErr
		}
		var links ROMLinksIdentity
		inspection, links, programmed, bundle, err = linkPreparedROMInputV2(context.Background(), prepared)
		identity = links
		staged.ROMLinks = &links
	} else {
		in, readErr := readROMInput(files["input.tar"])
		if readErr != nil {
			return readErr
		}
		var link ROMLinkIdentity
		inspection, link, programmed, bundle, err = linkROMInput(context.Background(), in)
		identity = link
		staged.ROMLink = &link
	}
	if err != nil {
		return err
	}
	if inspection.PackageID != staged.PackageID {
		return errors.New("retained ROM package differs from publication")
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	if !bytes.Equal(encoded, files["identity.json"]) || !bytes.Equal(programmed, files["programmed.rbf"]) {
		return errors.New("retained ROM identity or programmed bytes differ from independently linked input")
	}
	if (bundle == nil) != (staged.Composition == nil) {
		return errors.New("retained ROM expansion differs from publication")
	}
	if bundle != nil && bundle.Composition != *staged.Composition {
		return errors.New("retained ROM composition differs from publication")
	}
	after, err := handle.Lstat(name)
	if err != nil || !os.SameFile(info, after) {
		return errors.New("ROM publication changed while adopting")
	}
	staged.companions = append(staged.companions, Staged{root: staged.root, rootInfo: staged.rootInfo, publication: name, publicationInfo: info})
	staged.ProgrammedPath = filepath.Join(staged.root, name, "programmed.rbf")
	return nil
}
