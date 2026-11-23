//go:build linux

package tenfoot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
)

func readyNativeBindingApp(t *testing.T) *App {
	t.Helper()
	a := NewApp(NewClient("http://unused.invalid", nil), 1280, 720, 20)
	agent := strings.Repeat("e", 32)
	lease := strings.Repeat("b", 64)
	a.mu.Lock()
	a.kitHost = true
	a.delegatedV2Active = true
	a.nativeKeyboardQuarantined = true
	a.nativeKeyboardGeneration = 1
	a.healthHave = true
	a.healthObservedAt = time.Now()
	a.sessionObservedAt = time.Now()
	// TargetReady is idle launch readiness and can be false during a healthy
	// running C64 session; active input must not use it as a gate.
	a.health = hostclient.HealthResult{Ready: true, TargetReachable: true, TargetReady: false, Connection: hostclient.TargetConnection{TargetID: "kit", BootID: "boot", LeaseGeneration: lease}}
	a.kitLeaseHave = true
	a.kitLease = KitLeaseStatus{Generation: lease}
	a.session = hostclient.SessionResult{State: "active", TargetID: "kit", Development: true, CoreKeyboardHID: true, Input: &hostclient.SessionInput{Ready: true}, InputAgentInstance: agent, RuntimeInput: &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 9}}
	a.mu.Unlock()
	return a
}

func TestNativeAppRecoveryPermitRequiresCurrentCompleteBinding(t *testing.T) {
	for _, change := range []string{"none", "agent", "boot", "lease", "runtime", "generation", "missing", "stale-health", "stale-session"} {
		t.Run(change, func(t *testing.T) {
			a := readyNativeBindingApp(t)
			defer a.Stop()
			expected, err := a.nativeKeyboardPrepareBinding([16]byte{1})
			if err != nil {
				t.Fatal(err)
			}
			a.mu.Lock()
			switch change {
			case "agent":
				a.session.InputAgentInstance = strings.Repeat("f", 32)
			case "boot":
				a.health.Connection.BootID = "other"
			case "lease":
				a.health.Connection.LeaseGeneration = strings.Repeat("c", 64)
			case "runtime":
				a.session.RuntimeInput.RuntimeInstance = strings.Repeat("d", 32)
			case "generation":
				a.nativeKeyboardGeneration++
			case "missing":
				a.session.InputAgentInstance = ""
			case "stale-health":
				a.healthObservedAt = time.Now().Add(-6 * time.Second)
			case "stale-session":
				a.sessionObservedAt = time.Now().Add(-6 * time.Second)
			}
			a.mu.Unlock()
			reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: expected.InputAgentInstance, TargetID: expected.TargetID, BootID: expected.BootID}
			err = a.nativeKeyboardPermitAfterRecovery(expected, expected.InputAgentInstance, reply)
			_, permit := a.nativeKeyboardSnapshotPermit()
			if change == "none" {
				if err != nil || !permit {
					t.Fatalf("valid recovery %v", err)
				}
				a.mu.Lock()
				stillQuarantined := a.nativeKeyboardQuarantined
				a.mu.Unlock()
				if !stillQuarantined {
					t.Fatal("recovery marked guest ready")
				}
				if a.nativeKeyboardPermitAfterRecovery(expected, expected.InputAgentInstance, reply) == nil {
					t.Fatal("recovery reused")
				}
				a.mu.Lock()
				a.healthObservedAt = time.Now().Add(-6 * time.Second)
				a.mu.Unlock()
				if _, allowed := a.nativeKeyboardSnapshotPermit(); allowed {
					t.Fatal("expired snapshot permit remained valid")
				}
			} else if err == nil || permit {
				t.Fatalf("%s change admitted", change)
			}
		})
	}
}

type nativeFailedObservationTransport struct{}

func (nativeFailedObservationTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("observation unavailable")
}

func TestNativeAppFailedPollCannotReuseCachedAuthority(t *testing.T) {
	for _, which := range []string{"health", "session"} {
		t.Run(which, func(t *testing.T) {
			a := readyNativeBindingApp(t)
			defer a.Stop()
			a.client = NewClient("http://unused.invalid", &http.Client{Transport: nativeFailedObservationTransport{}})
			if which == "health" {
				a.fetchHealth(context.Background())
			} else {
				a.fetchSession(context.Background())
			}
			if _, err := a.nativeKeyboardPrepareBinding([16]byte{1}); err == nil {
				t.Fatal("failed observation reused cached authority")
			}
		})
	}
}

func TestNativeAppBindingRejectsPairedHost(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.client.paired = true
	if _, err := a.nativeKeyboardPrepareBinding([16]byte{1}); err == nil {
		t.Fatal("paired host admitted to local native route")
	}
}

func TestNativeAppBindingRequiresDeclaredKeyboardCapability(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	a.session.CoreKeyboardHID = false
	a.session.CoreKeyboard = false
	a.mu.Unlock()
	if _, err := a.nativeKeyboardPrepareBinding([16]byte{1}); err == nil {
		t.Fatal("unproven development keyboard capability admitted")
	}
}

func TestNativeAppReadyRequiresAcknowledgedDrainedFreshSource(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	expected, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	id := gateID(1)
	a.mu.Lock()
	_, err = a.nativeGuest.begin(expected, nativeRawBoundary{Publisher: expected.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: id, Held: []uint16{42}}}}, nativeHIDUsage)
	if err == nil {
		err = a.nativeGuest.rawEdge(expected, nativeRawEdge{Device: id, Sequence: 11, Code: 42, Down: false})
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if a.nativeKeyboardConfirmReady(expected) {
		t.Fatal("ready before BEGIN ACK")
	}
	token := strings.Repeat("c", 64)
	a.mu.Lock()
	a.nativeGuest.beginResult(a.nativeGuest.operation, expected, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	a.mu.Unlock()
	if a.nativeKeyboardConfirmReady(expected) {
		t.Fatal("ready before buffered release drained")
	}
	a.mu.Lock()
	_, ok := a.nativeGuest.nextEdge(expected)
	a.mu.Unlock()
	if !ok {
		t.Fatal("missing buffered edge")
	}
	if a.nativeKeyboardConfirmReady(expected) {
		t.Fatal("ready before edge ACK")
	}
	a.mu.Lock()
	a.nativeGuest.edgeResult(a.nativeGuest.operation, expected, localhid.Reply{Op: "edge", Status: "accepted", Token: token}, nil)
	a.mu.Unlock()
	if !a.nativeKeyboardConfirmReady(expected) {
		t.Fatal("acknowledged source remained unavailable")
	}
	a.mu.Lock()
	a.session.InputAgentInstance = strings.Repeat("f", 32)
	a.mu.Unlock()
	if a.nativeKeyboardConfirmReady(expected) {
		t.Fatal("changed agent remained ready")
	}
	a.mu.Lock()
	quarantined := a.nativeKeyboardQuarantined
	a.mu.Unlock()
	if !quarantined {
		t.Fatal("authority mismatch did not quarantine")
	}
}

func TestNativeAppLostTransportRequiresExactRecoveryBeforeFreshSnapshot(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	old, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	_, err = a.nativeGuest.begin(old, nativeRawBoundary{Publisher: old.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: gateID(1)}}}, nativeHIDUsage)
	oldOp := a.nativeGuest.operation
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	unrelated := old
	unrelated.QueueEpoch++
	if a.nativeKeyboardTransportLost(unrelated) {
		t.Fatal("unrelated connection quarantined current source")
	}
	if !a.nativeKeyboardTransportLost(old) {
		t.Fatal("current source EOF not quarantined")
	}
	if _, ok := a.nativeKeyboardSnapshotPermit(); ok {
		t.Fatal("EOF granted snapshot")
	}
	current, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: old.InputAgentInstance, TargetID: old.TargetID, BootID: old.BootID}
	if a.nativeKeyboardPermitAfterRecovery(current, strings.Repeat("f", 32), reply) == nil {
		t.Fatal("wrong previous agent retired source")
	}
	a.mu.Lock()
	phase := a.nativeGuest.phase
	a.mu.Unlock()
	if phase != nativeGuestUncertain {
		t.Fatal("failed proof retired source")
	}
	if err := a.nativeKeyboardPermitAfterRecovery(current, old.InputAgentInstance, reply); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	if a.nativeGuest.phase != nativeGuestIdle || a.nativeGuest.serial != oldOp.serial || a.nativeGuest.lastGeneration != old.AuthorityGeneration || !a.nativeKeyboardQuarantined {
		a.mu.Unlock()
		t.Fatal("recovery lost source history or marked ready")
	}
	a.nativeGuest.beginResult(oldOp, old, localhid.Reply{Op: "begin", Status: "ready", Token: strings.Repeat("c", 64)}, nil)
	if a.nativeGuest.phase != nativeGuestIdle {
		a.mu.Unlock()
		t.Fatal("old callback revived retired source")
	}
	a.mu.Unlock()
	if _, ok := a.nativeKeyboardSnapshotPermit(); !ok {
		t.Fatal("recovery did not permit fresh snapshot")
	}
}

func TestNativeAppRecoveredConnectionEOFFencesSnapshotAndOldCallbacks(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	old, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: old.InputAgentInstance, TargetID: old.TargetID, BootID: old.BootID}
	if err := a.nativeKeyboardPermitAfterRecovery(old, old.InputAgentInstance, reply); err != nil {
		t.Fatal(err)
	}
	if !a.nativeKeyboardTransportLost(old) {
		t.Fatal("recovered connection EOF ignored")
	}
	if _, ok := a.nativeKeyboardSnapshotPermit(); ok {
		t.Fatal("EOF left snapshot permit live")
	}
	if err := a.publishNativeKeyboardBoundary(old.AuthorityGeneration, nativeRawBoundary{Publisher: old.Publisher, Devices: []nativeRawDevice{{ID: gateID(1)}}}); err == nil {
		t.Fatal("EOF allowed BEGIN boundary")
	}
	next, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	if a.nativeKeyboardTransportLost(old) {
		t.Fatal("stale old connection invalidated successor")
	}
	nextReply := reply
	nextReply.Request++
	if err := a.nativeKeyboardPermitAfterRecovery(next, next.InputAgentInstance, nextReply); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.nativeKeyboardSnapshotPermit(); !ok {
		t.Fatal("fresh recovery did not permit snapshot")
	}
}

func TestNativeAppChangedAgentRequiresStartupRetirementProof(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	old, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	_, err = a.nativeGuest.begin(old, nativeRawBoundary{Publisher: old.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: gateID(1)}}}, nativeHIDUsage)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !a.nativeKeyboardTransportLost(old) {
		t.Fatal("old source not marked uncertain")
	}
	newAgent := strings.Repeat("f", 32)
	a.mu.Lock()
	a.session.InputAgentInstance = newAgent
	a.mu.Unlock()
	current, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: newAgent, TargetID: old.TargetID, BootID: old.BootID}
	if a.nativeKeyboardPermitAfterRecovery(current, old.InputAgentInstance, reply) == nil {
		t.Fatal("new agent claimed old reader drain")
	}
	reply.Status = "startup-retired"
	if a.nativeKeyboardPermitAfterRecovery(current, old.InputAgentInstance, reply) == nil {
		t.Fatal("unmanaged agent restart was treated as retired")
	}
	a.mu.Lock()
	a.managedKitSoftware = true
	a.mu.Unlock()
	if err := a.nativeKeyboardPermitAfterRecovery(current, old.InputAgentInstance, reply); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.nativeKeyboardSnapshotPermit(); !ok {
		t.Fatal("retired startup did not permit fresh snapshot")
	}
}

func TestNativeAppAuthorityLossPreservesExactTokenReset(t *testing.T) {
	for _, pending := range []bool{false, true} {
		a := readyNativeBindingApp(t)
		b, err := a.nativeKeyboardPrepareBinding([16]byte{1})
		if err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		_, err = a.nativeGuest.begin(b, nativeRawBoundary{Publisher: b.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: gateID(1)}}}, nativeHIDUsage)
		if err == nil {
			a.nativeGuest.beginResult(a.nativeGuest.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: strings.Repeat("c", 64)}, nil)
		}
		if pending && err == nil {
			err = a.nativeGuest.rawEdge(b, nativeRawEdge{Device: gateID(1), Sequence: 11, Code: 30, Down: true})
			if err == nil {
				_, _ = a.nativeGuest.nextEdge(b)
			}
		}
		a.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if !a.nativeKeyboardAuthorityLost(b) {
			t.Fatal("current authority loss ignored")
		}
		if a.nativeKeyboardAuthorityLost(b) {
			t.Fatal("old authority generation reused")
		}
		a.mu.Lock()
		if pending {
			a.nativeGuest.edgeResult(a.nativeGuest.operation, b, localhid.Reply{Op: "edge", Status: "accepted", Token: strings.Repeat("c", 64)}, nil)
		}
		reset, ok := a.nativeGuest.reset()
		a.mu.Unlock()
		if !ok || reset.Token != strings.Repeat("c", 64) {
			t.Fatal("loss did not preserve exact source reset")
		}
		a.Stop()
	}
}

func TestNativeAppLostAckAfterReadyStillQuarantines(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	b, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	_, err = a.nativeGuest.begin(b, nativeRawBoundary{Publisher: b.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: gateID(1)}}}, nativeHIDUsage)
	if err == nil {
		a.nativeGuest.beginResult(a.nativeGuest.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: strings.Repeat("c", 64)}, nil)
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !a.nativeKeyboardConfirmReady(b) {
		t.Fatal("source did not become ready")
	}
	a.mu.Lock()
	err = a.nativeGuest.rawEdge(b, nativeRawEdge{Device: gateID(1), Sequence: 11, Code: 30, Down: true})
	if err == nil {
		_, _ = a.nativeGuest.nextEdge(b)
		a.nativeGuest.edgeResult(a.nativeGuest.operation, b, localhid.Reply{}, errors.New("lost ACK"))
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !a.nativeKeyboardTransportLost(b) {
		t.Fatal("matching uncertain source loss ignored")
	}
	a.mu.Lock()
	quarantined, phase := a.nativeKeyboardQuarantined, a.nativeGuest.phase
	a.mu.Unlock()
	if !quarantined || phase != nativeGuestUncertain {
		t.Fatal("lost ACK treated as ready or retired")
	}
}

func nativeSetDescribedPadOnly(a *App) {
	pkg := strings.Repeat("b", 64)
	a.session.Development = false
	a.session.CoreKeyboard = false
	a.session.CoreKeyboardHID = false
	a.session.RuntimeInput = &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: strings.Repeat("a", 32), PackageID: pkg, Generation: 9}
	a.session.CorePackage = &hostclient.SessionCorePackage{PackageID: pkg, Generation: 9, ABI: hostclient.SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: "fes.gamepad", Major: 1}}}
}

func TestNativePadRecoveryRequiresResetOrExactUncertainProof(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		a := readyNativeBindingApp(t)
		old, err := a.nativeKeyboardPrepareBinding([16]byte{1})
		if err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		_, err = a.nativeGuest.begin(old, nativeRawBoundary{Publisher: old.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: gateID(1)}}}, nativeGuestHIDUsage)
		if err == nil {
			a.nativeGuest.beginResult(a.nativeGuest.operation, old, localhid.Reply{Op: "begin", Status: "ready", Token: strings.Repeat("c", 64)}, nil)
		}
		a.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if uncertain {
			if !a.nativeKeyboardTransportLost(old) {
				t.Fatal("loss ignored")
			}
		} else {
			if !a.nativeKeyboardAuthorityLost(old) {
				t.Fatal("focus loss ignored")
			}
		}
		a.mu.Lock()
		nativeSetDescribedPadOnly(a)
		expected, err := a.padOnlyAuthorityLocked(time.Now(), true)
		a.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: old.InputAgentInstance, TargetID: old.TargetID, BootID: old.BootID}
		a.mu.Lock()
		if !uncertain {
			if a.nativeKeyboardRetireForPadRecoveryLocked(expected, old.InputAgentInstance, reply) == nil {
				a.mu.Unlock()
				t.Fatal("live source bypassed RESET")
			}
			reset, ok := a.nativeGuest.reset()
			if !ok || reset.Token != strings.Repeat("c", 64) {
				a.mu.Unlock()
				t.Fatal("missing exact reset")
			}
			a.nativeGuest.resetResult(a.nativeGuest.operation, localhid.Reply{Op: "reset", Status: "neutral-confirmed", Token: reset.Token}, nil)
		} else {
			wrong := reply
			wrong.TargetID = "other"
			if a.nativeKeyboardRetireForPadRecoveryLocked(expected, old.InputAgentInstance, wrong) == nil {
				a.mu.Unlock()
				t.Fatal("wrong target retired uncertain source")
			}
			wrong = reply
			wrong.BootID = "other"
			if a.nativeKeyboardRetireForPadRecoveryLocked(expected, old.InputAgentInstance, wrong) == nil {
				a.mu.Unlock()
				t.Fatal("wrong boot retired uncertain source")
			}
			if a.nativeKeyboardRetireForPadRecoveryLocked(expected, strings.Repeat("f", 32), reply) == nil {
				a.mu.Unlock()
				t.Fatal("wrong prior agent retired uncertain source")
			}
		}
		serial, generation := a.nativeGuest.serial, a.nativeGuest.lastGeneration
		if err := a.nativeKeyboardRetireForPadRecoveryLocked(expected, old.InputAgentInstance, reply); err != nil {
			a.mu.Unlock()
			t.Fatal(err)
		}
		if a.nativeGuest.phase != nativeGuestIdle || a.nativeGuest.serial != serial || a.nativeGuest.lastGeneration != generation || a.nativeGuestSnapshotAllowed || !a.nativeKeyboardQuarantined {
			a.mu.Unlock()
			t.Fatal("pad recovery authorized keyboard or lost history")
		}
		a.session.CorePackage.ActiveInterfaces = append(a.session.CorePackage.ActiveInterfaces, hostclient.SessionCoreInterface{ID: "fes.keyboard.hid", Major: 1})
		if _, err := a.padOnlyAuthorityLocked(time.Now(), true); err == nil {
			a.mu.Unlock()
			t.Fatal("keyboard-capable successor remained pad-only")
		}
		a.mu.Unlock()
		a.Stop()
	}
}

func TestNativePadDrainRequiresExactPromotionEpochAndIdentity(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	nativeSetDescribedPadOnly(a)
	b, err := a.padOnlyAuthorityLocked(time.Now(), true)
	if err == nil {
		a.nativePadRetiring = true
		a.nativePadRetirementEpoch = 2
		a.nativePadRetirementBinding = b
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: b.AgentInstance, TargetID: b.TargetID, BootID: b.BootID}
	if a.nativeKeyboardConfirmPadDrain(1, b, reply) == nil {
		t.Fatal("stale promotion epoch retired source")
	}
	for _, wrong := range []localhid.Reply{func() localhid.Reply { x := reply; x.TargetID = "other"; return x }(), func() localhid.Reply { x := reply; x.BootID = "other"; return x }(), func() localhid.Reply { x := reply; x.AgentInstance = strings.Repeat("f", 32); return x }(), func() localhid.Reply { x := reply; x.Status = "unavailable"; return x }()} {
		if a.nativeKeyboardConfirmPadDrain(2, b, wrong) == nil {
			t.Fatal("mismatched drain proof accepted")
		}
	}
	a.mu.Lock()
	still := a.nativePadRetiring
	a.mu.Unlock()
	if !still {
		t.Fatal("invalid proof retired source")
	}
	if err := a.nativeKeyboardConfirmPadDrain(2, b, reply); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	still = a.nativePadRetiring
	a.mu.Unlock()
	if still {
		t.Fatal("confirmed drain remained pending")
	}
}

func TestNativePadDrainWaitsForLocalMappedWriteCompletion(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	nativeSetDescribedPadOnly(a)
	b, err := a.padOnlyAuthorityLocked(time.Now(), true)
	if err == nil {
		a.nativePadRetiring = true
		a.nativePadRetirementEpoch = 1
		a.nativePadRetirementBinding = b
		err = a.nativeMappedPad.begin(1, b)
	}
	if err == nil {
		err = a.nativeMappedPad.edge(1, b, gateID(1), 30, true, true, nativeMappedPadPress())
	}
	action, ok := a.nativeMappedPad.next(1, b)
	a.mu.Unlock()
	if err != nil || !ok {
		t.Fatalf("setup %v", err)
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: b.AgentInstance, TargetID: b.TargetID, BootID: b.BootID}
	if a.nativeKeyboardConfirmPadDrain(1, b, reply) == nil {
		t.Fatal("target proof bypassed local in-flight write")
	}
	a.mu.Lock()
	done := a.nativeMappedPad.complete(action, nil)
	a.mu.Unlock()
	if !done {
		t.Fatal("local completion not drained")
	}
	if err := a.nativeKeyboardConfirmPadDrain(1, b, reply); err != nil {
		t.Fatal(err)
	}
}

func TestNativePadChangedAgentDrainRequiresManagedRetirement(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	nativeSetDescribedPadOnly(a)
	b, err := a.padOnlyAuthorityLocked(time.Now(), true)
	if err == nil {
		a.nativePadRetiring = true
		a.nativePadRetirementEpoch = 1
		a.nativePadRetirementBinding = b
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "startup-retired", AgentInstance: strings.Repeat("f", 32), TargetID: b.TargetID, BootID: b.BootID}
	if a.nativeKeyboardConfirmPadDrain(1, b, reply) == nil {
		t.Fatal("unmanaged changed-agent drain accepted")
	}
	a.mu.Lock()
	a.managedKitSoftware = true
	a.mu.Unlock()
	if err := a.nativeKeyboardConfirmPadDrain(1, b, reply); err != nil {
		t.Fatal(err)
	}
}

func TestNativeAppStaleReadyCallbackCannotQuarantineSuccessor(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	old, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	next, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	_, err = a.nativeGuest.begin(next, nativeRawBoundary{Publisher: next.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: gateID(1)}}}, nativeHIDUsage)
	if err == nil {
		a.nativeGuest.beginResult(a.nativeGuest.operation, next, localhid.Reply{Op: "begin", Status: "ready", Token: strings.Repeat("c", 64)}, nil)
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !a.nativeKeyboardConfirmReady(next) {
		t.Fatal("successor not ready")
	}
	if a.nativeKeyboardConfirmReady(old) {
		t.Fatal("old worker claimed readiness")
	}
	a.mu.Lock()
	quarantined, gen := a.nativeKeyboardQuarantined, a.nativeKeyboardGeneration
	a.mu.Unlock()
	if quarantined || gen != next.AuthorityGeneration {
		t.Fatal("old worker quarantined successor")
	}
}

type nativeDelayedObservationTransport struct {
	started, release chan struct{}
	body             string
}

func (r *nativeDelayedObservationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	close(r.started)
	<-r.release
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(r.body)), Request: req}, nil
}

func TestNativeAppObservationFreshnessUsesRequestStart(t *testing.T) {
	for _, which := range []string{"health", "session"} {
		t.Run(which, func(t *testing.T) {
			body := `{"state":"idle"}`
			if which == "health" {
				body = `{"ready":true,"target":{"reachable":true,"connection":{"state":"active"}}}`
			}
			tr := &nativeDelayedObservationTransport{started: make(chan struct{}), release: make(chan struct{}), body: body}
			a := NewApp(NewClient("http://unused.invalid", &http.Client{Transport: tr}), 1280, 720, 20)
			defer a.Stop()
			done := make(chan struct{})
			go func() {
				if which == "health" {
					a.fetchHealth(context.Background())
				} else {
					a.fetchSession(context.Background())
				}
				close(done)
			}()
			<-tr.started
			beforeRelease := time.Now()
			close(tr.release)
			<-done
			a.mu.Lock()
			observed := a.sessionObservedAt
			if which == "health" {
				observed = a.healthObservedAt
			}
			a.mu.Unlock()
			if observed.IsZero() || observed.After(beforeRelease) {
				t.Fatal("response completion refreshed an old request")
			}
		})
	}
}

func TestNativeAppBoundaryRemainsDisabledWithoutExplicitPermit(t *testing.T) {
	a := NewApp(nil, 1280, 720, 20)
	defer a.Stop()
	a.mu.Lock()
	a.delegatedV2Active = true
	a.nativeKeyboardGeneration = 1
	a.mu.Unlock()
	if _, ok := a.nativeKeyboardSnapshotPermit(); ok {
		t.Fatal("default configuration allowed snapshot")
	}
}

func TestNativeAppUIOnlySnapshotDoesNotAuthorizeGuest(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	g := &nativeKeyboardGate{boundary: a, publisher: [16]byte{1}}
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	if err := g.add(gateID(1), in); err != nil {
		t.Fatal(err)
	}
	g.snapshotUI(func(int) ([]byte, error) { return gateCaps(30, 42, 88), nil }, func(int) ([]byte, error) { return gateCaps(42, 88), nil }, func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, []*nativeInput{in}, held, true, func(Command) {})
	}, func(in *nativeInput) { reconcileNativeKeyboardUI(in, []*nativeInput{in}, nil, false, func(Command) {}) })
	a.mu.Lock()
	d := a.nativeUIRaw.devices[gateID(1)]
	known := d != nil && !d.unknown && d.suppressed[42] && d.suppressed[88]
	a.mu.Unlock()
	if !known {
		t.Fatal("UI snapshot did not preserve held suppression")
	}
	if handled, _, err := g.dispatch(gateID(1), 1, 30, 1, time.Time{}); !handled || err != nil {
		t.Fatalf("first UI press %v %v", handled, err)
	}
	a.mu.Lock()
	d = a.nativeUIRaw.devices[gateID(1)]
	armed := d.down[30] && !d.suppressed[30]
	idle := a.nativeGuest.phase == nativeGuestIdle && len(a.nativeGuest.queue) == 0 && a.nativeKeyboardQuarantined
	a.mu.Unlock()
	if !armed || !idle {
		t.Fatal("UI-only snapshot suppressed fresh input or authorized guest")
	}
}

func TestNativeAppGateF12StillStopsWithoutLegacyKeyboardWrite(t *testing.T) {
	client := &http.Client{Transport: uiOnlyTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline test") })}
	a := NewApp(NewClient("http://example.invalid", client), 1280, 720, 20)
	defer a.Stop()
	a.mu.Lock()
	a.delegatedV2Active = true
	a.nativeKeyboardGeneration = 1
	a.session = hostclient.SessionResult{State: "active", CoreKeyboardHID: true}
	a.mu.Unlock()
	g := &nativeKeyboardGate{boundary: a, publisher: [16]byte{1}}
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	if err := g.add(gateID(1), in); err != nil {
		t.Fatal(err)
	}
	g.snapshotUI(func(int) ([]byte, error) { return gateCaps(88), nil }, func(int) ([]byte, error) { return make([]byte, nativeKeyBitmapBytes), nil }, func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, []*nativeInput{in}, held, true, func(Command) {})
	}, func(in *nativeInput) { reconcileNativeKeyboardUI(in, []*nativeInput{in}, nil, false, func(Command) {}) })
	if handled, quit, err := g.dispatch(gateID(1), 1, 88, 1, time.Now()); !handled || quit || err != nil {
		t.Fatalf("F12 gate %v %v %v", handled, quit, err)
	}
	a.mu.Lock()
	phase, tail := a.stopPhase, a.playHIDTail
	a.mu.Unlock()
	if phase != "stopping" || tail != nil {
		t.Fatalf("F12 did not stop safely: phase %s", phase)
	}
}

func TestNativeGuestNeverForwardsReservedF12(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	b, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	id := gateID(1)
	var source nativeGuestLifecycle
	req, err := source.begin(b, nativeRawBoundary{Publisher: b.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: id, Held: []uint16{88}}}}, nativeGuestHIDUsage)
	if err != nil || len(req.Devices) != 1 || len(req.Devices[0].Held) != 0 {
		t.Fatal("held F12 reached guest snapshot")
	}
	for _, edge := range []nativeRawEdge{{Device: id, Sequence: 11, Code: 88, Down: false}, {Device: id, Sequence: 12, Code: 88, Down: true}, {Device: id, Sequence: 13, Code: 88, Down: false}, {Device: id, Sequence: 14, Code: 30, Down: true}} {
		if err := source.rawEdge(b, edge); err != nil {
			t.Fatal(err)
		}
	}
	source.beginResult(source.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: strings.Repeat("c", 64)}, nil)
	next, ok := source.nextEdge(b)
	if !ok || next.Usage != nativeHIDUsage(30) || next.EventSeq != 1 {
		t.Fatal("F12 reached guest or introduced a wire gap")
	}
}

func TestNativeGuestFocusAndShortcutsFenceBeforeGuestEnqueue(t *testing.T) {
	for _, mode := range []string{"f12", "ctrl-home", "plain-home", "room"} {
		t.Run(mode, func(t *testing.T) {
			a := readyNativeBindingApp(t)
			defer a.Stop()
			b, err := a.nativeKeyboardPrepareBinding([16]byte{1})
			if err != nil {
				t.Fatal(err)
			}
			id := gateID(1)
			boundary := nativeRawBoundary{Publisher: b.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: id}}}
			a.mu.Lock()
			if err = a.nativeUIRaw.replace(boundary); err == nil {
				_, err = a.nativeGuest.begin(b, boundary, nativeGuestHIDUsage)
			}
			if err == nil {
				a.nativeGuest.beginResult(a.nativeGuest.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: strings.Repeat("c", 64)}, nil)
			}
			if mode == "room" {
				a.roomDuringPlay = true
			}
			a.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "room" {
				if _, err := a.nativeKeyboardObserveBinding(); err == nil {
					t.Fatal("room focus admitted a guest binding")
				}
			}
			seq, code := uint64(11), uint16(88)
			if mode == "ctrl-home" {
				if _, err := a.recordNativeKeyboardEdge(b.AuthorityGeneration, nativeRawEdge{Device: id, Sequence: seq, Code: 29, Down: true}); err != nil {
					t.Fatal(err)
				}
				seq++
				code = 102
			} else if mode == "plain-home" {
				code = 102
			} else if mode == "room" {
				code = 30
			}
			decision, err := a.recordNativeKeyboardEdge(b.AuthorityGeneration, nativeRawEdge{Device: id, Sequence: seq, Code: code, Down: true})
			if err != nil {
				t.Fatal(err)
			}
			a.mu.Lock()
			phase, queued := a.nativeGuest.phase, len(a.nativeGuest.queue)
			a.mu.Unlock()
			if mode == "plain-home" {
				if decision.Guest != nativeGuestBuffered || phase != nativeGuestReady || queued != 1 {
					t.Fatal("ordinary Home did not reach guest")
				}
			} else if decision.Guest != nativeGuestQuarantined || phase != nativeGuestNeedsReset || queued != 0 {
				t.Fatalf("%s leaked UI key into guest: phase %v queue %d", mode, phase, queued)
			}
		})
	}
}

func TestNativeAppBoundaryBuffersReleaseBeforeBeginAck(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	g := &nativeKeyboardGate{boundary: a, publisher: [16]byte{1}}
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	if err := g.add(gateID(1), in); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.nativeGuestBinding = nativeGuestBinding{Runtime: protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 9}, TargetID: "kit", BootID: "boot", InputAgentInstance: strings.Repeat("e", 32), LeaseGeneration: strings.Repeat("b", 64), Publisher: g.publisher, QueueEpoch: 1, AuthorityGeneration: a.nativeKeyboardGeneration}
	a.nativeGuestSnapshotAllowed = true
	a.mu.Unlock()
	ok, err := g.snapshot(func(int) ([]byte, error) { return gateCaps(42), nil }, func(int) ([]byte, error) { return gateCaps(42), nil }, func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, []*nativeInput{in}, held, true, func(Command) {})
	}, func(*nativeInput) {})
	if err != nil || !ok {
		t.Fatalf("boundary: %v %v", ok, err)
	}
	select {
	case <-a.nativeKeyboardWake:
	default:
		t.Fatal("BEGIN did not wake worker")
	}
	if handled, quit, err := g.dispatch(gateID(1), 1, 42, 0, time.Time{}); err != nil || !handled || quit {
		t.Fatalf("edge: %v %v %v", handled, quit, err)
	}
	select {
	case <-a.nativeKeyboardWake:
	default:
		t.Fatal("queued edge did not wake worker")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuestBeginRequest == nil || a.nativeGuest.phase != nativeGuestBeginning || len(a.nativeGuest.queue) != 1 || a.nativeGuest.queue[0].Down || len(a.nativeUIActions) != 0 {
		t.Fatal("release was not buffered/suppressed behind BEGIN")
	}
}

func TestNativeAppGuestQuarantinePreservesOtherUISources(t *testing.T) {
	a := NewApp(nil, 1280, 720, 20)
	defer a.Stop()
	now := time.Unix(100, 0)
	a.mu.Lock()
	a.delegatedV2Active = true
	a.nativeKeyboardGeneration = 1
	if err := a.nativeUIRaw.replace(nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: gateID(1)}, {ID: gateID(2)}}}); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	a.repeat.Down(CmdLeft, now)
	_, _ = a.repeat.DownNative([16]byte(gateID(1)), 30, CmdUp, now.Add(time.Millisecond))
	_, _ = a.repeat.DownNative([16]byte(gateID(2)), 31, CmdRight, now.Add(2*time.Millisecond))
	a.requestNativeKeyboardQuarantine(nativeFaultAuthority)
	if a.repeat.Held() != CmdRight {
		t.Fatal("guest fault canceled a trusted UI source")
	}
	a.quarantineNativeKeyboardUIOnly(gateID(2))
	if a.repeat.Held() != CmdUp {
		t.Fatal("device desync canceled another keyboard or pad")
	}
	a.quarantineNativeKeyboardUIOnly(gateID(1))
	if a.repeat.Held() != CmdLeft {
		t.Fatal("device desync canceled pad")
	}
}

func TestNativeMissingRuntimeIdentityShowsUnavailableAndKeepsStop(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	a.session.RuntimeInput = nil
	if _, err := a.nativeKeyboardBindingLocked([16]byte{1}, 1); err == nil {
		a.mu.Unlock()
		t.Fatal("missing runtime identity admitted")
	}
	if err := a.nativeUIRaw.replace(nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: gateID(1)}}}); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	if !strings.Contains(a.Snapshot().Status, kitLocalKeyboardUnavailableStatus) {
		t.Fatal("missing identity not shown as unavailable")
	}
	decision, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: nativeKeyF12, Down: true})
	if err != nil {
		t.Fatal(err)
	}
	a.handleNativeKeyboardUIOnly(gateID(1), nativeKeyF12, "f12", nativeHIDUsage(nativeKeyF12), true, decision.UIAction, time.Now())
	a.mu.Lock()
	phase := a.stopPhase
	a.mu.Unlock()
	if phase != "stopping" {
		t.Fatalf("Stop unavailable: %s", phase)
	}
}

// A session read that omits runtime_input (agent coordinator transition or
// slow runtime read) keeps keyboard and pad authority for the same play until
// the kept identity is older than the observation window.
func TestNativeInputAuthoritySurvivesRuntimeIdentityGap(t *testing.T) {
	gap := func(a *App) {
		result := a.session
		result.RuntimeInput = nil
		a.applySessionLocked(result)
		a.sessionObservedAt = time.Now() // fetchSession stamps the successful read
	}
	expire := func(a *App) { a.sessionRuntimeInputReportedAt = time.Now().Add(-runtimeInputRetention - time.Second) }
	t.Run("keyboard", func(t *testing.T) {
		a := readyNativeBindingApp(t)
		defer a.Stop()
		a.mu.Lock()
		a.sessionRuntimeInputReportedAt = time.Now()
		a.mu.Unlock()
		expected, err := a.nativeKeyboardPrepareBinding([16]byte{1})
		if err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		gap(a)
		a.mu.Unlock()
		if current, err := a.nativeKeyboardObserveBinding(); err != nil || current != expected {
			t.Fatalf("identity gap changed keyboard authority: %v", err)
		}
		a.mu.Lock()
		expire(a)
		a.mu.Unlock()
		if _, err := a.nativeKeyboardObserveBinding(); err == nil {
			t.Fatal("expired kept identity admitted the keyboard")
		}
	})
	t.Run("pad", func(t *testing.T) {
		a := readyNativeBindingApp(t)
		defer a.Stop()
		a.mu.Lock()
		defer a.mu.Unlock()
		a.kitLocalKeyboardUnavailable = true
		a.sessionRuntimeInputReportedAt = time.Now()
		before, err := a.padOnlyAuthorityLocked(time.Now(), false)
		if err != nil {
			t.Fatal(err)
		}
		gap(a)
		if after, err := a.padOnlyAuthorityLocked(time.Now(), false); err != nil || after != before {
			t.Fatalf("identity gap changed pad authority: %v", err)
		}
		expire(a)
		if _, err := a.padOnlyAuthorityLocked(time.Now(), false); err == nil {
			t.Fatal("expired kept identity admitted pad recovery")
		}
	})
}
