package fogcast

import (
	"context"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"sync"
	"testing"
	"time"
)

// Signal context use so a queued launch can be inspected without sleeps.
type mediaLifecycleContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
}

func (c *mediaLifecycleContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestCoreMediaLifecycleQueuedLaunchCannotRebindTarget(t *testing.T) {
	for _, kind := range []string{"package", "ordinary"} {
		t.Run(kind, func(t *testing.T) {
			s, first, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Media lifecycle")
			active := coreEntryActiveStatus(inspection, 7, true)
			first.statusResult, first.mediaStatus = active, active
			second := &defaultMediaPackageClient{packageLibraryClient: &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}}
			s.targets = []TargetConfig{{Name: "first", Enabled: true, TargetID: "first-id"}, {Name: "second", Enabled: true, TargetID: "second-id"}}
			s.selectedTarget = "first"
			s.targetClients = map[string]serviceClient{"first": first, "second": second}
			s.activeExecution, s.activeTarget = ExecutionFPGADevelopment, "first"
			s.activeGameID, s.activePackageID, s.activePackageGeneration = entry.GameID, entry.PackageID, 7
			s.plays = map[string]targetPlay{"first": {execution: ExecutionFPGADevelopment, gameID: entry.GameID}}
			// Model an admitted package launch between activation and media delivery.
			release, err := s.acquireLifecycle(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			queued := &mediaLifecycleContext{Context: parent, entered: make(chan struct{})}
			done := make(chan error, 1)
			gameID := entry.GameID
			if kind == "ordinary" {
				gameID = "snes-pending"
			}
			go func() { _, err := s.LaunchOn(queued, gameID, "second", nil); done <- err }()
			select {
			case <-queued.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("queued launch did not reach context/admission")
			}
			s.executionMu.Lock()
			bound := s.activeTarget
			s.executionMu.Unlock()
			if bound != "first" {
				t.Errorf("queued %s launch rebound admitted target to %q", kind, bound)
			}
			binding := protocol.DevelopmentMediaBinding{PackageID: entry.PackageID, Generation: 7, Target: "first", TargetID: "first-id"}
			_, mediaErr := s.loadDevelopmentMediaLocked(context.Background(), []byte("snapshot"), binding)
			if mediaErr != nil {
				t.Errorf("admitted first-target media failed: %v", mediaErr)
			}
			if first.mediaCalls != 1 || second.mediaCalls != 0 {
				t.Errorf("media calls first=%d second=%d, want 1/0", first.mediaCalls, second.mediaCalls)
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Error("canceled queued launch succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("queued launch did not cancel while lifecycle held")
			}
			s.executionMu.Lock()
			bound = s.activeTarget
			s.executionMu.Unlock()
			if bound != "first" {
				t.Errorf("queued launch cleanup changed active target to %q", bound)
			}
		})
	}
}

type mediaLifecycleClient struct {
	*defaultMediaPackageClient
	deliver func(context.Context) (protocol.Status, error)
}

func (c *mediaLifecycleClient) LoadDevelopmentMedia(ctx context.Context, _ int64, _ io.Reader, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	return c.deliver(ctx)
}

func TestCoreMediaLifecycleCleanupGetsFreshBoundedContext(t *testing.T) {
	for _, failure := range []string{"operation-deadline", "parent-canceled", "parent-deadline"} {
		t.Run(failure, func(t *testing.T) {
			s, base, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Cleanup context")
			s.uploadTimeout = 100 * time.Millisecond
			active := coreEntryActiveStatus(inspection, 8, true)
			base.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
				base.statusResult = active
				return active, nil
			}
			parent, cancel := context.WithCancel(context.Background())
			if failure == "parent-deadline" {
				cancel()
				parent, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			}
			defer cancel()
			client := &mediaLifecycleClient{defaultMediaPackageClient: base}
			client.deliver = func(ctx context.Context) (protocol.Status, error) {
				if failure == "parent-canceled" {
					cancel()
				}
				<-ctx.Done()
				return active, ctx.Err()
			}
			for name := range s.targetClients {
				s.targetClients[name] = client
			}
			stopSeen := false
			base.stopFn = func(ctx context.Context) (protocol.Status, error) {
				stopSeen = true
				if ctx.Err() != nil {
					t.Errorf("cleanup received spent context: %v", ctx.Err())
				}
				deadline, ok := ctx.Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining <= 0 || remaining > s.uploadTimeout {
					t.Errorf("cleanup deadline remaining=%v present=%v, want fresh bound <= %v", remaining, ok, s.uploadTimeout)
				}
				if len(s.lifecycleAdmission) != 0 {
					t.Error("cleanup released lifecycle admission")
				}
				if ctx.Err() != nil {
					return active, ctx.Err()
				}
				return protocol.Status{State: protocol.StateIdle}, nil
			}
			response, err := s.Launch(parent, entry.GameID, nil)
			if err == nil {
				t.Fatal("media timeout/cancellation reported successful launch")
			}
			if !stopSeen {
				t.Fatal("cleanup Stop was not attempted")
			}
			if response.Status.State != protocol.StateIdle || s.packageRejection != nil {
				t.Errorf("cleanup did not confirm idle: state=%q rejection=%v error=%v", response.Status.State, s.packageRejection, err)
			}
			if failure == "operation-deadline" && parent.Err() != nil {
				t.Errorf("outer context unexpectedly failed: %v", parent.Err())
			}
		})
	}
}
