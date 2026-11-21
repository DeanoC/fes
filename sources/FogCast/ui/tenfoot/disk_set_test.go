package tenfoot

import (
	"reflect"
	"testing"
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
