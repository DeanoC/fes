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

func TestCoreSaveFailureKeepsOnlyConfirmedResumedGenerationActive(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume", true: "recovery"}[recovery], func(t *testing.T) {
			name := "fes.pong"
			active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &name, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7, PersistenceMode: "persistent"}}
			runtime := &ownedDevelopmentContextRuntime{}
			runtime.reconciled = active
			runtime.stopErr = &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "save failed", Phase: "save"}
			coordinator := agent.New(runtime, time.Second, time.Second)
			coordinator.Initialize(context.Background())
			runtime.reconciled.LastError = runtime.stopErr
			if recovery {
				runtime.reconciled.State = protocol.StateFailed
				runtime.reconciled.LastError = &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "recovery", Phase: "recovery"}
			}
			status, err := coordinator.Stop(context.Background())
			want := protocol.StateActive
			if recovery {
				want = protocol.StateFailed
			}
			if err == nil || status.State != want || status.CorePackage == nil || status.CorePackage.Generation != 7 {
				t.Fatalf("status=%+v err=%v", status, err)
			}
		})
	}
}

type serializedDataRuntime struct {
	fakeRuntime
	started chan struct{}
	release chan struct{}
	writes  int
}

func (r *serializedDataRuntime) InspectCoreData(ctx context.Context, n int64, body io.Reader, id string) (protocol.CoreDataInspection, *protocol.APIError) {
	close(r.started)
	select {
	case <-r.release:
		return protocol.CoreDataInspection{}, nil
	case <-ctx.Done():
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "cancelled"}
	}
}
func (r *serializedDataRuntime) UpdateCoreSettings(context.Context, int64, io.Reader, protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	r.writes++
	return protocol.CoreDataInspection{}, nil
}
func TestCoreDataOperationsShareTargetLifecycleAndRefuseRecoveryWrites(t *testing.T) {
	runtime := &serializedDataRuntime{started: make(chan struct{}), release: make(chan struct{})}
	coordinator := agent.New(runtime, time.Second, time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		coordinator.InspectCoreData(context.Background(), 1, strings.NewReader("p"), strings.Repeat("a", 64))
	}()
	<-runtime.started
	_, err := coordinator.UpdateCoreSettings(context.Background(), 1, strings.NewReader("p"), protocol.CoreSettingsUpdate{ExpectedPackageID: strings.Repeat("a", 64), ExpectedRevision: "absent", PaddleSpeed: 1})
	if err == nil || err.Code != protocol.CodeBusy || runtime.writes != 0 {
		t.Fatalf("update escaped transition: %v", err)
	}
	if _, err := coordinator.Stop(context.Background()); err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("stop escaped transition: %v", err)
	}
	close(runtime.release)
	<-done
	if status := coordinator.Status(); status.State != protocol.StateIdle {
		t.Fatalf("read changed state: %+v", status)
	}
	runtime.reconciled = protocol.Status{State: protocol.StateFailed, Recovery: protocol.RecoveryRebootRequired}
	coordinator.Initialize(context.Background())
	_, err = coordinator.UpdateCoreSettings(context.Background(), 1, strings.NewReader("p"), protocol.CoreSettingsUpdate{ExpectedPackageID: strings.Repeat("a", 64), ExpectedRevision: "absent", PaddleSpeed: 1})
	if err == nil || err.Code != protocol.CodeBusy || runtime.writes != 0 {
		t.Fatalf("recovery update escaped: %v", err)
	}
}

func TestCoreReplacementRecoveryRetainsPreviousPackageGeneration(t *testing.T) {
	name := "fes.pong"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &name, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7, PersistenceMode: "persistent"}}
	runtime := &packageRuntime{fakeRuntime: fakeRuntime{reconciled: active}, attempted: true, packageErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "recovery required", Phase: "recovery"}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	coordinator.Initialize(context.Background())
	runtime.reconciled.State = protocol.StateFailed
	runtime.reconciled.Recovery = protocol.RecoveryRebootRequired
	runtime.reconciled.LastError = runtime.packageErr
	status, err := coordinator.LoadCore(context.Background(), 1, strings.NewReader("p"))
	if err == nil || status.State != protocol.StateFailed || status.Recovery != protocol.RecoveryRebootRequired || status.CorePackage == nil || status.CorePackage.Generation != 7 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
