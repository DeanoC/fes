// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"hash/crc32"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

func initialTapeHeader(scope BridgeSnapshot, request computer.InitialTapeRequest) ([]byte, error) {
	if !validBridgeSnapshot(scope) || scope.Capabilities != (BridgeCapabilities{15, 2, 12, FabricHandshakeAbsent}) ||
		scope.ResetMask != 13 || scope.NeverEnabled || !scope.MastersContained ||
		request.Identity.Minor != 0 || request.Identity.Build == [16]byte{} || request.Identity.Capabilities>>16 != 0 ||
		request.Identity.Capabilities&cg.FesComputerCapabilityMediaZx81Tape == 0 ||
		len(request.Image) < 1 || len(request.Image) > int(cg.FesDe25ComputerZx81TapeMaxBytes) || crc32.ChecksumIEEE(request.Image) != request.CRC32 ||
		request.GPO & ^cg.FesComputerRequestMask != cg.FesComputerOpcodeExecution<<24 {
		return nil, errors.New("native fabric: initial tape lacks exact held source and ownership")
	}
	header := make([]byte, g.FesEmbeddedTapeHeaderBytes)
	for offset, value := range map[int]uint32{0: g.FesEmbeddedTapeMagic, 4: g.FesEmbeddedTapeVersion, 8: g.FesEmbeddedTapeHeaderBytes, 12: uint32(len(request.Image)), int(g.FesEmbeddedTapeGpoOffset): request.GPO, int(g.FesEmbeddedTapeCrcOffset): request.CRC32} {
		binary.LittleEndian.PutUint32(header[offset:], value)
	}
	binary.LittleEndian.PutUint64(header[g.FesEmbeddedTapeOwnerOffset:], scope.Owner)
	binary.LittleEndian.PutUint64(header[g.FesEmbeddedTapeGenerationOffset:], scope.Generation)
	copy(header[g.FesEmbeddedTapeBuildOffset:], request.Identity.Build[:])
	digest := sha512.Sum384(request.Image)
	copy(header[g.FesEmbeddedTapeSha384Offset:], digest[:])
	return header, nil
}
