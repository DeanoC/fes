package fogcast

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type saveRetryServiceClient struct {
	mediaUnitServiceClient
	calls     int
	afterSave protocol.Status
	saveErr   error
}

func (c *saveRetryServiceClient) InsertLibraryMedia(context.Context, int64, io.Reader, protocol.LibraryMediaBinding) (protocol.Status, error) {
	panic("unexpected insert")
}
func (c *saveRetryServiceClient) SaveMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, error) {
	c.calls++
	return c.afterSave, c.saveErr
}
func retainedSaveServiceStatus() protocol.Status {
	s := computerSessionStatus(strings.Repeat("a", 64), 7, "ready")
	s.LastError = &protocol.APIError{Code: protocol.CodeSaveFailed, Phase: "save"}
	p := s.CorePackage
	p.PersistenceMode = "persistent"
	p.ActiveInterfaces = []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}
	p.MediaUnits = []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}
	return s
}
func newSaveRetryService(c *saveRetryServiceClient) *Service {
	return newService(Config{RequestTimeout: time.Second}, Paths{}, nil, &fakeServiceScanner{}, &fakeServicePreparer{}, c)
}
func TestServiceSaveRetryDispatchesOneExplicitCommand(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*saveRetryServiceClient)
		want   bool
	}{
		{"success", func(*saveRetryServiceClient) {}, true},
		{"absent checkpoint", func(c *saveRetryServiceClient) { c.afterSave.CorePackage.MediaUnits[0].Persistence.Revision = "absent" }, false},
		{"game drift", func(c *saveRetryServiceClient) {
			c.afterSave.CorePackage.MediaUnits[0].Persistence.GameID = "other-game"
		}, false},
		{"base drift", func(c *saveRetryServiceClient) {
			c.afterSave.CorePackage.MediaUnits[0].Persistence.BaseMediaID = strings.Repeat("d", 64)
		}, false},
		{"retained error", func(c *saveRetryServiceClient) {
			c.afterSave.LastError = &protocol.APIError{Code: protocol.CodeSaveFailed, Phase: "save"}
		}, false},
		{"lost reply", func(c *saveRetryServiceClient) {
			c.saveErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "transport"}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := retainedSaveServiceStatus()
			after := retainedSaveServiceStatus()
			after.LastError = nil
			after.CorePackage.MediaUnits[0].Persistence.Revision = strings.Repeat("c", 64)
			c := &saveRetryServiceClient{mediaUnitServiceClient: mediaUnitServiceClient{fakeServiceClient: fakeServiceClient{statusResult: before}}, afterSave: after}
			tc.change(c)
			s := newSaveRetryService(c)
			b := protocol.MediaUnitBinding{PackageID: before.CorePackage.PackageID, Generation: 7, Target: "dev"}
			got, e := s.SaveMedia(context.Background(), b)
			if (e == nil) != tc.want || c.calls != 1 {
				t.Fatalf("err=%v calls=%d status=%+v", e, c.calls, got)
			}
			if before.LastError == nil || before.CorePackage.MediaUnits[0].Persistence.Revision != "absent" {
				t.Fatal("preflight observation modified")
			}
			_, _ = s.SaveMedia(context.Background(), b)
			if c.calls != 2 {
				t.Fatal("lifecycle admission retained or replay", c.calls)
			}
		})
	}
}
func TestServiceSaveRetryKeepsTargetAndGenerationGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*protocol.Status, *protocol.MediaUnitBinding)
	}{
		{"other error", func(s *protocol.Status, _ *protocol.MediaUnitBinding) { s.LastError.Code = protocol.CodeTransferFailed }},
		{"other phase", func(s *protocol.Status, _ *protocol.MediaUnitBinding) { s.LastError.Phase = "recovery" }},
		{"recovery", func(s *protocol.Status, _ *protocol.MediaUnitBinding) { s.Recovery = protocol.RecoveryRebootRequired }},
		{"stale generation", func(_ *protocol.Status, b *protocol.MediaUnitBinding) { b.Generation++ }},
		{"stale package", func(_ *protocol.Status, b *protocol.MediaUnitBinding) { b.PackageID = strings.Repeat("d", 64) }},
		{"other target", func(_ *protocol.Status, b *protocol.MediaUnitBinding) { b.Target = "another-kit" }},
		{"other target identity", func(_ *protocol.Status, b *protocol.MediaUnitBinding) { b.TargetID = "different-target" }},
		{"volatile", func(s *protocol.Status, _ *protocol.MediaUnitBinding) { s.CorePackage.PersistenceMode = "volatile" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := retainedSaveServiceStatus()
			b := protocol.MediaUnitBinding{PackageID: before.CorePackage.PackageID, Generation: 7, Target: "dev"}
			tc.change(&before, &b)
			c := &saveRetryServiceClient{mediaUnitServiceClient: mediaUnitServiceClient{fakeServiceClient: fakeServiceClient{statusResult: before}}}
			s := newSaveRetryService(c)
			if _, e := s.SaveMedia(context.Background(), b); e == nil || c.calls != 0 {
				t.Fatal(e, c.calls)
			}
		})
	}
}
