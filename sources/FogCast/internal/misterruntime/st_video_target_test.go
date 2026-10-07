package misterruntime_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/misteross/expansion"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func stVideoTargetFixture(t *testing.T) ([]byte, []byte, []expansion.Asset) {
	t.Helper()
	old, rom, _ := apple2TargetFixture(t, true)
	reader := tar.NewReader(bytes.NewReader(old))
	var members [][2][]byte
	var base []byte
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "manifest.toml" {
			data = []byte(strings.ReplaceAll(string(data), "apple2", "atari-st") + "\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n")
		}
		if header.Name == "core.rbf" {
			base = data
		}
		members = append(members, [2][]byte{[]byte(header.Name), data})
	}
	pkg := canonicalTar(t, members...)
	staged, err := corepackage.Stage(context.Background(), t.TempDir(), int64(len(pkg)), bytes.NewReader(pkg))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	var parts []expansion.Asset
	for _, role := range []string{expansion.PartRoleExpansion, expansion.PartRoleVideo} {
		m := expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(base)), CartSize: int64(len(base)), Device: expansion.Device, Format: 1, Map: expansion.AtariStVideoMap, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: staged.Descriptor.Build.ID, ShellPackageID: staged.PackageID, ShellSHA256: staged.Descriptor.Payload.SHA256, Slot: expansion.VideoSlot, SlotMajor: 1}
		if role == expansion.PartRoleExpansion {
			m.Slot, m.Map, m.SlotIndex = expansion.AtariStSlot, expansion.AtariStMap, 1
		}
		part, err := expansion.NewAsset(m, base)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, part)
	}
	return pkg, rom, parts
}

type stVideoTargetControl struct {
	*romTargetControl
	partsCalls int
}

func (c *stVideoTargetControl) InspectPartsCore(ctx context.Context, path, id, payload string, parts []misterruntime.PartPath, receipt expansion.PartsComposition) (misterruntime.Protocol2Response, error) {
	return c.packageControl.InspectCore(ctx, path, id)
}
func (c *stVideoTargetControl) LoadPartsCore(context.Context, string, string, string, []misterruntime.PartPath, expansion.PartsComposition) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("ST firmware bypassed ROM dispatch")
	return misterruntime.Protocol2Response{}, nil
}
func (c *stVideoTargetControl) LoadROMPartsComposedCore(ctx context.Context, path, id string, parts []misterruntime.PartPath, payload string, receipt expansion.PartsComposition, programmed string, link corepackage.ROMLinkIdentity) (misterruntime.Protocol2Response, error) {
	c.partsCalls++
	if len(parts) != 2 || parts[0].Role != "expansion" || parts[1].Role != "video" || receipt.Layout != expansion.AtariStVideoLayout {
		c.t.Fatal("wrong closed ST parts roles")
	}
	for _, p := range parts {
		entries, err := os.ReadDir(p.Path)
		if err != nil || len(entries) != 2 {
			c.t.Fatal("parts source not retained", err)
		}
	}
	before, err := os.ReadFile(payload)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(before)) != receipt.PayloadSHA256 {
		c.t.Fatal("parts overlay identity lost", err)
	}
	after, err := os.ReadFile(programmed)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(after)) != link.ProgrammedSHA256 || bytes.Equal(before, after) {
		c.t.Fatal("ROM programmed identity lost", err)
	}
	response, err := c.packageControl.LoadCore(ctx, path, id)
	if err == nil {
		response.Capabilities.ROMLinking = 1
		response.ActivePackage.ROMLink = &link
		response.ActivePackage.PartsComposition = &receipt
		c.status2 = &response
	}
	return response, err
}
func TestSTVideoTargetRelinksROMPartsAndUsesExistingLifecycle(t *testing.T) {
	pkg, rom, parts := stVideoTargetFixture(t)
	transport, err := corepackage.PrepareROMInput(context.Background(), corepackage.ROMInput{Package: pkg, ROM: rom, Parts: parts})
	if err != nil {
		t.Fatal(err)
	}
	control := &stVideoTargetControl{romTargetControl: newROMTargetControl(t, 1)}
	root := t.TempDir()
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
	active, attempted, failure := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data))
	if failure != nil || !attempted || control.partsCalls != 1 || control.linkedCalls != 0 || active.ROMLink == nil || !reflect.DeepEqual(active.PartsComposition, transport.PartsComposition) {
		t.Fatalf("activation=%+v attempted=%v failure=%v dispatch=%d/%d", active, attempted, failure, control.partsCalls, control.linkedCalls)
	}
	adopted, err := corepackage.Adopt(root)
	if err != nil || len(adopted) != 1 || !reflect.DeepEqual(adopted[0].PartsComposition, active.PartsComposition) || !reflect.DeepEqual(adopted[0].ROMLink, active.ROMLink) {
		t.Fatal("restart lost paired identities", err)
	}
	if _, failure := runtime.Stop(context.Background()); failure != nil {
		t.Fatal(failure)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("retirement retained ST ROM/parts sources", entries)
	}
}
