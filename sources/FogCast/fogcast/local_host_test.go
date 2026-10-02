package fogcast

import (
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestShelfNoticeNamesEmptyMissingAndOfflineShelves(t *testing.T) {
	online := catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "sms-main", System: protocol.SystemSMS}}}
	offline := catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "sms-main", System: protocol.SystemSMS, Offline: true}}}
	dataStorm := catalog.Game{ID: "sms-data-storm", Title: "Data Storm 1.00", LibraryID: "sms-main", System: protocol.SystemSMS, State: catalog.SourceStateAvailable, RootOnline: true}
	saved := dataStorm
	saved.RootOnline = false
	builtin := catalog.Game{ID: "pong", Title: "Pong", LibraryID: catalog.BuiltinLibraryID, System: protocol.SystemPong, Kind: catalog.SourceKindBuiltin, State: catalog.SourceStateAvailable, RootOnline: true}

	if got := shelfNotice(online, []catalog.Game{dataStorm}); got != "" {
		t.Fatalf("ready shelf notice = %q", got)
	}
	if got := shelfNotice(online, nil); got != ShelfEmpty {
		t.Fatalf("empty shelf notice = %q", got)
	}
	if got := shelfNotice(online, []catalog.Game{builtin}); got != "" {
		t.Fatalf("builtin-only notice = %q", got)
	}
	if got := shelfNotice(offline, nil); got != ShelfMissing {
		t.Fatalf("missing content notice = %q", got)
	}
	if got := shelfNotice(offline, []catalog.Game{saved, builtin}); got != ShelfOffline {
		t.Fatalf("offline saved notice = %q", got)
	}
	if got := shelfNotice(online, []catalog.Game{saved}); got != ShelfOffline {
		t.Fatalf("saved rows offline notice = %q", got)
	}
	mixed := catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "sms-main", Offline: true}, {RootID: "snes-main"}}}
	if got := shelfNotice(mixed, []catalog.Game{dataStorm}); got != "" {
		t.Fatalf("partial offline with a ready title notice = %q", got)
	}
}
