package main

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/ui/kitlauncher"
)

func TestLocalInputConfigUsesAgentSocket(t *testing.T) {
	path, probe := localInputConfig()
	if path != "/run/fogcast/local-input.sock" {
		t.Fatalf("socket %q", path)
	}
	if path != input.DefaultLocalInputSocket {
		t.Fatalf("socket %q is not the agent default", path)
	}
	if probe == nil {
		t.Fatal("missing core probe")
	}
}

func TestRuntimeCoreBoundMatchesAgentObservation(t *testing.T) {
	gen := uint64(4)
	bound := misterruntime.Protocol2Response{
		OK: true, State: "running_development",
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: "pkg"},
		Generation:    &gen,
	}
	if !runtimeCoreBound(bound) {
		t.Fatal("active package was not bound")
	}
	idle := bound
	idle.State = "idle"
	if runtimeCoreBound(idle) {
		t.Fatal("idle status was bound")
	}
	zero := uint64(0)
	bound.Generation = &zero
	if runtimeCoreBound(bound) {
		t.Fatal("generation 0 was bound")
	}
	bound.Generation = nil
	if runtimeCoreBound(bound) {
		t.Fatal("missing generation was bound")
	}
	bound.Generation = &gen
	bound.ActivePackage = nil
	if runtimeCoreBound(bound) {
		t.Fatal("missing package was bound")
	}
}

func TestRuntimeMenuIdleRequiresCompletedIdleMenu(t *testing.T) {
	gen := uint64(4)
	ready := &misterruntime.Protocol2MenuDisplay{Available: true, Generation: 3}
	cases := []struct {
		name        string
		status      misterruntime.Protocol2Response
		bound, menu bool
	}{
		{
			name: "running core",
			status: misterruntime.Protocol2Response{
				OK: true, State: "running_development",
				ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: "pkg"},
				Generation:    &gen,
				MenuDisplay:   &misterruntime.Protocol2MenuDisplay{Available: true, Session: true, Generation: 3},
			},
			bound: true,
		},
		{name: "starting", status: misterruntime.Protocol2Response{OK: true, State: "starting", MenuDisplay: ready}},
		{
			name: "reboot required",
			status: misterruntime.Protocol2Response{
				OK: true, State: "reboot_required",
				Error:       &misterruntime.Protocol2Error{Code: "idle_failed", Message: "load idle failed", Phase: "lifecycle"},
				MenuDisplay: ready,
			},
		},
		{name: "idle without menu", status: misterruntime.Protocol2Response{OK: true, State: "idle"}},
		{name: "idle menu unavailable", status: misterruntime.Protocol2Response{OK: true, State: "idle", MenuDisplay: &misterruntime.Protocol2MenuDisplay{Generation: 3}}},
		{name: "idle menu generation zero", status: misterruntime.Protocol2Response{OK: true, State: "idle", MenuDisplay: &misterruntime.Protocol2MenuDisplay{Available: true}}},
		{name: "idle session display", status: misterruntime.Protocol2Response{OK: true, State: "idle", MenuDisplay: &misterruntime.Protocol2MenuDisplay{Available: true, Session: true, Generation: 3}}},
		{name: "idle not ok", status: misterruntime.Protocol2Response{State: "idle", MenuDisplay: ready}},
		{name: "idle menu", status: misterruntime.Protocol2Response{OK: true, State: "idle", MenuDisplay: ready}, menu: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sample := runtimeCoreSample(tc.status)
			if sample.Bound != tc.bound || sample.MenuIdle != tc.menu {
				t.Fatalf("sample = %+v, want bound %v menu %v", sample, tc.bound, tc.menu)
			}
			var wantGen uint64
			if tc.menu && tc.status.MenuDisplay != nil {
				wantGen = tc.status.MenuDisplay.Generation
			}
			if sample.MenuGeneration != wantGen {
				t.Fatalf("menu generation = %d, want %d", sample.MenuGeneration, wantGen)
			}
		})
	}
}

func TestProbeRuntimeCoreReadsIdleStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	const idle = `{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}`
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte(idle + "\n"))
	}()
	sample, err := probeRuntimeCore(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if sample != (kitlauncher.LocalCoreSample{}) {
		t.Fatalf("bare idle runtime sample = %+v", sample)
	}
	_, err = probeRuntimeCore(context.Background(), filepath.Join(t.TempDir(), "missing.sock"))
	if err == nil {
		t.Fatal("missing runtime socket succeeded")
	}
}
