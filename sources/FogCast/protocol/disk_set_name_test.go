package protocol

import "testing"

func TestDiskSetPosition(t *testing.T) {
	for _, tc := range []struct {
		name         string
		key          string
		index, count int
		ok           bool
	}{
		{"Copper Canyon (Disk 1 of 2).d64", "copper canyon.d64", 1, 2, true},
		{"Copper Canyon (disk 2 of 2).D64", "copper canyon.d64", 2, 2, true},
		{"Dungeon Master (Disk 2).st", "dungeon master.st", 2, 0, true},
		{"Elite (Tape 1 of 2).tzx", "elite.tzx", 1, 2, true},
		{"Game (1990)(Publisher)(Disk 1 of 3).d64", "game (1990)(publisher).d64", 1, 3, true},
		{"Game.d64", "", 0, 0, false},
		{"Game (Disk 3 of 2).d64", "", 0, 0, false},
		{"Game (Disk 0 of 2).d64", "", 0, 0, false},
		{"Game (Diskette).d64", "", 0, 0, false},
	} {
		key, index, count, ok := DiskSetPosition(tc.name)
		if key != tc.key || index != tc.index || count != tc.count || ok != tc.ok {
			t.Errorf("%q: got %q %d of %d ok=%v", tc.name, key, index, count, ok)
		}
	}
}
