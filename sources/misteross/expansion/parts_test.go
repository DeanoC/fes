package expansion

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func partsShell(t *testing.T) PartsShell {
	t.Helper()
	base, _ := fixtures()
	return PartsShell{PackageID: strings.Repeat("a", 64), BuildID: strings.Repeat("b", 32),
		Payload: base, Layout: ColecoVideoLayout}
}

func fixturePart(t *testing.T, shell PartsShell, role string, points ...cramCoordinate) Asset {
	t.Helper()
	decoded, err := loadRBF(shell.Payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range points {
		setCramBit(decoded.cram, p.x, p.y, cramBit(decoded.cram, p.x, p.y)^1)
	}
	cart := saveRBF(decoded)
	m := Manifest{CartSHA256: hash(cart), CartSize: int64(len(cart)), Device: Device,
		Format: 1, Map: ColecoVideoMap, RecipeSHA256: strings.Repeat("c", 64), Revision: strings.Repeat("d", 40),
		ShellBuildID: shell.BuildID, ShellPackageID: shell.PackageID, ShellSHA256: hash(shell.Payload),
		Slot: VideoSlot, SlotMajor: 1}
	if role == PartRoleExpansion {
		m.Slot, m.Map, m.SlotMajor = ColecoSlot, ColecoMapV2, 2
	}
	asset, err := NewAsset(m, cart)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestComposePartsUsesOriginalBaseAndSortedIdentity(t *testing.T) {
	shell := partsShell(t)
	video := fixturePart(t, shell, PartRoleVideo, cramCoordinate{1769, 1800}, cramCoordinate{2805, 3441})
	card := fixturePart(t, shell, PartRoleExpansion, cramCoordinate{2000, 100}, cramCoordinate{2805, 1799})
	composition, linked, err := ComposePartsContext(context.Background(), shell, []Asset{video, card})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(linked)
	if err != nil {
		t.Fatal(err)
	}
	if cramBit(decoded.cram, 2000, 100) != 0 || cramBit(decoded.cram, 2805, 1799) != 1 ||
		cramBit(decoded.cram, 1769, 1800) != 1 || cramBit(decoded.cram, 2805, 3441) != 1 ||
		cramBit(decoded.cram, 100, 64) != 1 || cramBit(decoded.cram, 3500, 80) != 1 {
		t.Fatal("composition lost a selected part or preserved shell bit")
	}
	if composition.Parts[0] != (PartSelection{Role: PartRoleExpansion, PartID: card.ID}) ||
		composition.Parts[1] != (PartSelection{Role: PartRoleVideo, PartID: video.ID}) ||
		composition.PayloadSHA256 != hash(linked) || composition.PayloadSize != int64(len(linked)) {
		t.Fatalf("unexpected composition: %+v", composition)
	}
	material := "fes-parts-composition-v1\x00" + shell.PackageID + "\x00" + ColecoVideoLayout + "\x00" +
		"expansion:" + card.ID + "\x00video:" + video.ID + "\x00" + hash(linked)
	if composition.ID != hash([]byte(material)) {
		t.Fatal("wrong composition identity domain")
	}
	again, relinked, err := ComposePartsContext(context.Background(), shell, []Asset{card, video})
	if err != nil || again.ID != composition.ID || !bytes.Equal(linked, relinked) {
		t.Fatal("composition depends on input order")
	}
	var archive bytes.Buffer
	if err := video.Write(&archive); err != nil {
		t.Fatal(err)
	}
	read, err := ReadAsset(&archive)
	if err != nil || read.ID != video.ID {
		t.Fatal("video archive roundtrip failed")
	}
	old := Shell{PackageID: shell.PackageID, BuildID: shell.BuildID, Payload: shell.Payload,
		Slot: VideoSlot, SlotMajor: 1}
	if err := Admit(old, video); err == nil || !strings.Contains(err.Error(), "parts composition") {
		t.Fatal("old CPU-only API admitted video")
	}
	if _, _, err := ComposeContext(context.Background(), old, video); err == nil {
		t.Fatal("old CPU-only composition accepted video")
	}
	if colecoSocketV2.y1 > colecoVideoSocket.y0 {
		t.Fatal("CPU and video rectangles overlap")
	}
}

func TestPartsAdmissionAndContainmentRejects(t *testing.T) {
	shell := partsShell(t)
	video := fixturePart(t, shell, PartRoleVideo, cramCoordinate{2000, 2000})
	card := fixturePart(t, shell, PartRoleExpansion, cramCoordinate{2000, 100})
	for name, parts := range map[string][]Asset{
		"missing-video": {card},
		"empty":         {},
		"duplicate":     {video, video},
		"video-in-cpu":  {fixturePart(t, shell, PartRoleVideo, cramCoordinate{2000, 1799})},
		"cpu-in-video":  {video, fixturePart(t, shell, PartRoleExpansion, cramCoordinate{2000, 1800})},
		"above-video":   {fixturePart(t, shell, PartRoleVideo, cramCoordinate{2000, 3442})},
		"outside-x":     {fixturePart(t, shell, PartRoleVideo, cramCoordinate{2806, 2000})},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ComposePartsContext(context.Background(), shell, parts); err == nil {
				t.Fatal("accepted invalid parts")
			}
		})
	}
	for name, change := range map[string]func(*PartsShell){
		"package": func(s *PartsShell) { s.PackageID = strings.Repeat("e", 64) },
		"build":   func(s *PartsShell) { s.BuildID = strings.Repeat("e", 32) },
		"layout":  func(s *PartsShell) { s.Layout = "untrusted-layout" },
		"payload": func(s *PartsShell) { s.Payload = bytes.Clone(s.Payload); s.Payload[0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			modified := shell
			change(&modified)
			if err := AdmitParts(modified, []Asset{video}); err == nil {
				t.Fatal("accepted changed shell")
			}
		})
	}
	manifest := card.Manifest
	manifest.Map, manifest.SlotMajor = ColecoMap, 1
	v1, err := NewAsset(manifest, card.Cart)
	if err != nil {
		t.Fatal(err)
	}
	if err := AdmitParts(shell, []Asset{video, v1}); err == nil {
		t.Fatal("accepted CPU v1 in developer parts layout")
	}
	changed := video
	changed.Cart = bytes.Clone(video.Cart)
	changed.Cart[len(changed.Cart)-1] ^= 1
	if err := AdmitParts(shell, []Asset{changed}); err == nil {
		t.Fatal("accepted changed video bytes")
	}
	// A correctly hashed asset with a different legal RBF header still fails
	// the physical comparison, not just manifest admission.
	decoded, err := loadRBF(video.Cart)
	if err != nil {
		t.Fatal(err)
	}
	decoded.header[100] ^= 1
	newCart := saveRBF(decoded)
	manifest = video.Manifest
	manifest.CartSHA256, manifest.CartSize = hash(newCart), int64(len(newCart))
	headerAsset, err := NewAsset(manifest, newCart)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ComposePartsContext(context.Background(), shell, []Asset{headerAsset}); err == nil ||
		!strings.Contains(err.Error(), "header") {
		t.Fatalf("header mutation not rejected: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ComposePartsContext(ctx, shell, []Asset{video}); err != context.Canceled {
		t.Fatalf("cancel not preserved: %v", err)
	}
}

func TestComposePartsROMPreservesPartsAndRejectsBothSockets(t *testing.T) {
	base, mapping := romFixture()
	shell := partsShell(t)
	shell.Payload = base
	video := fixturePart(t, shell, PartRoleVideo, cramCoordinate{2000, 2000})
	card := fixturePart(t, shell, PartRoleExpansion, cramCoordinate{2000, 100})
	rom := bytes.Repeat([]byte{0xa5}, 1024)
	composition, overlay, programmed, err := ComposePartsROM(context.Background(), shell, []Asset{video, card}, mapping, rom)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(programmed)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(overlay, programmed) || cramBit(decoded.cram, 2000, 2000) != 1 ||
		cramBit(decoded.cram, 2000, 100) != 1 || composition.PayloadSHA256 != hash(programmed) {
		t.Fatal("ROM and selected parts were not both represented")
	}
	_, otherOverlay, otherProgrammed, err := ComposePartsROM(context.Background(), shell, []Asset{card, video}, mapping,
		bytes.Repeat([]byte{0x5a}, 1024))
	if err != nil || !bytes.Equal(overlay, otherOverlay) || bytes.Equal(programmed, otherProgrammed) {
		t.Fatal("ROM linking changed overlay or ignored ROM bytes")
	}
	for _, point := range []cramCoordinate{{2000, 100}, {2000, 1800}, {2000, 3441}} {
		var cloned ROMMap
		raw, _ := json.Marshal(mapping)
		if err := json.Unmarshal(raw, &cloned); err != nil {
			t.Fatal(err)
		}
		cloned.Blocks[0].WordBits[0][0] = uint32(point.y*cramWidth + point.x)
		if _, _, _, err := ComposePartsROM(context.Background(), shell, []Asset{video}, cloned, rom); err == nil ||
			!strings.Contains(err.Error(), "reserved parts socket") {
			t.Fatalf("accepted ROM destination in reserved socket %v: %v", point, err)
		}
	}
}

func TestPartsCompositionIdentityRejectsIncompleteSelections(t *testing.T) {
	digest := strings.Repeat("a", 64)
	video := PartSelection{Role: PartRoleVideo, PartID: digest}
	card := PartSelection{Role: PartRoleExpansion, PartID: strings.Repeat("b", 64)}
	for name, selections := range map[string][]PartSelection{
		"empty":           nil,
		"cpu-only":        {card},
		"duplicate-video": {video, video},
		"unknown-role":    {video, {Role: "audio", PartID: digest}},
		"bad-digest":      {{Role: PartRoleVideo, PartID: "bad"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := PartsCompositionID(digest, ColecoVideoLayout, selections, digest); err == nil {
				t.Fatal("accepted incomplete or ambiguous identity")
			}
		})
	}
	ordered, err := PartsCompositionID(digest, ColecoVideoLayout, []PartSelection{card, video}, digest)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := PartsCompositionID(digest, ColecoVideoLayout, []PartSelection{video, card}, digest)
	if err != nil || ordered != reversed {
		t.Fatal("public identity depends on argument order")
	}
	if _, err := PartsCompositionID(digest, "untrusted-layout", []PartSelection{video}, digest); err == nil {
		t.Fatal("accepted caller-defined layout")
	}
}
