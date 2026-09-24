package targetclient

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
	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

type blobSource struct {
	blobs map[string][]byte
}

func (b blobSource) Advertises(id meshcontent.ContentID) bool {
	_, ok := b.blobs[id.String()]
	return ok
}

func (b blobSource) Open(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body, ok := b.blobs[id.String()]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), body...))), nil
}

type meshLeaseAPI struct{}

func (meshLeaseAPI) Health(string) protocol.Health { return protocol.Health{Ready: true} }
func (meshLeaseAPI) Status() protocol.Status {
	return protocol.Status{State: protocol.StateIdle}
}
func (meshLeaseAPI) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	return protocol.Status{State: protocol.StateIdle}, nil
}

func TestFreshSessionPullAcquiresLeaseAgainstFakeSource(t *testing.T) {
	payload := []byte("fresh-session-rom")
	other := []byte("foreign-session-rom")
	bare := []byte("bare-client-rom")
	primary := meshcontent.SumSHA256(payload)
	foreignID := meshcontent.SumSHA256(other)
	bareID := meshcontent.SumSHA256(bare)
	root := t.TempDir()
	store, err := kitcontent.Open(root, "kit-a", blobSource{blobs: map[string][]byte{
		primary.String():   payload,
		foreignID.String(): other,
		bareID.String():    bare,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	waitKitFree(t, manager)
	server := httptest.NewServer(httpapi.New(meshLeaseAPI{}, "kit-token", "test", slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.WithKitLease(manager), httpapi.WithMeshContent(store)))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	lease := NewKitLease(endpoint, "kit-token", server.Client(), "mesh-host", "mesh pull")
	defer lease.Close(context.Background())
	client := NewClient(endpoint, "kit-token", server.Client()).WithKitLease(lease)
	remoteFor := func(auth kitcontent.MutationAuthorizer) *kitcontent.Remote {
		t.Helper()
		remote, dialErr := kitcontent.Dial(context.Background(), endpoint, "kit-token", server.Client())
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		remote.SetMutationAuthorizer(auth)
		return remote
	}
	if err := remoteFor(client).LinkExpansion("early", primary); !errors.Is(err, ErrKitLeaseLost) {
		t.Fatalf("unleased link: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "links", "early")); !os.IsNotExist(statErr) {
		t.Fatalf("unleased link wrote the kit: %v", statErr)
	}
	if manager.Status().State != "free" {
		t.Fatalf("link claimed the kit: %+v", manager.Status())
	}

	state, err := remoteFor(client).Pull(context.Background(), primary)
	if err != nil || state != meshcontent.StatePresent {
		t.Fatalf("pull state %q err %v", state, err)
	}
	owned, generation := client.MeshKitLease()
	if !owned || generation == "" {
		t.Fatalf("owned %v generation %q", owned, generation)
	}
	if manager.Status().State != "held" || manager.Status().Owner != "mesh-host" || manager.Status().Generation != generation {
		t.Fatalf("status %+v", manager.Status())
	}
	got, err := os.ReadFile(filepath.Join(root, "objects", primary.Digest))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("object %q err %v", got, err)
	}
	if err := remoteFor(client).LinkExpansion("port", primary); err != nil {
		t.Fatal(err)
	}
	link, err := os.ReadFile(filepath.Join(root, "links", "port"))
	if err != nil || string(link) != primary.String()+"\n" {
		t.Fatalf("link %q err %v", link, err)
	}

	otherLease := NewKitLease(endpoint, "kit-token", server.Client(), "other-shell", "mesh pull")
	defer otherLease.Close(context.Background())
	otherClient := NewClient(endpoint, "kit-token", server.Client()).WithKitLease(otherLease)
	_, err = remoteFor(otherClient).Pull(context.Background(), foreignID)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != "KIT_LEASE_BUSY" {
		t.Fatalf("foreign pull: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "objects", foreignID.Digest)); !os.IsNotExist(statErr) {
		t.Fatalf("foreign pull wrote the kit: %v", statErr)
	}
	if err := remoteFor(otherClient).LinkExpansion("aux", primary); !errors.Is(err, ErrKitLeaseLost) {
		t.Fatalf("foreign link: %v", err)
	}

	bareClient := NewClient(endpoint, "kit-token", server.Client())
	_, err = remoteFor(bareClient).Pull(context.Background(), bareID)
	var denied *meshcontent.LeaseDeniedError
	if !errors.As(err, &denied) || denied.Code != "KIT_LEASE_REQUIRED" {
		t.Fatalf("bare pull: %v", err)
	}
	linkErr := remoteFor(bareClient).LinkExpansion("bare", primary)
	if linkErr == nil {
		t.Fatal("bare link succeeded")
	}
	denied = nil
	if !errors.As(linkErr, &denied) || denied.Code != "KIT_LEASE_REQUIRED" {
		t.Fatalf("bare link: %v", linkErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "objects", bareID.Digest)); !os.IsNotExist(statErr) {
		t.Fatalf("bare pull wrote the kit: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "links", "bare")); !os.IsNotExist(statErr) {
		t.Fatalf("bare link wrote the kit: %v", statErr)
	}
}

func waitKitFree(t *testing.T, manager *kitlease.Manager) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" {
		if time.Now().After(deadline) {
			t.Fatalf("kit status %+v", manager.Status())
		}
		time.Sleep(time.Millisecond)
	}
}
