package input

import (
	"context"
	"testing"
	"time"
)

func TestTargetControllerRejectsInvalidLease(t *testing.T) {
	controller := NewTargetController()
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("short"), Core: "SNES"}); err == nil {
		t.Fatal("short token accepted")
	}
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "/private"}); err == nil {
		t.Fatal("unsafe core accepted")
	}
}

func TestTargetControllerDetachIsIdempotent(t *testing.T) {
	controller := NewTargetController()
	if err := controller.Detach(context.Background(), 99); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetControllerOpenStreamRequiresActiveLease(t *testing.T) {
	controller := NewTargetController()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := controller.OpenStream(ctx, 1); err == nil {
		t.Fatal("stream opened without lease")
	}
}

func TestNewTargetControllerForProfileRefusesDevelopmentProfileConstruction(t *testing.T) {
	if controller := NewTargetControllerForProfile("127.0.0.1:18183", "/dev/uinput", true); controller != nil {
		t.Fatal("development profile constructed an input controller")
	}
	if controller := NewTargetControllerForProfile("127.0.0.1:18183", "/dev/uinput", false); controller == nil {
		t.Fatal("production profile did not construct input controller")
	}
}
