package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DeanoC/misteross/expansion"
)

// MaxSlotCards bounds the cards one transport may carry: the physical socket
// count of the largest multi-socket layout (Apple II slots 2, 4, 5 and 7).
const MaxSlotCards = 4

// SlotDirectory names one staged card publication for a physical slot. The
// directory holds exactly manifest.json and cart.rbf.
type SlotDirectory struct {
	Slot      int    `json:"slot"`
	Directory string `json:"directory"`
}

// SlotCompositionBundle retains the original sealed multi-socket shell and
// its independent cards in ascending slot order. The linked overlay and the
// composition tuple are transport evidence; each consumer recomputes them.
type SlotCompositionBundle struct {
	Package     []byte
	Assets      []expansion.Asset
	Composition expansion.SlotComposition
	Payload     []byte
}

// slotCompositionShell admits the multi-socket bus of an exact fes.computer
// 1.0 shell. The single-socket ZX81 and Coleco buses never qualify.
func slotCompositionShell(inspection Inspection, payload []byte) (expansion.Shell, error) {
	d := inspection.Descriptor
	if d.ABI.ID != "fes.computer" || d.ABI.Major != 1 || d.ABI.Minor != 0 {
		return expansion.Shell{}, errors.New("slot composition requires the fes.computer 1.0 ABI")
	}
	found := false
	for _, i := range d.Interfaces {
		switch i.ID {
		case expansion.Apple2Slot:
			if found || i.Required || i.Major != 1 || i.Minor != 0 {
				return expansion.Shell{}, errors.New("slot composition requires one optional fes.expansion.apple2-bus 1.0")
			}
			found = true
		case expansion.Slot, expansion.ColecoSlot:
			return expansion.Shell{}, errors.New("slot composition shell must not declare a single-socket bus")
		}
	}
	if !found {
		return expansion.Shell{}, errors.New("slot composition requires one optional fes.expansion.apple2-bus 1.0")
	}
	return expansion.Shell{PackageID: inspection.PackageID, BuildID: d.Build.ID, Payload: payload, Slot: expansion.Apple2Slot, SlotMajor: 1}, nil
}

// SlotSockets lists the physical sockets of a descriptor's multi-socket bus in
// ascending order, or nil when the package has none.
func SlotSockets(d Descriptor) []int {
	if _, err := slotCompositionShell(Inspection{Descriptor: d}, nil); err != nil {
		return nil
	}
	return expansion.SlotSockets(expansion.Apple2Slot, expansion.Apple2Map)
}

func sortedSlotAssets(assets []expansion.Asset) []expansion.Asset {
	ordered := append([]expansion.Asset(nil), assets...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Manifest.SlotIndex < ordered[j].Manifest.SlotIndex })
	return ordered
}

func slotExpansions(assets []expansion.Asset) []expansion.SlotExpansion {
	result := make([]expansion.SlotExpansion, 0, len(assets))
	for _, asset := range sortedSlotAssets(assets) {
		result = append(result, expansion.SlotExpansion{Slot: asset.Manifest.SlotIndex, ExpansionID: asset.ID})
	}
	return result
}

func inspectBase(base []byte) (Inspection, []byte, []byte, error) {
	manifest, payload, romMap, err := readArchive(base)
	if err != nil {
		return Inspection{}, nil, nil, err
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return Inspection{}, nil, nil, err
	}
	return Inspection{PackageID: packageIdentity(manifest, payload, romMap), Descriptor: descriptor}, payload, romMap, nil
}

// ValidateSlotExpansions binds cards to the exact sealed multi-socket shell,
// one per physical slot, without producing a bitstream.
func ValidateSlotExpansions(base []byte, assets []expansion.Asset) error {
	if len(assets) > MaxSlotCards {
		return errors.New("too many slot cards")
	}
	inspection, payload, _, err := inspectBase(base)
	if err != nil {
		return err
	}
	shell, err := slotCompositionShell(inspection, payload)
	if err != nil {
		return err
	}
	return expansion.AdmitSlots(shell, assets)
}

// ValidateSlotCards admits cards against the exact shell and links them once,
// so a card that changes CRAM outside its own socket is refused at import.
func ValidateSlotCards(ctx context.Context, base []byte, assets []expansion.Asset) error {
	if len(assets) == 0 || len(assets) > MaxSlotCards {
		return errors.New("slot validation requires 1..4 cards")
	}
	inspection, payload, _, err := inspectBase(base)
	if err != nil {
		return err
	}
	shell, err := slotCompositionShell(inspection, payload)
	if err != nil {
		return err
	}
	_, _, err = expansion.ComposeSlotsContext(ctx, shell, assets)
	return err
}

// ComposeSlotArchive links cards into a ROM-less multi-socket shell.
func ComposeSlotArchive(ctx context.Context, base []byte, assets []expansion.Asset) (SlotCompositionBundle, error) {
	if len(assets) == 0 || len(assets) > MaxSlotCards {
		return SlotCompositionBundle{}, errors.New("slot composition requires 1..4 cards")
	}
	inspection, payload, _, err := inspectBase(base)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	if inspection.Descriptor.ROM != nil || inspection.Descriptor.Format == 4 {
		return SlotCompositionBundle{}, errors.New("ROM-bearing shells compose cards through ROM input")
	}
	shell, err := slotCompositionShell(inspection, payload)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	composition, linked, err := expansion.ComposeSlotsContext(ctx, shell, assets)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	return SlotCompositionBundle{Package: bytes.Clone(base), Assets: sortedSlotAssets(assets), Composition: composition, Payload: linked}, nil
}

const slotCompositionMember = "slot-composition.json"

func slotMemberName(slot int) string { return "slot-" + strconv.Itoa(slot) + ".tar" }

func slotMemberIndex(name string) (int, bool) {
	value, ok := strings.CutPrefix(name, "slot-")
	if !ok {
		return 0, false
	}
	value, ok = strings.CutSuffix(value, ".tar")
	if !ok || len(value) != 1 || value[0] < '1' || value[0] > '7' {
		return 0, false
	}
	return int(value[0] - '0'), true
}

type canonicalMember struct {
	name string
	data []byte
}

func writeCanonicalMembers(members []canonicalMember, limit int64) ([]byte, error) {
	var out bytes.Buffer
	for _, member := range members {
		out.Write(canonicalHeader(member.name, int64(len(member.data))))
		out.Write(member.data)
		out.Write(make([]byte, (512-len(member.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	if int64(out.Len()) > limit {
		return nil, errors.New("transport exceeds size limit")
	}
	return out.Bytes(), nil
}

// canonicalReader walks canonical ustar members written by writeCanonicalMembers.
type canonicalReader struct {
	data   []byte
	offset int
}

func (r *canonicalReader) done() bool { return len(r.data)-r.offset == 1024 }

func (r *canonicalReader) peek() string {
	if len(r.data)-r.offset < 512 {
		return ""
	}
	name := r.data[r.offset : r.offset+100]
	if end := bytes.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	return string(name)
}

func (r *canonicalReader) read(name string, limit int64) ([]byte, error) {
	if len(r.data)-r.offset < 512 {
		return nil, errors.New("truncated transport")
	}
	header := r.data[r.offset : r.offset+512]
	size, err := canonicalSize(header[124:136])
	if err != nil || size < 1 || size > limit || !bytes.Equal(header, canonicalHeader(name, size)) {
		return nil, fmt.Errorf("invalid transport member %s", name)
	}
	r.offset += 512
	padded := int((size + 511) &^ 511)
	if padded > len(r.data)-r.offset || !allZero(r.data[r.offset+int(size):r.offset+padded]) {
		return nil, errors.New("invalid transport padding or length")
	}
	value := r.data[r.offset : r.offset+int(size)]
	r.offset += padded
	return value, nil
}

func (r *canonicalReader) finish() error {
	if !r.done() || !allZero(r.data[r.offset:]) {
		return errors.New("transport must end with exactly two zero blocks")
	}
	return nil
}

// readSlotAssets reads ascending slot-N.tar members whose manifests name N.
func (r *canonicalReader) readSlotAssets() ([]expansion.Asset, error) {
	var assets []expansion.Asset
	for strings.HasPrefix(r.peek(), "slot-") {
		slot, ok := slotMemberIndex(r.peek())
		if !ok || (len(assets) > 0 && assets[len(assets)-1].Manifest.SlotIndex >= slot) || len(assets) == MaxSlotCards {
			return nil, errors.New("invalid slot card member order")
		}
		encoded, err := r.read(slotMemberName(slot), expansion.MaxArchiveBytes)
		if err != nil {
			return nil, err
		}
		asset, err := expansion.ReadAsset(bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		if asset.Manifest.SlotIndex != slot {
			return nil, errors.New("slot card member names a different slot")
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func slotAssetMembers(assets []expansion.Asset) ([]canonicalMember, error) {
	var members []canonicalMember
	for _, asset := range sortedSlotAssets(assets) {
		var encoded bytes.Buffer
		if err := asset.Write(&encoded); err != nil {
			return nil, err
		}
		members = append(members, canonicalMember{slotMemberName(asset.Manifest.SlotIndex), encoded.Bytes()})
	}
	return members, nil
}

// IsSlotCompositionBundle recognizes the first transport member only.
func IsSlotCompositionBundle(data []byte) bool {
	return (&canonicalReader{data: data}).peek() == slotCompositionMember
}

// Write emits the closed transport after recomputing it from components.
func (b SlotCompositionBundle) Write() ([]byte, error) {
	verified, err := ComposeSlotArchive(context.Background(), b.Package, b.Assets)
	if err != nil {
		return nil, err
	}
	if !equalSlotComposition(verified.Composition, b.Composition) || !bytes.Equal(verified.Payload, b.Payload) {
		return nil, errors.New("slot composition does not match components")
	}
	identity, err := json.Marshal(b.Composition)
	if err != nil {
		return nil, err
	}
	cards, err := slotAssetMembers(b.Assets)
	if err != nil {
		return nil, err
	}
	members := append([]canonicalMember{{slotCompositionMember, identity}, {"package.tar", b.Package}}, cards...)
	members = append(members, canonicalMember{"linked.rbf", b.Payload})
	return writeCanonicalMembers(members, MaxCompositionArchiveSize)
}

func equalSlotComposition(left, right expansion.SlotComposition) bool {
	if left.ID != right.ID || left.PackageID != right.PackageID || left.ShellSHA256 != right.ShellSHA256 ||
		left.PayloadSHA256 != right.PayloadSHA256 || left.PayloadSize != right.PayloadSize || len(left.Expansions) != len(right.Expansions) {
		return false
	}
	for index := range left.Expansions {
		if left.Expansions[index] != right.Expansions[index] {
			return false
		}
	}
	return true
}

// ReadSlotCompositionBundle validates closed framing and independently
// recomputes the exact composition before returning any staging input.
func ReadSlotCompositionBundle(ctx context.Context, data []byte) (SlotCompositionBundle, error) {
	if len(data) > MaxCompositionArchiveSize || !IsSlotCompositionBundle(data) {
		return SlotCompositionBundle{}, errors.New("invalid slot composition transport")
	}
	reader := &canonicalReader{data: data}
	identity, err := reader.read(slotCompositionMember, 4096)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	pkg, err := reader.read("package.tar", MaxArchiveSize)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	assets, err := reader.readSlotAssets()
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	linked, err := reader.read("linked.rbf", MaxPayloadSize)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	if err := reader.finish(); err != nil {
		return SlotCompositionBundle{}, err
	}
	verified, err := ComposeSlotArchive(ctx, pkg, assets)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	canonical, err := json.Marshal(verified.Composition)
	if err != nil {
		return SlotCompositionBundle{}, err
	}
	if !bytes.Equal(canonical, identity) || !bytes.Equal(verified.Payload, linked) {
		return SlotCompositionBundle{}, errors.New("slot composition differs from independently linked components")
	}
	return verified, nil
}

// stageSlotCompositionBundle publishes the sealed shell, one private
// directory per card and the linked overlay beside it. Every companion shares
// the base publication token and is removed by Staged.Cleanup.
func stageSlotCompositionBundle(ctx context.Context, root string, bundle SlotCompositionBundle) (Staged, error) {
	staged, err := Stage(ctx, root, int64(len(bundle.Package)), bytes.NewReader(bundle.Package))
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
		return Staged{}, errors.New("slot composition staging root changed")
	}
	type item struct {
		name  string
		slot  int
		files map[string][]byte
	}
	var items []item
	for _, asset := range bundle.Assets {
		items = append(items, item{slotPublicationPrefix(asset.Manifest.SlotIndex) + staged.publication, asset.Manifest.SlotIndex,
			map[string][]byte{"manifest.json": asset.ManifestBytes, "cart.rbf": asset.Cart}})
	}
	items = append(items, item{"composition-" + staged.publication, 0, map[string][]byte{"linked.rbf": bundle.Payload}})
	for _, next := range items {
		if err = handle.Mkdir(next.name, 0700); err != nil {
			return Staged{}, err
		}
		retained, err := handle.Lstat(next.name)
		if err != nil {
			return Staged{}, err
		}
		staged.companions = append(staged.companions, Staged{root: root, rootInfo: info, publication: next.name, publicationInfo: retained})
		for file, content := range next.files {
			if err = writePrivate(handle, filepath.Join(next.name, file), content); err != nil {
				return Staged{}, err
			}
		}
		if err = handle.Chmod(next.name, 0500); err != nil {
			return Staged{}, err
		}
		if next.slot == 0 {
			staged.PayloadPath = filepath.Join(root, next.name, "linked.rbf")
		} else {
			staged.SlotDirectories = append(staged.SlotDirectories, SlotDirectory{Slot: next.slot, Directory: filepath.Join(root, next.name)})
		}
	}
	if err = ctx.Err(); err != nil {
		return Staged{}, err
	}
	composition := bundle.Composition
	composition.Expansions = append([]expansion.SlotExpansion(nil), bundle.Composition.Expansions...)
	staged.SlotComposition = &composition
	success = true
	return staged, nil
}

func slotPublicationPrefix(slot int) string { return "slot-" + strconv.Itoa(slot) + "-" }

// adoptSlotComposition reopens retained card publications, relinks them
// against the retained shell, and compares the overlay. It reports whether
// the publication is a slot composition.
func adoptSlotComposition(handle *os.Root, staged *Staged) (bool, error) {
	var assets []expansion.Asset
	var directories []SlotDirectory
	var companions []Staged
	for slot := 1; slot <= 7; slot++ {
		name := slotPublicationPrefix(slot) + staged.publication
		info, err := handle.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		files, err := readPublication(handle, name, info, map[string]int64{"manifest.json": expansion.MaxManifestBytes, "cart.rbf": MaxPayloadSize})
		if err != nil {
			return false, err
		}
		var manifest expansion.Manifest
		decoder := json.NewDecoder(bytes.NewReader(files["manifest.json"]))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			return false, err
		}
		asset, err := expansion.NewAsset(manifest, files["cart.rbf"])
		if err != nil {
			return false, err
		}
		if !bytes.Equal(asset.ManifestBytes, files["manifest.json"]) || manifest.SlotIndex != slot {
			return false, errors.New("retained slot card is not canonical for its slot")
		}
		assets = append(assets, asset)
		directories = append(directories, SlotDirectory{Slot: slot, Directory: filepath.Join(staged.root, name)})
		companions = append(companions, Staged{root: staged.root, rootInfo: staged.rootInfo, publication: name, publicationInfo: info})
	}
	if len(assets) == 0 {
		return false, nil
	}
	if _, err := handle.Lstat("expansion-" + staged.publication); !errors.Is(err, os.ErrNotExist) {
		return false, errors.New("publication mixes single-socket and slot cards")
	}
	name := "composition-" + staged.publication
	info, err := handle.Lstat(name)
	if err != nil {
		return false, errors.New("incomplete slot composition publication")
	}
	files, err := readPublication(handle, name, info, map[string]int64{"linked.rbf": MaxPayloadSize})
	if err != nil {
		return false, err
	}
	companions = append(companions, Staged{root: staged.root, rootInfo: staged.rootInfo, publication: name, publicationInfo: info})
	staged.companions = append(staged.companions, companions...)
	base, err := handle.OpenRoot(staged.publication)
	if err != nil {
		return false, err
	}
	defer base.Close()
	payload, err := readRootMember(base, "core.rbf", MaxPayloadSize)
	if err != nil {
		return false, err
	}
	shell, err := slotCompositionShell(Inspection{PackageID: staged.PackageID, Descriptor: staged.Descriptor}, payload)
	if err != nil {
		return false, err
	}
	identity, linked, err := expansion.ComposeSlotsContext(context.Background(), shell, assets)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(linked, files["linked.rbf"]) {
		return false, errors.New("adopted slot overlay differs from components")
	}
	staged.SlotComposition = &identity
	staged.SlotDirectories = directories
	staged.PayloadPath = filepath.Join(staged.root, name, "linked.rbf")
	return true, nil
}

// readPublication reads an exact private companion directory.
func readPublication(handle *os.Root, name string, info os.FileInfo, expected map[string]int64) (map[string][]byte, error) {
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0500 {
		return nil, errors.New("invalid companion publication")
	}
	child, err := handle.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("companion publication changed")
	}
	directory, err := child.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := directory.ReadDir(-1)
	directory.Close()
	if err != nil || len(entries) != len(expected) {
		return nil, errors.New("invalid companion directory members")
	}
	files := map[string][]byte{}
	for _, entry := range entries {
		limit, ok := expected[entry.Name()]
		if !ok {
			return nil, errors.New("unexpected companion directory member")
		}
		files[entry.Name()], err = readRootMember(child, entry.Name(), limit)
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}
