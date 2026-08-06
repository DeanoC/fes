package hostapi_test

import (
	"context"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/hostapi"
	"github.com/DeanoC/FogCast-POC/internal/mediasession"
)

type adapterComponent struct{}

func (adapterComponent) Start(context.Context, string) (mediasession.ComponentHandle, error) {
	return adapterHandle{}, nil
}

type adapterHandle struct{}

func (adapterHandle) Stop(context.Context) error { return nil }

func TestManagedMediaSessionAdaptsToHostAPIMediaSession(t *testing.T) {
	managed := mediasession.New(adapterComponent{}, adapterComponent{})
	var media hostapi.MediaSession = hostapi.NewMediaSessionAdapter(managed)
	handle, err := media.Start(context.Background(), "game-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
