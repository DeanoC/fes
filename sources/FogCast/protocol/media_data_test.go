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
	for _, change := range []func(http.Header){func(h http.Header) { h.Add(MediaGameHeader, b.GameID) }, func(h http.Header) { h.Set(MediaGameHeader, "../disk") }, func(h http.Header) { h.Del(MediaBaseHeader) }, func(h http.Header) { h.Set(MediaUnitHeader, "1") }} {
		bad := h.Clone()
		change(bad)
		if _, ok := LibraryMediaHeaders(bad); ok {
			t.Fatal("bad library binding accepted")
		}
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
