package misterruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

const fixturePackageID = "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0"

func fixtureLines(t *testing.T, name string) []string {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), MaximumLineBytes)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestProtocol2DE25ProducerIdleStatus(t *testing.T) {
	// Captured from NativeHardware's DE25 MENU fixture with the production C++
	// serializer, using FES_TEST_DE25_PROTOCOL_STATUS_RECORD. This exercises
	// the agent's independent decoder rather than constructing Go capabilities.
	lines := fixtureLines(t, "protocol-v2-de25-idle.jsonl")
	if len(lines) != 1 {
		t.Fatalf("producer responses=%d, want one idle response", len(lines))
	}
	server := newSequenceSocketFixture(t, []string{lines[0] + "\n"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := NewClient(server.path).Protocol2Status(ctx)
	if err != nil || !validIdle(response) {
		t.Fatalf("DE25 producer idle was refused: response=%+v error=%v", response, err)
	}
	if response.MenuDisplay == nil || !response.MenuDisplay.Available || !response.MenuDisplay.FrameMapping || len(response.Capabilities.ABIs) != 2 {
		t.Fatalf("not the allocated DE25 MENU producer: %+v", response)
	}
	requests := server.wait(t)
	if len(requests) != 1 || strings.TrimSpace(requests[0]) != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("status requests=%q", requests)
	}
}

func TestProtocol2DE25InspectionRetainsPreviousMenuFault(t *testing.T) {
	// Native TRWU8J8G, producer edfae407e: generation 3's MENU underflow
	// was recovered by generation 4 before this successful inspect_core.
	lines := fixtureLines(t, "protocol-v2-de25-recovered-menu-inspection.jsonl")
	if len(lines) != 1 {
		t.Fatal("expected one native inspection reply")
	}
	fixture := newSequenceSocketFixture(t, []string{lines[0] + "\n"})
	response, err := NewClient(fixture.path).InspectCore(context.Background(), "/tmp/package",
		"3da0f1dfe2c71520a6953c544d9107131a2d6ec6187882bc633992dfa2751776")
	if err != nil || !response.OK || response.InspectedPackage == nil || !response.InspectedPackage.Compatible {
		t.Fatalf("native inspection result=%+v err=%v", response, err)
	}
	if response.Error == nil || response.Error.Code != "io_failed" || response.Error.Phase != "menu" ||
		response.State != "idle" || response.MenuDisplay == nil || !response.MenuDisplay.Available ||
		response.MenuDisplay.Generation != 4 || response.MenuDisplay.Underflows != 0 || response.MenuDisplay.Error != nil {
		t.Fatalf("lost retained fault or recovered MENU state: %+v", response)
	}
	fixture.wait(t)
}

func TestProtocol2DE25ProducerComputerStatus(t *testing.T) {
	// Actual production daemon response after native catalog loading. The CPU
	// had started; rejecting its declared profile stranded agent cleanup.
	lines := fixtureLines(t, "protocol-v2-de25-computer.jsonl")
	if len(lines) != 1 {
		t.Fatalf("producer responses=%d, want one active response", len(lines))
	}
	server := newSequenceSocketFixture(t, []string{lines[0] + "\n"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := NewClient(server.path).Protocol2Status(ctx)
	if err != nil || response.State != "running_development" || response.ActivePackage == nil {
		t.Fatalf("DE25 active producer was refused: response=%+v error=%v", response, err)
	}
	active := response.ActivePackage
	if active.Descriptor.Target.ProgrammingProfile != "de25-hps-first-v1" || active.Observed.BuildID == nil || *active.Observed.BuildID != active.Descriptor.Build.ID {
		t.Fatalf("not the selected DE25 computer: %+v", active)
	}
	status := computerMediaStatus(response)
	binding := protocol.MediaUnitBinding{PackageID: active.PackageID, Generation: *response.Generation, Unit: 0}
	if !binding.AcceptsSize(status, 16375) || binding.AcceptsSize(status, 16376) {
		t.Fatalf("native DE25 media limit was lost during agent status conversion: %+v", status)
	}
	server.wait(t)
}

func TestProtocol2DE25StandardComputerProfileAndTapeLimit(t *testing.T) {
	// Actual C++ production-profile serializer from the injected allocation and
	// atomic-register lifecycle fixture after standard Start, before opening UI.
	// This is a software fixture, not a physical standard-core boot.
	line := fixtureLines(t, "protocol-v2-de25-standard-computer.jsonl")[0]
	server := newSequenceSocketFixture(t, []string{line + "\n"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := NewClient(server.path).Protocol2Status(ctx)
	if err != nil || response.ActivePackage == nil {
		t.Fatalf("standard profile refused: %v", err)
	}
	active := response.ActivePackage
	status := computerMediaStatus(response)
	if response.MenuDisplay == nil || !response.MenuDisplay.Session || !response.MenuDisplay.FrameMapping || response.MenuDisplay.Available {
		t.Fatalf("allocated session mapping agreement lost: %+v", response.MenuDisplay)
	}
	binding := protocol.MediaUnitBinding{PackageID: active.PackageID, Generation: *response.Generation, Unit: 0}
	if !binding.AcceptsSize(status, 16384) || binding.AcceptsSize(status, 16385) {
		t.Fatal("standard tape limit differs")
	}
	server.wait(t)
	bad := strings.Replace(line, `"max_bytes":16384`, `"max_bytes":16375`, 1)
	if _, err := decodeProtocol2Response([]byte(bad)); !errors.Is(err, errInvalidRuntimeResponse) {
		t.Fatalf("BRAM limit accepted on standard profile: %v", err)
	}
}

func TestProtocol2DE25ActiveStatusRefusesChangedIdentity(t *testing.T) {
	line := fixtureLines(t, "protocol-v2-de25-computer.jsonl")[0]
	for name, raw := range map[string]string{
		"foreign profile":        strings.ReplaceAll(line, "de25-hps-first-v1", "unknown-profile"),
		"crossed board":          strings.ReplaceAll(line, "de25_nano", "de10_nano"),
		"other board tape limit": strings.Replace(line, `"max_bytes":16375`, `"max_bytes":16384`, 1),
		"unobserved identity":    strings.Replace(line, `"observed":{"abi":{"id":"fes.computer","major":1,"minor":0},"build_id":"a689d5a85cfe5b44c1c96501958f6516"}`, `"observed":{"abi":null,"build_id":null}`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if raw == line {
				t.Fatal("negative control did not change the producer response")
			}
			if _, err := decodeProtocol2Response([]byte(raw)); !errors.Is(err, errInvalidRuntimeResponse) {
				t.Fatalf("changed DE25 identity was accepted: %v", err)
			}
		})
	}
}

func TestProtocol2DecoderConsumesFrozenRuntimeFixturesExactly(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2.jsonl")
	for index := 1; index < len(lines); index += 2 {
		if _, err := decodeProtocol2Response([]byte(lines[index])); err != nil {
			t.Fatalf("protocol-v2 line %d: %v", index+1, err)
		}
	}
	edges := fixtureLines(t, "protocol-v2-edge-responses.jsonl")
	for index, line := range edges {
		if _, err := decodeProtocol2Response([]byte(line)); err != nil {
			t.Fatalf("edge line %d: %v", index+1, err)
		}
	}
	maximum, err := decodeProtocol2Response([]byte(edges[len(edges)-1]))
	if err != nil || maximum.Generation == nil || *maximum.Generation != ^uint64(0) {
		t.Fatalf("maximum generation lost precision: %#v, %v", maximum.Generation, err)
	}
	for index, line := range fixtureLines(t, "protocol-v1-responses.jsonl") {
		if _, err := decodeProtocol2Response([]byte(line)); err == nil {
			t.Fatalf("retired protocol-v1 line %d accepted: %v", index+1, err)
		}
	}
}

func TestProtocol2DecoderRejectsClosedShapeAndSemanticViolations(t *testing.T) {
	valid := fixtureLines(t, "protocol-v2.jsonl")[1]
	inspection := fixtureLines(t, "protocol-v2.jsonl")[3]
	cases := map[string]string{
		"unknown field":                  strings.Replace(valid, `"inspected_package":null`, `"inspected_package":null,"extra":0`, 1),
		"duplicate field":                strings.Replace(valid, `"version":"fixture"`, `"version":"fixture","version":"again"`, 1),
		"missing field":                  strings.Replace(valid, `,"inspected_package":null`, ``, 1),
		"generation zero":                strings.Replace(valid, `"generation":null`, `"generation":0`, 1),
		"unsorted profiles":              strings.Replace(valid, `"development-contained-v1","fes-gp-v1"`, `"fes-gp-v1","development-contained-v1"`, 1),
		"duplicate ABI":                  strings.Replace(valid, `],"active_interfaces":[]`, `,{"id":"fes.simple-game","major":1,"minor":0,"interfaces":[]}],"active_interfaces":[]`, 1),
		"unknown phase":                  strings.Replace(valid, `"error":null`, `"error":{"code":"io_failed","message":"failed","phase":"other"}`, 1),
		"null interface array":           strings.Replace(valid, `"active_interfaces":[]`, `"active_interfaces":null`, 1),
		"null ABI minor":                 strings.Replace(valid, `"minor":0,"interfaces"`, `"minor":null,"interfaces"`, 1),
		"null description":               strings.Replace(inspection, `"description":"Synthetic test-only core bundle fixture; never deploy."`, `"description":null`, 1),
		"null required":                  strings.Replace(inspection, `"required":true`, `"required":null`, 1),
		"explicit empty optional system": strings.Replace(valid, `"system":null`, `"system":""`, 1),
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProtocol2Response([]byte(line)); !errors.Is(err, errInvalidRuntimeResponse) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProtocol2ErrorAcceptsMediaDataPhase(t *testing.T) {
	if !validProtocol2Error(Protocol2Error{Code: "corrupt_data", Message: "bad ATA chunk", Phase: "media_data"}) {
		t.Fatal("rejected media_data error phase")
	}
}

func TestLoadCoreNegotiatesReadOnlyThenMutatesExactlyOnce(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2.jsonl")
	fixture := newSequenceSocketFixture(t, []string{lines[1] + "\n", lines[5] + "\n"})
	response, err := NewClient(fixture.path).LoadCore(context.Background(),
		"/tmp/fogcast-development/core-packages/fixture", fixturePackageID)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.ActivePackage == nil || response.Generation == nil ||
		response.ActivePackage.PackageID != fixturePackageID || *response.Generation != 1 {
		t.Fatalf("response = %#v", response)
	}
	requests := fixture.wait(t)
	want := []string{
		`{"protocol":2,"operation":"status"}`,
		`{"protocol":2,"operation":"load_core","package_path":"/tmp/fogcast-development/core-packages/fixture","package_id":"` + fixturePackageID + `"}`,
	}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Fatalf("requests = %q", requests)
	}
}

func TestLoadCoreRejectsRetiredProtocolWithoutMutation(t *testing.T) {
	unsupported := `{"protocol":1,"ok":false,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"unsupported_protocol","message":"unsupported protocol"},"version":"old"}` + "\n"
	fixture := newSequenceSocketFixture(t, []string{unsupported})
	_, err := NewClient(fixture.path).LoadCore(context.Background(),
		"/tmp/fogcast-development/core-packages/fixture", fixturePackageID)
	if err == nil {
		t.Fatalf("error = %v", err)
	}
	if got := fixture.wait(t); len(got) != 1 || got[0] != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("requests = %q", got)
	}
}

func TestLoadCoreNeverRetriesMalformedOrFailedMutation(t *testing.T) {
	t.Run("malformed negotiation", func(t *testing.T) {
		fixture := newSequenceSocketFixture(t, []string{"{}\n"})
		_, err := NewClient(fixture.path).LoadCore(context.Background(),
			"/tmp/fogcast-development/core-packages/fixture", fixturePackageID)
		if !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("error = %v", err)
		}
		if got := fixture.wait(t); len(got) != 1 {
			t.Fatalf("requests = %q", got)
		}
	})
	t.Run("failed mutation", func(t *testing.T) {
		status := fixtureLines(t, "protocol-v2.jsonl")[1]
		failed := fixtureLines(t, "protocol-v2-edge-responses.jsonl")[2]
		fixture := newSequenceSocketFixture(t, []string{status + "\n", failed + "\n"})
		response, err := NewClient(fixture.path).LoadCore(context.Background(),
			"/tmp/fogcast-development/core-packages/fixture", fixturePackageID)
		if err != nil || response.OK || response.Error == nil || response.Error.Phase != "admission" {
			t.Fatalf("response=%#v err=%v", response, err)
		}
		if got := fixture.wait(t); len(got) != 2 {
			t.Fatalf("requests = %q", got)
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		client := NewClient(filepath.Join(t.TempDir(), "missing.sock"))
		_, err := client.LoadCore(context.Background(),
			"/tmp/fogcast-development/core-packages/fixture", fixturePackageID)
		if !errors.Is(err, errRuntimeConnection) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestLoadCoreReportsTheExactRequestWriteBoundary(t *testing.T) {
	t.Run("cancelled before negotiation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewClient(filepath.Join(t.TempDir(), "missing.sock")).LoadCore(ctx,
			"/tmp/package", fixturePackageID)
		if err == nil || protocol2MutationAttempted(err) {
			t.Fatalf("attempted=%t error=%v", protocol2MutationAttempted(err), err)
		}
	})
	t.Run("connection failed before negotiation", func(t *testing.T) {
		_, err := NewClient(filepath.Join(t.TempDir(), "missing.sock")).LoadCore(context.Background(),
			"/tmp/package", fixturePackageID)
		if err == nil || protocol2MutationAttempted(err) {
			t.Fatalf("attempted=%t error=%v", protocol2MutationAttempted(err), err)
		}
	})
	t.Run("connection failed before mutation write", func(t *testing.T) {
		status := fixtureLines(t, "protocol-v2.jsonl")[1]
		fixture := newNegotiationOnlySocketFixture(t, status+"\n")
		_, err := NewClient(fixture.path).LoadCore(context.Background(), "/tmp/package", fixturePackageID)
		if !errors.Is(err, errRuntimeConnection) || protocol2MutationAttempted(err) {
			t.Fatalf("attempted=%t error=%v", protocol2MutationAttempted(err), err)
		}
		if got := fixture.wait(t); len(got) != 1 || got[0] != `{"protocol":2,"operation":"status"}` {
			t.Fatalf("requests = %q", got)
		}
	})
	t.Run("response lost after mutation write", func(t *testing.T) {
		status := fixtureLines(t, "protocol-v2.jsonl")[1]
		fixture := newSequenceSocketFixture(t, []string{status + "\n", ""})
		_, err := NewClient(fixture.path).LoadCore(context.Background(), "/tmp/package", fixturePackageID)
		if err == nil || !protocol2MutationAttempted(err) {
			t.Fatalf("attempted=%t error=%v", protocol2MutationAttempted(err), err)
		}
		fixture.wait(t)
	})
}

func TestProtocol2MutationRequiresTheCompleteNewlineTerminatedFrame(t *testing.T) {
	frame := []byte("{\"protocol\":2}\n")
	if protocol2FrameDispatched(len(frame)-1, len(frame)) {
		t.Fatal("short write before final newline was classified as dispatched")
	}
	if !protocol2FrameDispatched(len(frame), len(frame)) {
		t.Fatal("complete newline-terminated frame was not classified as dispatched")
	}
}

func TestProtocol2OperationsRejectAnotherOperationsInspectionEnvelope(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2.jsonl")
	t.Run("status", func(t *testing.T) {
		fixture := newSequenceSocketFixture(t, []string{lines[3] + "\n"})
		_, err := NewClient(fixture.path).Protocol2Status(context.Background())
		if !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("error = %v", err)
		}
		fixture.wait(t)
	})
	t.Run("load", func(t *testing.T) {
		fixture := newSequenceSocketFixture(t, []string{lines[1] + "\n", lines[3] + "\n"})
		_, err := NewClient(fixture.path).LoadCore(context.Background(),
			"/tmp/fogcast-development/core-packages/fixture", fixturePackageID)
		if !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("error = %v", err)
		}
		fixture.wait(t)
	})
	t.Run("inspect", func(t *testing.T) {
		fixture := newSequenceSocketFixture(t, []string{lines[1] + "\n"})
		_, err := NewClient(fixture.path).InspectCore(context.Background(),
			"/tmp/fogcast-development/core-packages/fixture", fixturePackageID)
		if !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("error = %v", err)
		}
		fixture.wait(t)
	})
}

func TestProtocol2StatusRequiresASuccessfulReadOnlyReply(t *testing.T) {
	failed := fixtureLines(t, "protocol-v2-edge-responses.jsonl")[1]
	fixture := newSequenceSocketFixture(t, []string{failed + "\n"})
	_, err := NewClient(fixture.path).Protocol2Status(context.Background())
	if !errors.Is(err, errInvalidRuntimeResponse) {
		t.Fatalf("error = %v", err)
	}
	fixture.wait(t)
}

func TestProtocol2StatusAcceptsMenuDisplayEvidenceForIdle(t *testing.T) {
	valid := fixtureLines(t, "protocol-v2.jsonl")[1]
	menu := `,"menu_display":{"available":false,"package_id":"` + strings.Repeat("a", 64) + `","generation":0,"width":1280,"height":720,"stride":5120,"byte_count":3686400,"slot_bytes":4194304,"slots":2,"format":"bgrx8888","displayed_sequence":0,"underflows":0,"error":{"code":"io_failed","message":"open fes-window: No such file or directory","phase":"menu_window"}}`
	withMenu := strings.Replace(valid, `"inspected_package":null`, `"inspected_package":null`+menu, 1)
	fixture := newSequenceSocketFixture(t, []string{withMenu + "\n"})
	status, err := NewClient(fixture.path).Protocol2Status(context.Background())
	if err != nil || !validIdle(status) {
		t.Fatalf("menu status rejected as idle: %#v, %v", status, err)
	}
	fixture.wait(t)
	malformed := strings.Replace(withMenu, `"format":"bgrx8888"`, `"format":"bgrx8888","surprise":true`, 1)
	if _, err := decodeProtocol2Response([]byte(malformed)); !errors.Is(err, errInvalidRuntimeResponse) {
		t.Fatalf("unknown menu evidence accepted: %v", err)
	}
	for _, value := range []string{"true", "false"} {
		framed := strings.Replace(withMenu, `"available":false`, `"available":false,"frame_mapping":`+value, 1)
		decoded, err := decodeProtocol2Response([]byte(framed))
		if err != nil || decoded.MenuDisplay == nil || decoded.MenuDisplay.FrameMapping != (value == "true") {
			t.Fatalf("mapping lifetime lost: %#v, %v", decoded.MenuDisplay, err)
		}
	}
	for _, value := range []string{`null`, `1`, `"true"`, `{}`} {
		framed := strings.Replace(withMenu, `"available":false`, `"available":false,"frame_mapping":`+value, 1)
		if _, err := decodeProtocol2Response([]byte(framed)); !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("nonboolean mapping lifetime accepted: %s, %v", value, err)
		}
	}
	// The idle display's live re-read: optional, and with the transmitter
	// reads carried as their own all-or-nothing object.
	display := `,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":7,"recovery_abandoned":false}`
	link := `,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":7,"recovery_abandoned":true,"transmitter":{"hot_plug":true,"rx_sense":true,"pll_lock":true,"vic":4}}`
	for _, live := range []string{display, link} {
		carried := strings.Replace(withMenu, `"underflows":0,"error"`, `"underflows":0`+live+`,"error"`, 1)
		decoded, err := decodeProtocol2Response([]byte(carried))
		if err != nil || decoded.MenuDisplay == nil || decoded.MenuDisplay.Live == nil ||
			!decoded.MenuDisplay.Live.Enabled || decoded.MenuDisplay.Live.DisplayedSequence != 4 {
			t.Fatalf("live display evidence lost: %#v, %v", decoded.MenuDisplay, err)
		}
		// The absolute counter is its own member: the per-present delta's
		// one-scanline cap says nothing about it.
		if decoded.MenuDisplay.Live.UnderflowsTotal != 7 || decoded.MenuDisplay.Underflows != 0 {
			t.Fatalf("live counter confused with the present delta: %#v", decoded.MenuDisplay)
		}
		// Whether the runtime has stopped recovering this loss travels with
		// the sample: a client must not present to force recovery it withheld.
		if decoded.MenuDisplay.Live.RecoveryAbandoned != (live == link) {
			t.Fatalf("abandoned recovery lost: %#v", decoded.MenuDisplay.Live)
		}
		if (decoded.MenuDisplay.Live.Transmitter != nil) != (live == link) {
			t.Fatalf("transmitter reads mismatched their presence: %#v", decoded.MenuDisplay.Live)
		}
		if live == link && (!decoded.MenuDisplay.Live.Transmitter.PLLLock ||
			decoded.MenuDisplay.Live.Transmitter.VIC != 4) {
			t.Fatalf("transmitter reads lost: %#v", decoded.MenuDisplay.Live.Transmitter)
		}
		// Re-encoding a decoded response reproduces a response this same
		// decoder accepts: presence is all-or-nothing on both sides.
		encoded, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("live display evidence does not re-encode: %v", err)
		}
		if _, err := decodeProtocol2Response(encoded); err != nil {
			t.Fatalf("re-encoded live display evidence rejected: %s, %v", encoded, err)
		}
	}
	// The runtime's verdict on whether scanout is showing the committed
	// frame, made where both numbers are read in one breath. The sampled
	// displayed_sequence beside it is a diagnostic, not the comparison. The
	// verdict is optional: the two samples above carry none, as a runtime
	// older than it does, and that must decode as "unsaid" rather than be
	// refused or read as a loss.
	for _, live := range []string{display, link} {
		carried := strings.Replace(withMenu, `"underflows":0,"error"`, `"underflows":0`+live+`,"error"`, 1)
		decoded, err := decodeProtocol2Response([]byte(carried))
		if err != nil || decoded.MenuDisplay == nil || decoded.MenuDisplay.Live == nil ||
			decoded.MenuDisplay.Live.ScanoutFrame != "" {
			t.Fatalf("an absent verdict was not decoded as unsaid: %#v, %v", decoded.MenuDisplay, err)
		}
	}
	for _, verdict := range []string{"unknown", "matching", "mismatched"} {
		live := `,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,` +
			`"scanout_frame":"` + verdict + `","underflows_total":7,"recovery_abandoned":false}`
		carried := strings.Replace(withMenu, `"underflows":0,"error"`, `"underflows":0`+live+`,"error"`, 1)
		decoded, err := decodeProtocol2Response([]byte(carried))
		if err != nil || decoded.MenuDisplay == nil || decoded.MenuDisplay.Live == nil ||
			decoded.MenuDisplay.Live.ScanoutFrame != verdict {
			t.Fatalf("committed-frame verdict lost: %#v, %v", decoded.MenuDisplay, err)
		}
		encoded, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("committed-frame verdict does not re-encode: %v", err)
		}
		if _, err := decodeProtocol2Response(encoded); err != nil {
			t.Fatalf("re-encoded committed-frame verdict rejected: %s, %v", encoded, err)
		}
	}
	for _, live := range []string{
		`,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4}`,
		`,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":0,"scanout_frame":true}`,
		`,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":0,"scanout_frame":2}`,
		`,"live":{"enabled":1,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":0}`,
		`,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":0,"pll_lock":true}`,
		`,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":0,"transmitter":{"hot_plug":true,"rx_sense":true,"pll_lock":true}}`,
		`,"live":{"enabled":true,"faulted":false,"quiesced":false,"displayed_sequence":4,"underflows_total":0,"surprise":true}`,
	} {
		carried := strings.Replace(withMenu, `"underflows":0,"error"`, `"underflows":0`+live+`,"error"`, 1)
		if _, err := decodeProtocol2Response([]byte(carried)); !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("malformed live display evidence accepted: %s, %v", live, err)
		}
	}
	// Clients draw into granted slots; the staging format and damage
	// capability of the copying runtime are no longer status fields.
	for _, stale := range []string{
		strings.Replace(withMenu, `"underflows":0`, `"underflows":0,"damage":true`, 1),
		strings.Replace(withMenu, `"slots":2,"format":"bgrx8888"`, `"staging_format":"rgba8888"`, 1),
		strings.Replace(withMenu, `"format":"bgrx8888"`, `"format":8888`, 1),
	} {
		if _, err := decodeProtocol2Response([]byte(stale)); !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("stale menu shape accepted: %v", err)
		}
	}
}

func TestProtocol2StatusAcceptsPromotedMenuFailures(t *testing.T) {
	valid := fixtureLines(t, "protocol-v2.jsonl")[1]
	for _, phase := range []string{"menu", "menu_window"} {
		t.Run(phase, func(t *testing.T) {
			withError := strings.Replace(valid, `"error":null`, `"error":{"code":"io_failed","message":"menu scanout failed","phase":"`+phase+`"}`, 1)
			fixture := newSequenceSocketFixture(t, []string{withError + "\n"})
			status, err := NewClient(fixture.path).Protocol2Status(context.Background())
			if err != nil || !validIdle(status) {
				t.Fatalf("promoted %s error rejected: %#v, %v", phase, status, err)
			}
			fixture.wait(t)
		})
	}
}

func TestProtocol2PackageOperationsBindSuccessfulRepliesToRequestedIdentityAndState(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2.jsonl")
	otherID := strings.Repeat("d", 64)
	t.Run("load mismatched package", func(t *testing.T) {
		mismatch := strings.Replace(lines[5], fixturePackageID, otherID, 1)
		fixture := newSequenceSocketFixture(t, []string{lines[1] + "\n", mismatch + "\n"})
		_, err := NewClient(fixture.path).LoadCore(context.Background(), "/tmp/package", fixturePackageID)
		if !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("error=%v", err)
		}
		fixture.wait(t)
	})
	t.Run("load status shaped success", func(t *testing.T) {
		fixture := newSequenceSocketFixture(t, []string{lines[1] + "\n", lines[1] + "\n"})
		_, err := NewClient(fixture.path).LoadCore(context.Background(), "/tmp/package", fixturePackageID)
		if !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("error=%v", err)
		}
		fixture.wait(t)
	})
	t.Run("inspect mismatched package", func(t *testing.T) {
		mismatch := strings.Replace(lines[3], fixturePackageID, otherID, 1)
		fixture := newSequenceSocketFixture(t, []string{mismatch + "\n"})
		_, err := NewClient(fixture.path).InspectCore(context.Background(), "/tmp/package", fixturePackageID)
		if !errors.Is(err, errInvalidRuntimeResponse) {
			t.Fatalf("error=%v", err)
		}
		fixture.wait(t)
	})
}

func TestProtocol2RejectsContradictoryCompatibilityAndActiveEvidence(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2.jsonl")
	inspection := lines[3]
	active := strings.Replace(lines[5],
		`"active_interfaces":[{"id":"fes.gamepad","major":1,"minor":0}]`,
		`"active_interfaces":[{"id":"fes.gamepad","major":1,"minor":0},{"id":"fes.video.fixed-720p60","major":1,"minor":0}]`, 1)
	cases := map[string]string{
		"compatible profile absent": strings.Replace(inspection, `,"fes-gp-v1"`, ``, 1),
		"compatible ABI absent":     strings.Replace(inspection, `"id":"fes.simple-game"`, `"id":"vendor.other"`, 1),
		"compatible required interface absent": strings.Replace(inspection,
			`,{"id":"fes.video.fixed-720p60","major":1,"minor":0}`, ``, 1),
		"active interface not declared": strings.Replace(active,
			`{"id":"fes.gamepad","major":1,"minor":0}`, `{"id":"vendor.input","major":1,"minor":0}`, 1),
		"active interface intersection incomplete": strings.Replace(active,
			`,{"id":"fes.video.fixed-720p60","major":1,"minor":0}`, ``, 1),
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProtocol2Response([]byte(response)); !errors.Is(err, errInvalidRuntimeResponse) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestProtocol2CapabilitiesAllowDistinctVersionsOfOneABI(t *testing.T) {
	capabilities := Protocol2Capabilities{
		ProgrammingProfiles: []string{"fes-gp-v1"},
		ABIs: []Protocol2ABI{
			{ID: "fes.computer", Major: 1, Minor: 0},
			{ID: "fes.computer", Major: 1, Minor: 1},
		},
	}
	if !validCapabilities(capabilities) {
		t.Fatal("distinct advertised ABI versions were rejected")
	}
	capabilities.ABIs = append(capabilities.ABIs, capabilities.ABIs[1])
	if validCapabilities(capabilities) {
		t.Fatal("duplicate advertised ABI version was accepted")
	}
	capabilities.ABIs = []Protocol2ABI{
		{ID: "fes.computer", Major: 1, Minor: 1},
		{ID: "fes.computer", Major: 1, Minor: 0},
	}
	if validCapabilities(capabilities) {
		t.Fatal("out-of-order ABI versions were accepted")
	}
}

func TestProtocol2ActivePackageRequiresRegistryAndProfileObservationSemantics(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2.jsonl")
	fes := lines[5]
	video := `,{"id":"fes.video.fixed-720p60","major":1,"minor":0}`
	cases := map[string]string{
		"required interface absent from ABI and active lists": strings.ReplaceAll(fes, video, ""),
		"fes gp observed identity absent": strings.Replace(fes,
			`"observed":{"abi":{"id":"fes.simple-game","major":1,"minor":0},"build_id":"0123456789abcdef0123456789abcdef"}`,
			`"observed":{"abi":null,"build_id":null}`, 1),
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProtocol2Response([]byte(response)); !errors.Is(err, errInvalidRuntimeResponse) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestProtocol2DescriptorUsesSharedPayloadIndependentValidation(t *testing.T) {
	inspection := fixtureLines(t, "protocol-v2.jsonl")[3]
	cases := map[string]string{
		"empty HTTPS authority":   strings.Replace(inspection, `https://example.invalid/fes-pong`, `https://`, 1),
		"credentialed repository": strings.Replace(inspection, `https://example.invalid/fes-pong`, `https://user@example.invalid/repo`, 1),
		"name control":            strings.Replace(inspection, `"name":"FES Pong"`, `"name":"FES\u0001Pong"`, 1),
		"toolchain control":       strings.Replace(inspection, `synthetic fixture generator 1.0 (test-only)`, `synthetic\u0001tool`, 1),
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProtocol2Response([]byte(response)); !errors.Is(err, errInvalidRuntimeResponse) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestProtocol2DescriptorRepositoryMatchesSharedSixteenCasePolicy(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "corepackage", "testdata", "repository-uri-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Repository string `json:"repository"`
			Valid      bool   `json:"valid"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	inspection := fixtureLines(t, "protocol-v2.jsonl")[3]
	const original = "https://example.invalid/fes-pong"
	for _, testCase := range corpus.Cases {
		t.Run(testCase.Repository, func(t *testing.T) {
			response := strings.Replace(inspection, original, testCase.Repository, 1)
			_, err := decodeProtocol2Response([]byte(response))
			if (err == nil) != testCase.Valid {
				t.Fatalf("valid=%t error=%v", testCase.Valid, err)
			}
		})
	}
}

func TestLoadCoreRejectsInvalidIdentityAndPathBeforeNegotiation(t *testing.T) {
	client := NewClient(filepath.Join(t.TempDir(), "must-not-dial.sock"))
	for _, input := range []struct{ path, id string }{
		{"relative", fixturePackageID},
		{"/tmp/package", "ABC"},
		{"/tmp/../package", fixturePackageID},
	} {
		if _, err := client.LoadCore(context.Background(), input.path, input.id); !errors.Is(err, errInvalidRuntimeRequest) {
			t.Fatalf("LoadCore(%q,%q) error=%v", input.path, input.id, err)
		}
	}
}

func newNegotiationOnlySocketFixture(t *testing.T, response string) *sequenceSocketFixture {
	t.Helper()
	path := temporarySocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	fixture := &sequenceSocketFixture{path: path, done: make(chan fixtureResult, 1)}
	go func() {
		connection, acceptErr := listener.Accept()
		result := fixtureResult{err: acceptErr}
		if acceptErr == nil {
			result.request, result.err = readNewline(connection)
			// Stop accepting mutation connections before publishing negotiation
			// success. The accepted status connection remains usable after Close.
			// Closing afterwards races the client's next dial and can legitimately
			// allow a complete mutation write before the reply is lost.
			closeErr := listener.Close()
			if result.err == nil {
				result.err = closeErr
			}
			if result.err == nil {
				_, result.err = writeAll(connection, response)
			}
			_ = connection.Close()
		}
		fixture.done <- result
	}()
	return fixture
}

type sequenceSocketFixture struct {
	path string
	done chan fixtureResult
}

func newSequenceSocketFixture(t *testing.T, responses []string) *sequenceSocketFixture {
	t.Helper()
	path := temporarySocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	fixture := &sequenceSocketFixture{path: path, done: make(chan fixtureResult, len(responses))}
	go func() {
		defer listener.Close()
		for _, response := range responses {
			connection, acceptErr := listener.Accept()
			result := fixtureResult{err: acceptErr}
			if acceptErr == nil {
				result.request, result.err = readNewline(connection)
				if result.err == nil && response != "" {
					_, result.err = writeAll(connection, response)
				}
				if result.err == nil && response != "" {
					result.err = requireClientClose(connection)
				}
				_ = connection.Close()
			}
			fixture.done <- result
			if result.err != nil {
				return
			}
		}
	}()
	return fixture
}

func (fixture *sequenceSocketFixture) wait(t *testing.T) []string {
	t.Helper()
	var requests []string
	for cap(fixture.done) > len(requests) {
		select {
		case result := <-fixture.done:
			if result.err != nil {
				t.Fatal(result.err)
			}
			requests = append(requests, result.request)
		case <-time.After(time.Second):
			t.Fatal("sequence socket fixture did not finish")
		}
	}
	return requests
}

func TestROMDescriptorShapeRequiresClosedFormat3Table(t *testing.T) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixtureLines(t, "protocol-v2.jsonl")[3]), &response); err != nil {
		t.Fatal(err)
	}
	var inspection map[string]json.RawMessage
	if err := json.Unmarshal(response["inspected_package"], &inspection); err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(string(inspection["descriptor"]), `"format":2`, `"format":3,"rom":{"id":"machine","role":"firmware","source_size":8192,"file":"rom-map.json","size":100,"sha256":"`+strings.Repeat("a", 64)+`"}`, 1)
	if err := validateDescriptorShape([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"wrong format": strings.Replace(raw, `"format":3`, `"format":2`, 1),
		"missing size": strings.Replace(raw, `"source_size":8192,`, "", 1),
		"null size":    strings.Replace(raw, `"source_size":8192`, `"source_size":null`, 1),
		"unknown key":  strings.Replace(raw, `"role":"firmware"`, `"role":"firmware","extra":1`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateDescriptorShape([]byte(bad)); err == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
}

func TestProtocol2ConsumesRuntimeROMInspection(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-rom-package-responses.jsonl")
	if len(lines) != 2 {
		t.Fatal("expected inspection and running fixtures")
	}
	response, err := decodeProtocol2Response([]byte(lines[0]))
	if err != nil {
		t.Fatal(err)
	}
	inspection := response.InspectedPackage
	if !response.OK || inspection == nil || !inspection.Compatible || inspection.CompatibilityError != nil || response.Capabilities.ROMLinking != 1 || inspection.Descriptor.Format != 3 || inspection.Descriptor.ROM == nil {
		t.Fatalf("incorrect format3 inspection: %+v", inspection)
	}
	fixture := newSequenceSocketFixture(t, []string{lines[0] + "\n"})
	if _, err := NewClient(fixture.path).InspectCore(context.Background(), "/tmp/package", inspection.PackageID); err != nil {
		t.Fatal(err)
	}
	fixture.wait(t)
}

func TestProtocol2ROMLinkingCapabilityIsTyped(t *testing.T) {
	base := fixtureLines(t, "protocol-v2.jsonl")[1]
	advertised := strings.Replace(base, `"active_interfaces":[]`, `"active_interfaces":[],"rom_linking":1`, 1)
	if _, err := decodeProtocol2Response([]byte(advertised)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`null`, `"1"`, `true`, `1.0`} {
		malformed := strings.Replace(advertised, `"rom_linking":1`, `"rom_linking":`+bad, 1)
		if _, err := decodeProtocol2Response([]byte(malformed)); err == nil {
			t.Fatalf("invalid capability accepted: %s", bad)
		}
	}
}
