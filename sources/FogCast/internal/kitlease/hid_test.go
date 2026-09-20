package kitlease

import "testing"

func TestForeignHIDFailClosed(t *testing.T) {
	t.Parallel()
	ours := Status{State: "held", Owner: "fogcast@ai-dev-mac", Purpose: "interactive game/development session"}
	if ForeignHID(ours) {
		t.Fatalf("host grant treated foreign: %+v", ours)
	}
	active := Status{State: "active", Owner: "fogcast@ai-dev-mac"}
	if ForeignHID(active) {
		t.Fatalf("active host connection treated foreign: %+v", active)
	}
	if !ForeignHID(Status{State: "busy", Owner: "caster"}) {
		t.Fatal("busy connection must fail closed")
	}
	if !ForeignHID(Status{State: "blocked"}) {
		t.Fatal("blocked lease must fail closed")
	}
	if !ForeignHID(Status{State: "recovery-required"}) {
		t.Fatal("recovery-required connection must fail closed")
	}
	if !ForeignHID(Status{State: "held", Owner: HostlessOwner, Purpose: HostlessPurpose}) {
		t.Fatal("hostless grant must fail closed for HID")
	}
	if !ForeignHID(Status{State: "held", Owner: "other-host", Purpose: "hil"}) {
		t.Fatal("foreign owner must fail closed")
	}
	if ForeignHID(Status{State: "free"}) || ForeignHID(Status{}) {
		t.Fatal("free/empty is not an observed foreign grant")
	}
	if ForeignHID(Status{State: "held"}) {
		t.Fatal("held without owner is not observed foreign")
	}
}
