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
	"reflect"
	"sort"

	"github.com/DeanoC/misteross/expansion"
)

// MaxROMInputSize bounds the package, source ROM, one optional single-socket
// expansion or up to MaxSlotCards slot cards, and framing.
const MaxROMInputSize = MaxArchiveSize + MaxSlotCards*expansion.MaxArchiveBytes + (256 << 10) + 16384

// ROMInput transports source bytes only. The target derives all programmed bytes
// using the map sealed inside Package; callers cannot supply a map or bitstream.
type ROMInput struct {
	Package   []byte
	ROM       []byte
	Expansion *expansion.Asset
	// SlotExpansions are cards for a multi-socket shell, at most one per
	// physical slot. They exclude the single-socket Expansion.
	SlotExpansions []expansion.Asset
	// Parts carries the ST raster video and optional cartridge, excluding both CPU-only forms.
	Parts []expansion.Asset
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

// romInputReceipt binds the transported sources. With slot cards it also
// carries the sender's link evidence: the v2 composition tuple and the
// programmed digest, which the receiver compares after relinking.
type romInputReceipt struct {
	Format           int                         `json:"format"`
	PackageID        string                      `json:"package_id"`
	ROMID            string                      `json:"rom_id"`
	MapSHA256        string                      `json:"map_sha256"`
	SourceSHA256     string                      `json:"source_sha256"`
	SourceSize       int64                       `json:"source_size"`
	ExpansionID      string                      `json:"expansion_id,omitempty"`
	SlotExpansions   []expansion.SlotExpansion   `json:"slot_expansions,omitempty"`
	Composition      *expansion.SlotComposition  `json:"composition,omitempty"`
	Parts            []expansion.PartSelection   `json:"parts,omitempty"`
	PartsComposition *expansion.PartsComposition `json:"parts_composition,omitempty"`
	ProgrammedSHA256 string                      `json:"programmed_sha256,omitempty"`
	ProgrammedSize   int64                       `json:"programmed_size,omitempty"`
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
	if (in.Expansion != nil && len(in.SlotExpansions) != 0) || (len(in.Parts) != 0 && (in.Expansion != nil || len(in.SlotExpansions) != 0)) {
		return Inspection{}, nil, nil, romInputReceipt{}, errors.New("ROM input carries either one expansion or slot cards")
	}
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
	if len(in.Parts) != 0 {
		shell, err := PartsShell(inspection, payload)
		if err != nil || shell.Layout != expansion.AtariStVideoLayout {
			return Inspection{}, nil, nil, romInputReceipt{}, errors.New("ROM parts require the ST raster shell")
		}
		if err := expansion.AdmitParts(shell, in.Parts); err != nil {
			return Inspection{}, nil, nil, romInputReceipt{}, err
		}
		for _, asset := range orderedROMParts(in.Parts) {
			receipt.Parts = append(receipt.Parts, expansion.PartSelection{Role: partRole(asset), PartID: asset.ID})
		}
	}
	if len(in.SlotExpansions) != 0 {
		if len(in.SlotExpansions) > MaxSlotCards {
			return Inspection{}, nil, nil, romInputReceipt{}, errors.New("too many slot cards")
		}
		shell, err := slotCompositionShell(inspection, payload)
		if err != nil {
			return Inspection{}, nil, nil, romInputReceipt{}, err
		}
		if err = expansion.AdmitSlots(shell, in.SlotExpansions); err != nil {
			return Inspection{}, nil, nil, romInputReceipt{}, err
		}
		receipt.SlotExpansions = slotExpansions(in.SlotExpansions)
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

// WriteROMInput validates source bindings and emits a canonical archive of
// the sources. Slot cards are linked here as well, and the receipt carries
// that closed evidence for the receiver's independent relink.
func WriteROMInput(in ROMInput) ([]byte, error) {
	return WriteROMInputContext(context.Background(), in)
}

func WriteROMInputContext(ctx context.Context, in ROMInput) ([]byte, error) {
	transport, err := PrepareROMInput(ctx, in)
	return transport.Data, err
}

// ROMTransport is a closed ROM input and, for slot cards, the v2 composition
// the sender linked. The receiver relinks and must reproduce it exactly.
type ROMTransport struct {
	Data             []byte
	SlotComposition  *expansion.SlotComposition
	PartsComposition *expansion.PartsComposition
}

func PrepareROMInput(ctx context.Context, in ROMInput) (ROMTransport, error) {
	_, _, _, receipt, err := inspectROMInput(in)
	if err != nil {
		return ROMTransport{}, err
	}
	if len(in.SlotExpansions) != 0 || len(in.Parts) != 0 {
		linked, err := linkROMInput(ctx, in, romInputReceipt{})
		if err != nil {
			return ROMTransport{}, err
		}
		receipt = linked.receipt
	}
	data, err := writeROMInputReceipt(in, receipt)
	if err != nil {
		return ROMTransport{}, err
	}
	return ROMTransport{Data: data, SlotComposition: receipt.Composition, PartsComposition: receipt.PartsComposition}, nil
}

func orderedROMParts(parts []expansion.Asset) []expansion.Asset {
	ordered := append([]expansion.Asset(nil), parts...)
	sort.Slice(ordered, func(i, j int) bool { return partRole(ordered[i]) < partRole(ordered[j]) })
	return ordered
}

func writeROMInputReceipt(in ROMInput, receipt romInputReceipt) ([]byte, error) {
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	members := []canonicalMember{{"rom-link.json", encoded}, {"package.tar", in.Package}, {"rom.bin", in.ROM}}
	if in.Expansion != nil {
		var asset bytes.Buffer
		if err = in.Expansion.Write(&asset); err != nil {
			return nil, err
		}
		members = append(members, canonicalMember{"expansion.tar", asset.Bytes()})
	}
	cards, err := slotAssetMembers(in.SlotExpansions)
	if err != nil {
		return nil, err
	}
	for _, asset := range orderedROMParts(in.Parts) {
		var encoded bytes.Buffer
		if err := asset.Write(&encoded); err != nil {
			return nil, err
		}
		cards = append(cards, canonicalMember{"part-" + partRole(asset) + ".tar", encoded.Bytes()})
	}
	out, err := writeCanonicalMembers(append(members, cards...), MaxROMInputSize)
	if err != nil {
		return nil, errors.New("ROM input exceeds size limit")
	}
	return out, nil
}

// readROMInput validates closed framing and the source receipt. Slot link
// evidence is returned for comparison after the receiver relinks.
func readROMInput(data []byte) (ROMInput, romInputReceipt, error) {
	if len(data) > MaxROMInputSize || !IsROMInput(data) {
		return ROMInput{}, romInputReceipt{}, errors.New("invalid ROM input transport")
	}
	reader := &canonicalReader{data: data}
	receiptBytes, err := reader.read("rom-link.json", 4096)
	if err != nil {
		return ROMInput{}, romInputReceipt{}, err
	}
	pkg, err := reader.read("package.tar", MaxArchiveSize)
	if err != nil {
		return ROMInput{}, romInputReceipt{}, err
	}
	rom, err := reader.read("rom.bin", 256<<10)
	if err != nil {
		return ROMInput{}, romInputReceipt{}, err
	}
	in := ROMInput{Package: pkg, ROM: rom}
	if reader.peek() == "expansion.tar" {
		encoded, err := reader.read("expansion.tar", expansion.MaxArchiveBytes)
		if err != nil {
			return ROMInput{}, romInputReceipt{}, err
		}
		asset, err := expansion.ReadAsset(bytes.NewReader(encoded))
		if err != nil {
			return ROMInput{}, romInputReceipt{}, err
		}
		in.Expansion = &asset
	} else if reader.peek() == "part-video.tar" || reader.peek() == "part-expansion.tar" {
		for _, role := range []string{expansion.PartRoleExpansion, expansion.PartRoleVideo} {
			if reader.peek() != "part-"+role+".tar" {
				continue
			}
			encoded, err := reader.read("part-"+role+".tar", expansion.MaxArchiveBytes)
			if err != nil {
				return ROMInput{}, romInputReceipt{}, err
			}
			asset, err := expansion.ReadAsset(bytes.NewReader(encoded))
			if err != nil || partRole(asset) != role {
				return ROMInput{}, romInputReceipt{}, errors.New("ROM part role differs from member")
			}
			in.Parts = append(in.Parts, asset)
		}
	} else if !reader.done() {
		in.SlotExpansions, err = reader.readSlotAssets()
		if err != nil {
			return ROMInput{}, romInputReceipt{}, err
		}
	}
	if err := reader.finish(); err != nil {
		return ROMInput{}, romInputReceipt{}, errors.New("ROM input must end with exactly two zero blocks")
	}
	_, _, _, expected, err := inspectROMInput(in)
	if err != nil {
		return ROMInput{}, romInputReceipt{}, err
	}
	if len(in.SlotExpansions) != 0 || len(in.Parts) != 0 {
		var sent romInputReceipt
		decoder := json.NewDecoder(bytes.NewReader(receiptBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&sent); err != nil {
			return ROMInput{}, romInputReceipt{}, errors.New("ROM input receipt is invalid")
		}
		if ((len(in.Parts) == 0 && sent.Composition == nil) || (len(in.Parts) != 0 && sent.PartsComposition == nil)) || sent.ProgrammedSHA256 == "" || sent.ProgrammedSize < 1 {
			return ROMInput{}, romInputReceipt{}, errors.New("slot ROM input requires link evidence")
		}
		expected.Composition, expected.PartsComposition, expected.ProgrammedSHA256, expected.ProgrammedSize = sent.Composition, sent.PartsComposition, sent.ProgrammedSHA256, sent.ProgrammedSize
	}
	canonical, err := json.Marshal(expected)
	if err != nil {
		return ROMInput{}, romInputReceipt{}, err
	}
	if !bytes.Equal(receiptBytes, canonical) {
		return ROMInput{}, romInputReceipt{}, errors.New("ROM input receipt differs from components")
	}
	return in, expected, nil
}

type romLinkResult struct {
	inspection Inspection
	identity   ROMLinkIdentity
	programmed []byte
	bundle     *CompositionBundle
	slots      *SlotCompositionBundle
	parts      *PartsBundle
	receipt    romInputReceipt
}

// linkROMInput derives every programmed byte from the sealed map. A
// multi-socket shell always links through ComposeSlotsROM, which also rejects
// ROM destinations inside any socket. Receipt link evidence, when present,
// must equal this independent result.
func linkROMInput(ctx context.Context, in ROMInput, sent romInputReceipt) (romLinkResult, error) {
	if err := ctx.Err(); err != nil {
		return romLinkResult{}, err
	}
	inspection, base, mapping, receipt, err := inspectROMInput(in)
	if err != nil {
		return romLinkResult{}, err
	}
	m, err := expansion.ParseROMMap(ctx, mapping, inspection.Descriptor.Payload.SHA256, len(in.ROM))
	if err != nil {
		return romLinkResult{}, err
	}
	if shell, shellErr := PartsShell(inspection, base); shellErr == nil && shell.Layout == expansion.AtariStVideoLayout {
		if err := expansion.ValidatePartsROMDestinations(ctx, shell.Layout, m); err != nil {
			return romLinkResult{}, err
		}
	}
	result := romLinkResult{inspection: inspection}
	slotShell, slotErr := slotCompositionShell(inspection, base)
	switch {
	case len(in.Parts) != 0:
		shell, err := PartsShell(inspection, base)
		if err != nil {
			return romLinkResult{}, err
		}
		final, overlay, programmed, err := expansion.ComposePartsROM(ctx, shell, in.Parts, m, in.ROM)
		if err != nil {
			return romLinkResult{}, err
		}
		final.PayloadSHA256, final.PayloadSize = romDigest(overlay), int64(len(overlay))
		final.ID, err = expansion.PartsCompositionID(final.PackageID, final.Layout, final.Parts, final.PayloadSHA256)
		if err != nil {
			return romLinkResult{}, err
		}
		result.programmed = programmed
		result.parts = &PartsBundle{Package: in.Package, Assets: orderedROMParts(in.Parts), Composition: final, Payload: overlay}
		receipt.PartsComposition = &final
		receipt.ProgrammedSHA256, receipt.ProgrammedSize = romDigest(programmed), int64(len(programmed))
	case slotErr == nil:
		composition, overlay, programmed, linkErr := expansion.ComposeSlotsROM(ctx, slotShell, in.SlotExpansions, m, in.ROM)
		if linkErr != nil {
			return romLinkResult{}, linkErr
		}
		result.programmed = programmed
		if len(in.SlotExpansions) != 0 {
			result.slots = &SlotCompositionBundle{Package: in.Package, Assets: sortedSlotAssets(in.SlotExpansions), Composition: composition, Payload: overlay}
			receipt.Composition = &composition
			receipt.ProgrammedSHA256, receipt.ProgrammedSize = romDigest(programmed), int64(len(programmed))
		}
	case len(in.SlotExpansions) != 0:
		return romLinkResult{}, slotErr
	case in.Expansion == nil:
		result.programmed, err = expansion.LinkROM(ctx, base, m, in.ROM)
		if err != nil {
			return romLinkResult{}, err
		}
	default:
		shell, shellErr := compositionShell(inspection, base)
		if shellErr != nil {
			return romLinkResult{}, shellErr
		}
		identity, overlay, programmed, linkErr := expansion.ComposeROM(ctx, shell, *in.Expansion, m, in.ROM)
		if linkErr != nil {
			return romLinkResult{}, linkErr
		}
		result.programmed = programmed
		result.bundle = &CompositionBundle{Package: in.Package, Asset: *in.Expansion, Composition: identity, Payload: overlay}
	}
	if sent.Composition != nil || sent.PartsComposition != nil || sent.ProgrammedSHA256 != "" || sent.ProgrammedSize != 0 {
		if !reflect.DeepEqual(sent.Composition, receipt.Composition) || !reflect.DeepEqual(sent.PartsComposition, receipt.PartsComposition) ||
			sent.ProgrammedSHA256 != receipt.ProgrammedSHA256 || sent.ProgrammedSize != receipt.ProgrammedSize {
			return romLinkResult{}, errors.New("slot ROM link evidence differs from the independent link")
		}
	}
	result.receipt = receipt
	result.identity = ROMLinkIdentity{ROMID: receipt.ROMID, MapSHA256: receipt.MapSHA256, SourceSHA256: receipt.SourceSHA256, SourceSize: receipt.SourceSize, ProgrammedSHA256: romDigest(result.programmed), ProgrammedSize: int64(len(result.programmed))}
	return result, nil
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
	in, sent, err := readROMInput(data)
	if err != nil {
		return Staged{}, err
	}
	linked, err := linkROMInput(ctx, in, sent)
	if err != nil {
		return Staged{}, err
	}
	identity, programmed := linked.identity, linked.programmed
	var staged Staged
	switch {
	case linked.parts != nil:
		staged, err = stagePartsBundle(ctx, root, *linked.parts)
	case linked.bundle != nil:
		staged, err = stageCompositionBundle(ctx, root, *linked.bundle)
	case linked.slots != nil:
		staged, err = stageSlotCompositionBundle(ctx, root, *linked.slots)
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
	var slots *SlotCompositionBundle
	var parts *PartsBundle
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
		in, sent, readErr := readROMInput(files["input.tar"])
		if readErr != nil {
			return readErr
		}
		linked, linkErr := linkROMInput(context.Background(), in, sent)
		if linkErr != nil {
			return linkErr
		}
		inspection, programmed, bundle, slots, parts = linked.inspection, linked.programmed, linked.bundle, linked.slots, linked.parts
		link := linked.identity
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
	if (slots == nil) != (staged.SlotComposition == nil) ||
		(slots != nil && !equalSlotComposition(slots.Composition, *staged.SlotComposition)) {
		return errors.New("retained ROM slot composition differs from publication")
	}
	if (parts == nil) != (staged.PartsComposition == nil) || (parts != nil && !reflect.DeepEqual(parts.Composition, *staged.PartsComposition)) {
		return errors.New("retained ROM parts composition differs from publication")
	}

	after, err := handle.Lstat(name)
	if err != nil || !os.SameFile(info, after) {
		return errors.New("ROM publication changed while adopting")
	}
	staged.companions = append(staged.companions, Staged{root: staged.root, rootInfo: staged.rootInfo, publication: name, publicationInfo: info})
	staged.ProgrammedPath = filepath.Join(staged.root, name, "programmed.rbf")
	return nil
}
