package fogcast

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/protocol"
	"strings"
	"testing"
	"time"
)

func testBoundStop() (*Service, *fakeServiceClient, SessionStopBinding) {
	c := &fakeServiceClient{stopResult: protocol.Status{State: protocol.StateIdle}}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, c)
	s.targets = []TargetConfig{{Name: "kit", TargetID: "kit-id"}}
	s.targetClients["kit"] = c
	s.activeExecution, s.activeTarget, s.activeGameID = ExecutionFPGANative, "kit", "core-zx81"
	s.activePackageID, s.activePackageGeneration = strings.Repeat("a", 64), 9
	return s, c, SessionStopBinding{GameID: "core-zx81", Target: "kit", TargetID: "kit-id", PackageID: s.activePackageID, Generation: 9}
}
func TestStopExpectedRejectsChangedPlayBeforeDispatch(t *testing.T) {
	for _, field := range []string{"game", "target", "target-id", "package", "generation"} {
		t.Run(field, func(t *testing.T) {
			s, c, b := testBoundStop()
			switch field {
			case "game":
				b.GameID = "other"
			case "target":
				b.Target = "other"
			case "target-id":
				b.TargetID = "other"
			case "package":
				b.PackageID = strings.Repeat("b", 64)
			case "generation":
				b.Generation++
			}
			_, err := s.StopExpected(context.Background(), b)
			if !errors.Is(err, ErrSessionChanged) || c.stopCalls != 0 || s.activeGameID != "core-zx81" {
				t.Fatalf("err=%v stops=%d game=%s", err, c.stopCalls, s.activeGameID)
			}
		})
	}
}
func TestStopExpectedChecksAfterLifecycleAdmission(t *testing.T) {
	s, c, b := testBoundStop()
	release, err := s.acquireLifecycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.StopExpected(context.Background(), b); done <- err }()
	select {
	case err := <-done:
		release()
		t.Fatalf("bypassed admission: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	s.executionMu.Lock()
	s.activePackageGeneration++
	s.executionMu.Unlock()
	release()
	if err := <-done; !errors.Is(err, ErrSessionChanged) || c.stopCalls != 0 {
		t.Fatalf("err=%v stops=%d", err, c.stopCalls)
	}
}
func TestStopExpectedKeepsOrdinaryStopSaveSemantics(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "save failure"}[fail], func(t *testing.T) {
			s, c, b := testBoundStop()
			if fail {
				c.stopErr = &protocol.APIError{Code: protocol.CodeSaveFailed}
			}
			st, err := s.StopExpected(context.Background(), b)
			if c.stopCalls != 1 {
				t.Fatalf("stops=%d", c.stopCalls)
			}
			if fail {
				var api *protocol.APIError
				if !errors.As(err, &api) || api.Code != protocol.CodeSaveFailed || s.activeGameID != b.GameID {
					t.Fatalf("err=%v game=%s", err, s.activeGameID)
				}
			} else if err != nil || st.State != protocol.StateIdle || s.activeGameID != "" {
				t.Fatalf("status=%+v err=%v game=%s", st, err, s.activeGameID)
			}
		})
	}
}
