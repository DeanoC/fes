package fogcast

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

// The target-routing lifecycle is exercised using installed package entries.
func namedPackageFixture(t *testing.T) (*Service, *packageLibraryClient, *packageLibraryClient, catalog.CoreEntry) {
	t.Helper()
	raw := libraryPackageFixture(t, "0.1.0")
	s, first, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Pong package")
	dev := first.packageLibraryClient
	spare := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}, inspection: dev.inspection}
	for _, c := range []*packageLibraryClient{dev, spare} {
		c.coreLoad = func(_ context.Context, _ int64, body io.Reader) (protocol.Status, error) {
			if _, err := io.Copy(io.Discard, body); err != nil {
				return protocol.Status{}, err
			}
			status := coreEntryActiveStatus(inspection, 1, false)
			c.statusResult = status
			return status, nil
		}
		c.stopFn = func(context.Context) (protocol.Status, error) {
			c.statusResult = protocol.Status{State: protocol.StateIdle}
			return c.statusResult, nil
		}
	}
	s.targets = []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"}, {Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"}}
	s.selectedTarget = "dev"
	s.targetClients = map[string]serviceClient{"dev": dev, "spare": spare}
	return s, dev, spare, entry
}

func TestPackageLaunchOnNamedTargetPreservesSelectionAndRoutesStop(t *testing.T) {
	s, dev, spare, entry := namedPackageFixture(t)
	if _, err := s.LaunchOn(context.Background(), entry.GameID, "spare", nil); err != nil {
		t.Fatal(err)
	}
	if s.selectedTarget != "dev" || spare.coreCalls != 1 || dev.coreCalls != 0 {
		t.Fatalf("selected=%s loads=%d/%d", s.selectedTarget, dev.coreCalls, spare.coreCalls)
	}
	status, err := s.Status(context.Background())
	if err != nil || status.CorePackage == nil {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err = s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if spare.stopCalls != 1 || dev.stopCalls != 0 {
		t.Fatalf("stops=%d/%d", dev.stopCalls, spare.stopCalls)
	}
}

func TestPackageLaunchKeepsStopDeadlineBounded(t *testing.T) {
	s, dev, _, entry := namedPackageFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	original := dev.coreLoad
	dev.coreLoad = func(ctx context.Context, n int64, body io.Reader) (protocol.Status, error) {
		close(entered)
		<-release
		return original(ctx, n, body)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Launch(context.Background(), entry.GameID, nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("package load did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.Stop(ctx)
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked stop=%v", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if dev.stopCalls != 0 {
		t.Fatal("stop dispatched during package load")
	}
}

func TestPackageLaunchKeepsUnsafeRecoveryExplicit(t *testing.T) {
	s, dev, _, entry := namedPackageFixture(t)
	dev.statusResult = protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "retained error", Phase: "save"}}
	_, err := s.Launch(context.Background(), entry.GameID, nil)
	if err == nil || dev.coreCalls != 0 || dev.stopCalls != 0 {
		t.Fatalf("launch=%v loads=%d stops=%d", err, dev.coreCalls, dev.stopCalls)
	}
}
