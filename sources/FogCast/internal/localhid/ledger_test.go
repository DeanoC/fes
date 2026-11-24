package localhid

import (
	"errors"
	"reflect"
	"testing"
)

func device(n byte) DeviceID { var id DeviceID; id[0] = n; return id }
func expect(t *testing.T, l *Ledger, got []Delta, want ...Delta) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delta=%+v want %+v", got, want)
	}
	if !l.quarantined {
		id, ok := l.Pending()
		if !ok {
			t.Fatal("missing pending transaction")
		}
		if err := l.Confirm(id); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSnapshotHeldAndIndependentDeviceContributors(t *testing.T) {
	a, b := device(1), device(2)
	l, err := New(10, []DeviceSnapshot{{ID: a, Held: []uint8{4}}, {ID: b}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.Edge(11, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(12, b, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4, Down: true})
	got, err = l.Edge(13, b, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4})
	got, err = l.Edge(14, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(15, a, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(16, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4, Down: true})
	got, err = l.Edge(17, b, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Remove(18, a)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(19, b, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4})
}
func TestFencePreservesHeldSuppressionUntilEachRelease(t *testing.T) {
	a, b := device(1), device(2)
	l, err := New(0, []DeviceSnapshot{{ID: a}, {ID: b}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.Edge(1, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4, Down: true})
	got, err = l.Edge(2, b, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(3, a, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 5, Down: true})
	got, err = l.Fence()
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4}, Delta{Usage: 5})
	if err := l.Resume(); err != nil {
		t.Fatal(err)
	}
	got, err = l.Edge(4, b, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(5, a, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(6, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4, Down: true})
	got, err = l.Edge(7, b, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got)
	got, err = l.Edge(8, a, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4})
}
func TestGapQuarantinesButStillRequiresSourceNeutral(t *testing.T) {
	a := device(1)
	l, err := New(0, []DeviceSnapshot{{ID: a}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.Edge(1, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4, Down: true})
	if _, err = l.Edge(3, a, 4, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("gap %v", err)
	}
	if _, err = l.Edge(2, a, 4, false); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("replay %v", err)
	}
	got, err = l.Quarantine()
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4})
	if err = l.Resume(); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("resume %v", err)
	}
}
func TestAmbiguousReleaseStillRequiresFullSourceNeutral(t *testing.T) {
	a := device(1)
	l, err := New(0, []DeviceSnapshot{{ID: a}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.Edge(1, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, got, Delta{Usage: 4, Down: true})
	got, err = l.Edge(2, a, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Down {
		t.Fatal("expected release")
	}
	id, ok := l.Pending()
	if !ok {
		t.Fatal("missing pending release")
	}
	if err = l.Fail(id); err != nil {
		t.Fatal(err)
	}
	if _, err = l.Edge(3, a, 4, false); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("replayed edge %v", err)
	}
}
func TestInvalidSnapshotsDoNotCreateAuthority(t *testing.T) {
	for _, s := range [][]DeviceSnapshot{nil, {{ID: device(1), Held: []uint8{4, 4}}}, {{ID: device(1)}, {ID: device(1)}}, {{ID: DeviceID{}}}} {
		if _, err := New(0, s); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid snapshot %+v", s)
		}
	}
}

func TestPendingSinkOutcomeFencesNextOperation(t *testing.T) {
	a := device(1)
	l, err := New(0, []DeviceSnapshot{{ID: a}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Edge(1, a, 4, true); err != nil {
		t.Fatal(err)
	}
	first, ok := l.Pending()
	if !ok {
		t.Fatal("missing pending edge")
	}
	if _, err := l.Edge(2, a, 4, false); !errors.Is(err, ErrPending) {
		t.Fatalf("pending edge %v", err)
	}
	if _, err := l.Remove(2, a); !errors.Is(err, ErrPending) {
		t.Fatalf("pending remove %v", err)
	}
	if _, err := l.Fence(); !errors.Is(err, ErrPending) {
		t.Fatalf("pending fence %v", err)
	}
	if err := l.Confirm(first); err != nil {
		t.Fatal(err)
	}
	if err := l.Confirm(first); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate confirmation %v", err)
	}
	if _, err := l.Fence(); err != nil {
		t.Fatal(err)
	}
	second, ok := l.Pending()
	if !ok || second == first {
		t.Fatal("missing fresh fence transaction")
	}
	if err := l.Resume(); !errors.Is(err, ErrPending) {
		t.Fatalf("resume before neutral %v", err)
	}
	if err := l.Confirm(first); !errors.Is(err, ErrInvalid) {
		t.Fatalf("old confirmation accepted: %v", err)
	}
	if err := l.Confirm(second); err != nil {
		t.Fatal(err)
	}
	if err := l.Resume(); err != nil {
		t.Fatal(err)
	}
}
func TestAmbiguousDeviceRemovalQuarantinesSource(t *testing.T) {
	a, b := device(1), device(2)
	l, err := New(0, []DeviceSnapshot{{ID: a}, {ID: b}})
	if err != nil {
		t.Fatal(err)
	}
	delta, err := l.Edge(1, a, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, l, delta, Delta{Usage: 4, Down: true})
	delta, err = l.Remove(2, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) != 1 || delta[0].Down {
		t.Fatal("expected neutral delta")
	}
	id, ok := l.Pending()
	if !ok {
		t.Fatal("missing pending removal")
	}
	if err = l.Fail(id); err != nil {
		t.Fatal(err)
	}
	if _, err = l.Edge(3, b, 5, true); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("successor after uncertain neutral: %v", err)
	}
}
func TestSequenceAndTransactionOverflowFailClosed(t *testing.T) {
	a := device(1)
	l, err := New(^uint64(0), []DeviceSnapshot{{ID: a}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.Edge(0, a, 4, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sequence wrap accepted: %v", err)
	}
	if _, err = l.Quarantine(); err != nil {
		t.Fatal(err)
	}
	l, err = New(0, []DeviceSnapshot{{ID: a}})
	if err != nil {
		t.Fatal(err)
	}
	l.lastID = ^uint64(0)
	if _, err = l.Edge(1, a, 4, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("transaction wrap accepted: %v", err)
	}
	if _, err = l.Edge(2, a, 4, false); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("source not quarantined: %v", err)
	}
}

func TestQuarantineWaitsForLateSinkWriteBeforeNeutral(t *testing.T) {
	a := device(1)
	l, err := New(0, []DeviceSnapshot{{ID: a}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Edge(1, a, 4, true); err != nil {
		t.Fatal(err)
	}
	id, ok := l.Pending()
	if !ok {
		t.Fatal("no pending write")
	}
	if _, err := l.Quarantine(); !errors.Is(err, ErrPending) {
		t.Fatalf("quarantine before write drained: %v", err)
	}
	if _, err := l.Edge(2, a, 4, false); !errors.Is(err, ErrPending) {
		t.Fatalf("edge overtook pending write: %v", err)
	}
	// The owner receives sink completion only now; neutralization can follow.
	if err := l.Confirm(id); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("expected quarantined after drain: %v", err)
	}
	if _, err := l.Edge(2, a, 4, false); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("accepted post-quarantine edge: %v", err)
	}
	if _, err := l.Quarantine(); err != nil {
		t.Fatal(err)
	}
}
