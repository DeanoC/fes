package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/DeanoC/misteross/expansion"
)

// ROMInputV2 carries two private sources; the target obtains the map and base
// RBF only from the sealed package and derives every programmed byte itself.
type ROMInputV2 struct {
	Package   []byte
	BIOS      []byte
	Cartridge []byte
	Expansion *expansion.Asset
	// Parts carries the Coleco video part and an optional CPU part. It
	// excludes the single-socket Expansion.
	Parts []expansion.Asset
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

// romInputReceiptV2 binds the transported sources. With parts it also carries
// the sender's link evidence, which the receiver compares after relinking.
type romInputReceiptV2 struct {
	Format           int                         `json:"format"`
	PackageID        string                      `json:"package_id"`
	Sources          []ROMSourceIdentity         `json:"sources"`
	MapSHA256        string                      `json:"map_sha256"`
	ExpansionID      string                      `json:"expansion_id,omitempty"`
	Parts            []expansion.PartSelection   `json:"parts,omitempty"`
	PartsComposition *expansion.PartsComposition `json:"parts_composition,omitempty"`
	ProgrammedSHA256 string                      `json:"programmed_sha256,omitempty"`
	ProgrammedSize   int64                       `json:"programmed_size,omitempty"`
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
	if in.Expansion != nil && len(in.Parts) != 0 {
		return preparedROMInputV2{}, errors.New("two-source ROM input carries either one expansion or parts")
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
	if len(in.Parts) != 0 {
		receipt.Parts, err = admitROMParts(inspection, payload, in.Parts)
		if err != nil {
			return preparedROMInputV2{}, err
		}
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
	transport, err := PrepareROMInputV2(context.Background(), in)
	return transport.Data, err
}

// PrepareROMInputV2 validates both sources and emits a canonical archive.
// Parts are linked here as well; the receipt carries that closed evidence
// for the receiver's independent relink.
func PrepareROMInputV2(ctx context.Context, in ROMInputV2) (ROMTransport, error) {
	if ctx == nil {
		return ROMTransport{}, errors.New("missing ROM input context")
	}
	prepared, err := prepareROMInputV2(in)
	if err != nil {
		return ROMTransport{}, err
	}
	receipt := prepared.receipt
	if len(in.Parts) != 0 {
		linked, err := linkPreparedROMInputV2(ctx, prepared)
		if err != nil {
			return ROMTransport{}, err
		}
		receipt = linked.receipt
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return ROMTransport{}, err
	}
	members := []canonicalMember{
		{"rom-link-v2.json", encoded}, {"package.tar", in.Package}, {"bios.bin", in.BIOS}, {"cartridge.bin", in.Cartridge},
	}
	if in.Expansion != nil {
		var asset bytes.Buffer
		if err := in.Expansion.Write(&asset); err != nil {
			return ROMTransport{}, err
		}
		members = append(members, canonicalMember{"expansion.tar", asset.Bytes()})
	}
	parts, err := romPartMembers(in.Parts)
	if err != nil {
		return ROMTransport{}, err
	}
	data, err := writeCanonicalMembers(append(members, parts...), MaxROMInputSize)
	if err != nil {
		return ROMTransport{}, errors.New("two-source ROM input exceeds size limit")
	}
	return ROMTransport{Data: data, PartsComposition: receipt.PartsComposition}, nil
}

func readPreparedROMInputV2(data []byte) (preparedROMInputV2, error) {
	if len(data) > MaxROMInputSize || !IsROMInputV2(data) {
		return preparedROMInputV2{}, errors.New("invalid two-source ROM transport")
	}
	reader := &canonicalReader{data: data}
	receiptBytes, err := reader.read("rom-link-v2.json", 4096)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	pkg, err := reader.read("package.tar", MaxArchiveSize)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	bios, err := reader.read("bios.bin", 256<<10)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	cart, err := reader.read("cartridge.bin", 256<<10)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	in := ROMInputV2{Package: pkg, BIOS: bios, Cartridge: cart}
	var sent romInputReceiptV2
	decoder := json.NewDecoder(bytes.NewReader(receiptBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sent); err != nil {
		return preparedROMInputV2{}, errors.New("two-source receipt is invalid")
	}
	if reader.peek() == "expansion.tar" {
		encoded, err := reader.read("expansion.tar", expansion.MaxArchiveBytes)
		if err != nil {
			return preparedROMInputV2{}, err
		}
		asset, err := expansion.ReadAsset(bytes.NewReader(encoded))
		if err != nil {
			return preparedROMInputV2{}, err
		}
		in.Expansion = &asset
	} else if reader.hasROMParts() {
		in.Parts, err = reader.readROMParts()
		if err != nil {
			return preparedROMInputV2{}, err
		}
	}
	if err := reader.finish(); err != nil {
		return preparedROMInputV2{}, errors.New("ROM input must end with two zero blocks")
	}
	prepared, err := prepareROMInputV2(in)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	expected := prepared.receipt
	if len(in.Parts) != 0 {
		if sent.PartsComposition == nil || sent.ProgrammedSHA256 == "" || sent.ProgrammedSize < 1 {
			return preparedROMInputV2{}, errors.New("two-source parts input requires link evidence")
		}
		expected.PartsComposition, expected.ProgrammedSHA256, expected.ProgrammedSize = sent.PartsComposition, sent.ProgrammedSHA256, sent.ProgrammedSize
	}
	canonical, err := json.Marshal(expected)
	if err != nil {
		return preparedROMInputV2{}, err
	}
	if !bytes.Equal(receiptBytes, canonical) {
		return preparedROMInputV2{}, errors.New("two-source receipt differs from components")
	}
	prepared.receipt = expected
	return prepared, nil
}

type romLinksResult struct {
	inspection Inspection
	identity   ROMLinksIdentity
	programmed []byte
	bundle     *CompositionBundle
	parts      *PartsBundle
	receipt    romInputReceiptV2
}

// linkPreparedROMInputV2 derives every programmed byte from the sealed map.
// Receipt link evidence, when present, must equal this independent result.
func linkPreparedROMInputV2(ctx context.Context, prepared preparedROMInputV2) (romLinksResult, error) {
	if err := ctx.Err(); err != nil {
		return romLinksResult{}, err
	}
	inspection, base, receipt := prepared.inspection, prepared.base, prepared.receipt
	sent := receipt
	receipt.PartsComposition, receipt.ProgrammedSHA256, receipt.ProgrammedSize = nil, "", 0
	in := prepared.input
	source := append(append(make([]byte, 0, len(in.BIOS)+len(in.Cartridge)), in.BIOS...), in.Cartridge...)
	m := *prepared.romMap
	if err := validateROMPartsDestinations(ctx, inspection, base, m); err != nil {
		return romLinksResult{}, err
	}
	result := romLinksResult{inspection: inspection}
	var err error
	switch {
	case len(in.Parts) != 0:
		var parts *PartsBundle
		parts, result.programmed, err = linkROMParts(ctx, inspection, in.Package, base, in.Parts, m, source)
		if err != nil {
			return romLinksResult{}, err
		}
		result.parts = parts
		final := parts.Composition
		receipt.PartsComposition = &final
		receipt.ProgrammedSHA256, receipt.ProgrammedSize = romDigest(result.programmed), int64(len(result.programmed))
	case in.Expansion == nil:
		result.programmed, err = expansion.LinkROM(ctx, base, m, source)
		if err != nil {
			return romLinksResult{}, err
		}
	default:
		shell, err := compositionShell(inspection, base)
		if err != nil {
			return romLinksResult{}, err
		}
		identity, overlay, programmed, err := expansion.ComposeROM(ctx, shell, *in.Expansion, m, source)
		if err != nil {
			return romLinksResult{}, err
		}
		result.programmed = programmed
		result.bundle = &CompositionBundle{Package: in.Package, Asset: *in.Expansion, Composition: identity, Payload: overlay}
	}
	if sent.PartsComposition != nil || sent.ProgrammedSHA256 != "" || sent.ProgrammedSize != 0 {
		if !reflect.DeepEqual(sent.PartsComposition, receipt.PartsComposition) ||
			sent.ProgrammedSHA256 != receipt.ProgrammedSHA256 || sent.ProgrammedSize != receipt.ProgrammedSize {
			return romLinksResult{}, errors.New("two-source parts link evidence differs from the independent link")
		}
	}
	result.receipt = receipt
	result.identity = ROMLinksIdentity{Sources: receipt.Sources, MapSHA256: receipt.MapSHA256, ProgrammedSHA256: romDigest(result.programmed), ProgrammedSize: int64(len(result.programmed))}
	if !result.identity.ValidFor(inspection.Descriptor) {
		return romLinksResult{}, errors.New("invalid linked source identity")
	}
	return result, nil
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
	linked, err := linkPreparedROMInputV2(ctx, prepared)
	if err != nil {
		return Staged{}, err
	}
	identity, programmed := linked.identity, linked.programmed
	in := prepared.input
	var staged Staged
	switch {
	case linked.parts != nil:
		staged, err = stagePartsBundle(ctx, root, *linked.parts)
	case linked.bundle != nil:
		staged, err = stageCompositionBundle(ctx, root, *linked.bundle)
	default:
		staged, err = Stage(ctx, root, int64(len(in.Package)), bytes.NewReader(in.Package))
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
