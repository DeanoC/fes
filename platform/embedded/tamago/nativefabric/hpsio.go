// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

const hpsIOCount = 164

type hpsIORegister struct {
	Mask   uint32 `json:"mask"`
	Offset uint32 `json:"offset"`
	Value  uint32 `json:"value"`
}

type hpsIOMap struct {
	Base      uint64          `json:"base"`
	Format    uint32          `json:"format"`
	Handoff   string          `json:"handoff_sha256"`
	HPSIOHash string          `json:"hps_io_hash"`
	Registers []hpsIORegister `json:"registers"`
	Scope     string          `json:"scope"`
}

// HPSIOObservation reports the fixed pinmux subset only. The source context,
// live reads and owned closed bridges do not qualify other platform resources
// and do not supply BootHandoffVerifier or FPGA programming authority.
type HPSIOObservation struct {
	Source HPSBootObservation
	Digest [32]byte
	Count  uint32
}

func hpsIOOffset(offset uint32) bool {
	if offset&3 != 0 {
		return false
	}
	if offset >= 0x1000 && offset <= 0x109c || offset >= 0x1100 && offset <= 0x111c ||
		offset >= 0x1130 && offset <= 0x119c || offset >= 0x1200 && offset <= 0x124c ||
		offset >= 0x1400 && offset <= 0x14bc {
		return true
	}
	return offset >= 0x1300 && offset <= 0x1354 && offset != 0x1324 && offset != 0x134c
}

func (v *HPSContextVerifier) hpsIOMap(raw []byte) (hpsIOMap, error) {
	var mapping hpsIOMap
	if v == nil || len(raw) == 0 || len(raw) > 32<<10 {
		return mapping, errors.New("native fabric: bounded compiled HPS I/O map required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&mapping); err != nil {
		return hpsIOMap{}, err
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		return hpsIOMap{}, errors.New("native fabric: trailing HPS I/O map")
	}
	// A recursive round trip rejects duplicate fields, whitespace and reordered
	// nested rows, as well as noncanonical top-level bytes.
	if err := json.Unmarshal(raw, &extra); err != nil {
		return hpsIOMap{}, err
	}
	fields, ok := extra.(map[string]interface{})
	if !ok || len(fields) != 6 {
		return hpsIOMap{}, errors.New("native fabric: incomplete HPS I/O map fields")
	}
	canonical, err := json.Marshal(extra)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) || mapping.Format != 1 ||
		mapping.Scope != "sealed-cold-hps-io-v1" || mapping.Base != 0x10d12000 ||
		mapping.HPSIOHash != v.context.HPSIOHash || !nonzeroDigest(mapping.Handoff) ||
		len(mapping.Registers) != hpsIOCount {
		return hpsIOMap{}, errors.New("native fabric: unsupported complete HPS I/O map")
	}
	index := 0
	rows, ok := fields["registers"].([]interface{})
	if !ok {
		return hpsIOMap{}, errors.New("native fabric: incomplete HPS I/O rows")
	}
	for offset := uint32(0x1000); offset <= 0x14bc; offset += 4 {
		if !hpsIOOffset(offset) {
			continue
		}
		if index >= len(mapping.Registers) {
			return hpsIOMap{}, errors.New("native fabric: incomplete HPS I/O offsets")
		}
		fields, ok := rows[index].(map[string]interface{})
		if !ok || len(fields) != 3 {
			return hpsIOMap{}, errors.New("native fabric: incomplete HPS I/O row fields")
		}
		row := mapping.Registers[index]
		index++
		if row.Offset != offset || row.Mask != 0xffffffff || offset < 0x1130 && row.Value > 15 ||
			offset >= 0x1300 && offset <= 0x1354 && row.Value > 1 {
			return hpsIOMap{}, errors.New("native fabric: unsupported HPS I/O register, mask or value")
		}
	}
	if index != hpsIOCount {
		return hpsIOMap{}, errors.New("native fabric: incomplete HPS I/O register set")
	}
	return mapping, nil
}

// ProbeHPSIO requests two full secure read-only samples of the compiled map.
// Only the fixed register set is sent to the compiler; SMC takes no address or
// data pointer. A partial response, stale scope or changed digest rejects.
func (v *HPSContextVerifier) ProbeHPSIO(raw []byte) (HPSIOObservation, error) {
	if _, err := v.hpsIOMap(raw); err != nil {
		return HPSIOObservation{}, err
	}
	want := sha256.Sum256(raw)
	source, err := v.Verify(v.Identity())
	if err != nil {
		return HPSIOObservation{}, err
	}
	if !v.coldSource(source) || source.Bridges.Generation != 1 {
		return HPSIOObservation{}, errors.New("native fabric: HPS I/O requires the unchanged cold closed scope")
	}
	var observed [32]byte
	for part, operation := range []uint32{g.FesEmbeddedBridgeHpsIoPart0, g.FesEmbeddedBridgeHpsIoPart1} {
		r, err := v.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(operation), source.Bridges.Owner,
			uint64(g.FesEmbeddedBridgeCookie), source.Bridges.Generation})
		if err != nil {
			return HPSIOObservation{}, err
		}
		if r[0] != 0 || r[1] != uint64(g.FesEmbeddedBridgeHpsIoTag)<<32|uint64(part) {
			return HPSIOObservation{}, fmt.Errorf("native fabric: HPS I/O part %d rejected, status %d", part, r[0])
		}
		binary.BigEndian.PutUint64(observed[part*16:], r[2])
		binary.BigEndian.PutUint64(observed[part*16+8:], r[3])
	}
	after, err := v.Verify(v.Identity())
	if err != nil || source != after || observed != want {
		return HPSIOObservation{}, errors.New("native fabric: HPS I/O lost ownership or differs from compiled handoff")
	}
	return HPSIOObservation{Source: source, Digest: observed, Count: hpsIOCount}, nil
}
