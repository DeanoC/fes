package corepackage

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/misteross/expansion"
)

const MaxCompositionArchiveSize = MaxArchiveSize + expansion.MaxArchiveBytes + MaxPayloadSize + 16384

// CompositionBundle retains the original sealed package and independent cart.
// The linked bytes are transport evidence; each consumer recomputes them.
type CompositionBundle struct {
	Package     []byte
	Asset       expansion.Asset
	Composition expansion.Composition
	Payload     []byte
}

func ComposeArchive(base []byte, asset expansion.Asset) (CompositionBundle, error) {
	manifest, payload, romMap, err := readArchive(base)
	if err != nil {
		return CompositionBundle{}, err
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return CompositionBundle{}, err
	}
	shell, err := compositionShell(Inspection{PackageID: packageIdentity(manifest, payload, romMap), Descriptor: descriptor}, payload)
	if err != nil {
		return CompositionBundle{}, err
	}
	composition, linked, err := expansion.Compose(shell, asset)
	if err != nil {
		return CompositionBundle{}, err
	}
	return CompositionBundle{Package: bytes.Clone(base), Asset: asset, Composition: composition, Payload: linked}, nil
}

func (b CompositionBundle) Write(w io.Writer) error {
	verified, err := ComposeArchive(b.Package, b.Asset)
	if err != nil {
		return err
	}
	if verified.Composition != b.Composition || !bytes.Equal(verified.Payload, b.Payload) {
		return errors.New("composition does not match components")
	}
	var asset bytes.Buffer
	if err = b.Asset.Write(&asset); err != nil {
		return err
	}
	identity, err := json.Marshal(b.Composition)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	for _, e := range []struct {
		name string
		data []byte
	}{{"package.tar", b.Package}, {"expansion.tar", asset.Bytes()}, {"composition.json", identity}, {"linked.rbf", b.Payload}} {
		if err = tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0600, Size: int64(len(e.data))}); err != nil {
			return err
		}
		if _, err = tw.Write(e.data); err != nil {
			return err
		}
	}
	return tw.Close()
}

// ReadCompositionBundle validates closed transport framing and independently
// recomputes the exact composition before returning any target staging input.
func ReadCompositionBundle(r io.Reader) (CompositionBundle, error) {
	bounded := &io.LimitedReader{R: r, N: MaxCompositionArchiveSize + 1}
	tr := tar.NewReader(bounded)
	files := map[string][]byte{}
	limits := map[string]int64{"package.tar": MaxArchiveSize, "expansion.tar": expansion.MaxArchiveBytes, "composition.json": 4096, "linked.rbf": MaxPayloadSize}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return CompositionBundle{}, err
		}
		limit, ok := limits[h.Name]
		if !ok || files[h.Name] != nil || h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA || h.Size < 1 || h.Size > limit {
			return CompositionBundle{}, errors.New("invalid composition member")
		}
		data, err := io.ReadAll(io.LimitReader(tr, limit+1))
		if err != nil {
			return CompositionBundle{}, err
		}
		if int64(len(data)) != h.Size {
			return CompositionBundle{}, errors.New("truncated composition member")
		}
		files[h.Name] = data
	}
	padding, err := io.ReadAll(bounded)
	if err != nil {
		return CompositionBundle{}, err
	}
	if bounded.N == 0 || len(files) != 4 {
		return CompositionBundle{}, errors.New("invalid composition archive size")
	}
	for _, b := range padding {
		if b != 0 {
			return CompositionBundle{}, errors.New("trailing composition bytes")
		}
	}
	asset, err := expansion.ReadAsset(bytes.NewReader(files["expansion.tar"]))
	if err != nil {
		return CompositionBundle{}, err
	}
	verified, err := ComposeArchive(files["package.tar"], asset)
	if err != nil {
		return CompositionBundle{}, err
	}
	canonical, err := json.Marshal(verified.Composition)
	if err != nil {
		return CompositionBundle{}, err
	}
	if !bytes.Equal(canonical, files["composition.json"]) || !bytes.Equal(verified.Payload, files["linked.rbf"]) {
		return CompositionBundle{}, errors.New("composition differs from independently linked components")
	}
	return verified, nil
}

// StageComposition keeps companion publications beside the sealed base package.
// All three directories carry the same private ownership token and are removed
// through retained root/inode checks by Staged.Cleanup.
func StageComposition(ctx context.Context, root string, size int64, input io.Reader) (Staged, error) {
	if size < 1 || size > MaxCompositionArchiveSize {
		return Staged{}, errors.New("invalid composition size")
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: input}, size+1))
	if err != nil {
		return Staged{}, err
	}
	if int64(len(data)) != size {
		return Staged{}, errors.New("composition size mismatch")
	}
	bundle, err := ReadCompositionBundle(bytes.NewReader(data))
	if err != nil {
		return Staged{}, err
	}
	return stageCompositionBundle(ctx, root, bundle)
}

// stageCompositionBundle consumes a bundle already independently composed by this package.
func stageCompositionBundle(ctx context.Context, root string, bundle CompositionBundle) (Staged, error) {
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
		return Staged{}, errors.New("composition staging root changed")
	}
	for _, item := range []struct {
		prefix string
		files  map[string][]byte
	}{
		{"expansion-", map[string][]byte{"manifest.json": bundle.Asset.ManifestBytes, "cart.rbf": bundle.Asset.Cart}},
		{"composition-", map[string][]byte{"linked.rbf": bundle.Payload}},
	} {
		name := item.prefix + staged.publication
		if err = handle.Mkdir(name, 0700); err != nil {
			return Staged{}, err
		}
		retained, err := handle.Lstat(name)
		if err != nil {
			return Staged{}, err
		}
		staged.companions = append(staged.companions, Staged{root: root, rootInfo: info, publication: name, publicationInfo: retained})
		for file, content := range item.files {
			if err = writePrivate(handle, filepath.Join(name, file), content); err != nil {
				return Staged{}, err
			}
		}
		if err = handle.Chmod(name, 0500); err != nil {
			return Staged{}, err
		}
		if item.prefix == "expansion-" {
			staged.ExpansionDirectory = filepath.Join(root, name)
		} else {
			staged.PayloadPath = filepath.Join(root, name, "linked.rbf")
		}
	}
	if err = ctx.Err(); err != nil {
		return Staged{}, err
	}
	staged.Composition = &bundle.Composition
	success = true
	return staged, nil
}

func adoptComposition(handle *os.Root, staged *Staged) error {
	var contents = map[string]map[string][]byte{}
	for _, prefix := range []string{"expansion-", "composition-"} {
		name := prefix + staged.publication
		info, err := handle.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0500 {
			return errors.New("invalid composition publication")
		}
		child, err := handle.OpenRoot(name)
		if err != nil {
			return err
		}
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			child.Close()
			return errors.New("composition publication changed")
		}
		files := map[string][]byte{}
		expected := map[string]int64{"linked.rbf": MaxPayloadSize}
		if prefix == "expansion-" {
			expected = map[string]int64{"manifest.json": expansion.MaxManifestBytes, "cart.rbf": MaxPayloadSize}
		}
		directory, err := child.Open(".")
		if err != nil {
			child.Close()
			return err
		}
		entries, err := directory.ReadDir(-1)
		directory.Close()
		if err != nil || len(entries) != len(expected) {
			child.Close()
			return errors.New("invalid composition directory members")
		}
		for _, entry := range entries {
			limit, ok := expected[entry.Name()]
			if !ok {
				child.Close()
				return errors.New("unexpected composition directory member")
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
	if len(contents) != 2 {
		return errors.New("incomplete composition publication")
	}
	var manifest expansion.Manifest
	if err := json.Unmarshal(contents["expansion-"]["manifest.json"], &manifest); err != nil {
		return err
	}
	asset, err := expansion.NewAsset(manifest, contents["expansion-"]["cart.rbf"])
	if err != nil {
		return err
	}
	if !bytes.Equal(asset.ManifestBytes, contents["expansion-"]["manifest.json"]) {
		return errors.New("noncanonical expansion manifest")
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
	shell, err := compositionShell(Inspection{PackageID: staged.PackageID, Descriptor: staged.Descriptor}, payload)
	if err != nil {
		return err
	}
	identity, linked, err := expansion.Compose(shell, asset)
	if err != nil {
		return err
	}
	if !bytes.Equal(linked, contents["composition-"]["linked.rbf"]) {
		return errors.New("adopted linked bytes differ from components")
	}
	staged.Composition = &identity
	staged.ExpansionDirectory = filepath.Join(staged.root, "expansion-"+staged.publication)
	staged.PayloadPath = filepath.Join(staged.root, "composition-"+staged.publication, "linked.rbf")
	return nil
}

func compositionShell(inspection Inspection, payload []byte) (expansion.Shell, error) {
	d := inspection.Descriptor
	var slot string
	for _, i := range d.Interfaces {
		if i.ID == expansion.Slot || i.ID == expansion.ColecoSlot {
			if i.Required || i.Major != 1 || i.Minor != 0 {
				return expansion.Shell{}, errors.New("composition requires optional expansion bus 1.0")
			}
			if slot != "" {
				return expansion.Shell{}, errors.New("composition requires exactly one expansion bus")
			}
			slot = i.ID
		}
	}
	if slot == "" {
		return expansion.Shell{}, errors.New("composition requires exactly one expansion bus")
	}
	abi := "fes.simple-computer"
	if slot == expansion.ColecoSlot {
		abi = "fes.application"
	}
	if d.ABI.ID != abi || d.ABI.Major != 1 || d.ABI.Minor != 0 {
		return expansion.Shell{}, errors.New("composition bus does not match package ABI 1.0")
	}
	return expansion.Shell{PackageID: inspection.PackageID, BuildID: d.Build.ID, Payload: payload, Slot: slot, SlotMajor: 1}, nil
}

// ValidateExpansionArchive binds an expansion to its sealed package and declared
// optional socket without producing a bitstream. Targets independently compose.
func ValidateExpansionArchive(base []byte, asset expansion.Asset) error {
	manifest, payload, mapping, err := readArchive(base)
	if err != nil {
		return err
	}
	descriptor, err := decode(manifest, payload, mapping)
	if err != nil {
		return err
	}
	shell, err := compositionShell(Inspection{PackageID: packageIdentity(manifest, payload, mapping), Descriptor: descriptor}, payload)
	if err != nil {
		return err
	}
	return expansion.Admit(shell, asset)
}
