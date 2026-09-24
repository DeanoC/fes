package discovery

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/brutella/dnssd"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

func TestEncodeAndParseMeshProtocolAndCapabilityBag(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	text, err := EncodeTXT(id, Capabilities{
		Execute: []Execute{{
			Kind: ExecuteFPGANative,
			ABIs: []ABI{{ID: "fes.application", Major: 1}, {ID: "fes.simple-computer", Major: 1}},
		}},
		DisplaySink: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"protocol=1",
		"target_id=" + id,
		"node_id=" + id,
		"mesh=1.0",
		"cap=display_sink,execute:fpga_native:fes.application/1:fes.simple-computer/1",
	}
	if !reflect.DeepEqual(text, want) {
		t.Fatalf("TXT = %#v, want %#v", text, want)
	}
	joined := strings.ToLower(strings.Join(text, "\n"))
	for _, forbidden := range []string{"token", "secret", "lease", "title", "password", "bearer"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("TXT contains %q:\n%s", forbidden, joined)
		}
	}
	ad := ParseTXT(txtMap(text))
	if ad.NodeID != id || ad.TargetID != id || ad.NodeIDConflict {
		t.Fatalf("node id = %q target %q conflict %v", ad.NodeID, ad.TargetID, ad.NodeIDConflict)
	}
	if ad.Mesh == nil || ad.Mesh.String() != MeshProtocol || !ad.MeshSessionCompatible() || !ad.DirectBindable() {
		t.Fatalf("mesh = %#v compatible %v bindable %v", ad.Mesh, ad.MeshSessionCompatible(), ad.DirectBindable())
	}
	if !ad.Capabilities.DisplaySink || ad.PictureUp() || ad.SilenceReleasesLease() {
		t.Fatalf("display %#v picture %v lease %v", ad.Capabilities, ad.PictureUp(), ad.SilenceReleasesLease())
	}
	if len(ad.Capabilities.Execute) != 1 || ad.Capabilities.Execute[0].Kind != ExecuteFPGANative {
		t.Fatalf("execute = %#v", ad.Capabilities.Execute)
	}
	wantABIs := []ABI{{ID: "fes.application", Major: 1}, {ID: "fes.simple-computer", Major: 1}}
	if !reflect.DeepEqual(ad.Capabilities.Execute[0].ABIs, wantABIs) {
		t.Fatalf("abis = %#v", ad.Capabilities.Execute[0].ABIs)
	}
}

func TestKitAdvertisementOmitsUnknownFamiliesAndPrivateData(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	text, err := EncodeKitTXT(id)
	if err != nil {
		t.Fatal(err)
	}
	ad := ParseTXT(txtMap(text))
	if !ad.Capabilities.DisplaySink || !ad.Capabilities.InputSource {
		t.Fatalf("caps = %#v", ad.Capabilities)
	}
	if len(ad.Capabilities.Execute) != 1 || len(ad.Capabilities.Execute[0].ABIs) != 0 || ad.Capabilities.Execute[0].Kind != ExecuteFPGANative {
		t.Fatalf("execute = %#v", ad.Capabilities.Execute)
	}
	if strings.Contains(strings.Join(text, ","), "catalog") || strings.Contains(strings.Join(text, ","), "coordinator") {
		t.Fatalf("omitted capabilities were advertised: %#v", text)
	}
}

func TestPhase0OmissionStaysDirectlyBindable(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ad := ParseTXT(map[string]string{"protocol": "1", "target_id": id})
	if !ad.DirectBindable() || ad.Mesh != nil || ad.MeshSessionCompatible() || ad.NodeID != id || ad.NodeIDConflict {
		t.Fatalf("phase 0 = %#v", ad)
	}
	if ad.Capabilities.DisplaySink || len(ad.Capabilities.Execute) != 0 || ad.PictureUp() {
		t.Fatalf("phase 0 invented capabilities %#v", ad.Capabilities)
	}
}

func TestDisplaySinkIsNotPictureUp(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ad := ParseTXT(map[string]string{
		"protocol":  "1",
		"target_id": id,
		"node_id":   id,
		"mesh":      "1.0",
		"cap":       "display_sink,execute:fpga_native",
		"hdmi":      "up",
		"adv":       "ok",
	})
	if !ad.Capabilities.DisplaySink || ad.PictureUp() {
		t.Fatalf("display %v picture %v", ad.Capabilities.DisplaySink, ad.PictureUp())
	}
	if ad.MeshSessionCompatible() != true || ad.SilenceReleasesLease() {
		t.Fatalf("session %#v", ad)
	}
}

func TestForeignExecuteDoesNotFlipReady(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	text, err := EncodeTXT(id, Capabilities{Execute: []Execute{{Kind: ExecuteFPGANative, ABIs: []ABI{{ID: "fes.application", Major: 1}}}}})
	if err != nil {
		t.Fatal(err)
	}
	foreign := ParseTXT(txtMap(text))
	if !foreign.MeshSessionCompatible() || len(foreign.Capabilities.Execute) != 1 {
		t.Fatalf("foreign = %#v", foreign)
	}
	if ready, block := ReadyForBoundExecutor(false, foreign, nil); ready || block != meshcontent.BlockNone {
		t.Fatal("execute advertisement flipped Ready for an unbound title")
	}
	if ready, block := ReadyForBoundExecutor(true, foreign, nil); !ready || block != meshcontent.BlockNone {
		t.Fatal("bound composition lost Ready")
	}
	distant := meshcontent.NewCache()
	bios := meshcontent.SumSHA256([]byte("bios"))
	if err := distant.Hold(bios); err != nil {
		t.Fatal(err)
	}
	entry := meshcontent.Entry{
		TitleID: "coleco-frogger", System: "coleco", Launchable: true,
		Execute: []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}),
			meshcontent.BIOSSlot(bios),
		},
	}
	here := &ReadyHereInput{Entry: entry, Bound: meshcontent.Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Distant:  distant,
		Packages: []string{strings.Repeat("ab", 32)},
		ABIs:     []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}}
	if ready, block := ReadyForBoundExecutor(true, foreign, here); ready || block != meshcontent.BlockDistant {
		t.Fatalf("distant ready=%v block=%s", ready, block)
	}
	if !MeshMajorCompatible("1.0") || !MeshMajorCompatible("1.2") || MeshMajorCompatible("2.0") || MeshMajorCompatible("") {
		t.Fatal("mesh major compatibility")
	}
}

func TestMeshMajorMismatchStaysDirectlyBindable(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ad := ParseTXT(map[string]string{"protocol": "1", "target_id": id, "node_id": id, "mesh": "2.0", "cap": "execute:fpga_native"})
	if !ad.DirectBindable() || ad.MeshSessionCompatible() || ad.Mesh == nil || ad.Mesh.Major != 2 {
		t.Fatalf("major mismatch = %#v compatible %v", ad.Mesh, ad.MeshSessionCompatible())
	}
	broken := ParseTXT(map[string]string{"protocol": "1", "target_id": id, "mesh": "1"})
	if !broken.DirectBindable() || !broken.MeshUnusable || broken.MeshSessionCompatible() {
		t.Fatalf("malformed mesh = %#v", broken)
	}
}

func TestNodeIDConflictDoesNotMintASecondID(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	other := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	ad := ParseTXT(map[string]string{"protocol": "1", "target_id": id, "node_id": other, "mesh": "1.0"})
	if ad.NodeID != id || !ad.NodeIDConflict || !ad.DirectBindable() || ad.MeshSessionCompatible() {
		t.Fatalf("conflict node %q conflict %v bind %v mesh %v", ad.NodeID, ad.NodeIDConflict, ad.DirectBindable(), ad.MeshSessionCompatible())
	}
}

func TestParsedTTLDoesNotReleaseALease(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ad := ParseTXT(map[string]string{"protocol": "1", "target_id": id, "mesh": "1.0", "cap": "display_sink", "ttl": "30"})
	if ad.TTLSeconds == nil || *ad.TTLSeconds != 30 || ad.SilenceReleasesLease() || !ad.DirectBindable() {
		t.Fatalf("ttl %#v lease %v", ad.TTLSeconds, ad.SilenceReleasesLease())
	}
	bad := ParseTXT(map[string]string{"protocol": "1", "target_id": id, "ttl": "0"})
	if bad.TTLSeconds != nil || !bad.TTLUnusable || bad.SilenceReleasesLease() || !bad.DirectBindable() {
		t.Fatalf("bad ttl %#v", bad)
	}
}

func TestParseDropsCredentialsTitlesAndLeaseSecrets(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	secret := "super-secret-token-value"
	ad := ParseTXT(map[string]string{
		"protocol":  "1",
		"target_id": id,
		"token":     secret,
		"titles":    "Frogger,Pong",
		"lease":     "lease-secret-value",
		"cap":       "catalog,display_sink",
	})
	blob := strings.ToLower(ad.TargetID + ad.NodeID + ad.DiscoveryProtocol)
	if strings.Contains(blob, "secret") || strings.Contains(blob, "frogger") || strings.Contains(blob, "lease") {
		t.Fatalf("identity retained private data: %s", blob)
	}
	if !ad.Capabilities.DisplaySink || len(ad.Capabilities.Execute) != 0 {
		t.Fatalf("caps = %#v", ad.Capabilities)
	}
	encoded, err := EncodeKitTXT(id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(encoded, "\n"), secret) {
		t.Fatal("encoder wrote a credential")
	}
}

func TestBrokenExecuteBagStaysBindableAndUnusable(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	ad := ParseTXT(map[string]string{"protocol": "1", "target_id": id, "mesh": "1.0", "cap": "display_sink,execute:"})
	if !ad.DirectBindable() || !ad.CapabilitiesUnusable || ad.MeshSessionCompatible() || ad.Capabilities.DisplaySink {
		t.Fatalf("broken bag %#v", ad)
	}
}

func TestResolveBindsPhase0AndMeshAdvertisements(t *testing.T) {
	old := lookupType
	t.Cleanup(func() { lookupType = old })
	id := "01234567-89ab-cdef-0123-456789abcdef"
	kit, err := EncodeKitTXT(id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	lookupType = func(ctx context.Context, _ string, add dnssd.AddFunc, _ dnssd.RmvFunc) error {
		add(dnssd.BrowseEntry{Name: "phase0", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.10")}, Text: map[string]string{"target_id": id, "protocol": protocolVersion}})
		add(dnssd.BrowseEntry{Name: "mesh", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.11")}, Text: txtMap(kit)})
		add(dnssd.BrowseEntry{Name: "major", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.12")}, Text: map[string]string{"target_id": id, "protocol": protocolVersion, "mesh": "2.0", "cap": "execute:fpga_native"}})
		add(dnssd.BrowseEntry{Name: "secret", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.13")}, Text: map[string]string{"target_id": id, "protocol": protocolVersion, "token": "super-secret-token-value"}})
		add(dnssd.BrowseEntry{Name: "wrong-protocol", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.14")}, Text: map[string]string{"target_id": id, "protocol": "2"}})
		cancel()
		return ctx.Err()
	}
	got, err := Resolve(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"http://192.0.2.10:8182",
		"http://192.0.2.11:8182",
		"http://192.0.2.12:8182",
		"http://192.0.2.13:8182",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve = %#v, want %#v", got, want)
	}
}

func TestCollectInventoriesAdsAndTreatsSilenceAsAbsence(t *testing.T) {
	old := lookupType
	t.Cleanup(func() { lookupType = old })
	phase0 := "01234567-89ab-cdef-0123-456789abcdef"
	meshID := "fedcba98-7654-3210-fedc-ba9876543210"
	kit, err := EncodeKitTXT(meshID)
	if err != nil {
		t.Fatal(err)
	}
	kitText := txtMap(kit)
	kitText["ttl"] = "30"
	secret := "super-secret-token-value"
	ctx, cancel := context.WithCancel(context.Background())
	lookupType = func(ctx context.Context, _ string, add dnssd.AddFunc, _ dnssd.RmvFunc) error {
		add(dnssd.BrowseEntry{Name: "phase0", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.10")}, Text: map[string]string{"target_id": phase0, "protocol": protocolVersion, "token": secret, "lease": "lease-secret"}})
		add(dnssd.BrowseEntry{Name: "mesh", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.11")}, Text: kitText})
		add(dnssd.BrowseEntry{Name: "wrong-protocol", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.14")}, Text: map[string]string{"target_id": phase0, "protocol": "2"}})
		cancel()
		return ctx.Err()
	}
	got, err := Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("inventory = %#v", got)
	}
	if got[0].NodeID != phase0 || got[0].Mesh != "" || got[0].Cap != "" || got[0].Address != "http://192.0.2.10:8182" {
		t.Fatalf("phase 0 row = %#v", got[0])
	}
	if got[1].NodeID != meshID || got[1].TargetID != meshID || got[1].Mesh != MeshProtocol {
		t.Fatalf("mesh row = %#v", got[1])
	}
	if got[1].Cap == "" || !strings.Contains(got[1].Cap, "execute:"+ExecuteFPGANative) || !got[1].Capabilities.DisplaySink || !got[1].Capabilities.InputSource {
		t.Fatalf("cap bag = %#v", got[1])
	}
	if got[1].TTLSeconds == nil || *got[1].TTLSeconds != 30 || got[0].TTLSeconds != nil {
		t.Fatalf("ttl phase0=%v mesh=%v", got[0].TTLSeconds, got[1].TTLSeconds)
	}
	for _, node := range got {
		if node.SilenceReleasesLease() {
			t.Fatal("inventory silence released a lease")
		}
		blob := strings.ToLower(node.NodeID + node.TargetID + node.Mesh + node.Cap + node.Address)
		if strings.Contains(blob, "secret") || strings.Contains(blob, "lease") {
			t.Fatalf("inventory kept a private field: %s", blob)
		}
	}

	silent, cancelSilent := context.WithCancel(context.Background())
	lookupType = func(ctx context.Context, _ string, _ dnssd.AddFunc, _ dnssd.RmvFunc) error {
		cancelSilent()
		return ctx.Err()
	}
	absent, err := Collect(silent)
	if err != nil {
		t.Fatal(err)
	}
	if len(absent) != 0 {
		t.Fatalf("silence left rows %#v", absent)
	}
}

func TestEncodeRejectsInvalidABI(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	if _, err := EncodeTXT(id, Capabilities{Execute: []Execute{{Kind: ExecuteFPGANative, ABIs: []ABI{{ID: "FES.Pong", Major: 1}}}}}); err == nil {
		t.Fatal("invalid abi was encoded")
	}
}

func txtMap(text []string) map[string]string {
	out := make(map[string]string, len(text))
	for _, item := range text {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}
