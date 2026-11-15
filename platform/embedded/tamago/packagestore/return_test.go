// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

type returnLease struct {
	*testLease
	returns int
}

func (l *returnLease) ResumeAfterReturn(owner, generation uint64) error {
	l.mu.Lock()
	l.returns++
	if l.r.Active || owner != l.r.Owner || generation <= l.r.Generation {
		l.mu.Unlock()
		return errors.New("fixture lacks expected contained return")
	}
	l.r.Generation = generation
	l.mu.Unlock()
	return l.Resume()
}

func TestReturnedCoreAllowsFreshPackageReadAndInvalidatesOldHandoffs(t *testing.T) {
	body, request, _ := basic(t)
	files := fstest.MapFS{request.Path: &fstest.MapFile{Data: body}}
	l := &returnLease{testLease: lease()}
	f := &trackedFS{base: files, lease: l.testLease}
	s, err := New(f, l)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Contain(old)
	if err != nil {
		t.Fatal(err)
	}
	if before.ReturnedFrom() != (sdcontrol.Receipt{}) {
		t.Fatal("cold handoff invented a core return")
	}
	reads := f.reads.Load()
	if err := s.ResumeAfterReturn(17, 6); err != nil {
		t.Fatal(err)
	}
	if f.reads.Load() != reads || l.returns != 1 {
		t.Fatal("resume accessed the filesystem or replayed the return")
	}
	if _, err := before.Selection(); !errors.Is(err, ErrState) {
		t.Fatal("old handoff survived", err)
	}
	if _, err := s.Contain(old); !errors.Is(err, ErrState) {
		t.Fatal("old selection crossed the lease", err)
	}
	// Retain a different canonical package after the real ownership transition.
	manifest := read(t, "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	manifest = []byte(strings.Replace(string(manifest), "FES Pong", "FES Returned Pong", 1))
	if bytes.Equal(manifest, read(t, "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")) {
		t.Fatal("fixture mutation did not change the package")
	}
	payload := read(t, "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf")
	previousHash := hash(payload)
	payload[0] ^= 1
	manifest = []byte(strings.Replace(string(manifest), previousHash, hash(payload), 1))
	other := canonical(t, manifest, payload, nil)
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(other)), bytes.NewReader(other))
	if err != nil {
		t.Fatal(err)
	}
	files[request.Path] = &fstest.MapFile{Data: other}
	request.PackageID = archive.Inspection().PackageID
	if request.PackageID == old.Inspection().PackageID {
		t.Fatal("second package identity unchanged")
	}
	selected, err := s.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Contain(selected)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReturnedFrom() != (sdcontrol.Receipt{Owner: 17, Generation: 6}) || after.Receipt() != (sdcontrol.Receipt{Owner: 17, Generation: 9}) {
		t.Fatal(after.ReturnedFrom(), after.Receipt())
	}
	if _, err := after.Selection(); err != nil {
		t.Fatal(err)
	}
	if _, err := before.Selection(); !errors.Is(err, ErrState) {
		t.Fatal("older handoff became valid again", err)
	}
	if f.handles.Load() != 0 {
		t.Fatal("open source crossed storage containment")
	}
}

func TestMissingReturnCapabilityOrBadReceiptPreservesOrRetiresExactlyOnce(t *testing.T) {
	for _, mode := range []string{"unsupported", "owner", "stale", "query-error", "bad-owner", "bad-generation", "bad-state"} {
		t.Run(mode, func(t *testing.T) {
			body, request, _ := basic(t)
			base := lease()
			provider := &returnLease{testLease: base}
			var capability Lease = provider
			if mode == "unsupported" {
				capability = base
			}
			f := &trackedFS{base: fstest.MapFS{request.Path: &fstest.MapFile{Data: body}}, lease: base}
			s, err := New(f, capability)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := s.Retain(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			h, err := s.Contain(selected)
			if err != nil {
				t.Fatal(err)
			}
			owner, generation := uint64(17), uint64(6)
			switch mode {
			case "owner":
				owner++
			case "stale":
				generation = 4
			case "query-error":
				base.resumeErr = errors.New("unknown firmware grant")
			case "bad-owner":
				base.resumeChange = func(r *sdcontrol.Receipt) { r.Owner++ }
			case "bad-generation":
				base.resumeChange = func(r *sdcontrol.Receipt) { r.Generation++ }
			case "bad-state":
				base.resumeChange = func(r *sdcontrol.Receipt) { r.Prepared = false }
			}
			reads := f.reads.Load()
			err = s.ResumeAfterReturn(owner, generation)
			if mode == "unsupported" || mode == "owner" || mode == "stale" {
				if !errors.Is(err, ErrState) || provider.returns != 0 {
					t.Fatal(err, provider.returns)
				}
				if _, err := h.Selection(); err != nil {
					t.Fatal("local rejection lost retained content", err)
				}
			} else {
				if !errors.Is(err, ErrRetired) || provider.returns != 1 {
					t.Fatal(err, provider.returns)
				}
				if !errors.Is(s.ResumeAfterReturn(17, 6), ErrRetired) || provider.returns != 1 {
					t.Fatal("uncertain resume replayed")
				}
				if _, err := h.Selection(); !errors.Is(err, ErrRetired) {
					t.Fatal("retired handoff remained usable", err)
				}
			}
			if f.reads.Load() != reads {
				t.Fatal("failure accessed contained storage")
			}
		})
	}
}
