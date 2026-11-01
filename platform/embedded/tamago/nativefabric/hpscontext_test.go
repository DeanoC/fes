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

func hpsContextFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/hps-context.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func hpsContextReplies(digest [32]byte) [][4]uint64 {
	return [][4]uint64{
		{0, 0x46424f31000000df, 17, 1},
		{0, 0x4648433100000000, binary.BigEndian.Uint64(digest[:8]), binary.BigEndian.Uint64(digest[8:16])},
		{0, 0x4648433100000001, binary.BigEndian.Uint64(digest[16:24]), binary.BigEndian.Uint64(digest[24:])},
		{0, 0x46424f31000000df, 17, 1},
	}
}

func hpsContextCPU(t *testing.T, replies [][4]uint64, failure int, calls *int) FirmwareCaller {
	t.Helper()
	operations := []uint64{0, 4, 5, 0}
	return bridgeFirmwareFunc(func(request [7]uint64) ([4]uint64, error) {
		index := *calls
		*calls = *calls + 1
		if index >= len(operations) {
			t.Fatal("HPS context was retried")
		}
		want := [7]uint64{0xc2000301, operations[index], 17, 0x46455342, 1}
		if index == 0 || index == 3 {
			want[2], want[4] = 0, 0
		}
		if request != want {
			t.Fatalf("HPS context used a mutation or legacy channel: %x", request)
		}
		if index == failure {
			return [4]uint64{}, errors.New("lost synchronous firmware response")
		}
		return replies[index], nil
	})
}

func TestHPSContextUsesCompiledBytesAndRemainsSeparateFromLiveBootAdmission(t *testing.T) {
	raw := hpsContextFixture(t)
	digest := sha256.Sum256(raw)
	calls := 0
	v, err := NewHPSContextVerifier(hpsContextCPU(t, hpsContextReplies(digest), -1, &calls), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := interface{}(v).(BootHandoffVerifier); ok {
		t.Fatal("source context alone became live boot authority")
	}
	id := v.Identity()
	for i := range raw {
		raw[i] = 0 // caller-owned storage cannot alter the compiled expectation
	}
	observed, err := v.Verify(id)
	if err != nil || calls != 4 || observed.Digest != digest || observed.Bridges.Generation != 1 || !observed.Bridges.NeverEnabled {
		t.Fatal(observed, err, calls)
	}
	if id.PackageID != "6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63" {
		t.Fatal("fixture lost its actual sealed-package binding")
	}
}

func TestHPSContextRejectsChangedScopePartialRepliesAndDigestWithoutRetry(t *testing.T) {
	for _, name := range []string{"transport-before", "unready", "open", "masters", "missing-context", "legacy-tag", "wrong-part0", "wrong-part1", "transport-part1", "owner", "generation", "capability", "zero", "digest", "transport-after", "reserved-part", "busy"} {
		t.Run(name, func(t *testing.T) {
			raw := hpsContextFixture(t)
			replies := hpsContextReplies(sha256.Sum256(raw))
			failure, maximum := -1, 4
			switch name {
			case "transport-before":
				failure, maximum = 0, 1
			case "unready":
				replies[0] = [4]uint64{5, 0x46424f3100000000, 0, 0}
				maximum = 1
			case "open":
				replies[0][1], maximum = 0x46424f310000009d, 1
			case "masters":
				replies[0][1], maximum = 0x46424f310000005f, 1
			case "missing-context":
				replies[1] = [4]uint64{5, 0x4648433100000000, 0, 0}
				maximum = 2
			case "legacy-tag":
				replies[1][1], maximum = 0x46424f3100000000, 2
			case "wrong-part0":
				replies[1][1]++
				maximum = 2
			case "wrong-part1":
				replies[2][1]--
				maximum = 3
			case "transport-part1":
				failure, maximum = 2, 3
			case "owner":
				replies[3][2] += 2
			case "generation":
				replies[3][3]++
			case "capability":
				replies[3][1] = 0x46424f31000000ef
			case "zero":
				replies[1][2], replies[1][3], replies[2][2], replies[2][3] = 0, 0, 0, 0
			case "digest":
				replies[1][2] ^= 1
			case "transport-after":
				failure = 3
			case "reserved-part":
				replies[1][1] |= 0x100
				maximum = 2
			case "busy":
				replies[2][0], maximum = 1, 3
			}
			calls := 0
			v, err := NewHPSContextVerifier(hpsContextCPU(t, replies, failure, &calls), raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.Verify(v.Identity())
			if err == nil || calls != maximum || observed != (HPSBootObservation{}) {
				t.Fatal("failed HPS context published/retried", observed, err, calls)
			}
		})
	}
}

func TestHPSContextRejectsWrongPackageAndUnqualifiedDescriptorBeforeFirmware(t *testing.T) {
	raw := hpsContextFixture(t)
	cpu := bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) {
		t.Fatal("invalid source/package reached firmware")
		return [4]uint64{}, nil
	})
	v, err := NewHPSContextVerifier(cpu, raw)
	if err != nil {
		t.Fatal(err)
	}
	id := v.Identity()
	id.ProgrammedSHA256 = "0" + id.ProgrammedSHA256[1:]
	if _, err := v.Verify(id); err == nil {
		t.Fatal("different programmed bytes matched source context")
	}
	for key, value := range map[string]interface{}{"scope": "trusted", "format": 2, "device": "other", "core_size": 0, "control_abi": 1, "package_id": "", "extra": true} {
		var fields map[string]interface{}
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		fields[key] = value
		changed, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewHPSContextVerifier(cpu, append(changed, '\n')); err == nil {
			t.Fatal("unsupported context accepted", key)
		}
	}
	for _, changed := range [][]byte{nil, append(raw, raw...), append([]byte(" "), raw...), []byte(`{"format":1,"format":1}`)} {
		if _, err := NewHPSContextVerifier(cpu, changed); err == nil {
			t.Fatal("noncanonical or duplicate descriptor accepted")
		}
	}
}
