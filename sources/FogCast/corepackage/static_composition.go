package corepackage

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/DeanoC/misteross/expansion"
)

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
	s := descriptor.StaticExpansion
	shell := expansion.StaticShell{PackageID: packageIdentity(manifest, payload, persona), BuildID: descriptor.Build.ID, Base: payload, DefaultPersona: persona, BaseSHA256: descriptor.Payload.SHA256, DefaultPersonaSHA256: s.SHA256, Default: expansion.StaticPersona{ControlBuildID: s.ControlBuildID, HardwareID: uint32(s.HardwareID), RootQDBSHA256: s.RootQDBSHA256, InterfaceSHA256: s.InterfaceSHA256, ClockResetSHA256: s.ClockResetSHA256, POF0Hash: s.POF0Hash}}
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
	}{{"package.tar", verified.Package}}
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
	members = append(members, struct {
		name string
		body []byte
	}{"composition.json", identity})
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
	limits := map[string]int64{"package.tar": MaxArchiveSize, "expansion.tar": expansion.MaxArchiveBytes, "composition.json": 4096}
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
	if bounded.N == 0 || files["package.tar"] == nil || files["composition.json"] == nil || (len(files) != 2 && len(files) != 3) {
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
	if err != nil || !bytes.Equal(canonical, files["composition.json"]) {
		return StaticCompositionBundle{}, errors.New("static composition receipt differs from recomputed inputs")
	}
	return verified, nil
}
