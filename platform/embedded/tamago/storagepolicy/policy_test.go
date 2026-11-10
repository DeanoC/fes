// SPDX-License-Identifier: GPL-3.0-or-later
package storagepolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTrustedStoragePlanCannotAdoptDiagnosticOrAmbiguousIdentity(t *testing.T) {
	p := Plan{Format: 1, Scope: "nano-owned-fat-storage-v1", CID: strings.Repeat("a", 30), Sectors: 7774208, MBRSHA256: strings.Repeat("b", 64), RecoveryBytes: 1470, RecoverySHA256: strings.Repeat("c", 64), FitBytes: 954248, FitSHA256: strings.Repeat("d", 64)}
	raw, _ := json.Marshal(p)
	if got, err := ParsePlan(raw); err != nil || got != p {
		t.Fatal(got, err)
	}
	for _, invalid := range []string{strings.TrimSuffix(string(raw), "}") + `,"format":1}`, strings.TrimSuffix(string(raw), "}") + `,"path":"FES/OTHER"}`, strings.Replace(string(raw), "nano-owned-fat-storage-v1", "nano-sd-fat-file-v1", 1), strings.Replace(string(raw), p.CID, strings.Repeat("0", 30), 1), string(raw) + " true"} {
		if _, err := ParsePlan([]byte(invalid)); err == nil {
			t.Fatal("unsafe storage plan accepted", invalid)
		}
	}
}

func TestPartitionWriteConfinementRejectsOutsideBootAndBackupBeforeDevice(t *testing.T) {
	d := &confined{first: 32768, end: 294912, backup: 32774}
	for _, request := range []struct {
		lba  uint64
		size int
	}{{0, 512}, {32767, 512}, {32768, 512}, {32774, 512}, {32773, 1024}, {294912, 512}, {294911, 1024}, {32775, 511}} {
		if err := d.WriteSectors(request.lba, make([]byte, request.size)); err == nil {
			t.Fatal("unsafe write accepted", request)
		}
	}
}
