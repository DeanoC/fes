package meshcontent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSumSHA256EmptyAndRoundTrip(t *testing.T) {
	id := SumSHA256(nil)
	const empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if id.String() != AlgorithmSHA256+":"+empty {
		t.Fatalf("empty hash %s", id)
	}
	parsed, err := ParseContentID(id.String())
	if err != nil || parsed != id {
		t.Fatalf("parse %v %v", parsed, err)
	}
	adapted, err := FromSHA256(empty)
	if err != nil || adapted != id {
		t.Fatalf("adapt %v %v", adapted, err)
	}
}

func TestContentIDRejectsOtherAlgorithmsPathsAndPackageIDs(t *testing.T) {
	pkg := strings.Repeat("ab", 32)
	cases := []string{
		"blake3:" + strings.Repeat("ab", 32),
		"sha256:" + strings.ToUpper(strings.Repeat("ab", 32)),
		"sha256:" + pkg + "ff",
		pkg,
		"/tmp/frogger.rom",
		"sha256:" + pkg + ":extra",
	}
	for _, text := range cases {
		if _, err := ParseContentID(text); err == nil {
			t.Fatalf("parsed %q", text)
		}
	}
	if _, err := FromSHA256("SHA256"); !errors.Is(err, ErrContentID) {
		t.Fatalf("digest error %v", err)
	}
	_, err := ParseContentID("blake3:" + strings.Repeat("ab", 32))
	if !errors.Is(err, ErrAlgorithm) {
		t.Fatalf("algorithm error %v", err)
	}
}

func TestCatalogEntryShapeSeparatesTitlePackageAndSlots(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("cart"))
	ram := SumSHA256([]byte("ram"))
	entry := colecoEntry(bios, cart, ram)
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	ids := entry.ContentIDs()
	if len(ids) != 3 || ids[0] != bios || ids[1] != cart || ids[2] != ram {
		t.Fatalf("content ids %+v", ids)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "/") || strings.Contains(string(raw), "path") {
		t.Fatalf("entry carried a path: %s", raw)
	}
	var decoded Entry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(decoded)
	if err != nil || string(body) != string(raw) {
		t.Fatalf("round trip\n%s\n%s", body, raw)
	}
}

func TestEntryRejectsRBFContentIDAndTitleCollision(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	entry := colecoEntry(bios, SumSHA256([]byte("cart")), SumSHA256([]byte("ram")))
	entry.Slots[0].Content = &bios
	if err := entry.Validate(); !errors.Is(err, ErrEntry) {
		t.Fatalf("package slot accepted a content-id: %v", err)
	}
	entry = colecoEntry(bios, SumSHA256([]byte("cart")), SumSHA256([]byte("ram")))
	entry.TitleID = bios.String()
	if err := entry.Validate(); !errors.Is(err, ErrEntry) {
		t.Fatalf("title matched a content-id: %v", err)
	}
	entry = colecoEntry(bios, SumSHA256([]byte("cart")), SumSHA256([]byte("ram")))
	entry.TitleID = "coleco/frogger"
	if err := entry.Validate(); !errors.Is(err, ErrEntry) {
		t.Fatalf("path title: %v", err)
	}
	entry = Entry{TitleID: "coleco-cart", System: "coleco", Launchable: false}
	if err := entry.Validate(); err != nil {
		t.Fatalf("browse-only: %v", err)
	}
}

func TestPongIsPackageOnly(t *testing.T) {
	pkg := PackageABI{PackageID: strings.Repeat("cd", 32), ABI: "fes.application", Major: 1}
	entry := Entry{
		TitleID:    "pong",
		System:     "pong",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteFPGANative}},
		Slots:      []Slot{PackageSlot(pkg)},
	}
	ready, block := ReadyHere(entry, Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Packages: []string{pkg.PackageID},
		ABIs:     []EligibleABI{{ID: pkg.ABI, Major: pkg.Major}},
	})
	if !ready || block != BlockNone {
		t.Fatalf("pong ready=%v block=%s", ready, block)
	}
	if ids := entry.ContentIDs(); len(ids) != 0 {
		t.Fatalf("pong content ids %+v", ids)
	}
}

func TestReadyHereClassifiesContentWithoutPulling(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("cart"))
	ram := SumSHA256([]byte("ram"))
	entry := colecoEntry(bios, cart, ram)
	local := NewCache()
	distant := NewCache()
	if err := local.Hold(cart); err != nil {
		t.Fatal(err)
	}
	if err := local.Hold(ram); err != nil {
		t.Fatal(err)
	}
	if err := distant.Hold(bios); err != nil {
		t.Fatal(err)
	}
	other := NewCache()
	if other.Holds(bios) || other.Holds(cart) {
		t.Fatal("cache leaked across executors")
	}
	bound := Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Local: local, Distant: distant,
		Packages: []string{entry.Slots[0].Package.PackageID},
		ABIs:     []EligibleABI{{ID: entry.Slots[0].Package.ABI, Major: entry.Slots[0].Package.Major}},
	}
	ready, block := ReadyHere(entry, bound)
	if ready || block != BlockDistant {
		t.Fatalf("distant ready=%v block=%s", ready, block)
	}
	bound.Distant = NewCache()
	ready, block = ReadyHere(entry, bound)
	if ready || block != BlockContentMissing {
		t.Fatalf("missing ready=%v block=%s", ready, block)
	}
	bound.Checking = []ContentID{bios}
	ready, block = ReadyHere(entry, bound)
	if ready || block != BlockEnsureProgress {
		t.Fatalf("checking ready=%v block=%s", ready, block)
	}
	if err := local.Hold(bios); err != nil {
		t.Fatal(err)
	}
	ready, block = ReadyHere(entry, bound)
	if !ready || block != BlockNone {
		t.Fatalf("local ready=%v block=%s", ready, block)
	}
	bound.LeaseFree = false
	ready, block = ReadyHere(entry, bound)
	if ready || block != BlockLeaseHeld {
		t.Fatalf("lease ready=%v block=%s", ready, block)
	}
	bound.LeaseFree = true
	bound.MeshMajorOK = false
	ready, block = ReadyHere(entry, bound)
	if ready || block != BlockVersionSkew {
		t.Fatalf("major ready=%v block=%s", ready, block)
	}
	bound.MeshMajorOK = true
	bound.Execute = false
	ready, block = ReadyHere(entry, bound)
	if ready || block != BlockNoExecutor {
		t.Fatalf("unbound ready=%v block=%s", ready, block)
	}
	if err := local.Hold(ContentID{Algorithm: "blake3", Digest: bios.Digest}); err == nil {
		t.Fatal("cache stored a non-strawman id")
	}
}

func TestNativeEmuLaunchableNeedsNoPackage(t *testing.T) {
	cart := SumSHA256([]byte("cart"))
	bios := SumSHA256([]byte("bios"))
	ram := SumSHA256([]byte("ram"))
	entry := Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteNativeEmu}},
		Slots: []Slot{
			BIOSSlot(bios),
			PrimaryMediaSlot(cart),
			ExpansionSlot("port", ram),
		},
	}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	local := NewCache()
	for _, id := range entry.ContentIDs() {
		if err := local.Hold(id); err != nil {
			t.Fatal(err)
		}
	}
	ready, block := ReadyHere(entry, Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Local: local,
	})
	if !ready || block != BlockNone {
		t.Fatalf("native ready=%v block=%s", ready, block)
	}

	primaryOnly := Entry{
		TitleID:    "sms-alex",
		System:     "sms",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteNativeEmu}},
		Slots:      []Slot{PrimaryMediaSlot(cart)},
	}
	if err := primaryOnly.Validate(); err != nil {
		t.Fatal(err)
	}

	withPackage := primaryOnly
	withPackage.Slots = []Slot{
		PackageSlot(PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}),
		PrimaryMediaSlot(cart),
	}
	if err := withPackage.Validate(); !errors.Is(err, ErrEntry) {
		t.Fatalf("native_emu accepted a package slot: %v", err)
	}
	missingPrimary := Entry{
		TitleID:    "nes-still",
		System:     "nes",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteNativeEmu}},
	}
	if err := missingPrimary.Validate(); !errors.Is(err, ErrEntry) {
		t.Fatalf("native_emu without primary media: %v", err)
	}

	fpga := colecoEntry(bios, cart, ram)
	fpga.Slots = fpga.Slots[1:]
	if err := fpga.Validate(); !errors.Is(err, ErrEntry) {
		t.Fatalf("fpga without package: %v", err)
	}
	ready, block = ReadyHere(fpga, Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Local: local,
	})
	if ready || block != BlockInvalid {
		t.Fatalf("fpga without package ready=%v block=%s", ready, block)
	}
}

func TestTitleIDUsesCatalogGameID(t *testing.T) {
	cases := []string{
		"Coleco-Frogger",
		"coleco:frogger",
		"coleco\nfrogger",
	}
	for _, title := range cases {
		entry := Entry{TitleID: title, System: "coleco", Launchable: false}
		if err := entry.Validate(); !errors.Is(err, ErrEntry) {
			t.Fatalf("title %q: %v", title, err)
		}
	}
}

func TestBrowseOnlyIsNotReady(t *testing.T) {
	entry := Entry{TitleID: "coleco-cart", System: "coleco", Launchable: false}
	ready, block := ReadyHere(entry, Bound{Execute: true, LeaseFree: true, MeshMajorOK: true})
	if ready || block != BlockBrowseOnly {
		t.Fatalf("ready=%v block=%s", ready, block)
	}
}

func colecoEntry(bios, cart, ram ContentID) Entry {
	return Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteFPGANative}},
		Slots: []Slot{
			PackageSlot(PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}),
			BIOSSlot(bios),
			PrimaryMediaSlot(cart),
			ExpansionSlot("port", ram),
		},
	}
}
