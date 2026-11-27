package protocol

import (
	"net/http"
	"strings"
	"testing"
)

func TestLibraryDiskBindingAndStatus(t *testing.T) {
	b := LibraryMediaBinding{MediaUnitBinding: MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 7}, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	h := make(http.Header)
	b.SetHeaders(h)
	got, ok := LibraryMediaHeaders(h)
	if !ok || got != b {
		t.Fatal(got, ok)
	}
	for _, change := range []func(http.Header){func(h http.Header) { h.Add(MediaGameHeader, b.GameID) }, func(h http.Header) { h.Set(MediaGameHeader, "../disk") }, func(h http.Header) { h.Del(MediaBaseHeader) }, func(h http.Header) { h.Set(MediaUnitHeader, "3") }, func(h http.Header) { h.Set(MediaWriteProtectHeader, "1") }} {
		bad := h.Clone()
		change(bad)
		if _, ok := LibraryMediaHeaders(bad); ok {
			t.Fatal("bad library binding accepted")
		}
	}
	// pc386's B: takes a write-protected disk; the flag round-trips, and only
	// a size B: reads is admitted.
	pc := b
	pc.Unit, pc.WriteProtected = PcFloppyUnitB, true
	h = make(http.Header)
	pc.SetHeaders(h)
	if got, ok := LibraryMediaHeaders(h); !ok || got != pc || !pc.AdmitsSize(1228800) || pc.AdmitsSize(1474560) || pc.AdmitsSize(2949120) {
		t.Fatal(got, ok)
	}
	h.Set(MediaWriteProtectHeader, "yes")
	if _, ok := LibraryMediaHeaders(h); ok {
		t.Fatal("bad write-protect header accepted")
	}
	u := MediaUnitStatus{Interface: AtariStFloppyInterface(), MinBytes: uint32(AtariStFloppyBytes), MaxBytes: uint32(AtariStFloppyBytes), ChunkBytes: 512, State: MediaUnitReady, Persistence: &MediaDataStatus{Mode: "persistent", GameID: b.GameID, BaseMediaID: b.BaseMediaID, Revision: "absent"}}
	if !u.Valid() {
		t.Fatal(u)
	}
	u.State = MediaUnitEmpty
	if u.Valid() {
		t.Fatal("empty disk retained binding")
	}
	u.State = MediaUnitReady
	u.Persistence.Revision = "bad"
	if u.Valid() {
		t.Fatal("bad revision")
	}
}
