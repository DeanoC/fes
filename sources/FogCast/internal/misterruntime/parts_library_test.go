package misterruntime_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func libraryPartsFixture(t *testing.T) ([]byte, expansion.PartsComposition) {
	t.Helper()
	packed, err := os.ReadFile("../../corepackage/testdata/expansion-shell.rbf.gz")
	if err != nil {
		t.Fatal(err)
	}
	r, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	text := strings.NewReplacer("fes.simple-game", "fes.application", "fes.pong", "fes.coleco", "size = 12", fmt.Sprintf("size = %d", len(payload)), "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", sha).Replace(string(manifest))
	text += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n"
	pkg := canonicalTar(t, [2][]byte{[]byte("manifest.toml"), []byte(text)}, [2][]byte{[]byte("core.rbf"), payload})
	path := t.TempDir() + "/shell.fcore"
	if err := os.WriteFile(path, pkg, 0600); err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		t.Fatal(err)
	}
	var assets []expansion.Asset
	for _, video := range []bool{false, true} {
		slot, mapping, major := expansion.ColecoSlot, expansion.ColecoMapV2, 2
		if video {
			slot, mapping, major = expansion.VideoSlot, expansion.ColecoVideoMap, 1
		}
		asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: sha, CartSize: int64(len(payload)), Device: expansion.Device, Format: 1, Map: mapping, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: inspection.Descriptor.Build.ID, ShellPackageID: inspection.PackageID, ShellSHA256: sha, Slot: slot, SlotMajor: major}, payload)
		if err != nil {
			t.Fatal(err)
		}
		assets = append(assets, asset)
	}
	bundle, err := corepackage.ComposePartsArchive(context.Background(), pkg, assets)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := bundle.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return transport, bundle.Composition
}

type libraryPartsControl struct {
	packageControl
	t                                     *testing.T
	dataCalls, partsCalls, developerCalls int
	root                                  string
	dataError                             *misterruntime.Protocol2Error
	dropReply, wrongTuple, wrongMode      bool
}

func (c *libraryPartsControl) InspectPartsCore(ctx context.Context, path, id, payload string, parts []misterruntime.PartPath, receipt expansion.PartsComposition) (misterruntime.Protocol2Response, error) {
	return c.InspectCore(ctx, path, id)
}
func (c *libraryPartsControl) LoadPartsCore(context.Context, string, string, string, []misterruntime.PartPath, expansion.PartsComposition) (misterruntime.Protocol2Response, error) {
	c.developerCalls++
	return misterruntime.Protocol2Response{}, fmt.Errorf("unexpected developer dispatch")
}
func (c *libraryPartsControl) InspectCoreData(ctx context.Context, path, id, root string) (misterruntime.Protocol2Response, error) {
	c.dataCalls++
	c.root = root
	if c.dataError != nil {
		return misterruntime.Protocol2Response{Error: c.dataError}, nil
	}
	inspection, err := corepackageInspection(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	data := protocol.CoreData{PackageID: id, CoreID: inspection.Descriptor.Core.ID, Mode: "volatile", Revision: "absent", PaddleSpeed: 1}
	return misterruntime.Protocol2Response{OK: true, CoreData: &data}, nil
}
func (*libraryPartsControl) UpdateCoreSettings(context.Context, string, string, string, string, protocol.PaddleSpeed) (misterruntime.Protocol2Response, error) {
	panic("unexpected settings")
}
func (c *libraryPartsControl) LoadLibraryPartsCore(ctx context.Context, path, id, root, payload string, parts []misterruntime.PartPath, receipt expansion.PartsComposition) (misterruntime.Protocol2Response, error) {
	c.partsCalls++
	if c.dataCalls != 1 || root != misterruntime.CoreDataRoot || c.root != root || len(parts) != 2 {
		c.t.Fatalf("parts load bypassed data admission: calls=%d root=%s parts=%v", c.dataCalls, root, parts)
	}
	for _, part := range parts {
		if _, err := os.Stat(part.Path + "/cart.rbf"); err != nil {
			c.t.Fatal(err)
		}
	}
	if _, err := os.Stat(payload); err != nil {
		c.t.Fatal(err)
	}
	response, err := c.packageControl.LoadCore(ctx, path, id)
	if err != nil {
		return response, err
	}
	response.Capabilities = packageStatus(corepackage.Staged{Descriptor: response.ActivePackage.Descriptor}, 1, false).Capabilities
	response.ActivePackage.PersistenceMode = "volatile"
	response.ActivePackage.Observed.ABI = &misterruntime.Protocol2Contract{ID: "fes.application", Major: 1}
	receipt.Parts = append([]expansion.PartSelection(nil), receipt.Parts...)
	if c.wrongTuple {
		receipt.Parts[1].PartID = strings.Repeat("e", 64)
		receipt.ID, _ = expansion.PartsCompositionID(receipt.PackageID, receipt.Layout, receipt.Parts, receipt.PayloadSHA256)
	}
	if c.wrongMode {
		response.ActivePackage.PersistenceMode = "persistent"
	}
	response.ActivePackage.PartsComposition = &receipt
	c.status2 = &response
	if c.dropReply {
		return misterruntime.Protocol2Response{}, io.EOF
	}
	return response, nil
}

func TestLibraryPartsAdmitDataBeforeMutationAndRetainExactSelection(t *testing.T) {
	transport, receipt := libraryPartsFixture(t)
	for _, name := range []string{"load", "lost reply", "incompatible namespace", "wrong tuple", "wrong mode", "wrong package"} {
		t.Run(name, func(t *testing.T) {
			control := &libraryPartsControl{t: t, dropReply: name == "lost reply", wrongTuple: name == "wrong tuple", wrongMode: name == "wrong mode"}
			if name == "incompatible namespace" {
				control.dataError = &misterruntime.Protocol2Error{Code: "incompatible_data", Message: "existing namespace requires persistence", Phase: "core_data"}
			}
			root := t.TempDir()
			t.Cleanup(func() {
				adopted, err := corepackage.Adopt(root)
				if err != nil {
					t.Error(err)
					return
				}
				for _, staged := range adopted {
					if err := staged.Cleanup(); err != nil {
						t.Error(err)
					}
				}
			})
			barrier := &replacementBarrier{}
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))
			ctx := context.Background()
			id := receipt.PackageID
			if name == "wrong package" {
				id = strings.Repeat("f", 64)
			}
			active, attempted, failure := runtime.LoadLibraryPartsCoreOwned(ctx, ctx, ctx, int64(len(transport)), bytes.NewReader(transport), id)
			rejected := name == "incompatible namespace" || name == "wrong package"
			if rejected {
				entries, _ := os.ReadDir(root)
				if failure == nil || failure.Phase != "admission" || attempted || control.partsCalls != 0 || barrier.begins != 0 || len(entries) != 0 {
					t.Fatalf("preflight failure=%v attempted=%v parts=%d barrier=%d entries=%v", failure, attempted, control.partsCalls, barrier.begins, entries)
				}
				return
			}
			if control.dataCalls != 1 || control.partsCalls != 1 || control.developerCalls != 0 || barrier.begins != 1 || !attempted {
				t.Fatalf("dispatch data=%d parts=%d dev=%d barrier=%d attempted=%v", control.dataCalls, control.partsCalls, control.developerCalls, barrier.begins, attempted)
			}
			if name == "wrong tuple" || name == "wrong mode" {
				if failure == nil || (name == "wrong mode" && failure.Phase != "recovery") {
					t.Fatalf("bad receipt failure=%v", failure)
				}
				return
			}
			if failure != nil || active.PersistenceMode != "volatile" || !reflect.DeepEqual(active.PartsComposition, &receipt) {
				t.Fatalf("activation=%#v failure=%v", active, failure)
			}
			restarted := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
			status := restarted.Reconcile(ctx)
			if status.CorePackage == nil || !reflect.DeepEqual(status.CorePackage.PartsComposition, &receipt) || status.CorePackage.PersistenceMode != "volatile" {
				t.Fatalf("restart status=%#v", status)
			}
		})
	}
}
