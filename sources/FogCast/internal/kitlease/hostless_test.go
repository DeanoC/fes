package kitlease

import "testing"

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
