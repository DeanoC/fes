package kitcontent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

type memSource struct {
	blobs map[string][]byte
}

func (m memSource) Advertises(id meshcontent.ContentID) bool {
	_, ok := m.blobs[id.String()]
	return ok
}

func (m memSource) Open(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body, ok := m.blobs[id.String()]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), body...))), nil
}

func packageABI() meshcontent.PackageABI {
	return meshcontent.PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}
}

func TestStorePullCommitsSeparateSlotBytesAndLinksIdempotently(t *testing.T) {
	primary := []byte("source-rom-bytes")
	slot := []byte("expansion-slot-bytes")
	primaryID := meshcontent.SumSHA256(primary)
	slotID := meshcontent.SumSHA256(slot)
	if primaryID == slotID {
		t.Fatal("fixture digests collided")
	}
	root := t.TempDir()
	store, err := Open(root, "kit-a", memSource{blobs: map[string][]byte{
		primaryID.String(): primary,
		slotID.String():    slot,
	}}, []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if store.Slot(primaryID) != meshcontent.StateMissing || store.Slot(slotID) != meshcontent.StateMissing {
		t.Fatal("empty store reported present")
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
	result, err := meshcontent.Ensure(context.Background(), entry, "kit-a", store, meshcontent.EnsureOption{LeaseFree: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Execute || result.Block != meshcontent.BlockNone {
		t.Fatalf("execute=%v block=%s", result.Execute, result.Block)
	}
	if store.Slot(primaryID) != meshcontent.StatePresent || store.Slot(slotID) != meshcontent.StatePresent {
		t.Fatal("pull did not commit both slots")
	}
	gotPrimary, err := os.ReadFile(filepath.Join(root, "objects", primaryID.Digest))
	if err != nil || !bytes.Equal(gotPrimary, primary) {
		t.Fatalf("primary bytes %q err %v", gotPrimary, err)
	}
	gotSlot, err := os.ReadFile(filepath.Join(root, "objects", slotID.Digest))
	if err != nil || !bytes.Equal(gotSlot, slot) {
		t.Fatalf("slot bytes %q err %v", gotSlot, err)
	}
	link, err := os.ReadFile(filepath.Join(root, "links", "port"))
	if err != nil || string(link) != slotID.String()+"\n" {
		t.Fatalf("link %q err %v", link, err)
	}
	if bytes.Equal(gotPrimary, gotSlot) || bytes.Contains(gotPrimary, slot) {
		t.Fatal("primary and expansion bytes were collapsed")
	}
	if err := store.LinkExpansion("port", slotID); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(filepath.Join(root, "links", "port"))
	if err != nil || string(again) != string(link) {
		t.Fatalf("repeat link %q err %v", again, err)
	}
	partials, err := os.ReadDir(filepath.Join(root, "partial"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("partials left behind: %v %v", partials, err)
	}
}

func TestStorePullHonorsCancelAndDropsPartialBytes(t *testing.T) {
	payload := bytes.Repeat([]byte("rom"), 1024)
	id := meshcontent.SumSHA256(payload)
	release := make(chan struct{})
	source := &blockingSource{body: payload, id: id, started: make(chan struct{}), release: release}
	root := t.TempDir()
	store, err := Open(root, "kit-a", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, pullErr := store.Pull(ctx, id)
		done <- pullErr
	}()
	select {
	case <-source.started:
	case <-time.After(2 * time.Second):
		t.Fatal("pull did not start")
	}
	if store.Slot(id) != meshcontent.StateChecking {
		t.Fatalf("slot %s while pull is running", store.Slot(id))
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pull err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled pull did not return")
	}
	if store.Slot(id) != meshcontent.StateMissing {
		t.Fatalf("slot after cancel %s", store.Slot(id))
	}
	partials, err := os.ReadDir(filepath.Join(root, "partial"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("partials after cancel: %v %v", partials, err)
	}
	if _, err := os.Stat(filepath.Join(root, "objects", id.Digest)); !os.IsNotExist(err) {
		t.Fatalf("object visible after cancel: %v", err)
	}
	close(release)
}

func TestStoreCheckingDoesNotAllowExecute(t *testing.T) {
	payload := []byte("source-rom-bytes")
	id := meshcontent.SumSHA256(payload)
	release := make(chan struct{})
	source := &blockingSource{body: payload, id: id, started: make(chan struct{}), release: release}
	store, err := Open(t.TempDir(), "kit-a", source, []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		_, _ = store.Pull(context.Background(), id)
		close(finished)
	}()
	select {
	case <-source.started:
	case <-time.After(2 * time.Second):
		t.Fatal("pull did not start")
	}
	entry := meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(packageABI()),
			meshcontent.PrimaryMediaSlot(id),
		},
	}
	result, err := meshcontent.Ensure(context.Background(), entry, "kit-a", store, meshcontent.EnsureOption{LeaseFree: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Execute || result.Block != meshcontent.BlockEnsureProgress {
		t.Fatalf("execute=%v block=%s", result.Execute, result.Block)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("pull did not finish")
	}
}

func TestStoreOrphanPartialIsNotPresent(t *testing.T) {
	id := meshcontent.SumSHA256([]byte("source-rom-bytes"))
	root := t.TempDir()
	store, err := Open(root, "kit-a", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "partial"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "partial", id.Digest+".orphan"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	if store.Slot(id) != meshcontent.StateMissing {
		t.Fatalf("partial reported %s", store.Slot(id))
	}
}

func TestStoreDigestMismatchIsNotPresent(t *testing.T) {
	id := meshcontent.SumSHA256([]byte("source-rom-bytes"))
	root := t.TempDir()
	store, err := Open(root, "kit-a", memSource{blobs: map[string][]byte{
		id.String(): []byte("not-the-named-bytes"),
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Pull(context.Background(), id)
	if !errors.Is(err, meshcontent.ErrContentPullFailed) {
		t.Fatalf("err %v", err)
	}
	if store.Slot(id) != meshcontent.StateMissing {
		t.Fatalf("slot %s", store.Slot(id))
	}
	partials, err := os.ReadDir(filepath.Join(root, "partial"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("partials %v %v", partials, err)
	}
}

func TestStoreLinkFailureLeavesEarlierLink(t *testing.T) {
	first := []byte("slot-one")
	firstID := meshcontent.SumSHA256(first)
	missing := meshcontent.SumSHA256([]byte("slot-two"))
	root := t.TempDir()
	store, err := Open(root, "kit-a", memSource{blobs: map[string][]byte{firstID.String(): first}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pull(context.Background(), firstID); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkExpansion("port", firstID); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkExpansion("aux", missing); err == nil {
		t.Fatal("linked a missing slot")
	}
	link, err := os.ReadFile(filepath.Join(root, "links", "port"))
	if err != nil || string(link) != firstID.String()+"\n" {
		t.Fatalf("earlier link %q err %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(root, "links", "aux")); !os.IsNotExist(err) {
		t.Fatalf("failed link created a file: %v", err)
	}
}

// blockingSource writes one byte, signals started, then waits for ctx
// or release before returning the rest. A canceled pull therefore has
// a partial file to clean up.
type blockingSource struct {
	body    []byte
	id      meshcontent.ContentID
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (s *blockingSource) Advertises(id meshcontent.ContentID) bool { return id == s.id }

func (s *blockingSource) Open(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id != s.id {
		return nil, errors.New("missing")
	}
	return &blockingReader{ctx: ctx, data: s.body, started: s.started, release: s.release, once: &s.once}, nil
}

type blockingReader struct {
	ctx     context.Context
	data    []byte
	off     int
	started chan struct{}
	release <-chan struct{}
	once    *sync.Once
	waiting bool
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	if !r.waiting {
		p[0] = r.data[0]
		r.off = 1
		r.waiting = true
		r.once.Do(func() { close(r.started) })
		return 1, nil
	}
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case <-r.release:
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (r *blockingReader) Close() error { return nil }

func TestOpenDoesNotCreateDirectoriesAndSweepsPartial(t *testing.T) {
	root := t.TempDir()
	partial := filepath.Join(root, "partial")
	if err := os.MkdirAll(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "stale"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "kit-a", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "objects")); !os.IsNotExist(err) {
		t.Fatalf("open created objects: %v", err)
	}
	if _, err := os.Stat(filepath.Join(partial, "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale partial remained: %v", err)
	}
}

func TestPullRefusesLowFreeSpaceAndQuota(t *testing.T) {
	id := meshcontent.SumSHA256([]byte("abcd"))
	root := t.TempDir()
	store, err := Open(root, "kit-a", memSource{blobs: map[string][]byte{id.String(): []byte("abcd")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	origFree := availableBytes
	origQuota := meshContentQuota
	t.Cleanup(func() {
		availableBytes = origFree
		meshContentQuota = origQuota
	})
	availableBytes = func(string) (uint64, error) { return meshContentReserve - 1, nil }
	if _, err := store.Pull(context.Background(), id); !errors.Is(err, meshcontent.ErrContentPullFailed) {
		t.Fatalf("free space err %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "objects", id.Digest)); !os.IsNotExist(err) {
		t.Fatal("low free space committed an object")
	}
	availableBytes = origFree
	meshContentQuota = 3
	if _, err := store.Pull(context.Background(), id); !errors.Is(err, meshcontent.ErrContentPullFailed) {
		t.Fatalf("quota err %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "objects", id.Digest)); !os.IsNotExist(err) {
		t.Fatal("quota committed an object")
	}
}
