package fogcast

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type liveMediaServiceClient struct {
	fakeServiceClient
	replaceCalls int
	clearCalls   int
	lastSize     int64
	lastBytes    []byte
	err          error
	clearErrs    []error
}

func (c *liveMediaServiceClient) ReplaceLiveMedia(_ context.Context, size int64, body io.Reader, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	c.replaceCalls++
	c.lastSize = size
	c.lastBytes, _ = io.ReadAll(body)
	if c.err != nil {
		return c.statusResult, c.err
	}
	return c.statusResult, nil
}

func (c *liveMediaServiceClient) ClearLiveMedia(_ context.Context, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	c.clearCalls++
	if len(c.clearErrs) > 0 {
		err := c.clearErrs[0]
		c.clearErrs = c.clearErrs[1:]
		if err != nil {
			return c.statusResult, err
		}
	}
	if c.err != nil {
		return c.statusResult, c.err
	}
	return c.statusResult, nil
}

func TestReplaceLiveMediaArmsCoreMediaWithoutLoadMedia(t *testing.T) {
	store, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	payload := []byte{1, 2, 3, 4}
	media, created, err := store.ImportCoreMediaStream(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil || !created {
		t.Fatalf("import: %v created=%v", err, created)
	}
	id := strings.Repeat("a", 64)
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: id, Generation: 4, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	client := &liveMediaServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.activeExecution = ExecutionFPGADevelopment
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 4, Target: "dev"}
	got, err := s.ReplaceLiveMedia(context.Background(), media.MediaID, "maze.p", b)
	if err != nil || client.replaceCalls != 1 || client.clearCalls != 0 {
		t.Fatalf("err=%v replace=%d clear=%d", err, client.replaceCalls, client.clearCalls)
	}
	if !bytes.Equal(client.lastBytes, payload) || client.lastSize != int64(len(payload)) || !b.MatchesLive(got) {
		t.Fatalf("bytes=%v size=%d status=%+v", client.lastBytes, client.lastSize, got)
	}
	if _, err := s.ReplaceLiveMedia(context.Background(), media.MediaID, "maze.tzx", b); err == nil || client.replaceCalls != 1 {
		t.Fatal("non-.p name accepted")
	}
	oversized := bytes.Repeat([]byte{9}, int(protocol.MaxDevelopmentMediaBytes)+1)
	big, _, err := store.ImportCoreMediaStream(context.Background(), int64(len(oversized)), bytes.NewReader(oversized))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceLiveMedia(context.Background(), big.MediaID, "big.p", b); err == nil || client.replaceCalls != 1 {
		t.Fatal("oversized tape accepted")
	}
	client.err = protocol.LiveMediaBusyError()
	if _, err := s.ReplaceLiveMedia(context.Background(), media.MediaID, "maze.p", b); err == nil {
		t.Fatal("busy not surfaced")
	}
	client.err = nil
	cleared, err := s.ClearLiveMedia(context.Background(), b)
	if err != nil || client.clearCalls != 1 || !b.MatchesLive(cleared) {
		t.Fatalf("clear err=%v calls=%d", err, client.clearCalls)
	}
	stale := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 3, Target: "dev"}
	if _, err := s.ClearLiveMedia(context.Background(), stale); err == nil || client.clearCalls != 1 {
		t.Fatal("stale generation cleared")
	}
}

func TestClearLiveMediaRetriesBusyAndPreservesHardUnavailable(t *testing.T) {
	id := strings.Repeat("a", 64)
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: id, Generation: 4, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	client := &liveMediaServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, nil, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.activeExecution = ExecutionFPGADevelopment
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 4, Target: "dev"}
	hard := &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable", Phase: "input"}
	client.err = hard
	_, err := s.ClearLiveMedia(context.Background(), b)
	apiErr, ok := err.(*protocol.APIError)
	if !ok || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "input" || apiErr.Message != hard.Message || client.clearCalls != 1 {
		t.Fatalf("hard input unavailable err=%v calls=%d", err, client.clearCalls)
	}
	client.err = nil
	client.clearErrs = []error{protocol.LiveMediaBusyError()}
	cleared, err := s.ClearLiveMedia(context.Background(), b)
	if err != nil || client.clearCalls != 3 || !b.MatchesLive(cleared) {
		t.Fatalf("busy retry err=%v calls=%d status=%+v", err, client.clearCalls, cleared)
	}
	client.err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable", Phase: "programming"}
	before := client.clearCalls
	_, err = s.ClearLiveMedia(context.Background(), b)
	apiErr, ok = err.(*protocol.APIError)
	if !ok || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "programming" || client.clearCalls != before+1 {
		t.Fatalf("programming unavailable err=%v calls=%d", err, client.clearCalls-before)
	}
	client.err = protocol.LiveMediaBusyError()
	before = client.clearCalls
	_, err = s.ClearLiveMedia(context.Background(), b)
	apiErr, ok = err.(*protocol.APIError)
	if !ok || apiErr.Code != protocol.CodeBusy || apiErr.Phase != "input" || client.clearCalls != before+4 {
		t.Fatalf("exhausted busy err=%v calls=%d", err, client.clearCalls-before)
	}
}

func TestScopedMediaAdmissionUsesSessionTargetClient(t *testing.T) {
	a := &fakeServiceClient{healthResult: protocol.Health{APIVersion: "v1"}}
	b := &fakeServiceClient{healthErr: io.ErrUnexpectedEOF}
	s := &Service{
		targets:        []TargetConfig{{Name: "kit-a", Enabled: true}, {Name: "kit-b", Enabled: true}},
		selectedTarget: "kit-b",
		targetClients:  map[string]serviceClient{"kit-a": a, "kit-b": b},
	}
	if _, err := s.refreshTargetAdmission(WithSessionTarget(context.Background(), "kit-a")); err != nil {
		t.Fatalf("scoped admission: %v", err)
	}
	if a.healthCalls != 1 || b.healthCalls != 0 {
		t.Fatalf("health calls A=%d B=%d; want A only", a.healthCalls, b.healthCalls)
	}
}

func TestScopedLiveMediaReplaceAndEjectReachHealthyKitAWhenBUnavailable(t *testing.T) {
	store, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	payload := []byte{1, 2, 3}
	media, _, err := store.ImportCoreMediaStream(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("b", 64)
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: id, Generation: 4, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	a := &liveMediaServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
	s := &Service{
		catalog: store, uploadTimeout: time.Second, activeExecution: ExecutionFPGADevelopment,
		selectedTarget: "kit-b", activeTarget: "kit-b",
		targets:       []TargetConfig{{Name: "kit-a", Enabled: true}, {Name: "kit-b", Enabled: true}},
		targetClients: map[string]serviceClient{"kit-a": a}, // B is selected but unavailable.
	}
	binding := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 4, Target: "kit-a"}
	ctx := WithSessionTarget(context.Background(), "kit-a")
	if _, err := s.ReplaceLiveMedia(ctx, media.MediaID, "maze.p", binding); err != nil {
		t.Fatalf("replace on A: %v", err)
	}
	if _, err := s.ClearLiveMedia(ctx, binding); err != nil {
		t.Fatalf("eject on A: %v", err)
	}
	if a.replaceCalls != 1 || a.clearCalls != 1 {
		t.Fatalf("A replace=%d eject=%d; want both calls on A", a.replaceCalls, a.clearCalls)
	}
}

func TestLiveMediaSessionContextRoutesAwayFromForegroundTarget(t *testing.T) {
	store, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	payload := []byte("tape")
	media, _, err := store.ImportCoreMediaStream(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	statusA := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
	statusB := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("b", 64), Generation: 3, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
	a := &liveMediaServiceClient{fakeServiceClient: fakeServiceClient{statusResult: statusA}}
	b := &liveMediaServiceClient{fakeServiceClient: fakeServiceClient{statusResult: statusB}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, b)
	s.targets = []TargetConfig{{Name: "kit-a", Enabled: true, TargetID: "id-a"}, {Name: "kit-b", Enabled: true, TargetID: "id-b"}}
	s.targetClients = map[string]serviceClient{"kit-a": a, "kit-b": b}
	s.selectedTarget, s.activeTarget, s.activeExecution = "kit-b", "kit-b", ExecutionFPGADevelopment
	s.plays = map[string]targetPlay{"kit-a": {execution: ExecutionFPGADevelopment}, "kit-b": {execution: ExecutionFPGADevelopment}}
	binding := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 7, Target: "kit-a", TargetID: "id-a"}
	ctx := WithSessionTarget(context.Background(), "kit-a")
	if _, err := s.ReplaceLiveMedia(ctx, media.MediaID, "maze.p", binding); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClearLiveMedia(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if a.replaceCalls != 1 || a.clearCalls != 1 || b.replaceCalls != 0 || b.clearCalls != 0 {
		t.Fatalf("routed media calls A=%d/%d B=%d/%d", a.replaceCalls, a.clearCalls, b.replaceCalls, b.clearCalls)
	}
}
