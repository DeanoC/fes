package misterruntime_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func initialTargetInput(t *testing.T, kind string) corepackage.ROMInput {
	t.Helper()
	pkg, rom, parts := stVideoTargetFixture(t)
	reader := tar.NewReader(bytes.NewReader(pkg))
	var members [][2][]byte
	for {
		h, err := reader.Next()
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
		if h.Name == "manifest.toml" {
			data = append(data, []byte("\n[[interfaces]]\nid = \"fes.media.atari-st-floppy\"\nmajor = 1\nminor = 0\nrequired = true\n\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n")...)
		}
		members = append(members, [2][]byte{[]byte(h.Name), data})
	}
	pkg = canonicalTar(t, members...)
	staged, err := corepackage.Stage(context.Background(), t.TempDir(), int64(len(pkg)), bytes.NewReader(pkg))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	for n, part := range parts {
		m := part.Manifest
		m.ShellPackageID = staged.PackageID
		parts[n], err = expansion.NewAsset(m, part.Cart)
		if err != nil {
			t.Fatal(err)
		}
	}
	disk := bytes.Repeat([]byte{0xa5}, corepackage.InitialMediaBytes)
	in := corepackage.ROMInput{Package: pkg, ROM: rom, InitialMedia: &corepackage.InitialMedia{GameID: "st-desktop", BaseMediaID: fmt.Sprintf("%x", sha256.Sum256(disk)), Bytes: disk}}
	if kind == "video" {
		in.Parts = parts
	}
	if kind == "slot" {
		in.SlotExpansions = parts[:1]
	}
	return in
}

type initialTargetControl struct {
	*stVideoTargetControl
	initialCalls   int
	initial        *misterruntime.InitialMediaRequest
	disk           []byte
	lost, mismatch bool
	kind           string
}

func (c *initialTargetControl) InspectCoreData(_ context.Context, path, id, root string) (misterruntime.Protocol2Response, error) {
	i, err := corepackageInspection(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	return misterruntime.Protocol2Response{OK: true, CoreData: &protocol.CoreData{PackageID: id, CoreID: i.Descriptor.Core.ID, Mode: "volatile", Revision: "absent", PaddleSpeed: protocol.PaddleNormal}}, nil
}
func (c *initialTargetControl) UpdateCoreSettings(context.Context, string, string, string, string, protocol.PaddleSpeed) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("settings mutation during initial disk launch")
	return misterruntime.Protocol2Response{}, nil
}
func (c *initialTargetControl) LoadLibraryCore(context.Context, string, string, string) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("non-atomic library load")
	return misterruntime.Protocol2Response{}, nil
}
func (c *initialTargetControl) LoadROMSlotComposedCore(context.Context, string, string, []misterruntime.SlotExpansionPath, string, expansion.SlotComposition, string, corepackage.ROMLinkIdentity) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("diskless slot dispatch")
	return misterruntime.Protocol2Response{}, nil
}
func (c *initialTargetControl) LoadROMLinkedCoreWithInitialMedia(ctx context.Context, path, id, root, expansionPath, payload string, composition *expansion.Composition, programmed string, link corepackage.ROMLinkIdentity, initial *misterruntime.InitialMediaRequest) (misterruntime.Protocol2Response, error) {
	if c.kind != "plain" || root != misterruntime.CoreDataRoot {
		c.t.Fatal("wrong plain dispatch")
	}
	return c.finishInitial(ctx, path, id, programmed, link, nil, nil, initial)
}
func (c *initialTargetControl) LoadROMSlotComposedCoreWithInitialMedia(ctx context.Context, path, id string, paths []misterruntime.SlotExpansionPath, payload string, composition expansion.SlotComposition, programmed string, link corepackage.ROMLinkIdentity, initial *misterruntime.InitialMediaRequest) (misterruntime.Protocol2Response, error) {
	if c.kind != "slot" || len(paths) != 1 {
		c.t.Fatal("wrong slot dispatch")
	}
	return c.finishInitial(ctx, path, id, programmed, link, &composition, nil, initial)
}
func (c *initialTargetControl) LoadROMPartsComposedCoreWithInitialMedia(ctx context.Context, path, id string, paths []misterruntime.PartPath, payload string, composition expansion.PartsComposition, programmed string, link corepackage.ROMLinkIdentity, initial *misterruntime.InitialMediaRequest) (misterruntime.Protocol2Response, error) {
	if c.kind != "video" || len(paths) != 2 {
		c.t.Fatal("wrong parts dispatch")
	}
	return c.finishInitial(ctx, path, id, programmed, link, nil, &composition, initial)
}
func (c *initialTargetControl) finishInitial(ctx context.Context, path, id, programmed string, link corepackage.ROMLinkIdentity, slots *expansion.SlotComposition, parts *expansion.PartsComposition, initial *misterruntime.InitialMediaRequest) (misterruntime.Protocol2Response, error) {
	c.initialCalls++
	if initial == nil || initial.DataRoot != misterruntime.MediaDataRoot || initial.Size != 737280 || initial.Unit != 0 {
		c.t.Fatal("missing fixed-root initial request")
	}
	c.initial = initial
	var err error
	c.disk, err = os.ReadFile(initial.Path)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(c.disk)) != initial.BaseMediaID {
		c.t.Fatal("unsealed/mismatched initial source", err)
	}
	data, err := os.ReadFile(programmed)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != link.ProgrammedSHA256 {
		c.t.Fatal("ROM link missing", err)
	}
	r, err := c.packageControl.LoadCore(ctx, path, id)
	if err != nil {
		return r, err
	}
	a := r.ActivePackage
	a.PersistenceMode = "persistent"
	a.ROMLink = &link
	a.SlotComposition = slots
	a.PartsComposition = parts
	r.Capabilities.ROMLinking = 1
	a.Observed.ABI = &misterruntime.Protocol2Contract{ID: "fes.computer", Major: 1}
	r.Capabilities.ActiveInterfaces = nil
	for _, iface := range a.Descriptor.Interfaces {
		if iface.Required {
			r.Capabilities.ActiveInterfaces = append(r.Capabilities.ActiveInterfaces, misterruntime.Protocol2Interface{ID: iface.ID, Major: uint16(iface.Major), Minor: uint16(iface.Minor)})
		}
	}
	sort.Slice(r.Capabilities.ActiveInterfaces, func(i, j int) bool {
		return r.Capabilities.ActiveInterfaces[i].ID < r.Capabilities.ActiveInterfaces[j].ID
	})
	r.Capabilities.ProgrammingProfiles = []string{a.Descriptor.Target.ProgrammingProfile}
	r.Capabilities.ABIs = []misterruntime.Protocol2ABI{{ID: "fes.computer", Major: 1, Interfaces: r.Capabilities.ActiveInterfaces}}
	r.Capabilities.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: protocol.ComputerMediaChunkBytes, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: initial.GameID, BaseMediaID: initial.BaseMediaID, Revision: "absent"}}}
	if c.mismatch {
		r.Capabilities.MediaUnits[0].Persistence.GameID = "other-game"
	}
	c.status2 = &r
	if c.lost {
		return misterruntime.Protocol2Response{}, io.EOF
	}
	return r, nil
}
func TestInitialSTMediaTargetAtomicDispatchAndLostReply(t *testing.T) {
	for _, kind := range []string{"plain", "slot", "video"} {
		t.Run(kind, func(t *testing.T) {
			in := initialTargetInput(t, kind)
			transport, err := corepackage.PrepareROMInput(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := corepackage.Stage(context.Background(), t.TempDir(), int64(len(in.Package)), bytes.NewReader(in.Package))
			if err != nil {
				t.Fatal(err)
			}
			defer inspection.Cleanup()
			for _, lost := range []bool{false, true} {
				for _, mismatch := range []bool{false, true} {
					t.Run(fmt.Sprintf("lost=%v/mismatch=%v", lost, mismatch), func(t *testing.T) {
						c := &initialTargetControl{stVideoTargetControl: &stVideoTargetControl{romTargetControl: newROMTargetControl(t, 1)}, kind: kind, lost: lost, mismatch: mismatch}
						root := t.TempDir()
						r := misterruntime.NewRuntime(c, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(root))
						defer r.Stop(context.Background())
						var active misterruntime.CoreActivation
						var attempted bool
						var failure *protocol.APIError
						if kind == "plain" {
							active, attempted, failure = r.LoadLibraryCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data), inspection.PackageID)
						} else {
							active, attempted, failure = r.LoadComposedCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data), inspection.PackageID)
						}
						if !attempted || c.initialCalls != 1 || c.partsCalls != 0 || c.linkedCalls != 0 || !bytes.Equal(c.disk, in.InitialMedia.Bytes) {
							t.Fatal("atomic dispatch replayed/bypassed initial media", attempted, c.initialCalls)
						}
						if mismatch {
							if failure == nil {
								t.Fatal("wrong disk confirmed")
							}
						} else if failure != nil || active.PersistenceMode != "persistent" {
							t.Fatal("requested initial disk not confirmed", failure, active)
						}
						if mismatch {
							if _, err := os.Stat(c.initial.Path); err != nil {
								t.Fatal("ambiguous load removed live initial source", err)
							}
							if _, err := os.Stat(c.activePath); err != nil {
								t.Fatal("ambiguous load removed live package", err)
							}
						}
						if !mismatch {
							for _, ejected := range []bool{false, true} {
								if ejected {
									c.status2.Capabilities.MediaUnits[0].State = protocol.MediaUnitEmpty
									c.status2.Capabilities.MediaUnits[0].Persistence = nil
									c.status2.ActivePackage.PersistenceMode = "volatile"
								} else {
									c.status2.Capabilities.MediaUnits[0].Persistence.GameID = "replacement-game"
								}
								restarted := misterruntime.NewRuntime(c, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(root))
								status := restarted.Reconcile(context.Background())
								if status.State != protocol.StateActive || status.CorePackage == nil || c.initialCalls != 1 {
									t.Fatal("live mutation broke adoption or replayed initial disk", ejected, status)
								}
							}
						}
						if _, failure := r.Stop(context.Background()); failure != nil {
							t.Fatal("normal retirement failed", failure)
						}
						if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
							t.Fatal("normal Stop retained initial publications", entries, err)
						}

					})
				}
			}
			c := &initialTargetControl{stVideoTargetControl: &stVideoTargetControl{romTargetControl: newROMTargetControl(t, 1)}, kind: kind}
			barrier := &replacementBarrier{}
			root := t.TempDir()
			r := misterruntime.NewRuntime(c, "", 0, 0, misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))
			_, attempted, failure := r.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data))
			if failure == nil || failure.Code != protocol.CodeInvalidArchive || attempted || c.initialCalls != 0 || barrier.begins != 0 {
				t.Fatal("initial disk development load passed admission", failure, attempted)
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("rejected initial development leaked files", entries)
			}
		})
	}
}

func TestInitialSTMediaGenerationFenceBeforeConfirmation(t *testing.T) {
	in := initialTargetInput(t, "plain")
	transport, err := corepackage.PrepareROMInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := corepackage.Stage(context.Background(), t.TempDir(), int64(len(in.Package)), bytes.NewReader(in.Package))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost=%v", lost), func(t *testing.T) {
			c := &initialTargetControl{stVideoTargetControl: &stVideoTargetControl{romTargetControl: newROMTargetControl(t, 1)}, kind: "plain", lost: lost}
			// The wire can return an otherwise matching stale/decreasing generation.
			prior := uint64(7)
			c.status2.Generation = &prior
			c.generation = 5
			root := t.TempDir()
			r := misterruntime.NewRuntime(c, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(root))
			defer r.Stop(context.Background())
			_, attempted, failure := r.LoadLibraryCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data), staged.PackageID)
			if failure == nil || !attempted || c.initialCalls != 1 {
				t.Fatal("stale generation confirmed or replayed", failure, attempted, c.initialCalls)
			}
			if _, err := os.Stat(c.initial.Path); err != nil {
				t.Fatal("unresolved physical generation removed source", err)
			}
		})
	}
}
