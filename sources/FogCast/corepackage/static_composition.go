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

const staticCompositionMember = "static-composition.json"

const MaxStaticCompositionArchiveSize = MaxArchiveSize + expansion.MaxArchiveBytes + 16384

// StaticCompositionBundle retains the sealed format5 package and optional
// independently built partial asset. No derived/monolithic RBF is carried.
// Consumers recompute the selection from these exact inputs.
type StaticCompositionBundle struct {
	Package     []byte
	Expansion   *expansion.Asset
	Composition expansion.StaticComposition
}

func ComposeStaticArchive(base []byte, selected *expansion.Asset) (StaticCompositionBundle, error) {
	manifest, payload, persona, err := readArchive(base)
	if err != nil {
		return StaticCompositionBundle{}, err
	}
	descriptor, err := decode(manifest, payload, persona)
	if err != nil {
		return StaticCompositionBundle{}, err
	}
	if descriptor.Format != 5 || descriptor.StaticExpansion == nil {
		return StaticCompositionBundle{}, errors.New("static composition requires a format5 package")
	}
	shell := staticShell(descriptor, packageIdentity(manifest, payload, persona), payload, persona)
	plan, err := expansion.ComposeStatic(shell, selected)
	if err != nil {
		return StaticCompositionBundle{}, err
	}
	result := StaticCompositionBundle{Package: bytes.Clone(base), Composition: plan}
	if selected != nil {
		owned, err := expansion.NewAsset(selected.Manifest, selected.Cart)
		if err != nil {
			return StaticCompositionBundle{}, err
		}
		if owned.ID != selected.ID {
			return StaticCompositionBundle{}, errors.New("static expansion changed while retaining inputs")
		}
		result.Expansion = &owned
	}
	return result, nil
}

// descriptor and packageID have already been verified against all three files.
func staticShell(descriptor Descriptor, packageID string, payload, persona []byte) expansion.StaticShell {
	s := descriptor.StaticExpansion
	return expansion.StaticShell{PackageID: packageID, BuildID: descriptor.Build.ID, Base: payload, DefaultPersona: persona, BaseSHA256: descriptor.Payload.SHA256, DefaultPersonaSHA256: s.SHA256, Default: expansion.StaticPersona{ControlBuildID: s.ControlBuildID, HardwareID: uint32(s.HardwareID), RootQDBSHA256: s.RootQDBSHA256, InterfaceSHA256: s.InterfaceSHA256, ClockResetSHA256: s.ClockResetSHA256, POF0Hash: s.POF0Hash}}
}

func (b StaticCompositionBundle) Write(w io.Writer) error {
	verified, err := ComposeStaticArchive(b.Package, b.Expansion)
	if err != nil {
		return err
	}
	if verified.Composition != b.Composition {
		return errors.New("static composition does not match retained inputs")
	}
	identity, err := json.Marshal(verified.Composition)
	if err != nil {
		return err
	}
	members := []struct {
		name string
		body []byte
	}{{staticCompositionMember, identity}, {"package.tar", verified.Package}}
	if verified.Expansion != nil {
		var asset bytes.Buffer
		if err := verified.Expansion.Write(&asset); err != nil {
			return err
		}
		members = append(members, struct {
			name string
			body []byte
		}{"expansion.tar", asset.Bytes()})
	}
	writer := tar.NewWriter(w)
	for _, m := range members {
		if err := writer.WriteHeader(&tar.Header{Name: m.name, Mode: 0600, Size: int64(len(m.body)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := writer.Write(m.body); err != nil {
			return err
		}
	}
	return writer.Close()
}

// ReadStaticCompositionBundle closes transport membership and independently
// verifies package/default/selected bytes and the canonical selection receipt.
// It has no staging, programming or running-core operation.
func ReadStaticCompositionBundle(r io.Reader) (StaticCompositionBundle, error) {
	bounded := &io.LimitedReader{R: r, N: MaxStaticCompositionArchiveSize + 1}
	reader := tar.NewReader(bounded)
	files := map[string][]byte{}
	limits := map[string]int64{"package.tar": MaxArchiveSize, "expansion.tar": expansion.MaxArchiveBytes, staticCompositionMember: 4096}
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return StaticCompositionBundle{}, err
		}
		limit, ok := limits[h.Name]
		if !ok || files[h.Name] != nil || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) || h.Size < 1 || h.Size > limit {
			return StaticCompositionBundle{}, errors.New("invalid static composition member")
		}
		body, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return StaticCompositionBundle{}, err
		}
		if int64(len(body)) != h.Size {
			return StaticCompositionBundle{}, errors.New("truncated static composition member")
		}
		files[h.Name] = body
	}
	padding, err := io.ReadAll(bounded)
	if err != nil {
		return StaticCompositionBundle{}, err
	}
	if bounded.N == 0 || files["package.tar"] == nil || files[staticCompositionMember] == nil || (len(files) != 2 && len(files) != 3) {
		return StaticCompositionBundle{}, errors.New("invalid static composition framing")
	}
	for _, v := range padding {
		if v != 0 {
			return StaticCompositionBundle{}, errors.New("trailing static composition bytes")
		}
	}
	var selected *expansion.Asset
	if body, ok := files["expansion.tar"]; ok {
		asset, err := expansion.ReadAsset(bytes.NewReader(body))
		if err != nil {
			return StaticCompositionBundle{}, err
		}
		selected = &asset
	}
	verified, err := ComposeStaticArchive(files["package.tar"], selected)
	if err != nil {
		return StaticCompositionBundle{}, err
	}
	canonical, err := json.Marshal(verified.Composition)
	if err != nil || !bytes.Equal(canonical, files[staticCompositionMember]) {
		return StaticCompositionBundle{}, errors.New("static composition receipt differs from recomputed inputs")
	}
	return verified, nil
}

// IsStaticCompositionBundle recognizes the first canonical member only.
// Closed parsing and payload verification follow before publication.
func IsStaticCompositionBundle(data []byte) bool {
	return (&canonicalReader{data: data}).peek() == staticCompositionMember
}

// StageStaticComposition stages only verified base/default/selected files. It
// does not select a runtime command or touch a running machine.
func StageStaticComposition(ctx context.Context, root string, size int64, input io.Reader) (Staged, error) {
	if ctx == nil || input == nil || size < 1 || size > MaxStaticCompositionArchiveSize {
		return Staged{}, errors.New("invalid static composition staging input")
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: input}, size+1))
	if err != nil {
		return Staged{}, err
	}
	if int64(len(data)) != size {
		return Staged{}, errors.New("static composition size mismatch")
	}
	return StageStaticCompositionBytes(ctx, root, data)
}

func StageStaticCompositionBytes(ctx context.Context, root string, data []byte) (Staged, error) {
	if ctx == nil || len(data) < 1 || len(data) > MaxStaticCompositionArchiveSize {
		return Staged{}, errors.New("invalid static composition staging input")
	}
	if err := ctx.Err(); err != nil {
		return Staged{}, err
	}
	bundle, err := ReadStaticCompositionBundle(bytes.NewReader(data))
	if err != nil {
		return Staged{}, err
	}
	staged, err := stageArchive(ctx, root, bundle.Package, nil)
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
		return Staged{}, errors.New("static composition staging root changed")
	}
	publish := func(prefix string, files map[string][]byte) error {
		name := prefix + staged.publication
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
	staged.PersonaPath = filepath.Join(staged.Directory, "persona.rbf")
	if bundle.Expansion != nil {
		if err := publish("expansion-", map[string][]byte{"manifest.json": bundle.Expansion.ManifestBytes, "cart.rbf": bundle.Expansion.Cart}); err != nil {
			return Staged{}, err
		}
		staged.ExpansionDirectory = filepath.Join(root, "expansion-"+staged.publication)
		staged.PersonaPath = filepath.Join(staged.ExpansionDirectory, "cart.rbf")
	}
	receipt, err := json.Marshal(bundle.Composition)
	if err != nil {
		return Staged{}, err
	}
	if err := publish("static-composition-", map[string][]byte{"composition.json": receipt}); err != nil {
		return Staged{}, err
	}
	staged.PayloadPath = filepath.Join(staged.Directory, "core.rbf")
	staged.StaticComposition = &bundle.Composition
	if err := ctx.Err(); err != nil {
		return Staged{}, err
	}
	success = true
	return staged, nil
}

// Adopt uses the existing ownership/cleanup path. Recompute the selection from
// its retained inputs and receipt before retaining either companion directory.
func adoptStaticComposition(handle *os.Root, staged *Staged) (bool, error) {
	name := "static-composition-" + staged.publication
	info, err := handle.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if staged.Descriptor.Format == 5 {
			return false, errors.New("static package requires its retained composition receipt")
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if staged.Descriptor.Format != 5 || staged.Descriptor.StaticExpansion == nil {
		return false, errors.New("static composition requires a format5 publication")
	}
	incompatible := []string{"composition-", "parts-composition-", "part-video-", "part-expansion-", "rom-link-", "machine-rom-", "pcxt-initial-"}
	for slot := 1; slot <= 7; slot++ {
		incompatible = append(incompatible, slotPublicationPrefix(slot))
	}
	for _, prefix := range incompatible {
		if _, err := handle.Lstat(prefix + staged.publication); !errors.Is(err, os.ErrNotExist) {
			return false, errors.New("static publication mixes composition forms")
		}
	}
	receipt, err := readPublication(handle, name, info, map[string]int64{"composition.json": 4096})
	if err != nil {
		return false, err
	}
	companions := []Staged{{root: staged.root, rootInfo: staged.rootInfo, publication: name, publicationInfo: info}}
	var selected *expansion.Asset
	expansionName := "expansion-" + staged.publication
	expansionInfo, err := handle.Lstat(expansionName)
	if err == nil {
		files, err := readPublication(handle, expansionName, expansionInfo, map[string]int64{"manifest.json": expansion.MaxManifestBytes, "cart.rbf": MaxPayloadSize})
		if err != nil {
			return false, err
		}
		var manifest expansion.Manifest
		if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
			return false, err
		}
		asset, err := expansion.NewAsset(manifest, files["cart.rbf"])
		if err != nil || !bytes.Equal(asset.ManifestBytes, files["manifest.json"]) {
			return false, errors.New("invalid retained static expansion")
		}
		selected = &asset
		companions = append(companions, Staged{root: staged.root, rootInfo: staged.rootInfo, publication: expansionName, publicationInfo: expansionInfo})
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	base, err := handle.OpenRoot(staged.publication)
	if err != nil {
		return false, err
	}
	defer base.Close()
	baseInfo, err := base.Stat(".")
	if err != nil || !os.SameFile(baseInfo, staged.publicationInfo) {
		return false, errors.New("static package publication changed")
	}
	manifest, payload, persona, err := readRootContents(base)
	if err != nil {
		return false, err
	}
	descriptor, err := decode(manifest, payload, persona)
	if err != nil {
		return false, err
	}
	packageID := packageIdentity(manifest, payload, persona)
	if packageID != staged.PackageID || descriptor.Format != 5 || descriptor.StaticExpansion == nil {
		return false, errors.New("retained static package changed")
	}
	plan, err := expansion.ComposeStatic(staticShell(descriptor, packageID, payload, persona), selected)
	if err != nil {
		return false, err
	}
	canonical, err := json.Marshal(plan)
	if err != nil || !bytes.Equal(canonical, receipt["composition.json"]) {
		return false, errors.New("retained static composition receipt changed")
	}
	staged.companions = append(staged.companions, companions...)
	staged.StaticComposition = &plan
	staged.PayloadPath = filepath.Join(staged.Directory, "core.rbf")
	staged.PersonaPath = filepath.Join(staged.Directory, "persona.rbf")
	if selected != nil {
		staged.ExpansionDirectory = filepath.Join(staged.root, expansionName)
		staged.PersonaPath = filepath.Join(staged.ExpansionDirectory, "cart.rbf")
	}
	return true, nil
}
