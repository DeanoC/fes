package main

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/discovery"
)

func TestAgentAdvertisementUsesTargetIDAsNodeID(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	text, err := discovery.EncodeKitTXT(id)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	for _, item := range text {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			t.Fatalf("TXT item %q", item)
		}
		fields[key] = value
	}
	if fields["target_id"] != id || fields["node_id"] != id || fields["node_id"] != fields["target_id"] {
		t.Fatalf("identity fields %#v", fields)
	}
	if fields["protocol"] != "1" || fields["mesh"] != discovery.MeshProtocol {
		t.Fatalf("versions %#v", fields)
	}
	if !strings.Contains(fields["cap"], "execute:"+discovery.ExecuteFPGANative) || !strings.Contains(fields["cap"], "display_sink") {
		t.Fatalf("cap = %q", fields["cap"])
	}
	if strings.Contains(fields["cap"], "fes.") {
		t.Fatalf("agent advertised ABI families it has not inventoried: %q", fields["cap"])
	}
	ad := discovery.ParseTXT(fields)
	if ad.PictureUp() || !ad.Capabilities.DisplaySink || ad.SilenceReleasesLease() {
		t.Fatalf("parsed %#v picture %v", ad.Capabilities, ad.PictureUp())
	}
	if !ad.DirectBindable() || !ad.MeshSessionCompatible() {
		t.Fatalf("bind %v mesh %v", ad.DirectBindable(), ad.MeshSessionCompatible())
	}
	for _, forbidden := range []string{"token", "secret", "lease", "title", "password"} {
		if strings.Contains(strings.ToLower(strings.Join(text, "\n")), forbidden) {
			t.Fatalf("advertisement contains %q", forbidden)
		}
	}
}
