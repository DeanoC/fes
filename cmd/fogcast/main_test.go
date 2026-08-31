package main

import (
	"context"
	"io"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/fogcastcli"
	"github.com/DeanoC/FogCast/protocol"
)

type mainFakeService struct {
	closed bool
}

func (*mainFakeService) Scan(context.Context) (catalog.ScanReport, error) {
	return catalog.ScanReport{}, nil
}
func (*mainFakeService) Games(context.Context) ([]catalog.Game, error) { return nil, nil }
func (*mainFakeService) Search(context.Context, string) ([]catalog.Game, error) {
	return nil, nil
}
func (*mainFakeService) Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return protocol.CachedLaunchResponse{}, nil
}
func (*mainFakeService) Health(context.Context) (protocol.Health, error) {
	return protocol.Health{}, nil
}
func (*mainFakeService) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{}, nil
}
func (*mainFakeService) Stop(context.Context) (protocol.Status, error) {
	return protocol.Status{}, nil
}
func (f *mainFakeService) Close() error { f.closed = true; return nil }

func TestRunForwardsContextArgumentsAndInjectedOpen(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "sentinel")
	service := &mainFakeService{}
	opened := false
	exit := run(ctx, []string{"games"}, io.Discard, io.Discard, func(openCtx context.Context, _ fogcast.Paths) (fogcastcli.Service, error) {
		opened = true
		if openCtx != ctx {
			t.Fatal("run replaced caller context")
		}
		return service, nil
	})
	if exit != 0 || !opened || !service.closed {
		t.Fatalf("exit=%d opened=%v closed=%v", exit, opened, service.closed)
	}
}
