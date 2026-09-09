package misterruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func dataResponse(t *testing.T) string {
	t.Helper()
	base := fixtureLines(t, "protocol-v2.jsonl")[1]
	return strings.TrimSuffix(base, "}") + `,"core_data":{"package_id":"` + fixturePackageID + `","core_id":"fes.pong","layout":{"id":"fes.pong.progress","major":1,"minor":0},"mode":"persistent","revision":"absent","paddle_speed":1,"best_rally":0}}`
}
func TestCoreDataProtocolAdmitsExactClosedDescriptor(t *testing.T) {
	if _, err := decodeProtocol2Response([]byte(dataResponse(t))); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{`"revision":"absent"`, `"revision":""`}, {`"paddle_speed":1`, `"paddle_speed":3`}, {`"best_rally":0`, `"best_rally":65536`}, {`"core_id":"fes.pong"`, `"core_id":"../pong"`}, {`"mode":"persistent"`, `"mode":"guess"`}, {`"best_rally":0`, `"best_rally":null`}} {
		if _, err := decodeProtocol2Response([]byte(strings.Replace(dataResponse(t), pair[0], pair[1], 1))); err == nil {
			t.Fatalf("accepted %s", pair[1])
		}
	}
}
func TestCoreDataRPCUsesTrustedRootAndOneMutation(t *testing.T) {
	fixture := newSequenceSocketFixture(t, []string{dataResponse(t) + "\n"})
	client := NewClient(fixture.path)
	_, err := client.UpdateCoreSettings(context.Background(), "/tmp/package", fixturePackageID, "/media/fat/fogcast/core-data", "absent", 2)
	if err != nil {
		t.Fatal(err)
	}
	req := fixture.wait(t)
	if len(req) != 1 {
		t.Fatalf("requests %v", req)
	}
	var got map[string]any
	if json.Unmarshal([]byte(req[0]), &got) != nil {
		t.Fatal(req)
	}
	if got["operation"] != "update_core_settings" || got["data_root"] != "/media/fat/fogcast/core-data" || got["expected_revision"] != "absent" || got["paddle_speed"] != float64(2) || got["package_id"] != fixturePackageID {
		t.Fatal(got)
	}
}

func TestCoreDataRecoveryRetainsActiveIdentity(t *testing.T) {
	line := fixtureLines(t, "protocol-v2.jsonl")[5]
	line = strings.Replace(line, `"state":"running_development"`, `"state":"reboot_required"`, 1)
	line = strings.Replace(line, `"ok":true`, `"ok":false`, 1)
	line = strings.Replace(line, `"error":null`, `"error":{"code":"idle_failed","message":"save recovery","phase":"recovery"}`, 1)
	line = strings.Replace(line, `"persistence_mode":"volatile"`, `"persistence_mode":"persistent"`, 1)
	if _, err := decodeProtocol2Response([]byte(line)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(line, `"generation":1`, `"generation":null`, 1), strings.Replace(line, `"persistence_mode":"persistent"`, `"persistence_mode":"volatile"`, 1)} {
		if _, err := decodeProtocol2Response([]byte(bad)); err == nil {
			t.Fatal("accepted impossible recovery")
		}
	}
}

func TestCoreDataDecoderConsumesRuntimePersistenceFixtures(t *testing.T) {
	for i, line := range fixtureLines(t, "protocol-v2-persistence-responses.jsonl") {
		if _, err := decodeProtocol2Response([]byte(line)); err != nil {
			t.Fatalf("fixture line %d: %v", i+1, err)
		}
	}
}
func TestCoreDataDescriptorConsumesSharedRecordRevisions(t *testing.T) {
	raw, err := os.ReadFile("testdata/core-persistence-v1/records.json")
	if err != nil {
		t.Fatal(err)
	}
	var records struct {
		CoreID string                   `json:"core_id"`
		Layout protocol.RuntimeContract `json:"layout"`
		Valid  []struct {
			Name, Revision, Hex string
			Words               []uint16
		}
	}
	if err = json.Unmarshal(raw, &records); err != nil {
		t.Fatal(err)
	}
	for _, record := range records.Valid {
		wire, err := hex.DecodeString(record.Hex)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(wire)
		if fmt.Sprintf("%x", digest) != record.Revision {
			t.Fatal("fixture revision")
		}
		d := protocol.CoreData{PackageID: fixturePackageID, CoreID: records.CoreID, Layout: &records.Layout, Mode: "persistent", Revision: record.Revision, PaddleSpeed: protocol.PaddleSpeed(record.Words[0]), BestRally: record.Words[1]}
		data, _ := json.Marshal(d)
		base := fixtureLines(t, "protocol-v2.jsonl")[1]
		line := strings.TrimSuffix(base, "}") + `,"core_data":` + string(data) + `}`
		response, err := decodeProtocol2Response([]byte(line))
		if err != nil || response.CoreData.Revision != record.Revision || response.CoreData.BestRally != record.Words[1] {
			t.Fatalf("record=%s err=%v", record.Name, err)
		}
	}
}

func TestCoreDataDecoderAcceptsTypedStoragePhase(t *testing.T) {
	for _, code := range []string{"corrupt_data", "incompatible_data", "stale_revision", "save_failed", "busy"} {
		line := fixtureLines(t, "protocol-v2.jsonl")[1]
		line = strings.Replace(line, `"ok":true`, `"ok":false`, 1)
		line = strings.Replace(line, `"error":null`, `"error":{"code":"`+code+`","message":"data request failed","phase":"core_data"}`, 1)
		if _, err := decodeProtocol2Response([]byte(line)); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
	}
}

func TestOwnedPackageStopUsesProtocol2AndRetainsUnsafeRecovery(t *testing.T) {
	recovery := fixtureLines(t, "protocol-v2-persistence-responses.jsonl")[3]
	fixture := newSequenceSocketFixture(t, []string{recovery + "\n"})
	staged := &corepackage.Staged{PackageID: strings.Repeat("a", 64)}
	runtime := NewRuntime(NewClient(fixture.path), "", time.Millisecond, time.Second)
	runtime.activePackage = staged
	_, mode, apiErr := runtime.StopOwnedWithRecovery(context.Background(), context.Background())
	if apiErr == nil || apiErr.Phase != "recovery" || mode != protocol.RecoveryRebootRequired || runtime.activePackage != staged {
		t.Fatalf("mode=%q error=%v stage=%p", mode, apiErr, runtime.activePackage)
	}
	requests := fixture.wait(t)
	if len(requests) != 1 || requests[0] != `{"protocol":2,"operation":"stop"}` {
		t.Fatalf("requests=%v", requests)
	}
}
