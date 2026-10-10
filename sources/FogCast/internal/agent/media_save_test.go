package agent_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/protocol"
)

type saveRetryRuntime struct {
	unitRuntime
	calls   int
	saved   []protocol.MediaUnitStatus
	saveErr *protocol.APIError
	onSave  func()
}

func (r *saveRetryRuntime) InsertLibraryMedia(context.Context, context.Context, int64, io.Reader, protocol.LibraryMediaBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	panic("unexpected insert")
}
func (r *saveRetryRuntime) SaveMedia(context.Context, protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	r.calls++
	if r.onSave != nil {
		r.onSave()
	}
	return r.saved, r.saveErr
}
func retainedSaveAgentStatus() protocol.Status {
	s := computerAgentStatus()
	s.LastError = &protocol.APIError{Code: protocol.CodeSaveFailed, Phase: "save"}
	p := s.CorePackage
	p.ActiveInterfaces = []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}
	p.PersistenceMode = "persistent"
	p.MediaUnits = []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}
	return s
}
func TestCoordinatorSaveRetryPublishesOnlyConfirmedSameDisk(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*saveRetryRuntime)
		want   bool
	}{
		{"success", func(*saveRetryRuntime) {}, true},
		{"absent checkpoint", func(r *saveRetryRuntime) { r.saved[0].Persistence.Revision = "absent" }, false},
		{"save fails", func(r *saveRetryRuntime) {
			r.saveErr = &protocol.APIError{Code: protocol.CodeSaveFailed, Phase: "save"}
		}, false},
		{"ambiguous transport", func(r *saveRetryRuntime) {
			r.saveErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "transport"}
		}, false},
		{"game drift", func(r *saveRetryRuntime) { r.saved[0].Persistence.GameID = "other-game" }, false},
		{"base drift", func(r *saveRetryRuntime) { r.saved[0].Persistence.BaseMediaID = strings.Repeat("d", 64) }, false},
		{"ejected", func(r *saveRetryRuntime) { r.saved[0].State = "empty"; r.saved[0].Persistence = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := retainedSaveAgentStatus()
			r := &saveRetryRuntime{unitRuntime: unitRuntime{fakeRuntime: fakeRuntime{reconciled: before}}, saved: protocol.CloneMediaUnits(before.CorePackage.MediaUnits)}
			r.saved[0].Persistence.Revision = strings.Repeat("c", 64)
			tc.change(r)
			c := agent.New(r, time.Second, time.Second)
			c.Initialize(context.Background())
			b := protocol.MediaUnitBinding{PackageID: before.CorePackage.PackageID, Generation: before.CorePackage.Generation}
			got, e := c.SaveMedia(context.Background(), b)
			if (e == nil) != tc.want || r.calls != 1 {
				t.Fatalf("err=%v calls=%d", e, r.calls)
			}
			u, _ := protocol.MediaUnit(got.CorePackage, 0)
			if u.Persistence == nil || u.Persistence.GameID != "st-desktop" || u.Persistence.BaseMediaID != strings.Repeat("b", 64) {
				t.Fatal("durable identity lost", u)
			}
			if tc.want {
				if got.LastError != nil || u.Persistence.Revision != r.saved[0].Persistence.Revision {
					t.Fatal("success not published", got)
				}
			} else if got.LastError == nil || u.Persistence.Revision != "absent" {
				t.Fatal("failure cleared error or published revision", got)
			}
			// A second operator command proves the admission token was released. Each command dispatches once.
			_, _ = c.SaveMedia(context.Background(), b)
			if r.calls != 2 {
				t.Fatal("admission retained or request replayed", r.calls)
			}
		})
	}
}
func TestCoordinatorSaveRetryRefusesOtherStateAndKeepsInsertEjectStrict(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*protocol.Status)
	}{
		{"other error", func(s *protocol.Status) { s.LastError.Code = protocol.CodeTransferFailed }},
		{"other phase", func(s *protocol.Status) { s.LastError.Phase = "recovery" }},
		{"recovery", func(s *protocol.Status) { s.Recovery = protocol.RecoveryRebootRequired }},
		{"stale generation", func(s *protocol.Status) { s.CorePackage.Generation++ }},
		{"volatile", func(s *protocol.Status) { s.CorePackage.PersistenceMode = "volatile" }},
		{"ejected", func(s *protocol.Status) { s.CorePackage.MediaUnits[0].State = "empty" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := retainedSaveAgentStatus()
			tc.change(&s)
			r := &saveRetryRuntime{unitRuntime: unitRuntime{fakeRuntime: fakeRuntime{reconciled: s}}}
			c := agent.New(r, time.Second, time.Second)
			c.Initialize(context.Background())
			b := protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 6}
			if _, e := c.SaveMedia(context.Background(), b); e == nil || r.calls != 0 {
				t.Fatal(e, r.calls)
			}
		})
	}
	s := retainedSaveAgentStatus()
	r := &saveRetryRuntime{unitRuntime: unitRuntime{fakeRuntime: fakeRuntime{reconciled: s}}}
	c := agent.New(r, time.Second, time.Second)
	c.Initialize(context.Background())
	b := protocol.MediaUnitBinding{PackageID: s.CorePackage.PackageID, Generation: 6}
	if _, e := c.EjectMedia(context.Background(), b); e == nil || r.ejects != 0 {
		t.Fatal("eject relaxed", e)
	}
	if _, e := c.InsertMedia(context.Background(), 737280, strings.NewReader("unused"), b); e == nil || r.inserts != 0 {
		t.Fatal("insert relaxed", e)
	}
}

func TestCoordinatorSaveRetryDoesNotOverwriteChangedIdentityOrFault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*protocol.Status)
	}{
		{"later fault", func(s *protocol.Status) {
			s.LastError = &protocol.APIError{Code: protocol.CodeTransferFailed, Phase: "transport"}
		}},
		{"new generation", func(s *protocol.Status) { s.CorePackage.Generation++ }},
		{"recovery", func(s *protocol.Status) { s.Recovery = protocol.RecoveryRebootRequired }},
		{"other durable game", func(s *protocol.Status) { s.CorePackage.MediaUnits[0].Persistence.GameID = "other-game" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := retainedSaveAgentStatus()
			changed := retainedSaveAgentStatus()
			tc.change(&changed)
			r := &saveRetryRuntime{unitRuntime: unitRuntime{fakeRuntime: fakeRuntime{reconciled: before}}, saved: protocol.CloneMediaUnits(before.CorePackage.MediaUnits)}
			r.saved[0].Persistence.Revision = strings.Repeat("c", 64)
			c := agent.New(r, time.Second, time.Second)
			c.Initialize(context.Background())
			r.onSave = func() { r.reconciled = changed; c.Initialize(context.Background()) }
			b := protocol.MediaUnitBinding{PackageID: before.CorePackage.PackageID, Generation: before.CorePackage.Generation}
			got, e := c.SaveMedia(context.Background(), b)
			if e == nil || r.calls != 1 {
				t.Fatal("changed status overwritten", e, r.calls)
			}
			if got.LastError == nil || got.LastError.Code != changed.LastError.Code || got.Recovery != changed.Recovery || got.CorePackage.Generation != changed.CorePackage.Generation || got.CorePackage.MediaUnits[0].Persistence.GameID != changed.CorePackage.MediaUnits[0].Persistence.GameID {
				t.Fatal("later status lost", got)
			}
		})
	}
}
