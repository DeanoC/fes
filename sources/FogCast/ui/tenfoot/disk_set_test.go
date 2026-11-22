package tenfoot

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestDiskSetMembersFindsTheSetInItsFolder(t *testing.T) {
	file := func(name string) FirmwarePickerRow {
		return FirmwarePickerRow{Name: name, Path: "/m/" + name, Kind: firmwarePickerKindFile}
	}
	rows := []FirmwarePickerRow{
		{Name: "..", Path: "/", Kind: firmwarePickerKindParent},
		file("Copper Canyon (Disk 2 of 3).d64"),
		file("Copper Canyon (Disk 1 of 3).d64"),
		file("Copper Canyon (Disk 3 of 3).d64"),
		file("Copper Canyon (Disk 1 of 2).st"),
		file("Other Game (Disk 1 of 2).d64"),
		file("Copper Canyon.d64"),
	}
	got := diskSetMembers(rows, "/m/Copper Canyon (Disk 2 of 3).d64")
	var names []string
	for _, r := range got {
		names = append(names, r.Name)
	}
	if want := []string{"Copper Canyon (Disk 1 of 3).d64", "Copper Canyon (Disk 2 of 3).d64", "Copper Canyon (Disk 3 of 3).d64"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("set %v, want %v", names, want)
	}
	for name, path := range map[string]string{
		"untagged":          "/m/Copper Canyon.d64",
		"alone in folder":   "/m/Copper Canyon (Disk 1 of 2).st",
		"not in the folder": "/elsewhere/Copper Canyon (Disk 1 of 3).d64",
	} {
		if got := diskSetMembers(rows, path); got != nil {
			t.Errorf("%s: %v", name, got)
		}
	}
	twice := append(append([]FirmwarePickerRow(nil), rows...), file("copper canyon (disk 2 of 3).D64"))
	if got := diskSetMembers(twice, "/m/Copper Canyon (Disk 1 of 3).d64"); got != nil {
		t.Errorf("two files for disk 2 made a set: %v", got)
	}
}

func TestSetDiskRowsMarkTheDriveAndFocusTheNext(t *testing.T) {
	entry := hostclient.CoreEntry{MediaRole: "cassette", MediaID: "1", MediaName: "Game (Tape 1 of 3).tap",
		MoreMedia: []hostclient.SetMedia{{MediaID: "2", MediaName: "Game (Tape 2 of 3).tap"}, {MediaID: "3", MediaName: "Game (Tape 3 of 3).tap"}}}
	for _, tc := range []struct {
		inDrive string
		next    int
		marked  int
	}{
		{"", 1, 0},
		{"Game (Tape 2 of 3).tap", 2, 1},
		{"Game (Tape 3 of 3).tap", 0, 2},
		{"something else.tap", -1, -1},
	} {
		rows, next := setDiskRows(entry, tc.inDrive)
		if len(rows) != 3 || next != tc.next || rows[0].Name != "Tape 1 of 3 · Game (Tape 1 of 3).tap"+map[bool]string{true: " · In the drive", false: ""}[tc.marked == 0] {
			t.Fatalf("in drive %q: next=%d rows=%+v", tc.inDrive, next, rows)
		}
		for i, r := range rows {
			if strings.HasSuffix(r.Name, "In the drive") != (i == tc.marked) || r.SetMediaID != []string{"1", "2", "3"}[i] {
				t.Fatalf("in drive %q: row %d %+v", tc.inDrive, i, r)
			}
		}
	}
	if rows, _ := setDiskRows(hostclient.CoreEntry{MediaRole: "disk", MediaID: "1"}, ""); rows != nil {
		t.Fatalf("a single disk made rows: %+v", rows)
	}
	// A disk 1 with no file name cannot be put back by name.
	noName := entry
	noName.MediaName = ""
	if rows, next := setDiskRows(noName, ""); len(rows) != 2 || next != 0 || rows[0].SetMediaID != "2" {
		t.Fatalf("unnamed disk 1: next=%d rows=%+v", next, rows)
	}
}
