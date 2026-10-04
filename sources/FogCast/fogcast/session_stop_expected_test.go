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
	s.activeTarget = "kit"
	packageID := strings.Repeat("a", 64)
	s.plays["kit"] = targetPlay{execution: ExecutionFPGANative, gameID: "core-zx81", packageID: packageID, packageGeneration: 9}
	return s, c, SessionStopBinding{GameID: "core-zx81", Target: "kit", TargetID: "kit-id", PackageID: packageID, Generation: 9}
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
			if !errors.Is(err, ErrSessionChanged) || c.stopCalls != 0 || s.plays["kit"].gameID != "core-zx81" {
				t.Fatalf("err=%v stops=%d kit=%+v", err, c.stopCalls, s.plays["kit"])
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
	play := s.plays["kit"]
	play.packageGeneration++
	s.plays["kit"] = play
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
				if !errors.As(err, &api) || api.Code != protocol.CodeSaveFailed || s.plays["kit"].gameID != b.GameID {
					t.Fatalf("err=%v kit=%+v", err, s.plays["kit"])
				}
			} else if _, retained := s.plays["kit"]; err != nil || st.State != protocol.StateIdle || retained {
				t.Fatalf("status=%+v err=%v kit=%+v", st, err, s.plays["kit"])
			}
		})
	}
}

func TestStopExpectedPreparationWaitsForBindingAdmission(t *testing.T) {
	s, c, b := testBoundStop()
	release, err := s.acquireLifecycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	prepared := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := s.StopExpectedWithPreparation(context.Background(), b, func(context.Context) { prepared <- struct{}{} })
		done <- err
	}()
	select {
	case <-prepared:
		release()
		t.Fatal("teardown bypassed lifecycle admission")
	case <-time.After(20 * time.Millisecond):
	}
	s.executionMu.Lock()
	play := s.plays["kit"]
	play.packageGeneration++
	s.plays["kit"] = play
	s.executionMu.Unlock()
	release()
	if err := <-done; !errors.Is(err, ErrSessionChanged) {
		t.Fatal(err)
	}
	select {
	case <-prepared:
		t.Fatal("teardown ran for a rejected binding")
	default:
	}
	if c.stopCalls != 0 {
		t.Fatalf("stops=%d", c.stopCalls)
	}
}
func TestStopExpectedPreparationHoldsLifecycleThroughStop(t *testing.T) {
	s, c, b := testBoundStop()
	entered := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := s.StopExpectedWithPreparation(context.Background(), b, func(context.Context) { close(entered); <-resume })
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	release, err := s.acquireLifecycle(ctx)
	cancel()
	if err == nil {
		release()
		close(resume)
		<-done
		t.Fatal("lifecycle released during teardown")
	}
	if c.stopCalls != 0 {
		t.Fatal("stop dispatched before teardown finished")
	}
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c.stopCalls != 1 {
		t.Fatalf("stops=%d", c.stopCalls)
	}
}
