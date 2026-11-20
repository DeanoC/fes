package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaRecordHTTPController struct {
	mediaUnitHTTPController
	inspected []protocol.MediaRecordIdentity
	discarded []protocol.MediaRecordDiscard
	exported  []protocol.MediaRecordDiscard
	err       *protocol.APIError
}

func (c *mediaRecordHTTPController) InspectMediaRecord(_ context.Context, id protocol.MediaRecordIdentity) (protocol.MediaRecord, *protocol.APIError) {
	c.inspected = append(c.inspected, id)
	return protocol.MediaRecord{State: protocol.MediaRecordPresent, Revision: strings.Repeat("b", 64), Bytes: 737453, Changed: true}, c.err
}

func (c *mediaRecordHTTPController) DiscardMediaRecord(_ context.Context, d protocol.MediaRecordDiscard) (protocol.MediaRecord, *protocol.APIError) {
	c.discarded = append(c.discarded, d)
	return protocol.MediaRecord{State: protocol.MediaRecordAbsent, Revision: "absent"}, c.err
}

func (c *mediaRecordHTTPController) ExportMediaRecord(_ context.Context, d protocol.MediaRecordDiscard) ([]byte, *protocol.APIError) {
	c.exported = append(c.exported, d)
	return []byte(strings.Repeat("d", 737280)), c.err
}

func TestSavedMediaRecordRoutes(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &mediaRecordHTTPController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	id := protocol.MediaRecordIdentity{CoreID: "fes.atari-st", GameID: "atari-st-desktop", BaseMediaID: strings.Repeat("a", 64)}
	revision := strings.Repeat("b", 64)
	send := func(path, body string, change func(*http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer bearer")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(httpapi.KitLeaseHeader, grant.Token)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	identity, _ := json.Marshal(id)
	discard, _ := json.Marshal(protocol.MediaRecordDiscard{MediaRecordIdentity: id, ExpectedRevision: revision})

	// Inspect changes nothing, so it needs no lease.
	w := send("/v1/library/media/record", string(identity), func(r *http.Request) { r.Header.Del(httpapi.KitLeaseHeader) })
	var record protocol.MediaRecord
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &record) != nil || record.State != protocol.MediaRecordPresent || record.Revision != revision || !record.Changed ||
		len(c.inspected) != 1 || c.inspected[0] != id {
		t.Fatalf("inspect %d %s %+v", w.Code, w.Body, c.inspected)
	}
	w = send("/v1/library/media/record/discard", string(discard), nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &record) != nil || record.State != protocol.MediaRecordAbsent ||
		len(c.discarded) != 1 || c.discarded[0].MediaRecordIdentity != id || c.discarded[0].ExpectedRevision != revision {
		t.Fatalf("discard %d %s %+v", w.Code, w.Body, c.discarded)
	}
	// Export hands back the saved image and needs no lease.
	w = send("/v1/library/media/record/export", string(discard), func(r *http.Request) { r.Header.Del(httpapi.KitLeaseHeader) })
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/octet-stream" || w.Body.Len() != 737280 ||
		len(c.exported) != 1 || c.exported[0].ExpectedRevision != revision {
		t.Fatalf("export %d %d bytes %+v", w.Code, w.Body.Len(), c.exported)
	}
	if w = send("/v1/library/media/record/export", string(identity), nil); w.Code != 400 || len(c.exported) != 1 {
		t.Fatalf("export without a revision: %d", w.Code)
	}
	// Discard removes saved data, so it needs the kit lease.
	if w = send("/v1/library/media/record/discard", string(discard), func(r *http.Request) { r.Header.Del(httpapi.KitLeaseHeader) }); w.Code == 200 || len(c.discarded) != 1 {
		t.Fatalf("unleased discard %d", w.Code)
	}
	for name, body := range map[string]string{
		"revision on inspect": string(discard),
		"unknown field":       `{"core_id":"fes.atari-st","game_id":"g","base_media_id":"` + id.BaseMediaID + `","unit":0,"extra":1}`,
		"other unit":          `{"core_id":"fes.atari-st","game_id":"g","base_media_id":"` + id.BaseMediaID + `","unit":1}`,
		"bad game":            `{"core_id":"fes.atari-st","game_id":"-g","base_media_id":"` + id.BaseMediaID + `","unit":0}`,
		"bad core":            `{"core_id":"Fes","game_id":"g","base_media_id":"` + id.BaseMediaID + `","unit":0}`,
		"bad base":            `{"core_id":"fes.atari-st","game_id":"g","base_media_id":"abc","unit":0}`,
		"trailing":            string(identity) + "{}",
	} {
		if w = send("/v1/library/media/record", body, nil); w.Code != 400 {
			t.Fatalf("%s admitted: %d", name, w.Code)
		}
	}
	for name, body := range map[string]string{
		"no revision":     string(identity),
		"absent revision": strings.Replace(string(discard), revision, "absent", 1),
	} {
		if w = send("/v1/library/media/record/discard", body, nil); w.Code != 400 {
			t.Fatalf("discard %s admitted: %d", name, w.Code)
		}
	}
	if w = send("/v1/library/media/record", string(identity), func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }); w.Code != 400 {
		t.Fatalf("wrong content type admitted: %d", w.Code)
	}
	if len(c.inspected) != 1 || len(c.discarded) != 1 {
		t.Fatalf("invalid requests dispatched: %d %d", len(c.inspected), len(c.discarded))
	}
	// The kit's refusal while a core runs reaches the caller as busy.
	c.err = &protocol.APIError{Code: protocol.CodeBusy, Message: "stop the running core before discarding saved media"}
	if w = send("/v1/library/media/record/discard", string(discard), nil); w.Code != http.StatusConflict && w.Code != http.StatusServiceUnavailable && w.Code != http.StatusLocked {
		t.Fatalf("busy discard status %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "BUSY") {
		t.Fatalf("busy discard body %s", w.Body)
	}
}

func TestSavedMediaRecordValidity(t *testing.T) {
	digest := strings.Repeat("c", 64)
	for _, tc := range []struct {
		record protocol.MediaRecord
		valid  bool
	}{
		{protocol.MediaRecord{State: "absent", Revision: "absent"}, true},
		{protocol.MediaRecord{State: "present", Revision: digest, Bytes: 737453}, true},
		{protocol.MediaRecord{State: "corrupt", Revision: digest, Bytes: 3}, true},
		{protocol.MediaRecord{State: "present", Revision: digest, Bytes: 737453, Changed: true}, true},
		{protocol.MediaRecord{State: "absent", Revision: "absent", Changed: true}, false},
		{protocol.MediaRecord{State: "corrupt", Revision: digest, Bytes: 3, Changed: true}, false},
		{protocol.MediaRecord{State: "absent", Revision: digest}, false},
		{protocol.MediaRecord{State: "absent", Revision: "absent", Bytes: 1}, false},
		{protocol.MediaRecord{State: "present", Revision: "absent", Bytes: 1}, false},
		{protocol.MediaRecord{State: "present", Revision: digest}, false},
		{protocol.MediaRecord{State: "kept", Revision: digest, Bytes: 1}, false},
	} {
		if tc.record.Valid() != tc.valid {
			t.Fatalf("%+v valid=%v", tc.record, !tc.valid)
		}
	}
}

func TestSavedMediaRecordBatch(t *testing.T) {
	c := &mediaRecordHTTPController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c))
	send := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/library/media/records", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer bearer")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	ids := []protocol.MediaRecordIdentity{
		{CoreID: "fes.atari-st", GameID: "one", BaseMediaID: strings.Repeat("a", 64)},
		{CoreID: "fes.atari-st", GameID: "two", BaseMediaID: strings.Repeat("a", 64)},
	}
	body, _ := json.Marshal(map[string]any{"records": ids})
	w := send(string(body))
	var got struct {
		Records []protocol.MediaRecord `json:"records"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Records) != 2 ||
		len(c.inspected) != 2 || c.inspected[0] != ids[0] || c.inspected[1] != ids[1] {
		t.Fatalf("batch %d %s %+v", w.Code, w.Body, c.inspected)
	}
	many := make([]protocol.MediaRecordIdentity, protocol.MaxMediaRecordBatch+1)
	for i := range many {
		many[i] = ids[0]
	}
	tooMany, _ := json.Marshal(map[string]any{"records": many})
	bad := ids[1]
	bad.Unit = 1
	oneBad, _ := json.Marshal(map[string]any{"records": []protocol.MediaRecordIdentity{ids[0], bad}})
	for name, body := range map[string]string{
		"empty": `{"records":[]}`, "none": `{}`, "too many": string(tooMany), "one bad": string(oneBad),
		"unknown field": `{"records":[],"extra":1}`,
	} {
		if w = send(body); w.Code != 400 {
			t.Fatalf("%s admitted: %d", name, w.Code)
		}
	}
	if len(c.inspected) != 2 {
		t.Fatalf("invalid batch dispatched: %d", len(c.inspected))
	}
	// One refused inspection fails the whole answer rather than shortening it.
	c.err = &protocol.APIError{Code: protocol.CodeBusy, Message: "runtime is busy"}
	if w = send(string(body)); w.Code == 200 || !strings.Contains(w.Body.String(), "BUSY") {
		t.Fatalf("busy batch %d %s", w.Code, w.Body)
	}
}
