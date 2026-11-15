// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMenuProgramObservationsFollowVerifiedLifecycle(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	if len(f.observations) != 1 || f.observations[0].Frames != 1 || f.observations[0].Menu != 1 {
		t.Fatal("initial displayed frame was not observed")
	}
	f.frame(t, 0x47)
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	if _, err := f.s.Launch(context.Background(), 1, request); err == nil {
		t.Fatal("stale request admitted")
	}
	if len(f.observations) != 2 {
		t.Fatal("stale request produced menu evidence")
	}
	session, err := f.s.Launch(context.Background(), 0, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.observations) != 3 || f.observations[2].Event != "return" {
		t.Fatal("launch lacks menu closure evidence")
	}
	f.frameWhileRunning(t)
	if len(f.observations) != 3 {
		t.Fatal("running UI manufactured frame evidence")
	}
	if _, err := f.s.Return(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ParkMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.observations) != 5 {
		t.Fatal("missing menu restoration or final closure", f.observations)
	}
	var metadata selectedMenuProgramRecord
	if err := json.Unmarshal(f.r.f.files.files[f.request.MetadataPath].Data, &metadata); err != nil {
		t.Fatal(err)
	}
	hash := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	wantHashes := [3]string{hash(programJSON(t, metadata.Memory)), hash(programJSON(t, metadata.Control)), hash(programJSON(t, metadata.Access))}
	for _, o := range f.observations {
		if o.Scope != "nano-program-menu-v1" || o.Menu < 1 || o.Menu > 2 || o.Identity.PackageID != f.request.PackageID ||
			o.SourceSHA256 != hex.EncodeToString(f.r.f.loader.boot.digest[:]) ||
			o.Opened.Generation != o.SourceGeneration+2 || o.Opened.ResetMask != 5 || o.Opened.Owner != f.r.f.lease.r.Owner ||
			([3]string{o.MemorySHA256, o.ControlSHA256, o.AccessSHA256}) != wantHashes ||
			o.Frame.Generation != 1 || uint64(o.Frame.Sequence) != o.Frames || o.Frame.Submitted != o.Frame.Sequence ||
			o.Frame.Slot != uint32((o.Frames-1)%2) {
			t.Fatal("unbound successful menu observation", o)
		}
		if o.Event == "frame" {
			if o.After != o.Opened {
				t.Fatal("frame manufactured closure")
			}
		} else if o.Event == "return" {
			want := o.Opened
			want.Generation++
			want.ResetMask = 15
			want.MastersContained = true
			if o.After != want {
				t.Fatal("return lacks exact independent closure")
			}
		} else {
			t.Fatal("unexpected event")
		}
	}
	// Optional cross-language fixture contains actual service observations and
	// independent selected metadata, not hand-written expected UART records.
	if dir := os.Getenv("FES_NANO_MENU_OBSERVATIONS"); dir != "" {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "observations.log"))
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range f.observations {
			raw, err := json.Marshal(o)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = fmt.Fprintf(file, "FES_NATIVE_MENU_STATE %s\n", raw); err != nil {
				t.Fatal(err)
			}
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
		contract := map[string]any{"identity": f.observations[0].Identity, "source_sha256": f.observations[0].SourceSHA256,
			"owner": f.observations[0].Opened.Owner, "memory_sha256": wantHashes[0], "control_sha256": wantHashes[1], "access_sha256": wantHashes[2]}
		raw, err := json.Marshal(contract)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "contract.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *programMenuFixture) frameWhileRunning(t *testing.T) {
	t.Helper()
	if err := f.s.PresentMenu(context.Background(), menuFrame(0x88)); err != nil {
		t.Fatal(err)
	}
}

func TestMenuProgramObservationFailureRetiresWithoutResumingStorage(t *testing.T) {
	t.Run("initial-frame", func(t *testing.T) { assertMenuRetired(t, newProgramMenuFixture(t, "observe-frame")) })
	for _, event := range []string{"frame", "return"} {
		t.Run(event, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			f.fault = "observe-" + event
			before := f.r.f.lease.returns
			if event == "frame" {
				if err := f.s.PresentMenu(context.Background(), menuFrame(1)); err == nil {
					t.Fatal("trace error ignored")
				}
			} else {
				request, _, _ := addROMProgram(t, f.r, 0, 8192)
				if _, err := f.s.Launch(context.Background(), 0, request); err == nil {
					t.Fatal("trace error ignored")
				}
			}
			if f.r.f.lease.returns != before {
				t.Fatal("failed trace resumed storage")
			}
			count := len(f.observations)
			assertMenuRetired(t, f)
			if len(f.observations) != count {
				t.Fatal("terminal trace replayed")
			}
		})
	}
}
