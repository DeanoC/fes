package misterruntime

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestPackageHealthAndIdleUseProtocol2(t *testing.T) {
	idle := fixtureLines(t, "protocol-v2.jsonl")[1] + "\n"
	socket := newSequenceSocketFixture(t, []string{idle, idle})
	runtime := NewRuntime(NewClient(socket.path), "", time.Millisecond, time.Second)
	if health := runtime.Health("test"); !health.Ready {
		t.Fatal("protocol-2 idle is not ready")
	}
	if !runtime.ConfirmIdle(context.Background()) {
		t.Fatal("protocol-2 idle was not confirmed")
	}
	want := []string{`{"protocol":2,"operation":"status"}`, `{"protocol":2,"operation":"status"}`}
	if got := socket.wait(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %v", got)
	}
}
