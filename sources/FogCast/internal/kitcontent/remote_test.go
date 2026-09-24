package kitcontent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

type meshAPI struct{}

func (meshAPI) Health(string) protocol.Health { return protocol.Health{Ready: true} }
func (meshAPI) Status() protocol.Status {
	return protocol.Status{State: protocol.StateIdle}
}
func (meshAPI) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	return protocol.Status{State: protocol.StateIdle}, nil
}

func TestRemoteEnsurePullsOntoTheKitStore(t *testing.T) {
	primary := []byte("source-rom-bytes")
	slot := []byte("expansion-slot-bytes")
	primaryID := meshcontent.SumSHA256(primary)
	slotID := meshcontent.SumSHA256(slot)
	root := t.TempDir()
	store, err := Open(root, "kit-a", memSource{blobs: map[string][]byte{
		primaryID.String(): primary,
		slotID.String():    slot,
	}}, []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(meshAPI{}, "kit-token", "test", slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.WithMeshContent(store)))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := Dial(context.Background(), endpoint, "kit-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if remote.NodeID() != "kit-a" {
		t.Fatalf("node %s", remote.NodeID())
	}
	entry := meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(packageABI()),
			meshcontent.PrimaryMediaSlot(primaryID),
			meshcontent.ExpansionSlot("port", slotID),
		},
	}
	result, err := meshcontent.Ensure(context.Background(), entry, "kit-a", remote, meshcontent.EnsureOption{LeaseFree: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Execute {
		t.Fatalf("block %s", result.Block)
	}
	got, err := os.ReadFile(filepath.Join(root, "objects", primaryID.Digest))
	if err != nil || !bytes.Equal(got, primary) {
		t.Fatalf("primary %q err %v", got, err)
	}
	link, err := os.ReadFile(filepath.Join(root, "links", "port"))
	if err != nil || string(link) != slotID.String()+"\n" {
		t.Fatalf("link %q err %v", link, err)
	}
}

func TestRemotePullHonorsCancel(t *testing.T) {
	payload := bytes.Repeat([]byte("rom"), 256)
	id := meshcontent.SumSHA256(payload)
	release := make(chan struct{})
	defer close(release)
	source := &blockingSource{body: payload, id: id, started: make(chan struct{}), release: release}
	root := t.TempDir()
	store, err := Open(root, "kit-a", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(meshAPI{}, "kit-token", "test", slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.WithMeshContent(store)))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := Dial(context.Background(), endpoint, "kit-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, pullErr := remote.Pull(ctx, id)
		done <- pullErr
	}()
	select {
	case <-source.started:
	case <-time.After(2 * time.Second):
		t.Fatal("remote pull did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pull err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remote pull did not return")
	}
	deadline := time.Now().Add(2 * time.Second)
	for store.Slot(id) == meshcontent.StateChecking && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if store.Slot(id) != meshcontent.StateMissing {
		t.Fatalf("slot %s", store.Slot(id))
	}
	partials, err := os.ReadDir(filepath.Join(root, "partial"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("partials %v %v", partials, err)
	}
}

func TestUnboundRemoteDoesNotPull(t *testing.T) {
	id := meshcontent.SumSHA256([]byte("source-rom-bytes"))
	store, err := Open(t.TempDir(), "kit-a", memSource{blobs: map[string][]byte{id.String(): []byte("source-rom-bytes")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(meshAPI{}, "kit-token", "test", slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.WithMeshContent(store)))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := Dial(context.Background(), endpoint, "kit-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	entry := meshcontent.Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
		Slots:      []meshcontent.Slot{meshcontent.PrimaryMediaSlot(id)},
	}
	_, err = meshcontent.Ensure(context.Background(), entry, "other-node", remote, meshcontent.EnsureOption{LeaseFree: true})
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if store.Slot(id) != meshcontent.StateMissing {
		t.Fatal("unbound ensure pulled")
	}
}
