package catalog_test

import (
	"reflect"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestParseDumpStripsRegionRevisionAndFlags(t *testing.T) {
	dump := catalog.ParseDump("Sonic the Hedgehog (USA) (Rev A) [!][Hack]")
	if dump.CanonicalTitle != "Sonic the Hedgehog" {
		t.Fatalf("canonical = %q", dump.CanonicalTitle)
	}
	if dump.Region != "usa" || dump.Revision != "a" {
		t.Fatalf("region/rev = %q %q", dump.Region, dump.Revision)
	}
	if !reflect.DeepEqual(dump.Flags, []string{"hack"}) {
		t.Fatalf("flags = %#v", dump.Flags)
	}
	if !dump.HasHack() || dump.HasPrerelease() {
		t.Fatalf("flag helpers = hack=%v prerelease=%v", dump.HasHack(), dump.HasPrerelease())
	}
}

func TestParseDumpHandlesCommaTagsAndAliases(t *testing.T) {
	dump := catalog.ParseDump("Streets of Rage 2 (Japan, Rev 2) (Beta)")
	if dump.CanonicalTitle != "Streets of Rage 2" || dump.Region != "japan" || dump.Revision != "2" {
		t.Fatalf("dump = %#v", dump)
	}
	if !reflect.DeepEqual(dump.Flags, []string{"beta"}) {
		t.Fatalf("flags = %#v", dump.Flags)
	}
	if catalog.ParseDump("Game (EU)").Region != "europe" {
		t.Fatal("EU alias")
	}
	if catalog.ParseDump("Game (Unl)").FlagString() != "unl" {
		t.Fatal("unl alias")
	}
	rev10 := catalog.ParseDump("Game (USA) (Rev 10)")
	if rev10.CanonicalTitle != "Game" || rev10.Revision != "10" {
		t.Fatalf("rev 10 = %#v", rev10)
	}
}

func TestParseDumpLeavesUndecoratedTitles(t *testing.T) {
	dump := catalog.ParseDump("Alpha")
	if dump.CanonicalTitle != "Alpha" || dump.Region != "" || dump.Revision != "" || len(dump.Flags) != 0 {
		t.Fatalf("dump = %#v", dump)
	}
}

func TestGroupKeyUsesFoldedCanonicalTitle(t *testing.T) {
	left := catalog.GroupKey(protocol.SystemSNES, "Sonic The Hedgehog")
	right := catalog.GroupKey(protocol.SystemSNES, "sonic the hedgehog")
	if left == "" || left != right {
		t.Fatalf("group keys = %q %q", left, right)
	}
	if catalog.GroupKey(protocol.SystemMegaDrive, "Sonic The Hedgehog") == left {
		t.Fatal("platform must be part of group key")
	}
}

func TestRegionRankFollowsPreferredOrder(t *testing.T) {
	if catalog.RegionRank("usa", nil) != 0 || catalog.RegionRank("world", nil) != 1 {
		t.Fatal("default ranks")
	}
	if catalog.RegionRank("japan", []string{"japan", "usa"}) != 0 {
		t.Fatal("custom preferred")
	}
	if catalog.RegionRank("", nil) != 200 || catalog.RegionRank("brazil", nil) != 100 {
		t.Fatal("unknown ranks")
	}
}

func TestPreferredDumpRanksRegionThenDumpPenalty(t *testing.T) {
	usa := catalog.Game{ID: "snes-sonic-usa", Region: "usa", DumpFlags: ""}
	beta := catalog.Game{ID: "snes-sonic-beta", Region: "usa", DumpFlags: "beta"}
	japan := catalog.Game{ID: "snes-sonic-japan", Region: "japan"}
	picked := catalog.PreferredDump([]catalog.Game{beta, japan, usa}, nil)
	if picked.ID != "snes-sonic-usa" || picked.VariantCount != 3 {
		t.Fatalf("preferred = %#v", picked)
	}
}
