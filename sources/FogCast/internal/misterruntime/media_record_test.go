package misterruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func mediaRecordResponse(t *testing.T, record string) string {
	t.Helper()
	base := fixtureLines(t, "protocol-v2.jsonl")[1]
	return strings.TrimSuffix(base, "}") + `,"media_data":` + record + "}"
}

func TestSavedMediaRecordProtocolShape(t *testing.T) {
	revision := strings.Repeat("b", 64)
	for _, record := range []string{
		`{"state":"absent","revision":"absent","bytes":0}`,
		`{"state":"present","revision":"` + revision + `","bytes":737453}`,
		`{"state":"corrupt","revision":"` + revision + `","bytes":3}`,
	} {
		if _, err := decodeProtocol2Response([]byte(mediaRecordResponse(t, record))); err != nil {
			t.Fatalf("%s: %v", record, err)
		}
	}
	for _, record := range []string{
		`{"state":"absent","revision":"absent"}`,
		`{"state":"absent","revision":"absent","bytes":0,"extra":1}`,
		`{"state":"present","revision":"` + revision + `","bytes":"737453"}`,
		`null`,
	} {
		if _, err := decodeProtocol2Response([]byte(mediaRecordResponse(t, record))); err == nil {
			t.Fatalf("accepted %s", record)
		}
	}
}

func TestSavedMediaRecordRPCNamesOneRecordUnderTheTrustedRoot(t *testing.T) {
	id := protocol.MediaRecordIdentity{CoreID: "fes.atari-st", GameID: "atari-st-desktop", BaseMediaID: strings.Repeat("a", 64)}
	revision := strings.Repeat("b", 64)
	fixture := newSequenceSocketFixture(t, []string{
		mediaRecordResponse(t, `{"state":"present","revision":"`+revision+`","bytes":737453}`) + "\n",
		mediaRecordResponse(t, `{"state":"absent","revision":"absent","bytes":0}`) + "\n",
	})
	client := NewClient(fixture.path)
	inspected, err := client.InspectMediaData(context.Background(), MediaDataRoot, id)
	if err != nil || inspected.MediaData == nil || inspected.MediaData.Revision != revision {
		t.Fatalf("inspect %+v %v", inspected.MediaData, err)
	}
	discarded, err := client.DiscardMediaData(context.Background(), MediaDataRoot, protocol.MediaRecordDiscard{MediaRecordIdentity: id, ExpectedRevision: revision})
	if err != nil || discarded.MediaData == nil || discarded.MediaData.State != protocol.MediaRecordAbsent {
		t.Fatalf("discard %+v %v", discarded.MediaData, err)
	}
	requests := fixture.wait(t)
	if len(requests) != 2 {
		t.Fatalf("requests %v", requests)
	}
	var inspect, discard map[string]any
	if json.Unmarshal([]byte(requests[0]), &inspect) != nil || json.Unmarshal([]byte(requests[1]), &discard) != nil {
		t.Fatal(requests)
	}
	if inspect["operation"] != "inspect_media_data" || inspect["data_root"] != "/media/fat/fogcast/core-data/media" || inspect["core_id"] != id.CoreID ||
		inspect["game_id"] != id.GameID || inspect["base_media_id"] != id.BaseMediaID || inspect["unit"] != float64(0) || len(inspect) != 7 {
		t.Fatalf("inspect request %v", inspect)
	}
	if discard["operation"] != "discard_media_data" || discard["expected_revision"] != revision || len(discard) != 8 {
		t.Fatalf("discard request %v", discard)
	}
	// An invalid identity never reaches the runtime.
	bad := id
	bad.Unit = 1
	if _, err := client.InspectMediaData(context.Background(), MediaDataRoot, bad); err == nil {
		t.Fatal("unit 1 sent")
	}
	if _, err := client.DiscardMediaData(context.Background(), MediaDataRoot, protocol.MediaRecordDiscard{MediaRecordIdentity: id, ExpectedRevision: "absent"}); err == nil {
		t.Fatal("absent revision sent")
	}
}
