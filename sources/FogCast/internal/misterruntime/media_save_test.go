package misterruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func retainedSaveResponse(t *testing.T) Protocol2Response {
	r := writableSTResponse(t, true)
	r.OK = false
	r.Error = &Protocol2Error{Code: "save_failed", Phase: "save", Message: "private diagnostic"}
	return r
}
func TestRuntimeSaveRetryDispatchesOnceAndChecksDurableIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Protocol2Response)
		want   bool
	}{
		{"success", func(*Protocol2Response) {}, true},
		{"absent checkpoint", func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].Persistence.Revision = "absent" }, false},
		{"other game", func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].Persistence.GameID = "other-game" }, false},
		{"other base", func(r *Protocol2Response) {
			r.Capabilities.MediaUnits[0].Persistence.BaseMediaID = strings.Repeat("d", 64)
		}, false},
		{"stale generation", func(r *Protocol2Response) { g := uint64(8); r.Generation = &g }, false},
		{"retained error", func(r *Protocol2Response) { r.Error = &Protocol2Error{Code: "save_failed", Phase: "save"} }, false},
		{"failed checkpoint", func(r *Protocol2Response) {
			r.OK = false
			r.Error = &Protocol2Error{Code: "save_failed", Phase: "save"}
		}, false},
		{"ejected", func(r *Protocol2Response) {
			r.Capabilities.MediaUnits[0].State = "empty"
			r.Capabilities.MediaUnits[0].Persistence = nil
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := retainedSaveResponse(t)
			after := writableSTResponse(t, true)
			after.Capabilities.MediaUnits[0].Persistence.Revision = strings.Repeat("c", 64)
			tc.change(&after)
			c := &durableMediaControl{computerMediaControl: &computerMediaControl{statuses: []Protocol2Response{before}}, response: after}
			r := NewRuntime(c, "", 0, 0)
			b := protocol.MediaUnitBinding{PackageID: before.ActivePackage.PackageID, Generation: 7}
			units, e := r.SaveMedia(context.Background(), b)
			if (e == nil) != tc.want || c.saveCalls != 1 {
				t.Fatalf("err=%v calls=%d", e, c.saveCalls)
			}
			if tc.want && (len(units) != 1 || units[0].Persistence.Revision != strings.Repeat("c", 64)) {
				t.Fatal(units)
			}
			if _, ok := computerMediaUnitState(before, b); ok {
				t.Fatal("generic response guard relaxed")
			}
		})
	}
}
func TestRuntimeSaveRetryRejectsOtherErrorPhaseAndStateBeforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Protocol2Response)
	}{
		{"other error", func(r *Protocol2Response) { r.Error.Code = "transfer_failed" }},
		{"other phase", func(r *Protocol2Response) { r.Error.Phase = "recovery" }},
		{"missing phase", func(r *Protocol2Response) { r.Error.Phase = "" }},
		{"failed state", func(r *Protocol2Response) { r.State = "failed" }},
		{"idle", func(r *Protocol2Response) { r.State = "idle" }},
		{"other execution", func(r *Protocol2Response) { r.Execution = "legacy" }},
		{"stale package", func(r *Protocol2Response) { r.ActivePackage.PackageID = strings.Repeat("d", 64) }},
		{"stale generation", func(r *Protocol2Response) { g := uint64(8); r.Generation = &g }},
		{"no generation", func(r *Protocol2Response) { r.Generation = nil }},
		{"no writable interface", func(r *Protocol2Response) { r.Capabilities.ActiveInterfaces = nil }},
		{"volatile", func(r *Protocol2Response) { r.ActivePackage.PersistenceMode = "volatile" }},
		{"unbound", func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].Persistence = nil }},
		{"ejected", func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].State = "empty" }},
		{"invalid base", func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].Persistence.BaseMediaID = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := retainedSaveResponse(t)
			id := before.ActivePackage.PackageID
			tc.change(&before)
			c := &durableMediaControl{computerMediaControl: &computerMediaControl{statuses: []Protocol2Response{before}}}
			r := NewRuntime(c, "", 0, 0)
			if _, e := r.SaveMedia(context.Background(), protocol.MediaUnitBinding{PackageID: id, Generation: 7}); e == nil || c.saveCalls != 0 {
				t.Fatal(e, c.saveCalls)
			}
		})
	}
}
