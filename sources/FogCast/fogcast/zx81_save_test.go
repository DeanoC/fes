package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type zx81RememberClient struct {
	status       protocol.Status
	statusErr    error
	data         []byte
	err          error
	ackErr       error
	acks         int
	captureCalls int
	stopCalls    int
	events       []string
}

type zx81FailingCoreEntryCatalog struct {
	*catalog.Store
	lookups int
}

func (c *zx81FailingCoreEntryCatalog) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	c.lookups++
	return catalog.CoreEntry{}, errors.New("catalog unavailable")
}

func (c *zx81RememberClient) ProbeContent(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
	return protocol.CacheProbeResponse{}, nil
}
func (c *zx81RememberClient) UploadContent(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error) {
	return protocol.CacheUploadResponse{}, nil
}
func (c *zx81RememberClient) Health(context.Context) (protocol.Health, error) {
	return protocol.Health{}, nil
}
func (c *zx81RememberClient) Status(context.Context) (protocol.Status, error) {
	return c.status, c.statusErr
}
func (c *zx81RememberClient) Stop(context.Context) (protocol.Status, error) {
	c.stopCalls++
	c.events = append(c.events, "stop")
	return protocol.Status{State: protocol.StateIdle}, nil
}
func (c *zx81RememberClient) CaptureZX81Tape(context.Context, string, uint64) ([]byte, error) {
	c.captureCalls++
	c.events = append(c.events, "capture")
	return append([]byte(nil), c.data...), c.err
}
func (c *zx81RememberClient) AcknowledgeZX81Tape(context.Context, string, uint64) error {
	c.acks++
	c.events = append(c.events, "acknowledge")
	return c.ackErr
}

func newZX81StopFixture(t *testing.T) (*Service, *zx81RememberClient, *catalog.Store, catalog.CoreEntry) {
	t.Helper()
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pkg := strings.Repeat("a", 64)
	entry, err := store.CreateCoreEntry(ctx, "ZX81", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	client := &zx81RememberClient{status: protocol.Status{State: protocol.StateActive, CorePackage: zx81SavePackageStatus(pkg, 3)}}
	service := &Service{
		catalog: store, requestTimeout: time.Second, uploadTimeout: time.Second,
		selectedTarget: "kit", activeTarget: "kit", activeExecution: ExecutionFPGANative,
		plays: map[string]targetPlay{"kit": {
			execution: ExecutionFPGANative, gameID: entry.GameID, system: catalog.CorePlatform, coreID: "fes.zx81",
			packageID: pkg, packageGeneration: 3,
		}},
		targets:       []TargetConfig{{Name: "kit", Enabled: true}},
		targetClients: map[string]serviceClient{"kit": client},
	}
	return service, client, store, entry
}

func zx81SavePackageStatus(packageID string, generation uint64) *protocol.CorePackageStatus {
	return &protocol.CorePackageStatus{
		PackageID: packageID, Generation: generation,
		ABI: protocol.RuntimeContract{ID: protocol.ComputerABIID, Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{
			{ID: protocol.Zx81TapeInterface().ID, Major: 1},
			{ID: protocol.Zx81TapeWriteInterface().ID, Major: 1},
		},
	}
}

func captureZX81SaveLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

func zx81LogRecord(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected a structured ZX81 SAVE log")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &record); err != nil {
		t.Fatalf("decode structured log %q: %v", lines[len(lines)-1], err)
	}
	return record
}

func TestStopPersistsPendingZX81ProgramBeforeStoppingTarget(t *testing.T) {
	service, client, store, entry := newZX81StopFixture(t)
	want := []byte{0x00, 0x0a, 0x76}
	client.data = want

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Stop = %+v, %v", status, err)
	}
	if !reflect.DeepEqual(client.events, []string{"capture", "acknowledge", "stop"}) {
		t.Fatalf("SAVE/Stop order = %v", client.events)
	}
	if client.stopCalls != 1 || client.acks != 1 {
		t.Fatalf("Stop calls=%d acknowledgements=%d", client.stopCalls, client.acks)
	}
	saves, err := store.ListZX81Saves(context.Background(), entry.GameID)
	if err != nil || len(saves) != 1 || saves[0].Size != int64(len(want)) {
		t.Fatalf("persisted saves = %+v, %v", saves, err)
	}
	listed, err := service.ListZX81Saves(context.Background(), entry.GameID)
	if err != nil || len(listed) != 1 || listed[0].MediaID != saves[0].MediaID || listed[0].Size != int64(len(want)) {
		t.Fatalf("game tape shelf after Stop = %+v, %v; stored save = %+v", listed, err, saves[0])
	}
	_, reader, err := store.OpenCoreMedia(context.Background(), saves[0].MediaID)
	if err != nil {
		t.Fatalf("open saved media: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("saved tape = %v, %v; want %v", got, err, want)
	}
}

func TestStopWithNoPendingZX81SaveAddsNoTapeAndSucceeds(t *testing.T) {
	service, client, store, entry := newZX81StopFixture(t)
	logs := captureZX81SaveLogs(t)

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Stop = %+v, %v", status, err)
	}
	if client.captureCalls != 1 || client.acks != 0 || client.stopCalls != 1 {
		t.Fatalf("capture=%d ack=%d stop=%d", client.captureCalls, client.acks, client.stopCalls)
	}
	if saves, err := store.ListZX81Saves(context.Background(), entry.GameID); err != nil || len(saves) != 0 {
		t.Fatalf("clean Stop stored saves %+v: %v", saves, err)
	}
	record := zx81LogRecord(t, logs)
	if record["reason"] != "no_pending_save" || record["captured_bytes"] != float64(0) {
		t.Fatalf("empty capture log = %+v", record)
	}
}

func TestStopPersistsPendingTimexProgram(t *testing.T) {
	for _, coreID := range []string{"fes.ts1000", "fes.ts1500"} {
		t.Run(coreID, func(t *testing.T) {
			service, client, store, entry := newZX81StopFixture(t)
			play := service.plays["kit"]
			play.coreID = coreID
			service.plays["kit"] = play
			want := []byte{0x00, 0x0a, 0x76}
			client.data = want

			status, err := service.Stop(context.Background())
			if err != nil || status.State != protocol.StateIdle {
				t.Fatalf("Stop = %+v, %v", status, err)
			}
			if !reflect.DeepEqual(client.events, []string{"capture", "acknowledge", "stop"}) {
				t.Fatalf("SAVE/Stop order = %v", client.events)
			}
			saves, err := store.ListZX81Saves(context.Background(), entry.GameID)
			if err != nil || len(saves) != 1 || saves[0].Size != int64(len(want)) {
				t.Fatalf("persisted saves = %+v, %v", saves, err)
			}
		})
	}
}

func TestStopSkipsZX80WithoutCapturing(t *testing.T) {
	service, client, _, _ := newZX81StopFixture(t)
	play := service.plays["kit"]
	play.coreID = "fes.zx80"
	service.plays["kit"] = play
	logs := captureZX81SaveLogs(t)

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Stop = %+v, %v", status, err)
	}
	if client.captureCalls != 0 || client.stopCalls != 1 {
		t.Fatalf("capture=%d stop=%d", client.captureCalls, client.stopCalls)
	}
	record := zx81LogRecord(t, logs)
	if record["reason"] != "core_does_not_save" || record["core_id"] != "fes.zx80" {
		t.Fatalf("ZX80 skip log = %+v", record)
	}
}

func TestStopPersistsCatalogIdentifiedTimexProgram(t *testing.T) {
	for _, coreID := range []string{"fes.ts1000", "fes.ts1500"} {
		t.Run(coreID, func(t *testing.T) {
			service, client, store, _ := newZX81StopFixture(t)
			entry, err := store.CreateCoreEntry(context.Background(), coreID, coreID, strings.Repeat("a", 64))
			if err != nil {
				t.Fatal(err)
			}
			play := service.plays["kit"]
			play.coreID = ""
			play.gameID = entry.GameID
			service.plays["kit"] = play
			want := []byte{0x00, 0x0a, 0x76}
			client.data = want

			status, err := service.Stop(context.Background())
			if err != nil || status.State != protocol.StateIdle {
				t.Fatalf("Stop = %+v, %v", status, err)
			}
			if !reflect.DeepEqual(client.events, []string{"capture", "acknowledge", "stop"}) {
				t.Fatalf("SAVE/Stop order = %v", client.events)
			}
			saves, err := store.ListZX81Saves(context.Background(), entry.GameID)
			if err != nil || len(saves) != 1 || saves[0].Size != int64(len(want)) {
				t.Fatalf("persisted saves = %+v, %v", saves, err)
			}
		})
	}
}

func TestStopSkipsCatalogIdentifiedZX80(t *testing.T) {
	service, client, store, _ := newZX81StopFixture(t)
	entry, err := store.CreateCoreEntry(context.Background(), "ZX80", "fes.zx80", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	play := service.plays["kit"]
	play.coreID = ""
	play.gameID = entry.GameID
	service.plays["kit"] = play
	logs := captureZX81SaveLogs(t)

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Stop = %+v, %v", status, err)
	}
	if client.captureCalls != 0 || client.stopCalls != 1 {
		t.Fatalf("capture=%d stop=%d", client.captureCalls, client.stopCalls)
	}
	record := zx81LogRecord(t, logs)
	if record["reason"] != "core_does_not_save" || record["core_id"] != "fes.zx80" {
		t.Fatalf("catalog ZX80 skip log = %+v", record)
	}
}

func TestStopDoesNotReadCatalogForLaunchBoundNonZX81Core(t *testing.T) {
	service, client, store, _ := newZX81StopFixture(t)
	catalogFailure := &zx81FailingCoreEntryCatalog{Store: store}
	service.catalog = catalogFailure
	play := service.plays["kit"]
	play.coreID = "fes.pong"
	service.plays["kit"] = play
	logs := captureZX81SaveLogs(t)

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Stop = %+v, %v", status, err)
	}
	if catalogFailure.lookups != 0 {
		t.Fatalf("catalog lookups = %d, want none for launch-bound %s", catalogFailure.lookups, play.coreID)
	}
	if client.stopCalls != 1 || client.captureCalls != 0 {
		t.Fatalf("Stop calls=%d captures=%d, want Stop and no ZX81 capture", client.stopCalls, client.captureCalls)
	}
	record := zx81LogRecord(t, logs)
	if record["reason"] != "core_does_not_save" || record["core_id"] != play.coreID {
		t.Fatalf("non-ZX81 skip log = %+v", record)
	}
}

func TestStopAllowsUnboundDevelopmentPlayWhenStatusFails(t *testing.T) {
	service, client, _, _ := newZX81StopFixture(t)
	service.activeExecution = ExecutionFPGADevelopment
	service.plays["kit"] = targetPlay{execution: ExecutionFPGADevelopment, system: catalog.CorePlatform}
	client.statusErr = errors.New("status unavailable")
	logs := captureZX81SaveLogs(t)

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Stop = %+v, %v", status, err)
	}
	if client.stopCalls != 1 || client.captureCalls != 0 {
		t.Fatalf("Stop calls=%d captures=%d, want Stop and no unconfirmed ZX81 capture", client.stopCalls, client.captureCalls)
	}
	record := zx81LogRecord(t, logs)
	if record["reason"] != "status_read_failed" || record["error"] != "status unavailable" {
		t.Fatalf("unbound status failure log = %+v", record)
	}
}

func TestStopUnboundWritableZX81BlocksOnlyWhenCaptureHasBytes(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantReason string
		wantBlock  bool
	}{
		{name: "empty capture", wantReason: "no_pending_save"},
		{name: "captured bytes", data: []byte{0x00, 0x0a, 0x76}, wantReason: "pending_save_has_no_game_binding", wantBlock: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, client, _, _ := newZX81StopFixture(t)
			service.activeExecution = ExecutionFPGADevelopment
			service.plays["kit"] = targetPlay{execution: ExecutionFPGADevelopment, system: catalog.CorePlatform}
			client.data = test.data
			logs := captureZX81SaveLogs(t)

			status, err := service.Stop(context.Background())
			if test.wantBlock {
				var apiErr *protocol.APIError
				if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeSaveFailed {
					t.Fatalf("Stop error = %v, want save_failed", err)
				}
				if client.stopCalls != 0 {
					t.Fatalf("target Stop calls = %d, want 0", client.stopCalls)
				}
			} else if err != nil || status.State != protocol.StateIdle || client.stopCalls != 1 {
				t.Fatalf("Stop = %+v, %v; calls=%d", status, err, client.stopCalls)
			}
			if client.captureCalls != 1 {
				t.Fatalf("capture calls = %d, want 1", client.captureCalls)
			}
			record := zx81LogRecord(t, logs)
			if record["reason"] != test.wantReason {
				t.Fatalf("SAVE log = %+v, want reason %q", record, test.wantReason)
			}
		})
	}
}

func TestStopBlocksOnZX81SaveIdentityOrCapabilityMismatch(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*zx81RememberClient, *Service)
		wantReason     string
		wantObserved   string
		wantGeneration float64
	}{
		{name: "status read failed", mutate: func(c *zx81RememberClient, _ *Service) { c.statusErr = errors.New("status unavailable") }, wantReason: "status_read_failed"},
		{name: "missing core package", mutate: func(c *zx81RememberClient, _ *Service) { c.status.CorePackage = nil }, wantReason: "core_package_missing", wantObserved: ""},
		{name: "package id mismatch", mutate: func(c *zx81RememberClient, _ *Service) { c.status.CorePackage.PackageID = strings.Repeat("b", 64) }, wantReason: "package_id_mismatch", wantObserved: strings.Repeat("b", 64), wantGeneration: 3},
		{name: "generation mismatch", mutate: func(c *zx81RememberClient, _ *Service) { c.status.CorePackage.Generation = 4 }, wantReason: "generation_mismatch", wantObserved: strings.Repeat("a", 64), wantGeneration: 4},
		{name: "required tape write capability missing", mutate: func(c *zx81RememberClient, _ *Service) {
			c.status.CorePackage.ActiveInterfaces = c.status.CorePackage.ActiveInterfaces[:1]
		}, wantReason: "required_tape_write_capability_missing", wantObserved: strings.Repeat("a", 64), wantGeneration: 3},
		{name: "incomplete expected generation", mutate: func(_ *zx81RememberClient, s *Service) {
			play := s.plays["kit"]
			play.packageGeneration = 0
			s.plays["kit"] = play
		}, wantReason: "incomplete_save_binding"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, client, _, _ := newZX81StopFixture(t)
			logs := captureZX81SaveLogs(t)
			test.mutate(client, service)

			_, err := service.Stop(context.Background())
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeSaveFailed {
				t.Fatalf("Stop error = %v, want save_failed", err)
			}
			if client.stopCalls != 0 {
				t.Fatalf("target Stop called %d times after SAVE mismatch", client.stopCalls)
			}
			record := zx81LogRecord(t, logs)
			if record["reason"] != test.wantReason {
				t.Fatalf("log reason = %v, want %q: %+v", record["reason"], test.wantReason, record)
			}
			if test.wantObserved != "" && record["observed_package_id"] != test.wantObserved {
				t.Fatalf("observed package = %v, want %q: %+v", record["observed_package_id"], test.wantObserved, record)
			}
			if test.wantGeneration != 0 && record["observed_generation"] != test.wantGeneration {
				t.Fatalf("observed generation = %v, want %v: %+v", record["observed_generation"], test.wantGeneration, record)
			}
			if test.wantReason == "required_tape_write_capability_missing" &&
				(record["expected_tape_write_required"] != true || record["observed_tape_write_capable"] != false) {
				t.Fatalf("capability mismatch values missing from log: %+v", record)
			}
		})
	}
}

func TestRememberZX81ProgramKeepsPendingSaveOnCaptureOrAckFailure(t *testing.T) {
	service, client, store, entry := newZX81StopFixture(t)
	play := service.plays["kit"]
	client.data = []byte{0x00, 0x0a, 0x76}

	client.err = errors.New("capture failed")
	if err := service.rememberZX81Program(context.Background(), client, play); err == nil {
		t.Fatal("capture failure must block Stop")
	}
	if client.acks != 0 {
		t.Fatal("capture failure was acknowledged")
	}
	client.err = nil
	client.ackErr = errors.New("acknowledgement failed")
	if err := service.rememberZX81Program(context.Background(), client, play); err == nil {
		t.Fatal("acknowledgement failure must block Stop")
	}
	if client.acks != 1 {
		t.Fatalf("acknowledgements = %d, want failed acknowledgement", client.acks)
	}
	if saves, err := store.ListZX81Saves(context.Background(), entry.GameID); err != nil || len(saves) != 1 {
		t.Fatalf("durable row before failed acknowledgement = %+v, %v", saves, err)
	}
}
