// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func hpsIOFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/hps-io.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func hpsIOReplies(context, ioMap []byte) [][4]uint64 {
	digest := sha256.Sum256(ioMap)
	r := hpsContextReplies(sha256.Sum256(context))
	r = append(r, [4]uint64{0, 0x4648493100000000, binary.BigEndian.Uint64(digest[:8]), binary.BigEndian.Uint64(digest[8:16])},
		[4]uint64{0, 0x4648493100000001, binary.BigEndian.Uint64(digest[16:24]), binary.BigEndian.Uint64(digest[24:])})
	return append(r, hpsContextReplies(sha256.Sum256(context))...)
}

func hpsIOCPU(t *testing.T, replies [][4]uint64, failure int, calls *int) FirmwareCaller {
	t.Helper()
	ops := []uint64{0, 4, 5, 0, 12, 13, 0, 4, 5, 0}
	return bridgeFirmwareFunc(func(request [7]uint64) ([4]uint64, error) {
		index := *calls
		*calls++
		if index >= len(ops) {
			t.Fatal("HPS I/O was retried")
		}
		want := [7]uint64{0xc2000301, ops[index], 17, 0x46455342, 1}
		if ops[index] == 0 {
			want[2], want[4] = 0, 0
		}
		if request != want {
			t.Fatalf("HPS I/O used a mutation, address or different scope: %x", request)
		}
		if index == failure {
			return [4]uint64{}, errors.New("lost HPS I/O transport")
		}
		return replies[index], nil
	})
}

func TestHPSIORequiresBothSecureHalvesAndUnchangedColdSource(t *testing.T) {
	raw, mapping := hpsContextFixture(t), hpsIOFixture(t)
	calls := 0
	v, err := NewHPSContextVerifier(hpsIOCPU(t, hpsIOReplies(raw, mapping), -1, &calls), raw)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := v.ProbeHPSIO(mapping)
	if err != nil || observation.Count != 164 || observation.Digest != sha256.Sum256(mapping) ||
		observation.Source.Digest != sha256.Sum256(raw) || observation.Source.Bridges.Generation != 1 || calls != 10 {
		t.Fatal(observation, err, calls)
	}
	if _, ok := interface{}(v).(BootHandoffVerifier); ok {
		t.Fatal("pinmux subset granted general boot admission")
	}
}

func TestHPSIORejectsEveryTransportAndStatusFailureWithoutPublishingOrRetry(t *testing.T) {
	raw, mapping := hpsContextFixture(t), hpsIOFixture(t)
	for position := 0; position < 10; position++ {
		for _, transport := range []bool{true, false} {
			replies := hpsIOReplies(raw, mapping)
			failure := -1
			if transport {
				failure = position
			} else {
				replies[position][0] = 1
			}
			calls := 0
			v, err := NewHPSContextVerifier(hpsIOCPU(t, replies, failure, &calls), raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.ProbeHPSIO(mapping)
			if err == nil || observed != (HPSIOObservation{}) || calls != position+1 {
				t.Fatal(position, transport, observed, err, calls)
			}
		}
	}
	for _, name := range []string{"part0-tag", "part1-tag", "part0-index", "part1-index", "part0-digest", "part1-digest", "final-owner", "final-generation", "final-open", "source-digest", "warm"} {
		t.Run(name, func(t *testing.T) {
			replies := hpsIOReplies(raw, mapping)
			switch name {
			case "part0-tag":
				replies[4][1] = 0x4648433100000000
			case "part1-tag":
				replies[5][1] = 0x4648433100000001
			case "part0-index":
				replies[4][1]++
			case "part1-index":
				replies[5][1]--
			case "part0-digest":
				replies[4][2] ^= 1
			case "part1-digest":
				replies[5][3] ^= 1
			case "final-owner":
				replies[9][2] += 2
			case "final-generation":
				replies[9][3]++
			case "final-open":
				replies[9][1] &= ^uint64(2)
			case "source-digest":
				replies[7][2] ^= 1
			case "warm":
				replies[0][1] &= ^uint64(0x40)
				replies[3][1] = replies[0][1]
			}
			calls := 0
			v, err := NewHPSContextVerifier(hpsIOCPU(t, replies, -1, &calls), raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.ProbeHPSIO(mapping)
			if err == nil || observed != (HPSIOObservation{}) || calls > 10 {
				t.Fatal(name, observed, err, calls)
			}
		})
	}
}

func TestHPSIORejectsIncompleteOrArbitraryMapsBeforeFirmware(t *testing.T) {
	cpu := bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) {
		t.Fatal("bad HPS I/O map reached firmware")
		return [4]uint64{}, nil
	})
	v, err := NewHPSContextVerifier(cpu, hpsContextFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	raw := hpsIOFixture(t)
	for _, name := range []string{"format", "base", "scope", "handoff", "iohash", "missing-row", "extra-row", "offset", "duplicate", "mask", "value", "missing-value", "extra-field"} {
		var fields map[string]interface{}
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		rows := fields["registers"].([]interface{})
		row := rows[0].(map[string]interface{})
		switch name {
		case "format":
			fields["format"] = true
		case "base":
			fields["base"] = 0x10d12004
		case "scope":
			fields["scope"] = "qualified"
		case "handoff":
			fields["handoff_sha256"] = ""
		case "iohash":
			fields["hps_io_hash"] = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		case "missing-row":
			fields["registers"] = rows[:163]
		case "extra-row":
			fields["registers"] = append(rows, row)
		case "offset":
			row["offset"] = 0x14c0
		case "duplicate":
			rows[1] = row
		case "mask":
			row["mask"] = 15
		case "value":
			row["value"] = 16
		case "missing-value":
			delete(row, "value")
		case "extra-field":
			row["write"] = true
		}
		changed, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if observed, err := v.ProbeHPSIO(append(changed, '\n')); err == nil || observed != (HPSIOObservation{}) {
			t.Fatal(name, observed, err)
		}
	}
	for _, changed := range [][]byte{nil, make([]byte, 32769), append([]byte(" "), raw...), append(raw, raw...), []byte(`{"format":1,"format":1}`)} {
		if _, err := v.ProbeHPSIO(changed); err == nil {
			t.Fatal("noncanonical HPS I/O map accepted")
		}
	}
	if _, err := (*HPSContextVerifier)(nil).ProbeHPSIO(raw); err == nil {
		t.Fatal("nil verifier accepted")
	}
}
