package corepackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// RomInit is the host-produced bitstream programmed after the sealed package
// identity is checked. The target does not recompute it.
type RomInit struct {
	Package     []byte
	Programmed  []byte
	ImageSHA256 string
	Composition []byte
}

type romInitReceipt struct {
	ImageSHA256      string `json:"image_sha256"`
	ProgrammedSHA256 string `json:"programmed_sha256"`
}

func IsRomInit(data []byte) bool {
	if len(data) < 512 {
		return false
	}
	name := data[:100]
	if end := bytes.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	return string(name) == "rom-init.json"
}

func WriteRomInit(bundle RomInit) ([]byte, error) {
	if len(bundle.Package) == 0 || len(bundle.Programmed) == 0 || len(bundle.ImageSHA256) != 64 {
		return nil, errors.New("rom init is incomplete")
	}
	sum := sha256.Sum256(bundle.Programmed)
	receipt, err := json.Marshal(romInitReceipt{
		ImageSHA256:      bundle.ImageSHA256,
		ProgrammedSHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return nil, err
	}
	members := []struct {
		name string
		data []byte
	}{
		{"rom-init.json", receipt},
		{"package.tar", bundle.Package},
		{"programmed.rbf", bundle.Programmed},
	}
	if len(bundle.Composition) > 0 {
		members = append(members, struct {
			name string
			data []byte
		}{"composition.tar", bundle.Composition})
	}
	var out bytes.Buffer
	for _, member := range members {
		if len(member.data) == 0 || int64(len(member.data)) > MaxPayloadSize {
			return nil, fmt.Errorf("rom init member %s is empty or too large", member.name)
		}
		if _, err = out.Write(canonicalHeader(member.name, int64(len(member.data)))); err != nil {
			return nil, err
		}
		if _, err = out.Write(member.data); err != nil {
			return nil, err
		}
		padding := (512 - len(member.data)%512) % 512
		if _, err = out.Write(make([]byte, padding)); err != nil {
			return nil, err
		}
	}
	if _, err = out.Write(make([]byte, 1024)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func ReadRomInit(data []byte) (RomInit, error) {
	if !IsRomInit(data) {
		return RomInit{}, errors.New("rom init archive is missing its receipt")
	}
	receiptBytes, offset, err := romInitMember(data, 0, "rom-init.json")
	if err != nil {
		return RomInit{}, err
	}
	pkg, offset, err := romInitMember(data, offset, "package.tar")
	if err != nil {
		return RomInit{}, err
	}
	programmed, offset, err := romInitMember(data, offset, "programmed.rbf")
	if err != nil {
		return RomInit{}, err
	}
	var composition []byte
	if len(data)-offset != 1024 {
		composition, offset, err = romInitMember(data, offset, "composition.tar")
		if err != nil {
			return RomInit{}, err
		}
	}
	if len(data)-offset != 1024 || !allZero(data[offset:]) {
		return RomInit{}, errors.New("rom init archive must end with exactly two zero blocks")
	}
	var receipt romInitReceipt
	if err = json.Unmarshal(receiptBytes, &receipt); err != nil || len(receipt.ImageSHA256) != 64 || len(receipt.ProgrammedSHA256) != 64 {
		return RomInit{}, errors.New("rom init receipt is invalid")
	}
	sum := sha256.Sum256(programmed)
	if hex.EncodeToString(sum[:]) != receipt.ProgrammedSHA256 {
		return RomInit{}, errors.New("rom init programmed bitstream does not match its receipt")
	}
	return RomInit{Package: pkg, Programmed: programmed, ImageSHA256: receipt.ImageSHA256, Composition: composition}, nil
}

func romInitMember(data []byte, offset int, name string) ([]byte, int, error) {
	if len(data)-offset < 512 {
		return nil, 0, fmt.Errorf("rom init member %s is truncated", name)
	}
	header := data[offset : offset+512]
	size, err := canonicalSize(header[124:136])
	if err != nil || size < 1 || size > MaxPayloadSize || !bytes.Equal(header, canonicalHeader(name, size)) {
		return nil, 0, fmt.Errorf("rom init member %s is not canonical", name)
	}
	offset += 512
	padded := int((size + 511) &^ 511)
	if padded > len(data)-offset {
		return nil, 0, fmt.Errorf("rom init member %s is truncated", name)
	}
	if !allZero(data[offset+int(size) : offset+padded]) {
		return nil, 0, fmt.Errorf("rom init member %s has nonzero padding", name)
	}
	value := append([]byte(nil), data[offset:offset+int(size)]...)
	return value, offset + padded, nil
}

func ArchivePayload(archive []byte) ([]byte, error) {
	manifest, payload, mapping, err := readArchive(archive)
	if err == nil {
		_, err = decode(manifest, payload, mapping)
	}
	return payload, err
}
