package misterruntime

import (
	"context"
	"path/filepath"
	"testing"
)

func TestClientSNESUsesRuntimeOwnedCartridge(t *testing.T) {
	fixture := newSocketFixture(t, `{"protocol":1,"ok":true,"state":"running_game","execution":"game","system":"snes","core":"SNES","error":null,"version":"test"}`+"\n", true)
	_, err := NewClient(fixture.path).Launch(context.Background(), LaunchRequest{System: "snes", RBF: "/usr/share/mister-runtime/cores/snes.rbf", Media: map[string]string{"cartridge": "/cache/game.sfc"}, Settings: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := fixture.wait(t); got != `{"protocol":1,"operation":"launch","system":"snes","rbf":"/usr/share/mister-runtime/cores/snes.rbf","media":{"cartridge":"/cache/game.sfc"},"settings":{}}` {
		t.Fatalf("request: %s", got)
	}
}
func TestClientSNESRejectsWrongShapeBeforeDial(t *testing.T) {
	for _, mutate := range []func(*LaunchRequest){
		func(r *LaunchRequest) { r.Media = nil }, func(r *LaunchRequest) { r.Media = map[string]string{} }, func(r *LaunchRequest) { r.Media["firmware"] = "/cache/dsp.bin" }, func(r *LaunchRequest) { r.Settings["mapper"] = "lorom" }, func(r *LaunchRequest) { r.RBF = megaDriveRBFPath },
	} {
		r := LaunchRequest{System: "snes", RBF: "/usr/share/mister-runtime/cores/snes.rbf", Media: map[string]string{"cartridge": "/cache/game.sfc"}, Settings: map[string]string{}}
		mutate(&r)
		if _, err := NewClient(filepath.Join(t.TempDir(), "missing.sock")).Launch(context.Background(), r); err != errInvalidRuntimeRequest {
			t.Fatalf("did not reject before dial: %v", err)
		}
	}
}
