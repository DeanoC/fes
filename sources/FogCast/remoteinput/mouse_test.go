package remoteinput

import "testing"

func TestMouseVectorTransientState(t *testing.T) {
	for _, xy := range [][2]int16{{-32768, 32767}, {0, 0}, {127, -128}} {
		e := MouseEvent(xy[0], xy[1], 3)
		x, y, b, ok := MouseVector(e)
		if !ok || x != xy[0] || y != xy[1] || b != 3 {
			t.Fatalf("vector %+v %d %d %d %v", e, x, y, b, ok)
		}
		var s State
		if err := s.Apply(e); err != nil {
			t.Fatal(err)
		}
		snap := s.Snapshot()
		if !snap.Mouse || snap.MouseButtons != 3 || len(snap.Pressed) != 0 || len(snap.Axes) != 0 {
			t.Fatalf("snapshot %+v", snap)
		}
		s.ReleaseAll()
		if s.Snapshot().MouseButtons != 0 {
			t.Fatal("held buttons after release")
		}
	}
	for _, bad := range []Event{{Device: DeviceMouse}, {Device: DeviceMouse, Kind: KindRelative, Action: ActionRelative, Code: 4}, {Player: 1, Device: DeviceMouse, Kind: KindRelative, Action: ActionRelative}} {
		if _, _, _, ok := MouseVector(bad); ok {
			t.Fatalf("accepted %+v", bad)
		}
	}
}
