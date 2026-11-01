package gfx

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type releasingMenuClient struct {
	*testMenuClient
	releaseMu  sync.Mutex
	releases   int
	releaseErr error
}

func (c *releasingMenuClient) ReleaseWindow() error {
	c.releaseMu.Lock()
	defer c.releaseMu.Unlock()
	c.releases++
	return c.releaseErr
}
func (c *releasingMenuClient) releaseCount() int {
	c.releaseMu.Lock()
	defer c.releaseMu.Unlock()
	return c.releases
}

func TestMenuPauseReleasesWindowOnlyAfterFrameDrains(t *testing.T) {
	block := make(chan struct{})
	client := &releasingMenuClient{testMenuClient: &testMenuClient{generation: 3, ready: make(chan struct{}, 3), presentBlock: block}}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("frame did not start")
	}
	done := make(chan error, 1)
	go func() { done <- d.PauseForLaunch(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("pause completed before drain: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if client.releaseCount() != 0 {
		t.Fatal("released mapping while a frame could still write it")
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if client.releaseCount() != 1 {
		t.Fatal("drained pause retained mapping")
	}
	d.Resume()
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("rejected launch could not resume")
	}
	if err := d.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.releaseCount() != 2 {
		t.Fatal("resumed mapping was not released")
	}
}

func TestMenuPausePropagatesWindowReleaseFailure(t *testing.T) {
	failure := errors.New("unmap failed")
	client := &releasingMenuClient{testMenuClient: &testMenuClient{}, releaseErr: failure}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.PauseForLaunch(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("pause error=%v", err)
	}
	d.mu.Lock()
	paused := d.paused
	d.mu.Unlock()
	if !paused {
		t.Fatal("failed release allowed submissions to resume")
	}
}

func TestCancelledMenuPauseKeepsInflightMapping(t *testing.T) {
	block := make(chan struct{})
	client := &releasingMenuClient{testMenuClient: &testMenuClient{generation: 3, ready: make(chan struct{}, 1), presentBlock: block}}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("frame did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Pause(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pause error=%v", err)
	}
	if client.releaseCount() != 0 {
		t.Fatal("cancelled wait released inflight mapping")
	}
	close(block)
	if err := d.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.releaseCount() != 1 {
		t.Fatal("later completed pause retained mapping")
	}
}

func TestResumedMenuPauseCannotReleaseANewFrameMapping(t *testing.T) {
	block := make(chan struct{})
	client := &releasingMenuClient{testMenuClient: &testMenuClient{generation: 3, ready: make(chan struct{}, 1), presentBlock: block}}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("frame did not start")
	}
	done := make(chan error, 1)
	go func() { done <- d.Pause(context.Background()) }()
	until := time.Now().Add(time.Second)
	for {
		d.mu.Lock()
		paused := d.paused
		d.mu.Unlock()
		if paused {
			break
		}
		if time.Now().After(until) {
			close(block)
			t.Fatal("pause did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	d.Resume()
	close(block)
	if err := <-done; err == nil {
		t.Fatal("superseded pause reported launch-ready")
	}
	if client.releaseCount() != 0 {
		t.Fatal("superseded pause released mapping after resume")
	}
}
