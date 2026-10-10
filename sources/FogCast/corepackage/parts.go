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
	"reflect"
	"sort"

	"github.com/DeanoC/misteross/expansion"
)

// Developer parts are deliberately separate from library expansion selection.
// Their fixed output and base GP capabilities stay those of the sealed shell.
const MaxPartsArchiveSize = MaxArchiveSize + 2*expansion.MaxArchiveBytes + MaxPayloadSize + 16384

type PartDirectory struct {
	Role      string `json:"role"`
	Directory string `json:"directory"`
}
type PartsBundle struct {
	Package     []byte
	Assets      []expansion.Asset
	Composition expansion.PartsComposition
	Payload     []byte
}

func PartsShell(inspection Inspection, payload []byte) (expansion.PartsShell, error) {
	d := inspection.Descriptor
	st := d.Format == 3 && d.Core.ID == "fes.atari-st" && d.ROM != nil && d.ROM.Role == "firmware"
	coleco := d.Format == 2 && d.Core.ID == "fes.coleco"
	abi := "fes.application"
	if st {
		abi = "fes.computer"
	}
	if (!st && !coleco) || d.ABI.ID != abi || d.ABI.Major != 1 || d.ABI.Minor != 0 {
		return expansion.PartsShell{}, errors.New("parts require a closed Coleco application or ST firmware shell")
	}
	video, cpu := false, false
	layout := ""
	for _, i := range d.Interfaces {
		switch i.ID {
		case expansion.VideoSlot, expansion.NativeVideoSlot:
			if video || i.Required || i.Major != 1 || i.Minor != 0 || (st && i.ID != expansion.VideoSlot) {
				return expansion.PartsShell{}, errors.New("unsupported video fabric socket")
			}
			video = true
			layout = expansion.ColecoVideoLayout
			if st {
				layout = expansion.AtariStVideoLayout
			}
			if i.ID == expansion.NativeVideoSlot {
				layout = expansion.ColecoNativeVideoLayout
			}
		case expansion.AtariStSlot:
			if !st || cpu || i.Required || i.Major != 1 || i.Minor != 0 {
				return expansion.PartsShell{}, errors.New("parts require optional ST cartridge bus 1.0")
			}
			cpu = true
		case expansion.ColecoSlot:
			if st || cpu || i.Required || i.Major != 2 || i.Minor != 0 {
				return expansion.PartsShell{}, errors.New("parts require the optional Coleco CPU bus 2.0")
			}
			cpu = true
		}
	}
	if !video || !cpu {
		return expansion.PartsShell{}, errors.New("parts require explicitly declared video and CPU sockets")
	}
	return expansion.PartsShell{PackageID: inspection.PackageID, BuildID: d.Build.ID, Payload: payload, Layout: layout}, nil
}

func partRole(asset expansion.Asset) string {
	if asset.Manifest.Slot == expansion.VideoSlot || asset.Manifest.Slot == expansion.NativeVideoSlot {
		return expansion.PartRoleVideo
	}
	return expansion.PartRoleExpansion
}

func ComposePartsArchive(ctx context.Context, pkg []byte, assets []expansion.Asset) (PartsBundle, error) {
	manifest, payload, mapping, err := readArchive(pkg)
	if err != nil {
		return PartsBundle{}, err
	}
	d, err := decode(manifest, payload, mapping)
	if err != nil {
		return PartsBundle{}, err
	}
	shell, err := PartsShell(Inspection{PackageID: packageIdentity(manifest, payload, mapping), Descriptor: d}, payload)
	if err != nil {
		return PartsBundle{}, err
	}
	ordered := append([]expansion.Asset(nil), assets...)
	sort.Slice(ordered, func(i, j int) bool { return partRole(ordered[i]) < partRole(ordered[j]) })
	identity, linked, err := expansion.ComposePartsContext(ctx, shell, ordered)
	if err != nil {
		return PartsBundle{}, err
	}
	return PartsBundle{Package: bytes.Clone(pkg), Assets: ordered, Composition: identity, Payload: linked}, nil
}

func (b PartsBundle) Write(ctx context.Context) ([]byte, error) {
	verified, err := ComposePartsArchive(ctx, b.Package, b.Assets)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(b.Composition, verified.Composition) || !bytes.Equal(b.Payload, verified.Payload) {
		return nil, errors.New("parts bundle differs from independently composed components")
	}
	receipt, err := json.Marshal(verified.Composition)
	if err != nil {
		return nil, err
	}
	members := []struct {
		name string
		data []byte
	}{{"parts.json", receipt}, {"package.tar", b.Package}}
	for _, asset := range verified.Assets {
		var encoded bytes.Buffer
		if err := asset.Write(&encoded); err != nil {
			return nil, err
		}
		members = append(members, struct {
			name string
			data []byte
		}{"part-" + partRole(asset) + ".tar", encoded.Bytes()})
	}
	members = append(members, struct {
		name string
		data []byte
	}{"linked.rbf", verified.Payload})
	var out bytes.Buffer
	for _, m := range members {
		out.Write(canonicalHeader(m.name, int64(len(m.data))))
		out.Write(m.data)
		out.Write(make([]byte, (512-len(m.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	if out.Len() > MaxPartsArchiveSize {
		return nil, errors.New("parts archive exceeds bound")
	}
	return out.Bytes(), nil
}

func ReadPartsBundle(ctx context.Context, data []byte) (PartsBundle, error) {
	if len(data) > MaxPartsArchiveSize {
		return PartsBundle{}, errors.New("parts archive exceeds bound")
	}
	offset := 0
	read := func(name string, limit int64) ([]byte, error) {
		if len(data)-offset < 512 {
			return nil, errors.New("truncated parts member")
		}
		header := data[offset : offset+512]
		size, err := canonicalSize(header[124:136])
		if err != nil || size < 1 || size > limit || !bytes.Equal(header, canonicalHeader(name, size)) {
			return nil, fmt.Errorf("invalid parts member %s", name)
		}
		offset += 512
		padded := int((size + 511) &^ 511)
		if padded > len(data)-offset || !allZero(data[offset+int(size):offset+padded]) {
			return nil, errors.New("invalid parts member padding")
		}
		value := data[offset : offset+int(size)]
		offset += padded
		return value, nil
	}
	receipt, err := read("parts.json", 4096)
	if err != nil {
		return PartsBundle{}, err
	}
	var identity expansion.PartsComposition
	decoder := json.NewDecoder(bytes.NewReader(receipt))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return PartsBundle{}, err
	}
	if len(identity.Parts) < 1 || len(identity.Parts) > 2 {
		return PartsBundle{}, errors.New("parts must contain video and an optional expansion")
	}
	pkg, err := read("package.tar", MaxArchiveSize)
	if err != nil {
		return PartsBundle{}, err
	}
	var assets []expansion.Asset
	previous := ""
	for _, part := range identity.Parts {
		if (part.Role != "video" && part.Role != "expansion") || part.Role <= previous {
			return PartsBundle{}, errors.New("parts roles must ascend")
		}
		previous = part.Role
		encoded, err := read("part-"+part.Role+".tar", expansion.MaxArchiveBytes)
		if err != nil {
			return PartsBundle{}, err
		}
		asset, err := expansion.ReadAsset(bytes.NewReader(encoded))
		if err != nil {
			return PartsBundle{}, err
		}
		assets = append(assets, asset)
	}
	linked, err := read("linked.rbf", MaxPayloadSize)
	if err != nil {
		return PartsBundle{}, err
	}
	if len(data)-offset != 1024 || !allZero(data[offset:]) {
		return PartsBundle{}, errors.New("parts archive must end with exactly two zero blocks")
	}
	verified, err := ComposePartsArchive(ctx, pkg, assets)
	if err != nil {
		return PartsBundle{}, err
	}
	canonical, err := json.Marshal(verified.Composition)
	if err != nil {
		return PartsBundle{}, err
	}
	if !bytes.Equal(receipt, canonical) || !bytes.Equal(linked, verified.Payload) {
		return PartsBundle{}, errors.New("parts differ from independently linked components")
	}
	return verified, nil
}

func StageParts(ctx context.Context, root string, size int64, input io.Reader) (Staged, error) {
	if size < 1 || size > MaxPartsArchiveSize {
		return Staged{}, errors.New("invalid parts archive size")
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: input}, size+1))
	if err != nil {
		return Staged{}, err
	}
	if int64(len(data)) != size {
		return Staged{}, errors.New("parts archive size mismatch")
	}
	bundle, err := ReadPartsBundle(ctx, data)
	if err != nil {
		return Staged{}, err
	}
	return stagePartsBundle(ctx, root, bundle)
}

func stagePartsBundle(ctx context.Context, root string, bundle PartsBundle) (Staged, error) {
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
		return Staged{}, errors.New("parts staging root changed")
	}
	publish := func(name string, files map[string][]byte) error {
		if err := handle.Mkdir(name, 0700); err != nil {
			return err
		}
		retained, err := handle.Lstat(name)
		if err != nil {
			return err
		}
		staged.companions = append(staged.companions, Staged{root: root, rootInfo: info, publication: name, publicationInfo: retained})
		for file, content := range files {
			if err := writePrivate(handle, filepath.Join(name, file), content); err != nil {
				return err
			}
		}
		return handle.Chmod(name, 0500)
	}
	for _, asset := range bundle.Assets {
		role := partRole(asset)
		name := "part-" + role + "-" + staged.publication
		if err := publish(name, map[string][]byte{"manifest.json": asset.ManifestBytes, "cart.rbf": asset.Cart}); err != nil {
			return Staged{}, err
		}
		staged.PartDirectories = append(staged.PartDirectories, PartDirectory{Role: role, Directory: filepath.Join(root, name)})
	}
	name := "parts-composition-" + staged.publication
	if err := publish(name, map[string][]byte{"linked.rbf": bundle.Payload}); err != nil {
		return Staged{}, err
	}
	staged.PayloadPath = filepath.Join(root, name, "linked.rbf")
	staged.PartsComposition = &bundle.Composition
	if err := ctx.Err(); err != nil {
		return Staged{}, err
	}
	success = true
	return staged, nil
}

func adoptParts(handle *os.Root, staged *Staged) error {
	contents := map[string]map[string][]byte{}
	for _, prefix := range []string{"part-expansion-", "part-video-", "parts-composition-"} {
		name := prefix + staged.publication
		info, err := handle.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0500 {
			return errors.New("invalid parts publication")
		}
		child, err := handle.OpenRoot(name)
		if err != nil {
			return err
		}
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			child.Close()
			return errors.New("parts publication changed")
		}
		expected := map[string]int64{"manifest.json": expansion.MaxManifestBytes, "cart.rbf": MaxPayloadSize}
		if prefix == "parts-composition-" {
			expected = map[string]int64{"linked.rbf": MaxPayloadSize}
		}
		dir, err := child.Open(".")
		if err != nil {
			child.Close()
			return err
		}
		entries, err := dir.ReadDir(-1)
		dir.Close()
		if err != nil || len(entries) != len(expected) {
			child.Close()
			return errors.New("invalid parts directory members")
		}
		files := map[string][]byte{}
		for _, entry := range entries {
			limit, ok := expected[entry.Name()]
			if !ok {
				child.Close()
				return errors.New("unexpected parts member")
			}
			files[entry.Name()], err = readRootMember(child, entry.Name(), limit)
			if err != nil {
				child.Close()
				return err
			}
		}
		child.Close()
		contents[prefix] = files
		staged.companions = append(staged.companions, Staged{root: staged.root, rootInfo: staged.rootInfo, publication: name, publicationInfo: info})
	}
	if len(contents) == 0 {
		return nil
	}
	if contents["part-video-"] == nil || contents["parts-composition-"] == nil || staged.Composition != nil || staged.SlotComposition != nil {
		return errors.New("incomplete or mixed parts publication")
	}
	var assets []expansion.Asset
	for _, role := range []string{"expansion", "video"} {
		files := contents["part-"+role+"-"]
		if files == nil {
			continue
		}
		var m expansion.Manifest
		if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
			return err
		}
		asset, err := expansion.NewAsset(m, files["cart.rbf"])
		if err != nil {
			return err
		}
		if !bytes.Equal(asset.ManifestBytes, files["manifest.json"]) || partRole(asset) != role {
			return errors.New("invalid adopted part manifest")
		}
		assets = append(assets, asset)
		staged.PartDirectories = append(staged.PartDirectories, PartDirectory{Role: role, Directory: filepath.Join(staged.root, "part-"+role+"-"+staged.publication)})
	}
	base, err := handle.OpenRoot(staged.publication)
	if err != nil {
		return err
	}
	defer base.Close()
	payload, err := readRootMember(base, "core.rbf", MaxPayloadSize)
	if err != nil {
		return err
	}
	shell, err := PartsShell(Inspection{PackageID: staged.PackageID, Descriptor: staged.Descriptor}, payload)
	if err != nil {
		return err
	}
	identity, linked, err := expansion.ComposePartsContext(context.Background(), shell, assets)
	if err != nil {
		return err
	}
	if !bytes.Equal(linked, contents["parts-composition-"]["linked.rbf"]) {
		return errors.New("adopted parts differ from independently linked bytes")
	}
	staged.PartsComposition = &identity
	staged.PayloadPath = filepath.Join(staged.root, "parts-composition-"+staged.publication, "linked.rbf")
	return nil
}
