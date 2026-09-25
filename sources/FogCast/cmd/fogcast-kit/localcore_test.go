package main

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/misterruntime"
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
	bound, err := probeRuntimeCore(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if bound {
		t.Fatal("idle runtime reported a bound core")
	}
	_, err = probeRuntimeCore(context.Background(), filepath.Join(t.TempDir(), "missing.sock"))
	if err == nil {
		t.Fatal("missing runtime socket succeeded")
	}
}
