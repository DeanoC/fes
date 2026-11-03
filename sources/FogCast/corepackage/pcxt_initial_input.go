package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// PcxtInitialInput carries a sealed format-2 package and its immutable library disk.
type PcxtInitialInput struct {
	Package []byte
	Media   InitialMedia
}

type pcxtInitialReceipt struct {
	PackageID string              `json:"package_id"`
	Media     initialMediaReceipt `json:"media"`
}

func inspectPcxtInitialInput(in PcxtInitialInput) (Inspection, pcxtInitialReceipt, error) {
	manifest, payload, mapping, err := readArchiveMembers(in.Package, false)
	if err != nil {
		return Inspection{}, pcxtInitialReceipt{}, err
	}
	d, err := decode(manifest, payload, mapping)
	if err != nil {
		return Inspection{}, pcxtInitialReceipt{}, err
	}
	if d.Format != 2 || d.Core.ID != "fes.pcxt-dos" || !SupportedComputerABI(d.ABI) || d.ABI.Minor < 1 {
		return Inspection{}, pcxtInitialReceipt{}, errors.New("initial PC/XT disk requires format-2 PC/XT ABI 1.1")
	}
	base, writable := false, false
	for _, i := range d.Interfaces {
		if i.Required && i.Major == 1 && i.Minor == 0 {
			if i.ID == "fes.media.pcxt-floppy" {
				base = true
			}
			if i.ID == "fes.media.pcxt-floppy-write" {
				writable = true
			}
		}
	}
	m := in.Media
	if !base || !writable || len(m.Bytes) != 368640 || m.Unit != 0 || len(m.GameID) > 256 || !initialMediaGameID.MatchString(m.GameID) || !hex64RE.MatchString(m.BaseMediaID) || romDigest(m.Bytes) != m.BaseMediaID {
		return Inspection{}, pcxtInitialReceipt{}, errors.New("invalid PC/XT initial disk or immutable identity")
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload, mapping), Descriptor: d}
	return inspection, pcxtInitialReceipt{PackageID: inspection.PackageID, Media: initialMediaReceipt{GameID: m.GameID, BaseMediaID: m.BaseMediaID, Unit: m.Unit, Size: int64(len(m.Bytes))}}, nil
}

func IsPcxtInitialInput(data []byte) bool {
	return (&canonicalReader{data: data}).peek() == "pcxt-initial.json"
}

func WritePcxtInitialInput(in PcxtInitialInput) ([]byte, error) {
	_, receipt, err := inspectPcxtInitialInput(in)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	return writeCanonicalMembers([]canonicalMember{{"pcxt-initial.json", encoded}, {"package.tar", in.Package}, {"initial-media.img", in.Media.Bytes}}, MaxROMInputSize)
}

func readPcxtInitialInput(data []byte) (PcxtInitialInput, Inspection, error) {
	if !IsPcxtInitialInput(data) || len(data) > MaxROMInputSize {
		return PcxtInitialInput{}, Inspection{}, errors.New("invalid PC/XT input")
	}
	r := &canonicalReader{data: data}
	receiptBytes, err := r.read("pcxt-initial.json", 4096)
	if err != nil {
		return PcxtInitialInput{}, Inspection{}, err
	}
	pkg, err := r.read("package.tar", MaxArchiveSize)
	if err != nil {
		return PcxtInitialInput{}, Inspection{}, err
	}
	disk, err := r.read("initial-media.img", 368640)
	if err != nil {
		return PcxtInitialInput{}, Inspection{}, err
	}
	if err = r.finish(); err != nil {
		return PcxtInitialInput{}, Inspection{}, err
	}
	var sent pcxtInitialReceipt
	dec := json.NewDecoder(bytes.NewReader(receiptBytes))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&sent); err != nil {
		return PcxtInitialInput{}, Inspection{}, err
	}
	in := PcxtInitialInput{Package: pkg, Media: InitialMedia{InterfaceID: "fes.media.pcxt-floppy", GameID: sent.Media.GameID, BaseMediaID: sent.Media.BaseMediaID, Unit: sent.Media.Unit, Bytes: disk}}
	inspection, expected, err := inspectPcxtInitialInput(in)
	if err != nil || sent != expected {
		return PcxtInitialInput{}, Inspection{}, errors.New("PC/XT input receipt does not match bytes")
	}
	encoded, _ := json.Marshal(expected)
	if !bytes.Equal(encoded, receiptBytes) {
		return PcxtInitialInput{}, Inspection{}, errors.New("noncanonical PC/XT receipt")
	}
	return in, inspection, nil
}

func StagePcxtInitialInput(ctx context.Context, root string, data []byte) (Staged, error) {
	in, _, err := readPcxtInitialInput(data)
	if err != nil {
		return Staged{}, err
	}
	staged, err := Stage(ctx, root, int64(len(in.Package)), bytes.NewReader(in.Package))
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
		return Staged{}, errors.New("PC/XT staging root changed")
	}
	name := "pcxt-initial-" + staged.publication
	if err = handle.Mkdir(name, 0700); err != nil {
		return Staged{}, err
	}
	retained, err := handle.Lstat(name)
	if err != nil {
		return Staged{}, err
	}
	staged.companions = append(staged.companions, Staged{root: root, rootInfo: info, publication: name, publicationInfo: retained})
	for file, content := range map[string][]byte{"input.tar": data, "initial-media.img": in.Media.Bytes} {
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
	staged.InitialMedia = stagedInitialMedia(&in.Media, filepath.Join(root, name, "initial-media.img"))
	success = true
	return staged, nil
}

func adoptPcxtInitialInput(handle *os.Root, staged *Staged) error {
	name := "pcxt-initial-" + staged.publication
	info, err := handle.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0500 {
		return errors.New("invalid PC/XT publication")
	}
	files, err := readPublication(handle, name, info, map[string]int64{"input.tar": MaxROMInputSize, "initial-media.img": 368640})
	if err != nil {
		return err
	}
	in, inspection, err := readPcxtInitialInput(files["input.tar"])
	if err != nil || inspection.PackageID != staged.PackageID || !bytes.Equal(in.Media.Bytes, files["initial-media.img"]) {
		return errors.New("PC/XT publication changed")
	}
	after, err := handle.Lstat(name)
	if err != nil || !os.SameFile(info, after) {
		return errors.New("PC/XT publication changed while adopting")
	}
	staged.companions = append(staged.companions, Staged{root: staged.root, rootInfo: staged.rootInfo, publication: name, publicationInfo: info})
	staged.InitialMedia = stagedInitialMedia(&in.Media, filepath.Join(staged.root, name, "initial-media.img"))
	return nil
}
