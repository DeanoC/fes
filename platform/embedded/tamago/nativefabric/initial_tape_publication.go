// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
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

// PublishInitialTape reuses only a completed program arena under its original
// pinned mapping. The trusted caller must already own the control-only scope;
// this method supplies no boot/device/firmware permission. It sends no address.
// An uncertain publication, firmware response, mapping or source retires the
// arena without releasing its memory. Completion is private to this method;
// old Region callbacks cannot release the new lease. No selected call retries.
func (a *ConfigurationArena) PublishInitialTape(ctx context.Context, caller FirmwareCaller, scope BridgeSnapshot, request computer.InitialTapeRequest) (out computer.InitialTapeReceipt, err error) {
	if a == nil || ctx == nil || caller == nil {
		return out, errors.New("native fabric: initial tape publisher missing")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed || a.address != 0x94800000 || len(a.memory) != MaxProgramBytes || len(a.metadata) != ProgramMetadataBytes ||
		a.active == nil || a.active == a.tapeRegion || !a.ready || a.tapeGeneration != 0 && (scope.Owner != a.tapeOwner || scope.Generation <= a.tapeGeneration) {
		return out, errors.New("native fabric: initial tape arena unavailable, outstanding or consumed")
	}
	request.Image = bytes.Clone(request.Image)
	header, err := initialTapeHeader(scope, request)
	if err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = a.barrier(); err != nil {
		a.failed = true
		return out, err
	}
	a.active = &Region{Address: a.address, Bytes: a.memory[:len(request.Image):len(request.Image)]}
	a.tapeRegion = a.active
	a.ready = false
	a.tapeOwner, a.tapeGeneration = scope.Owner, scope.Generation
	complete := false
	defer func() {
		if !complete {
			a.failed = true
		}
	}()
	clear(a.metadata)
	copy(a.metadata, header)
	copy(a.active.Bytes, request.Image)
	if err = a.barrier(); err != nil {
		return out, err
	}
	unchanged := func() bool {
		return bytes.Equal(a.active.Bytes, request.Image) && bytes.Equal(a.metadata[:len(header)], header) && bytes.Equal(a.metadata[len(header):], make([]byte, len(a.metadata)-len(header)))
	}
	if !unchanged() {
		return out, errors.New("native fabric: initial tape changed during publication")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	r, err := caller.Invoke([7]uint64{uint64(g.FesEmbeddedTapeCall), uint64(g.FesEmbeddedTapeLoad), scope.Owner, uint64(g.FesEmbeddedTapeCookie), scope.Generation})
	if err != nil {
		return out, err
	}
	if r[1]>>32 != uint64(g.FesEmbeddedTapeTag) {
		return out, errors.New("native fabric: unknown initial tape firmware tag")
	}
	if r[0] == uint64(g.FesEmbeddedTapeLimit) {
		return out, fmt.Errorf("%w: secure initial tape limit", computer.ErrLimit)
	}
	if r[0] != uint64(g.FesEmbeddedTapeOK) || r[1] != uint64(g.FesEmbeddedTapeTag)<<32|uint64(len(request.Image)) || r[2] != scope.Generation {
		return out, fmt.Errorf("native fabric: initial tape firmware response rejected (status=%d)", r[0])
	}
	receipt := computer.InitialTapeReceipt{GPO: uint32(r[3]), Bytes: uint32(r[1]), CRC32: uint32(r[3] >> 32)}
	if err = computer.ValidateInitialTapeReceipt(request, receipt); err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = a.barrier(); err != nil {
		return out, err
	}
	if !unchanged() {
		return out, errors.New("native fabric: initial tape publication changed before completion")
	}
	a.ready = true
	complete = true
	return receipt, nil
}
