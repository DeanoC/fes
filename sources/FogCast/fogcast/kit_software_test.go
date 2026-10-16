package fogcast

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/protocol"
)

type inventoryTestClient struct {
	*fakeServiceClient
	id      string
	calls   int
	blocked chan struct{}
	entered chan struct{}
}

func (c *inventoryTestClient) KitSoftwareReleases(ctx context.Context) ([]software.Release, error) {
	c.calls++
	if c.blocked != nil {
		close(c.entered)
		select {
		case <-c.blocked:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("unbounded inventory request")
	}
	return []software.Release{{ID: c.id}}, nil
}
func TestKitSoftwareInventorySelectedAndPairedScope(t *testing.T) {
	selected := &inventoryTestClient{fakeServiceClient: &fakeServiceClient{healthResult: protocol.Health{APIVersion: "v1"}}, id: strings.Repeat("a", 64)}
	paired := &inventoryTestClient{fakeServiceClient: &fakeServiceClient{}, id: strings.Repeat("b", 64)}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, selected)
	got, err := service.KitSoftwareReleases(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != selected.id {
		t.Fatalf("selected inventory: %v %v", got, err)
	}
	service.pairedTargetConfigs = []TargetConfig{{Name: "paired", Enabled: true, TargetID: "paired-id"}}
	service.pairedTargetClients["paired"] = paired
	// A launch on another target must not block this read-only paired request.
	service.targetMu.Lock()
	done := make(chan error, 1)
	go func() {
		got, err := service.KitSoftwareReleases(WithPairedTarget(context.Background(), "paired-id"))
		if err == nil && (len(got) != 1 || got[0].ID != paired.id) {
			err = errors.New("wrong paired inventory")
		}
		done <- err
	}()
	select {
	case err := <-done:
		service.targetMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		service.targetMu.Unlock()
		t.Fatal("paired inventory waited for foreground lock")
	}
	if _, err := service.KitSoftwareReleases(WithPairedTarget(context.Background(), "unknown")); err == nil {
		t.Fatal("unknown paired target used selected kit")
	}
	if selected.calls != 1 || paired.calls != 1 {
		t.Fatalf("wrong reads: %d %d", selected.calls, paired.calls)
	}
}

func TestKitSoftwareInventoryReleasesForegroundBeforeSlowRead(t *testing.T) {
	client := &inventoryTestClient{fakeServiceClient: &fakeServiceClient{healthResult: protocol.Health{APIVersion: "v1"}}, blocked: make(chan struct{}), entered: make(chan struct{})}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	done := make(chan error, 1)
	go func() { _, err := service.KitSoftwareReleases(context.Background()); done <- err }()
	select {
	case <-client.entered:
	case <-time.After(time.Second):
		t.Fatal("inventory did not begin")
	}
	defer close(client.blocked)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	release, err := service.acquireLifecycle(ctx)
	if err != nil {
		t.Fatal("slow inventory retained foreground admission", err)
	}
	release()
	// Caller cancellation must still end the slow read within the configured bound.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked read ignored configured request deadline")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inventory read was unbounded")
	}
}
