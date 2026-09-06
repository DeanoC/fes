package misterruntime

import (
	"context"
	"path/filepath"
	"testing"
)

func TestClientPongUsesEmptyObjects(t *testing.T) {
	fixture := newSocketFixture(t, `{"protocol":1,"ok":true,"state":"running_game","execution":"game","system":"pong","core":"Pong","error":null,"version":"test"}`+"\n", true)
	_, err := NewClient(fixture.path).Launch(context.Background(), LaunchRequest{System: "pong", RBF: "/usr/share/mister-runtime/cores/pong.rbf", Media: map[string]string{}, Settings: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := fixture.wait(t); got != `{"protocol":1,"operation":"launch","system":"pong","rbf":"/usr/share/mister-runtime/cores/pong.rbf","media":{},"settings":{}}` {
		t.Fatalf("wire request = %s", got)
	}
}

func TestClientPongRejectsUnexpectedPayload(t *testing.T) {
	for _, mutate := range []func(*LaunchRequest){
		func(r *LaunchRequest) { r.Media = nil },
		func(r *LaunchRequest) { r.Media["cartridge"] = "/tmp/game.bin" },
		func(r *LaunchRequest) { r.Settings["region"] = "auto" },
		func(r *LaunchRequest) { r.RBF = megaDriveRBFPath },
	} {
		request := LaunchRequest{System: "pong", RBF: "/usr/share/mister-runtime/cores/pong.rbf", Media: map[string]string{}, Settings: map[string]string{}}
		mutate(&request)
		_, err := NewClient(filepath.Join(t.TempDir(), "absent.sock")).Launch(context.Background(), request)
		if err != errInvalidRuntimeRequest {
			t.Fatalf("request not rejected before dialing: %v", err)
		}
	}
}
