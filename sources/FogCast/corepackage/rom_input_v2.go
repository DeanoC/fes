package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/misteross/expansion"
)

// ROMInputV2 carries two private sources; the target obtains the map and base
// RBF only from the sealed package and derives every programmed byte itself.
type ROMInputV2 struct {
	Package   []byte
	BIOS      []byte
	Cartridge []byte
	Expansion *expansion.Asset
}

type ROMSourceIdentity struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	SourceSHA256 string `json:"source_sha256"`
	SourceSize   int64  `json:"source_size"`
}

type ROMLinksIdentity struct {
	Sources          []ROMSourceIdentity `json:"sources"`
	MapSHA256        string              `json:"map_sha256"`
	ProgrammedSHA256 string              `json:"programmed_sha256"`
	ProgrammedSize   int64               `json:"programmed_size"`
}

func (r ROMLinksIdentity) ValidFor(d Descriptor) bool {
	if d.Format != 4 || d.ROMMap == nil || len(d.ROMs) != 2 || len(r.Sources) != 2 ||
		r.MapSHA256 != d.ROMMap.SHA256 || !hex64RE.MatchString(r.MapSHA256) ||
		!hex64RE.MatchString(r.ProgrammedSHA256) || r.ProgrammedSize < 1 || r.ProgrammedSize > MaxPayloadSize {
		return false
	}
	for i, source := range r.Sources {
		if source.ID != d.ROMs[i].ID || source.Role != d.ROMs[i].Role ||
			source.SourceSize != d.ROMs[i].SourceSize || !hex64RE.MatchString(source.SourceSHA256) {
			return false
		}
	}
	return true
}

type romInputReceiptV2 struct {
	Format      int                 `json:"format"`
	PackageID   string              `json:"package_id"`
	Sources     []ROMSourceIdentity `json:"sources"`
	MapSHA256   string              `json:"map_sha256"`
	ExpansionID string              `json:"expansion_id,omitempty"`
}

// Validation results stay private to one operation; they are never cached
// across requests or accepted from a caller.
type preparedROMInputV2 struct {
	input         ROMInputV2
	inspection    Inspection
	base, mapping []byte
	receipt       romInputReceiptV2
	romMap        *expansion.ROMMap
}

func inspectROMInputV2(in ROMInputV2) (Inspection, []byte, []byte, romInputReceiptV2, error) {
	p, err := prepareROMInputV2(in)
	return p.inspection, p.base, p.mapping, p.receipt, err
}

func prepareROMInputV2(in ROMInputV2) (preparedROMInputV2, error) {
	manifest, payload, mapping, err := readArchive(in.Package)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	d, parsed, err := decodeWithROMMap(manifest, payload, mapping)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	if d.Format != 4 || len(d.ROMs) != 2 || d.ROMMap == nil ||
		int64(len(in.BIOS)) != d.ROMs[0].SourceSize || int64(len(in.Cartridge)) != d.ROMs[1].SourceSize {
		return preparedROMInputV2{}, errors.New("two-source ROM input requires format 4 and exact BIOS/cartridge sizes")
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload, mapping), Descriptor: d}
	receipt := romInputReceiptV2{Format: 2, PackageID: inspection.PackageID, MapSHA256: d.ROMMap.SHA256,
		Sources: []ROMSourceIdentity{
			{ID: d.ROMs[0].ID, Role: d.ROMs[0].Role, SourceSHA256: romDigest(in.BIOS), SourceSize: int64(len(in.BIOS))},
			{ID: d.ROMs[1].ID, Role: d.ROMs[1].Role, SourceSHA256: romDigest(in.Cartridge), SourceSize: int64(len(in.Cartridge))},
		}}
	if in.Expansion != nil {
		shell, err := compositionShell(inspection, payload)
		if err != nil {
			return preparedROMInputV2{}, err
		}
		if err := expansion.Admit(shell, *in.Expansion); err != nil {
			return preparedROMInputV2{}, err
		}
		receipt.ExpansionID = in.Expansion.ID
	}
	return preparedROMInputV2{in, inspection, payload, mapping, receipt, parsed}, nil
}

func IsROMInputV2(data []byte) bool {
	if len(data) < 512 {
		return false
	}
	name := data[:100]
	if end := bytes.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	return string(name) == "rom-link-v2.json"
}

func WriteROMInputV2(in ROMInputV2) ([]byte, error) {
	_, _, _, receipt, err := inspectROMInputV2(in)
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
	}{
		{"rom-link-v2.json", encoded}, {"package.tar", in.Package}, {"bios.bin", in.BIOS}, {"cartridge.bin", in.Cartridge},
	}
	if in.Expansion != nil {
		var asset bytes.Buffer
		if err := in.Expansion.Write(&asset); err != nil {
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
		return nil, errors.New("two-source ROM input exceeds size limit")
	}
	return out.Bytes(), nil
}

func readPreparedROMInputV2(data []byte) (preparedROMInputV2, error) {
	if len(data) > MaxROMInputSize || !IsROMInputV2(data) {
		return preparedROMInputV2{}, errors.New("invalid two-source ROM transport")
	}
	offset := 0
	read := func(name string, limit int64) ([]byte, error) {
		if len(data)-offset < 512 {
			return nil, errors.New("truncated ROM input")
		}
		header := data[offset : offset+512]
		size, err := canonicalSize(header[124:136])
		if err != nil || size < 1 || size > limit || !bytes.Equal(header, canonicalHeader(name, size)) {
			return nil, fmt.Errorf("invalid ROM member %s", name)
		}
		offset += 512
		padded := int((size + 511) &^ 511)
		if padded > len(data)-offset || !allZero(data[offset+int(size):offset+padded]) {
			return nil, errors.New("invalid ROM member padding or length")
		}
		value := data[offset : offset+int(size)]
		offset += padded
		return value, nil
	}
	receipt, err := read("rom-link-v2.json", 4096)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	pkg, err := read("package.tar", MaxArchiveSize)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	bios, err := read("bios.bin", 256<<10)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	cart, err := read("cartridge.bin", 256<<10)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	in := ROMInputV2{Package: pkg, BIOS: bios, Cartridge: cart}
	if len(data)-offset != 1024 {
		encoded, err := read("expansion.tar", expansion.MaxArchiveBytes)
		if err != nil {
			return preparedROMInputV2{}, err
		}
		asset, err := expansion.ReadAsset(bytes.NewReader(encoded))
		if err != nil {
			return preparedROMInputV2{}, err
		}
		in.Expansion = &asset
	}
	if len(data)-offset != 1024 || !allZero(data[offset:]) {
		return preparedROMInputV2{}, errors.New("ROM input must end with two zero blocks")
	}
	prepared, err := prepareROMInputV2(in)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	canonical, err := json.Marshal(prepared.receipt)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	if !bytes.Equal(receipt, canonical) {
		return preparedROMInputV2{}, errors.New("two-source receipt differs from components")
	}
	return prepared, nil
}

func linkPreparedROMInputV2(ctx context.Context, prepared preparedROMInputV2) (Inspection, ROMLinksIdentity, []byte, *CompositionBundle, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, ROMLinksIdentity{}, nil, nil, err
	}
	inspection, base, receipt := prepared.inspection, prepared.base, prepared.receipt
	in := prepared.input
	source := append(append(make([]byte, 0, len(in.BIOS)+len(in.Cartridge)), in.BIOS...), in.Cartridge...)
	m := *prepared.romMap
	var err error
	var programmed []byte
	var bundle *CompositionBundle
	if in.Expansion == nil {
		programmed, err = expansion.LinkROM(ctx, base, m, source)
	} else {
		shell, shellErr := compositionShell(inspection, base)
		if shellErr != nil {
			return Inspection{}, ROMLinksIdentity{}, nil, nil, shellErr
		}
		identity, overlay, result, linkErr := expansion.ComposeROM(ctx, shell, *in.Expansion, m, source)
		err = linkErr
		programmed = result
		bundle = &CompositionBundle{Package: in.Package, Asset: *in.Expansion, Composition: identity, Payload: overlay}
	}
	if err != nil {
		return Inspection{}, ROMLinksIdentity{}, nil, nil, err
	}
	identity := ROMLinksIdentity{Sources: receipt.Sources, MapSHA256: receipt.MapSHA256, ProgrammedSHA256: romDigest(programmed), ProgrammedSize: int64(len(programmed))}
	if !identity.ValidFor(inspection.Descriptor) {
		return Inspection{}, ROMLinksIdentity{}, nil, nil, errors.New("invalid linked source identity")
	}
	return inspection, identity, programmed, bundle, nil
}

// StageROMInputV2 verifies and links both sources before publishing any package.
func StageROMInputV2(ctx context.Context, root string, size int64, reader io.Reader) (Staged, error) {
	if ctx == nil || reader == nil || size < 1 || size > MaxROMInputSize {
		return Staged{}, errors.New("invalid two-source staging request")
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, size+1))
	if err != nil {
		return Staged{}, err
	}
	if int64(len(data)) != size {
		return Staged{}, errors.New("two-source input size mismatch")
	}
	prepared, err := readPreparedROMInputV2(data)
	if err != nil {
		return Staged{}, err
	}
	_, identity, programmed, bundle, err := linkPreparedROMInputV2(ctx, prepared)
	if err != nil {
		return Staged{}, err
	}
	in := prepared.input
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
	staged.ROMLinks = &identity
	staged.ProgrammedPath = filepath.Join(root, name, "programmed.rbf")
	success = true
	return staged, nil
}
