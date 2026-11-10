package fatcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

func validPlan() Plan {
	return Plan{Format: 1, Scope: "nano-sd-fat-file-v1", CID: strings.Repeat("a", 30), Sectors: 7774208, MBRSHA256: strings.Repeat("b", 64),
		Path: "FES/SDP-0123456789abcdef.JSN", RecoveryBytes: 1024, RecoverySHA256: strings.Repeat("c", 64), FitBytes: 1024, FitSHA256: strings.Repeat("d", 64)}
}

func TestPlanAndPayloadRejectUnboundCardPathAndProtectedFiles(t *testing.T) {
	p := validPlan()
	data, _ := json.Marshal(p)
	if got, err := ParsePlan(data); err != nil || got != p {
		t.Fatal(got, err)
	}
	for _, raw := range [][]byte{nil, append(data, []byte(" true")...), []byte(`{"format":1,"format":1}`), []byte(`{"unknown":1}`), []byte("null"), []byte(strings.Repeat(" ", 4097))} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid plan accepted", string(raw))
		}
	}
	for _, path := range []string{"u-boot.itb", "FES/RECOVERY.JSON", "FES/SDP-0123456789ABCDEF.JSN", "../FES/SDP-0123456789abcdef.JSN", strings.Repeat("a", 8192)} {
		q := p
		q.Path = path
		if q.Validate() == nil {
			t.Fatal("invalid path accepted", path)
		}
		if _, err := Payload(path, true); err == nil {
			t.Fatal("unsafe payload path accepted")
		}
	}
	for _, updated := range []bool{false, true} {
		payload, err := Payload(p.Path, updated)
		if err != nil {
			t.Fatal(err)
		}
		var content map[string]any
		if json.Unmarshal(payload, &content) != nil || content["path"] != p.Path {
			t.Fatal("invalid deterministic JSON")
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
