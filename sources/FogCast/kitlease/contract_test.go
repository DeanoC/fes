package kitlease

import (
	"encoding/json"
	"testing"
	"time"
)

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

func TestHostlessAndForeignSession(t *testing.T) {
	t.Parallel()
	hostless := Status{State: "held", Owner: HostlessOwner, Purpose: HostlessPurpose}
	if !HostlessSession(hostless) || ForeignSession(hostless) {
		t.Fatalf("hostless %+v", hostless)
	}
	foreign := Status{State: "held", Owner: "caster", Purpose: "hil"}
	if HostlessSession(foreign) || !ForeignSession(foreign) {
		t.Fatalf("foreign %+v", foreign)
	}
	wrongPurpose := Status{State: "held", Owner: HostlessOwner, Purpose: "other"}
	if HostlessSession(wrongPurpose) || !ForeignSession(wrongPurpose) {
		t.Fatalf("wrong purpose %+v", wrongPurpose)
	}
	free := Status{State: "free"}
	if HostlessSession(free) || ForeignSession(free) {
		t.Fatalf("free %+v", free)
	}
}

func TestContractJSONRoundTrips(t *testing.T) {
	t.Parallel()
	expires := time.Date(2026, time.September, 16, 12, 34, 56, 789000000, time.UTC)
	cases := []struct {
		name  string
		value any
		new   func() any
	}{
		{
			name:  "status",
			value: Status{ExpiresInMS: 1234, State: "held", Generation: "generation", Owner: "fogcast@host", Purpose: "interactive", ExpiresAt: expires, Reason: "reason"},
			new:   func() any { return new(Status) },
		},
		{
			name:  "claim",
			value: ClaimRequest{RequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Owner: "fogcast@host", Purpose: "interactive"},
			new:   func() any { return new(ClaimRequest) },
		},
		{
			name:  "takeover",
			value: TakeoverRequest{ClaimRequest: ClaimRequest{RequestID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Owner: "operator", Purpose: "recovery"}, ExpectedGeneration: "generation", Reason: "repair"},
			new:   func() any { return new(TakeoverRequest) },
		},
		{
			name:  "grant",
			value: Grant{Status: Status{ExpiresInMS: 1234, State: "held", Generation: "generation", Owner: "fogcast@host", Purpose: "interactive", ExpiresAt: expires}, Token: "token"},
			new:   func() any { return new(Grant) },
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			decoded := test.new()
			if err := json.Unmarshal(encoded, decoded); err != nil {
				t.Fatal(err)
			}
			reencoded, err := json.Marshal(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if string(reencoded) != string(encoded) {
				t.Fatalf("JSON changed after round trip: %s != %s", reencoded, encoded)
			}
		})
	}
}
