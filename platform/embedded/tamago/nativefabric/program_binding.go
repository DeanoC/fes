// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
)

const programROMHeaderBytes = 128

type programRole uint32

const (
	programRoleMenu     programRole = 1
	programRoleComputer programRole = 2
	programRoleVersion              = 3
	programRoleOffset               = 632
)

func (r programRole) controlABI() uint32 {
	switch r {
	case programRoleMenu:
		return 0x10203043
	case programRoleComputer:
		return 0x10203041
	default:
		return 0
	}
}

// encodeRoleProgramBinding binds an explicit personality to the same per-load
// owner, package, build and staged bytes. Only a matched platform can opt into
// this wire; neither the role nor its ABI grants a master or memory range.
func encodeRoleProgramBinding(source HPSBootObservation, m coldConfigurationMap, role programRole, image []byte) ([]byte, int, error) {
	if role.controlABI() == 0 || role == programRoleMenu && len(image) != 0 {
		return nil, 0, errors.New("native fabric: invalid compiled program role")
	}
	header, err := encodeProgramBinding(source, m)
	if err != nil {
		return nil, 0, err
	}
	raw := make([]byte, ProgramHeaderBytes+programROMHeaderBytes)
	copy(raw, header)
	binary.LittleEndian.PutUint32(raw[4:], programRoleVersion)
	binary.LittleEndian.PutUint32(raw[programRoleOffset:], uint32(role))
	binary.LittleEndian.PutUint32(raw[programRoleOffset+4:], role.controlABI())
	offset := 0
	if role == programRoleComputer {
		var ext []byte
		ext, offset, err = programROMExtension(m.CoreSize, image)
		if err != nil {
			return nil, 0, err
		}
		copy(raw[ProgramHeaderBytes:], ext)
	}
	return raw, offset, nil
}

// encodeROMProgramBinding binds a retained ROM to the same package/build and
// closed owner as the bitstream. It supplies no platform address or authority.
// The ROM immediately follows the aligned bitstream in the existing arena.
func encodeROMProgramBinding(source HPSBootObservation, m coldConfigurationMap, image []byte) ([]byte, int, error) {
	header, err := encodeProgramBinding(source, m)
	if err != nil {
		return nil, 0, err
	}
	ext, offset, err := programROMExtension(m.CoreSize, image)
	if err != nil {
		return nil, 0, err
	}
	raw := make([]byte, ProgramHeaderBytes+programROMHeaderBytes)
	copy(raw, header)
	binary.LittleEndian.PutUint32(raw[4:], 2)
	copy(raw[ProgramHeaderBytes:], ext)
	return raw, offset, nil
}

func programROMExtension(coreBytes int, image []byte) ([]byte, int, error) {
	if coreBytes <= 0 || coreBytes > MaxProgramBytes {
		return nil, 0, errors.New("native fabric: invalid core size for ROM publication")
	}
	offset := (coreBytes + 63) &^ 63
	if (len(image) != 4096 && len(image) != 8192) || offset > MaxProgramBytes || len(image) > MaxProgramBytes-offset {
		return nil, 0, errors.New("native fabric: ROM does not fit the retained program arena")
	}
	// Digest one private snapshot, even when the caller still owns its slice.
	image = append([]byte(nil), image...)
	ext := make([]byte, programROMHeaderBytes)
	for at, value := range map[int]uint32{0: 0x31425246, 4: 1, 8: programROMHeaderBytes,
		12: uint32(offset), 16: uint32(len(image)), 20: crc32.ChecksumIEEE(image)} {
		binary.LittleEndian.PutUint32(ext[at:], value)
	}
	sha256sum, sha384sum := sha256.Sum256(image), sha512.Sum384(image)
	copy(ext[24:], sha256sum[:])
	copy(ext[56:], sha384sum[:])
	return ext, offset, nil
}

const (
	ProgramHeaderBytes   = 640
	ProgramMetadataBytes = 4096
	MaxProgramBytes      = (8 << 20) - ProgramMetadataBytes
	maxProgramChunks     = 9
	programBoundReply    = uint64(1 << 31)
)

// encodeProgramBinding contains byte identities and the already observed scope,
// never a DMA address, bridge mask or hardware capability. Secure firmware reads
// its fixed descriptor location and captures this request before checking the
// actual staged bytes. It is not package authentication or boot attestation.
func encodeProgramBinding(source HPSBootObservation, m coldConfigurationMap) ([]byte, error) {
	if source.Digest == [32]byte{} || source.Bridges.Owner == 0 || source.Bridges.Generation == 0 ||
		m.CoreSize <= 0 || m.CoreSize > MaxProgramBytes || m.CoreSize%4 != 0 ||
		m.ChunkBytes <= 0 || m.ChunkBytes > 0xffff0 || m.ChunkBytes%64 != 0 ||
		len(m.ChunkSHA384) != (m.CoreSize-1)/m.ChunkBytes+1 || len(m.ChunkSHA384) > maxProgramChunks {
		return nil, errors.New("native fabric: invalid per-load program binding")
	}
	raw := make([]byte, ProgramHeaderBytes)
	for offset, value := range map[int]uint32{0: 0x46505231, 4: 1, 8: ProgramHeaderBytes,
		12: uint32(m.CoreSize), 16: uint32(m.ChunkBytes), 20: uint32(len(m.ChunkSHA384))} {
		binary.LittleEndian.PutUint32(raw[offset:], value)
	}
	binary.LittleEndian.PutUint64(raw[24:], source.Bridges.Owner)
	binary.LittleEndian.PutUint64(raw[32:], source.Bridges.Generation)
	copy(raw[40:], source.Digest[:])
	fields := []struct {
		offset, bytes int
		value         string
	}{{72, 32, m.PackageID}, {104, 16, m.BuildID}, {120, 32, m.CoreSHA256}, {152, 48, m.CoreSHA384}}
	for i, value := range m.ChunkSHA384 {
		fields = append(fields, struct {
			offset, bytes int
			value         string
		}{200 + i*48, 48, value})
	}
	for _, field := range fields {
		if !validHex(field.value, field.bytes*2) {
			return nil, errors.New("native fabric: malformed program identity or digest")
		}
		decoded, _ := hex.DecodeString(field.value)
		nonzero := false
		for _, b := range decoded {
			nonzero = nonzero || b != 0
		}
		if !nonzero {
			return nil, errors.New("native fabric: empty program identity or digest")
		}
		copy(raw[field.offset:], decoded)
	}
	return raw, nil
}
