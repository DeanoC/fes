// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func TestStoredPreflightRejectsBeforeRetiringStorage(t *testing.T) {
	for _, mode := range []string{"nil-verifier", "nil-selection", "empty-selection", "control-map", "configuration-map", "package", "build", "whole-digest", "chunk-digest"} {
		t.Run(mode, func(t *testing.T) {
			v, cpu, lease, store, selected, control, configuration, _, _ := storedComputer(t)
			before, calls := lease.r, cpu.calls
			expected := selected.Inspection().PackageID
			candidate := selected
			var c hpsControlMap
			var m coldConfigurationMap
			if err := json.Unmarshal(control, &c); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(configuration, &m); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "nil-verifier":
				v = nil
			case "nil-selection":
				candidate = nil
			case "empty-selection":
				candidate = new(packagestore.Selection)
			case "control-map":
				c.CPUBase += 4
			case "configuration-map":
				m.Base += 64
			case "package":
				v.context.PackageID = strings.Repeat("a", 64)
				c.PackageID, m.PackageID = v.context.PackageID, v.context.PackageID
			case "build":
				v.context.BuildID = strings.Repeat("b", 32)
				c.BuildID, m.BuildID = v.context.BuildID, v.context.BuildID
			case "whole-digest":
				m.CoreSHA384 = strings.Repeat("c", 96)
			case "chunk-digest":
				m.ChunkSHA384[len(m.ChunkSHA384)-1] = strings.Repeat("d", 96)
			}
			control, configuration = canonicalConfiguration(t, c), canonicalConfiguration(t, m)
			plan, err := v.PreflightStoredConfiguration(control, configuration, candidate)
			if err == nil || plan != nil || cpu.calls != calls || lease.r != before || !lease.r.Active {
				t.Fatal("invalid selection affected storage or firmware", err, lease.r, cpu.calls)
			}
			// Rejection leaves the healthy store available for a corrected choice.
			if _, err := store.Retain(context.Background(), packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: expected}); err != nil {
				t.Fatal("preflight rejection retired the store", err)
			}
		})
	}
}

func TestStoredPreflightFreezesMapsBeforeContainmentAndNeverReopensFiles(t *testing.T) {
	v, cpu, lease, store, selected, control, configuration, _, data := storedComputer(t)
	before, calls := lease.r, cpu.calls
	plan, err := v.PreflightStoredConfiguration(control, configuration, selected)
	if err != nil || cpu.calls != calls || lease.r != before {
		t.Fatal("preflight performed I/O", err)
	}
	clear(control)
	clear(configuration)
	handoff, err := store.Contain(selected)
	if err != nil {
		t.Fatal(err)
	}
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	transfer, source, err := plan.NewTransfer(handoff, arena.Reserve)
	if err != nil || source.Bridges.Generation != 4 || lease.r.Active {
		t.Fatal(source, err)
	}
	cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: transfer.region}
	for step := 0; step < 30 && !transfer.Status().Done; step++ {
		if _, _, err = transfer.Advance(); err != nil {
			t.Fatal("caller mutation changed the frozen transfer", err)
		}
	}
	if !transfer.Status().Done || cpu.configurations != 1 {
		t.Fatal(transfer.Status(), cpu.configurations)
	}
	calls = cpu.calls
	if _, _, err := plan.NewTransfer(handoff, arena.Reserve); err == nil || cpu.calls != calls {
		t.Fatal("completed plan was reusable", err)
	}
}

func TestStoredPlanConsumesFailuresWithoutAnotherReservationOrFirmwareCall(t *testing.T) {
	for _, mode := range []string{"success", "missing-handoff", "different-selection", "missing-reservation", "reservation-error", "changed-source", "changed-generation", "changed-storage"} {
		t.Run(mode, func(t *testing.T) {
			v, cpu, lease, store, selected, control, configuration, _, _ := storedComputer(t)
			plan, err := v.PreflightStoredConfiguration(control, configuration, selected)
			if err != nil {
				t.Fatal(err)
			}
			copied := *plan
			if mode == "different-selection" {
				// Identical package bytes/ID, but not the selection that was checked.
				selected, err = store.Retain(context.Background(), packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: v.Identity().PackageID})
				if err != nil {
					t.Fatal(err)
				}
			}
			handoff, err := store.Contain(selected)
			if err != nil {
				t.Fatal(err)
			}
			reservations, releases := 0, 0
			var region *Region
			reserve := func(data []byte) (*Region, error) {
				reservations++
				region = &Region{Address: 0x94800000, Bytes: bytes.Clone(data), Release: func() error { releases++; return nil }}
				if mode == "reservation-error" {
					return region, errors.New("reservation outcome uncertain")
				}
				return region, nil
			}
			switch mode {
			case "missing-handoff":
				handoff = nil
			case "missing-reservation":
				reserve = nil
			case "changed-source":
				cpu.digest[0] ^= 1
			case "changed-generation":
				cpu.state.Generation++
			case "changed-storage":
				lease.r.Generation++
			}
			calls := cpu.calls
			transfer, _, err := plan.NewTransfer(handoff, reserve)
			if mode == "success" {
				if err != nil || transfer == nil || reservations != 1 {
					t.Fatal("valid plan rejected", err)
				}
			} else if err == nil || transfer != nil {
				t.Fatal("invalid plan accepted", err)
			}
			if mode == "missing-handoff" || mode == "different-selection" || mode == "missing-reservation" || mode == "changed-storage" {
				if cpu.calls != calls {
					t.Fatal("invalid handoff reached firmware")
				}
			}
			if mode == "success" || mode == "reservation-error" {
				if reservations != 1 || plan.state.region != region {
					t.Fatal("lost reservation, including on error")
				}
			} else if reservations != 0 {
				t.Fatal("invalid ownership reserved DMA")
			}
			calls, count := cpu.calls, reservations
			for _, rearm := range []bool{false, true} {
				if rearm {
					_, _, err = copied.NewReconfigurationTransfer(handoff, reserve)
				} else {
					_, _, err = copied.NewTransfer(handoff, reserve)
				}
				if err == nil || cpu.calls != calls || reservations != count || releases != 0 {
					t.Fatal("consumed plan retried or released uncertain memory", err)
				}
			}
		})
	}
}

func TestStoredPlanRejectsConcurrentConsumptionBeforeTheFirstReservationReturns(t *testing.T) {
	v, cpu, _, store, selected, control, configuration, _, _ := storedComputer(t)
	plan, err := v.PreflightStoredConfiguration(control, configuration, selected)
	if err != nil {
		t.Fatal(err)
	}
	copied := *plan
	handoff, err := store.Contain(selected)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	result := make(chan error, 1)
	go func() {
		_, _, err := plan.NewTransfer(handoff, func(data []byte) (*Region, error) {
			close(entered)
			<-release
			return nil, errors.New("retain the uncertain arena")
		})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first reservation never started")
	}
	calls := cpu.calls
	if _, _, err := copied.NewReconfigurationTransfer(handoff, func([]byte) (*Region, error) {
		t.Error("concurrent consumer reserved another arena")
		return nil, errors.New("unexpected reservation")
	}); err == nil || cpu.calls != calls {
		t.Fatal("concurrent plan consumption reached firmware", err)
	}
	unblock()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("uncertain reservation succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("consumed operation did not return")
	}
}
