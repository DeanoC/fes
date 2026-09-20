package fogcast

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

type mediaServiceClient struct {
	fakeServiceClient
	calls int
	hook  func()
}

func (c *mediaServiceClient) LoadDevelopmentMedia(ctx context.Context, n int64, r io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	c.calls++
	if c.hook != nil {
		c.hook()
	}
	return c.statusResult, nil
}
func TestDevelopmentMediaServiceBindsCurrentGenerationWithoutReplacingExecution(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
	c := &mediaServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, c)
	s.activeExecution = ExecutionFPGADevelopment
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 8, Target: "dev"}
	if _, err := s.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b); err == nil || c.calls != 0 {
		t.Fatal("stale generation dispatched")
	}
	b.Generation = 9
	got, err := s.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b)
	if err != nil || !b.Matches(got) || c.calls != 1 || s.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("status=%+v calls=%d error=%v", got, c.calls, err)
	}
	for _, body := range []string{"", strings.Repeat("x", 16385)} {
		if _, err := s.LoadDevelopmentMedia(context.Background(), int64(len(body)), strings.NewReader(body), b); err == nil {
			t.Fatal("invalid body accepted")
		}
	}
	if c.calls != 1 {
		t.Fatal("invalid body dispatched")
	}
}

func TestDevelopmentMediaRejectsTargetSwitchWithCollidingPackageGeneration(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
	client := &mediaServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	s.targets = []TargetConfig{{Name: "target-a", TargetID: "id-a"}, {Name: "target-b", TargetID: "id-b"}}
	s.selectedTarget = "target-b"
	s.targetClients["target-b"] = client
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "target-a", TargetID: "id-a"}
	if _, err := s.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b); err == nil || client.calls != 0 {
		t.Fatal("media crossed targets with colliding generation")
	}
	b.Target = "target-b"
	if _, err := s.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), b); err == nil || client.calls != 0 {
		t.Fatal("media ignored stable target identity")
	}
}
