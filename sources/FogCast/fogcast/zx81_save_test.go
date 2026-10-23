package fogcast

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type zx81RememberClient struct {
	status protocol.Status
	data   []byte
	err    error
	ackErr error
	acks   int
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
	return c.status, nil
}
func (c *zx81RememberClient) Stop(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateIdle}, nil
}
func (c *zx81RememberClient) CaptureZX81Tape(context.Context, string, uint64) ([]byte, error) {
	return append([]byte(nil), c.data...), c.err
}
func (c *zx81RememberClient) AcknowledgeZX81Tape(context.Context, string, uint64) error {
	c.acks++
	return c.ackErr
}

func TestRememberZX81ProgramStoresACaptureAndIgnoresAnEmptyOne(t *testing.T) {
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
	service := &Service{catalog: store}
	client := &zx81RememberClient{
		status: protocol.Status{State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{
			PackageID: pkg, Generation: 3,
			ABI: protocol.RuntimeContract{ID: protocol.ComputerABIID, Major: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{
				{ID: protocol.Zx81TapeInterface().ID, Major: 1},
				{ID: protocol.Zx81TapeWriteInterface().ID, Major: 1},
			},
		}},
	}
	play := targetPlay{gameID: entry.GameID, packageID: pkg, packageGeneration: 3}
	if err := service.rememberZX81Program(ctx, client, play); err != nil {
		t.Fatalf("empty capture: %v", err)
	}
	if saves, err := store.ListZX81Saves(ctx, entry.GameID); err != nil || len(saves) != 0 {
		t.Fatalf("empty capture stored %+v %v", saves, err)
	}
	client.data = []byte{0x00, 0x0a, 0x76}
	if err := service.rememberZX81Program(ctx, client, play); err != nil {
		t.Fatalf("capture: %v", err)
	}
	saves, err := store.ListZX81Saves(ctx, entry.GameID)
	if err != nil || len(saves) != 1 || saves[0].Size != 3 || !strings.HasPrefix(saves[0].Name, "Saved program ") {
		t.Fatalf("capture = %+v %v", saves, err)
	}
	if client.acks != 1 {
		t.Fatalf("acknowledgements = %d, want 1 after durable insert", client.acks)
	}
}

func TestRememberZX81ProgramKeepsPendingSaveOnCaptureOrAckFailure(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pkg := strings.Repeat("a", 64)
	entry, err := store.CreateCoreEntry(ctx, "ZX81", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	client := &zx81RememberClient{
		data: []byte{0x00, 0x0a, 0x76},
		status: protocol.Status{State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{
			PackageID: pkg, Generation: 3,
			ABI: protocol.RuntimeContract{ID: protocol.ComputerABIID, Major: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{
				{ID: protocol.Zx81TapeInterface().ID, Major: 1},
				{ID: protocol.Zx81TapeWriteInterface().ID, Major: 1},
			},
		}},
	}
	play := targetPlay{gameID: entry.GameID, packageID: pkg, packageGeneration: 3}
	service := &Service{catalog: store}
	client.err = errors.New("capture failed")
	if err := service.rememberZX81Program(ctx, client, play); err == nil {
		t.Fatal("capture failure must block Stop")
	}
	if client.acks != 0 {
		t.Fatal("capture failure was acknowledged")
	}
	client.err = nil
	client.ackErr = errors.New("acknowledgement failed")
	if err := service.rememberZX81Program(ctx, client, play); err == nil {
		t.Fatal("acknowledgement failure must block Stop")
	}
	if client.acks != 1 {
		t.Fatalf("acknowledgements = %d, want failed acknowledgement", client.acks)
	}
	if saves, err := store.ListZX81Saves(ctx, entry.GameID); err != nil || len(saves) != 1 {
		t.Fatalf("durable row before failed acknowledgement = %+v, %v", saves, err)
	}
}
