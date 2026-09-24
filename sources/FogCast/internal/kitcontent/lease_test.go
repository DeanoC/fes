package kitcontent

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestRemotePullAcquiresLeaseAndCopiesFakeSource(t *testing.T) {
	payload := []byte("remote-lease-rom")
	id := meshcontent.SumSHA256(payload)
	root := t.TempDir()
	store, err := Open(root, "kit-a", memSource{blobs: map[string][]byte{id.String(): payload}}, []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}})
	if err != nil {
		t.Fatal(err)
	}
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" {
		if time.Now().After(deadline) {
			t.Fatalf("kit status %+v", manager.Status())
		}
		time.Sleep(time.Millisecond)
	}
	server := httptest.NewServer(httpapi.New(meshAPI{}, "kit-token", "test", slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.WithKitLease(manager), httpapi.WithMeshContent(store)))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := Dial(context.Background(), endpoint, "kit-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Pull(context.Background(), id); err == nil {
		t.Fatal("pull without a lease authorizer wrote the kit")
	}
	if _, statErr := os.Stat(filepath.Join(root, "objects", id.Digest)); !os.IsNotExist(statErr) {
		t.Fatalf("unleased remote pull wrote %v", statErr)
	}

	lease := targetclient.NewKitLease(endpoint, "kit-token", server.Client(), "mesh-host", "mesh pull")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(endpoint, "kit-token", server.Client()).WithKitLease(lease)
	remote.SetMutationAuthorizer(client)
	state, err := remote.Pull(context.Background(), id)
	if err != nil || state != meshcontent.StatePresent {
		t.Fatalf("pull state %s err %v", state, err)
	}
	owned, generation := client.MeshKitLease()
	if !owned || generation == "" || manager.Status().Generation != generation {
		t.Fatalf("owned %v generation %q status %+v", owned, generation, manager.Status())
	}
	got, err := os.ReadFile(filepath.Join(root, "objects", id.Digest))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("object %q err %v", got, err)
	}
	if err := remote.LinkExpansion("port", id); err != nil {
		t.Fatal(err)
	}
	link, err := os.ReadFile(filepath.Join(root, "links", "port"))
	if err != nil || string(link) != id.String()+"\n" {
		t.Fatalf("link %q err %v", link, err)
	}
}
