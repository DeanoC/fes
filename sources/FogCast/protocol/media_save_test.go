package protocol

import (
	"strings"
	"testing"
)

func saveRetryStatus() Status {
	return Status{State: StateActive, Development: true, LastError: &APIError{Code: CodeSaveFailed, Phase: "save"}, CorePackage: &CorePackageStatus{
		PackageID: strings.Repeat("a", 64), Generation: 7, ABI: RuntimeContract{ID: "fes.computer", Major: 1}, PersistenceMode: "persistent",
		ActiveInterfaces: []RuntimeInterface{{ID: AtariStFloppyInterface().ID, Major: 1}, {ID: AtariStFloppyWriteInterface().ID, Major: 1}},
		MediaUnits:       []MediaUnitStatus{{Interface: AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: MediaUnitReady, Persistence: &MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}}
}
func TestMediaSaveRetryAdmissionIsNarrow(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Status)
		want   bool
	}{
		{"retained save failure", func(*Status) {}, true},
		{"clean checkpoint", func(s *Status) { s.LastError = nil }, true},
		{"other error", func(s *Status) { s.LastError.Code = CodeTransferFailed }, false},
		{"other phase", func(s *Status) { s.LastError.Phase = "recovery" }, false},
		{"absent phase", func(s *Status) { s.LastError.Phase = "" }, false},
		{"recovery", func(s *Status) { s.Recovery = RecoveryRebootRequired }, false},
		{"idle", func(s *Status) { s.State = StateIdle }, false},
		{"failed", func(s *Status) { s.State = StateFailed }, false},
		{"not development", func(s *Status) { s.Development = false }, false},
		{"stale package", func(s *Status) { s.CorePackage.PackageID = strings.Repeat("c", 64) }, false},
		{"stale generation", func(s *Status) { s.CorePackage.Generation++ }, false},
		{"absent package", func(s *Status) { s.CorePackage = nil }, false},
		{"no writable interface", func(s *Status) { s.CorePackage.ActiveInterfaces = s.CorePackage.ActiveInterfaces[:1] }, false},
		{"other ABI", func(s *Status) { s.CorePackage.ABI.ID = "fes.application" }, false},
		{"volatile package", func(s *Status) { s.CorePackage.PersistenceMode = "volatile" }, false},
		{"unbound", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence = nil }, false},
		{"ejected", func(s *Status) { s.CorePackage.MediaUnits[0].State = MediaUnitEmpty }, false},
		{"loading", func(s *Status) { s.CorePackage.MediaUnits[0].State = MediaUnitLoading }, false},
		{"failed unit", func(s *Status) { s.CorePackage.MediaUnits[0].State = "failed" }, false},
		{"volatile unit", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence.Mode = "volatile" }, false},
		{"invalid game", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence.GameID = "../st" }, false},
		{"invalid base", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence.BaseMediaID = "bad" }, false},
		{"invalid revision", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence.Revision = "bad" }, false},
		{"invalid unit size", func(s *Status) { s.CorePackage.MediaUnits[0].MaxBytes-- }, false},
	}
	b := MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 7}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := saveRetryStatus()
			tc.change(&s)
			if got := b.MatchesForSave(s); got != tc.want {
				t.Fatalf("admission=%v want=%v", got, tc.want)
			}
			if s.LastError != nil && b.Matches(s) {
				t.Fatal("generic media guard relaxed")
			}
			if tc.name == "retained save failure" && s.LastError == nil {
				t.Fatal("predicate cleared error")
			}
		})
	}
	for _, bad := range []MediaUnitBinding{{PackageID: b.PackageID, Generation: 0}, {PackageID: "bad", Generation: 7}, {PackageID: b.PackageID, Generation: 7, Unit: 1}} {
		if bad.MatchesForSave(saveRetryStatus()) {
			t.Fatal("invalid binding admitted", bad)
		}
	}
}
func TestMediaSaveResultKeepsDurableIdentity(t *testing.T) {
	b := MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 7}
	before := saveRetryStatus()
	for _, tc := range []struct {
		name   string
		change func(*Status)
		want   bool
	}{
		{"new revision", func(*Status) {}, true},
		{"absent checkpoint", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence.Revision = "absent" }, false},
		{"other game", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence.GameID = "another-game" }, false},
		{"other base", func(s *Status) { s.CorePackage.MediaUnits[0].Persistence.BaseMediaID = strings.Repeat("d", 64) }, false},
		{"retained error", func(s *Status) { s.LastError = &APIError{Code: CodeSaveFailed, Phase: "save"} }, false},
		{"stale generation", func(s *Status) { s.CorePackage.Generation++ }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := saveRetryStatus()
			after.LastError = nil
			after.CorePackage.MediaUnits[0].Persistence.Revision = strings.Repeat("c", 64)
			tc.change(&after)
			if got := b.MatchesSaveResult(before, after); got != tc.want {
				t.Fatalf("result=%v want=%v", got, tc.want)
			}
		})
	}
}
