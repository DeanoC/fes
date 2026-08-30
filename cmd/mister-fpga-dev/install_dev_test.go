//go:build linux && fpgadev

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

type installFixtureRunner struct {
	commandRunner
	installs, recovers, uninstalls int
	packageRoot                    string
}

type failingInstallRunner struct {
	commandRunner
	err error
}

func (r failingInstallRunner) Install(context.Context, string) error { return r.err }
func (r failingInstallRunner) Recover(context.Context) error         { return r.err }
func (r failingInstallRunner) Uninstall(context.Context) error       { return r.err }

func (r *installFixtureRunner) Install(_ context.Context, packageRoot string) error {
	r.installs++
	r.packageRoot = packageRoot
	return nil
}
func (r *installFixtureRunner) Recover(context.Context) error   { r.recovers++; return nil }
func (r *installFixtureRunner) Uninstall(context.Context) error { r.uninstalls++; return nil }

func TestInstallCommandGrammarAndRootGate(t *testing.T) {
	runner := &installFixtureRunner{}
	for _, command := range []string{"install-profile", "recover-install", "uninstall-profile"} {
		var out, errOut bytes.Buffer
		if got := runInstallCommandWithPrivilege([]string{command}, &out, &errOut, runner, func() bool { return true }); got != exitOK {
			t.Fatalf("%s exit=%d out=%q err=%q", command, got, out.String(), errOut.String())
		}
	}
	if runner.installs != 1 || runner.recovers != 1 || runner.uninstalls != 1 {
		t.Fatalf("runner calls=%#v", runner)
	}
	var out, errOut bytes.Buffer
	if got := runInstallCommandWithPrivilege([]string{"install-profile"}, &out, &errOut, runner, func() bool { return false }); got != exitUsage {
		t.Fatalf("unprivileged exit=%d", got)
	}
	_ = fpgadev.ErrRebootRequested
}

func TestInstallCommandAcceptsWrappedRebootRequest(t *testing.T) {
	runner := &installFixtureRunner{}
	// The fixture's method cannot vary its return value, so use a focused
	// runner that wraps the manager's reboot sentinel.
	wrapper := &wrappedInstallRunner{commandRunner: runner}
	var out, errOut bytes.Buffer
	if got := runInstallCommandWithPrivilege([]string{"install-profile"}, &out, &errOut, wrapper, func() bool { return true }); got != exitOK {
		t.Fatalf("exit=%d out=%q err=%q", got, out.String(), errOut.String())
	}
}

func TestInstallCommandForwardsVerifiedPackageRoot(t *testing.T) {
	runner := &installFixtureRunner{}
	var out, errOut bytes.Buffer
	if got := runInstallCommandWithPrivilege([]string{"install-profile", "--package-root", "/tmp/private-package"}, &out, &errOut, runner, func() bool { return true }); got != exitOK {
		t.Fatalf("exit=%d out=%q err=%q", got, out.String(), errOut.String())
	}
	if runner.packageRoot != "/tmp/private-package" {
		t.Fatalf("package root=%q", runner.packageRoot)
	}
}

func TestInstallCommandUnavailableIncludesFailureDetail(t *testing.T) {
	want := "package-aware install requires a verified package root"
	var out, errOut bytes.Buffer
	got := runInstallCommandWithPrivilege(
		[]string{"install-profile"},
		&out,
		&errOut,
		failingInstallRunner{err: errors.New(want + "\n\t\x00")},
		func() bool { return true },
	)
	if got != exitFailure || out.Len() != 0 {
		t.Fatalf("exit=%d out=%q err=%q", got, out.String(), errOut.String())
	}
	if detail := errOut.String(); !strings.Contains(detail, "code=unavailable detail=") || !strings.Contains(detail, want) {
		t.Fatalf("unavailable detail=%q", detail)
	} else if strings.Count(detail, "\n") != 1 || strings.ContainsAny(strings.TrimSuffix(detail, "\n"), "\r\n\t\x00") {
		t.Fatalf("unavailable detail is not one sanitized line: %q", detail)
	}
}

func TestProductionAgentStopperRequiresConclusiveAbsence(t *testing.T) {
	cases := []struct {
		name       string
		identities []fpgadev.ProcessIdentity
		scanErr    error
		wantErr    bool
	}{
		{name: "absent", wantErr: false},
		{name: "present", identities: []fpgadev.ProcessIdentity{{PID: 17}}, wantErr: true},
		{name: "ambiguous", scanErr: errors.New("process scan incomplete"), wantErr: true},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			stop := stopAgentOnlyWhenAbsent(fakeAgentObserver{identities: test.identities, err: test.scanErr})
			err := stop(context.Background())
			if (err != nil) != test.wantErr {
				t.Fatalf("stop error=%v wantErr=%t", err, test.wantErr)
			}
		})
	}
	if err := stopAgentOnlyWhenAbsent(nil)(context.Background()); err == nil {
		t.Fatal("nil observer unexpectedly proved agent absence")
	}
}

type fakeAgentObserver struct {
	identities []fpgadev.ProcessIdentity
	err        error
	snapshot   func(context.Context) ([]fpgadev.ProcessIdentity, error)
}

func (o fakeAgentObserver) SnapshotContext(ctx context.Context) ([]fpgadev.ProcessIdentity, error) {
	if o.snapshot != nil {
		return o.snapshot(ctx)
	}
	return append([]fpgadev.ProcessIdentity(nil), o.identities...), o.err
}

func TestProductionAgentStopperStopsAndProvesAbsentAtUsrSbin(t *testing.T) {
	testProductionAgentStopperStopsAndProvesAbsent(t, "/usr/sbin/mister-agent")
}

func TestProductionAgentStopperActuallyStopsAndProvesRealUsrSbinProcessAbsentAfterTransientAuthorityFailure(t *testing.T) {
	if os.Getenv("FOGCAST_TEST_REAL_SBIN_AGENT") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	directory := filepath.Join(t.TempDir(), "usr", "sbin")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "mister-agent")
	if err := os.WriteFile(executable, raw, 0o700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestProductionAgentStopperActuallyStopsAndProvesRealUsrSbinProcessAbsentAfterTransientAuthorityFailure$")
	child.Env = append(os.Environ(), "FOGCAST_TEST_REAL_SBIN_AGENT=1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	observer, err := newProductionAgentPathObserver(executable)
	if err != nil {
		t.Fatalf("construct real sbin observer: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		identities, scanErr := observer.SnapshotContext(context.Background())
		if scanErr != nil {
			t.Fatalf("scan real sbin agent: %v", scanErr)
		}
		if len(identities) == 1 && identities[0].PID == child.Process.Pid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real sbin agent PID %d was not attested: %#v", child.Process.Pid, identities)
		}
		time.Sleep(10 * time.Millisecond)
	}
	targets := []recordedAgentTarget{{path: executable, observer: observer}}
	authorityErr := errors.New("legacy authority proof failed")
	authorityCalls := 0
	stopErr := stopRecordedAgents(targets, func(context.Context) error {
		authorityCalls++
		if authorityCalls == 1 {
			return authorityErr
		}
		return nil
	}, signalProductionAgent)(context.Background())
	if stopErr != nil {
		t.Fatalf("stop real sbin agent after reconciled authority proof: %v", stopErr)
	}
	if authorityCalls != 2 {
		t.Fatalf("authority calls=%d want=2", authorityCalls)
	}
	proofContext, cancelProof := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelProof()
	if err := proveRecordedAgentsAbsent(targets)(proofContext); err != nil {
		t.Fatalf("prove real sbin agent absent after reconciled stop: %v", err)
	}
	if err := child.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("wait real sbin agent: %v", err)
		}
	}
}

func TestProductionAgentStopperStillStopsAndProvesAbsentAtUsrBin(t *testing.T) {
	testProductionAgentStopperStopsAndProvesAbsent(t, "/usr/bin/mister-agent")
}

func TestProductionAgentProofStopsUsrSbinRespawnBetweenCallbacks(t *testing.T) {
	original := fpgadev.ProcessIdentity{PID: 643, StartTime: 17, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	replacement := fpgadev.ProcessIdentity{PID: 17646, StartTime: 29, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	population := []fpgadev.ProcessIdentity{original}
	observer := fakeAgentObserver{snapshot: func(context.Context) ([]fpgadev.ProcessIdentity, error) {
		return append([]fpgadev.ProcessIdentity(nil), population...), nil
	}}
	targets := []recordedAgentTarget{{path: productionLegacyAgent, observer: observer}}
	var signalled []fpgadev.ProcessIdentity
	stopAgent := stopRecordedAgents(targets, func(context.Context) error { return nil }, func(_ context.Context, _ recordedAgentObserver, identity fpgadev.ProcessIdentity, signal syscall.Signal) error {
		if signal != syscall.SIGTERM {
			t.Fatalf("signal=%v want=terminated", signal)
		}
		signalled = append(signalled, identity)
		population = nil
		return nil
	})
	if err := stopAgent(context.Background()); err != nil {
		t.Fatalf("initial stop: %v", err)
	}
	population = []fpgadev.ProcessIdentity{replacement}
	if err := stopAgent(context.Background()); err != nil {
		t.Fatalf("proof stop after respawn: %v", err)
	}
	if len(signalled) != 2 || signalled[0] != original || signalled[1] != replacement || len(population) != 0 {
		t.Fatalf("signalled=%#v population=%#v", signalled, population)
	}
}

func TestProductionAgentStopperStopsOrphanedFATChildAfterAuthority(t *testing.T) {
	identity := fpgadev.ProcessIdentity{PID: 645, StartTime: 22, Device: 18, Inode: 20, SHA256: strings.Repeat("b", 64)}
	present := false
	observer := fakeAgentObserver{snapshot: func(context.Context) ([]fpgadev.ProcessIdentity, error) {
		if present {
			return []fpgadev.ProcessIdentity{identity}, nil
		}
		return nil, nil
	}}
	signals := 0
	stop := stopRecordedAgents(
		[]recordedAgentTarget{{path: productionAgentFATExecutable, observer: observer}},
		func(context.Context) error {
			// Model a TERM-resistant child orphaned when its shell supervisor is
			// killed. The child appears only after authority quiescence.
			present = true
			return nil
		},
		func(_ context.Context, _ recordedAgentObserver, got fpgadev.ProcessIdentity, signal syscall.Signal) error {
			if got != identity || signal != syscall.SIGTERM {
				t.Fatalf("signal identity=%#v signal=%v", got, signal)
			}
			signals++
			present = false
			return nil
		},
	)
	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop orphaned FAT child: %v", err)
	}
	if signals != 1 {
		t.Fatalf("signals=%d want=1", signals)
	}
	if err := proveRecordedAgentsAbsent([]recordedAgentTarget{{path: productionAgentFATExecutable, observer: observer}})(context.Background()); err != nil {
		t.Fatalf("prove orphaned FAT child absent: %v", err)
	}
}

func TestProductionAgentStopperEscalatesTermResistantLeftover(t *testing.T) {
	identity := fpgadev.ProcessIdentity{PID: 643, StartTime: 17, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	present := true
	observer := fakeAgentObserver{
		snapshot: func(context.Context) ([]fpgadev.ProcessIdentity, error) {
			if present {
				return []fpgadev.ProcessIdentity{identity}, nil
			}
			return nil, nil
		},
	}
	var signals []syscall.Signal
	targets := []recordedAgentTarget{{path: "/usr/sbin/mister-agent", observer: observer}}
	err := stopRecordedAgents(targets, func(context.Context) error { return nil }, func(_ context.Context, _ recordedAgentObserver, _ fpgadev.ProcessIdentity, signal syscall.Signal) error {
		signals = append(signals, signal)
		if signal == syscall.SIGKILL {
			present = false
		}
		return nil
	})(context.Background())
	if err != nil {
		t.Fatalf("stop TERM-resistant agent: %v", err)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("signals=%v want=[terminated killed]", signals)
	}
}

func TestProductionAgentStopperTerminatesRespawnAfterFirstEmptyScan(t *testing.T) {
	original := fpgadev.ProcessIdentity{PID: 643, StartTime: 17, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	replacement := fpgadev.ProcessIdentity{PID: 17646, StartTime: 29, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	population := []fpgadev.ProcessIdentity{original}
	spawnAfterEmpty := false
	observer := fakeAgentObserver{snapshot: func(context.Context) ([]fpgadev.ProcessIdentity, error) {
		if spawnAfterEmpty && len(population) == 0 {
			spawnAfterEmpty = false
			population = []fpgadev.ProcessIdentity{replacement}
			return nil, nil
		}
		return append([]fpgadev.ProcessIdentity(nil), population...), nil
	}}
	var signalled []fpgadev.ProcessIdentity
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := stopRecordedTargetsWithGrace(ctx, []recordedAgentTarget{{path: productionLegacyAgent, observer: observer}}, func(_ context.Context, _ recordedAgentObserver, identity fpgadev.ProcessIdentity, signal syscall.Signal) error {
		if signal != syscall.SIGTERM {
			t.Fatalf("signal=%v want=terminated", signal)
		}
		signalled = append(signalled, identity)
		population = nil
		if identity == original {
			spawnAfterEmpty = true
		}
		return nil
	}, 25*time.Millisecond, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("stop replacement after empty scan: %v", err)
	}
	if len(signalled) != 2 || signalled[0] != original || signalled[1] != replacement || len(population) != 0 {
		t.Fatalf("signalled=%#v population=%#v", signalled, population)
	}
}

func testProductionAgentStopperStopsAndProvesAbsent(t *testing.T, path string) {
	t.Helper()
	identity := fpgadev.ProcessIdentity{PID: 643, StartTime: 17, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	present := true
	observer := fakeAgentObserver{
		snapshot: func(context.Context) ([]fpgadev.ProcessIdentity, error) {
			if present {
				return []fpgadev.ProcessIdentity{identity}, nil
			}
			return nil, nil
		},
	}
	authorityStopped := false
	var signals []syscall.Signal
	targets := []recordedAgentTarget{{path: path, observer: observer}}
	stop := stopRecordedAgents(targets, func(context.Context) error {
		authorityStopped = true
		return nil
	}, func(_ context.Context, _ recordedAgentObserver, got fpgadev.ProcessIdentity, signal syscall.Signal) error {
		if !authorityStopped {
			t.Fatal("agent was signalled before its boot authority stopped")
		}
		if got != identity {
			t.Fatalf("signalled identity=%#v want=%#v", got, identity)
		}
		signals = append(signals, signal)
		present = false
		return nil
	})
	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop %s: %v", path, err)
	}
	if err := proveRecordedAgentsAbsent(targets)(context.Background()); err != nil {
		t.Fatalf("prove %s absent: %v", path, err)
	}
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals=%v want=[terminated]", signals)
	}
}

func TestProductionAgentStopperFailClosedWhenLeftoverCannotBeProvenGone(t *testing.T) {
	identity := fpgadev.ProcessIdentity{PID: 643, StartTime: 17, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	tests := []struct {
		name          string
		targets       []recordedAgentTarget
		stopAuthority func(context.Context) error
		signal        recordedAgentSignaler
		proveOnly     bool
	}{
		{
			name: "initial scan error",
			targets: []recordedAgentTarget{{path: "/usr/sbin/mister-agent", observer: fakeAgentObserver{
				err: errors.New("process scan incomplete"),
			}}},
		},
		{
			name: "termination error",
			targets: []recordedAgentTarget{{path: "/usr/sbin/mister-agent", observer: fakeAgentObserver{
				identities: []fpgadev.ProcessIdentity{identity},
			}}},
			signal: func(context.Context, recordedAgentObserver, fpgadev.ProcessIdentity, syscall.Signal) error {
				return errors.New("identity-bound signal failed")
			},
		},
		{
			name: "still present after TERM and KILL",
			targets: []recordedAgentTarget{{path: "/usr/sbin/mister-agent", observer: fakeAgentObserver{
				identities: []fpgadev.ProcessIdentity{identity},
			}}},
		},
		{
			name: "final proof scan error",
			targets: []recordedAgentTarget{{path: "/usr/sbin/mister-agent", observer: fakeAgentObserver{
				err: errors.New("process scan incomplete"),
			}}},
			proveOnly: true,
		},
		{name: "incomplete path proof", targets: []recordedAgentTarget{{path: "/usr/sbin/mister-agent"}}},
		{
			name:    "boot authority stop failed",
			targets: []recordedAgentTarget{{path: "/usr/sbin/mister-agent", observer: fakeAgentObserver{}}},
			stopAuthority: func(context.Context) error {
				return errors.New("legacy supervisor remains present")
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if test.proveOnly {
				if err := proveRecordedAgentsAbsent(test.targets)(context.Background()); err == nil {
					t.Fatal("inconclusive proof unexpectedly succeeded")
				}
				return
			}
			stopAuthority := test.stopAuthority
			if stopAuthority == nil {
				stopAuthority = func(context.Context) error { return nil }
			}
			signal := test.signal
			if signal == nil {
				signal = func(context.Context, recordedAgentObserver, fpgadev.ProcessIdentity, syscall.Signal) error {
					return nil
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := stopRecordedAgents(test.targets, stopAuthority, signal)(ctx); err == nil {
				t.Fatal("inconclusive stop unexpectedly succeeded")
			}
		})
	}
}

func TestProductionAgentStopperUsesOneStableWindowAcrossBinAndSbin(t *testing.T) {
	binScans := 0
	binRespawnObserved := false
	bin := fakeAgentObserver{snapshot: func(context.Context) ([]fpgadev.ProcessIdentity, error) {
		binScans++
		if binScans == 2 {
			binRespawnObserved = true
			return []fpgadev.ProcessIdentity{{PID: 77}}, nil
		}
		return nil, nil
	}}
	sbin := fakeAgentObserver{}
	targets := []recordedAgentTarget{
		{path: productionAgentExecutable, observer: bin},
		{path: productionLegacyAgent, observer: sbin},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := waitRecordedTargetsStableAbsent(ctx, targets, 25*time.Millisecond); err != nil {
		t.Fatalf("composite stable absence: %v", err)
	}
	if !binRespawnObserved || binScans < 4 {
		t.Fatalf("bin scans=%d respawn=%t; stable timer did not reset across both paths", binScans, binRespawnObserved)
	}
}

func TestProductionAgentTargetsCoverBinAndMissingSbin(t *testing.T) {
	targets, err := productionAgentTargets()
	if err != nil {
		t.Fatalf("construct production targets with missing executables: %v", err)
	}
	if len(targets) != 3 || targets[0].path != productionAgentExecutable || targets[1].path != productionLegacyAgent || targets[2].path != productionAgentFATExecutable {
		t.Fatalf("targets=%#v", targets)
	}
	for _, target := range targets {
		if target.observer == nil {
			t.Fatalf("target %s has no proof observer", target.path)
		}
	}
}

func TestProductionAgentPathObserverRetriesProcessExitDuringDiscovery(t *testing.T) {
	attempts := 0
	observer := &productionAgentPathObserver{
		path: "/usr/sbin/mister-agent",
		scanAttempt: func(context.Context) ([]fpgadev.ProcessIdentity, bool, error) {
			attempts++
			return nil, attempts < 3, nil
		},
	}
	identities, err := observer.SnapshotContext(context.Background())
	if err != nil || len(identities) != 0 || attempts != 3 {
		t.Fatalf("transient exit identities=%v attempts=%d err=%v", identities, attempts, err)
	}
}

func TestProductionAgentPathObserverFindsDeletedExecutable(t *testing.T) {
	if os.Getenv("FOGCAST_TEST_DELETED_AGENT") == "1" {
		select {}
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "mister-agent")
	if err := os.WriteFile(executable, raw, 0o700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestProductionAgentPathObserverFindsDeletedExecutable$")
	child.Env = append(os.Environ(), "FOGCAST_TEST_DELETED_AGENT=1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	time.Sleep(20 * time.Millisecond)
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	observer, err := newProductionAgentPathObserver(executable)
	if err != nil {
		t.Fatalf("construct deleted-path observer: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		identities, scanErr := observer.SnapshotContext(context.Background())
		if scanErr != nil {
			t.Fatalf("scan deleted executable: %v", scanErr)
		}
		for _, identity := range identities {
			if identity.PID == child.Process.Pid {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("deleted executable PID %d was not observed", child.Process.Pid)
}

func TestProductionAgentAuthorityStopsRealShellScriptSupervisor(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "mister-supervise")
	agent := filepath.Join(directory, "mister-agent")
	config := filepath.Join(directory, "agent.toml")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.05; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	observer, err := newScriptSupervisorObserver(script, []string{"mister-agent", agent, "--config", config})
	if err != nil {
		t.Fatalf("construct script supervisor observer: %v", err)
	}
	child := exec.Command(script, "mister-agent", agent, "--config", config)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		identities, scanErr := observer.SnapshotContext(context.Background())
		if scanErr != nil {
			t.Fatalf("scan shell supervisor: %v", scanErr)
		}
		if len(identities) == 1 && identities[0].PID == child.Process.Pid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("shell supervisor PID %d was not attested: %#v", child.Process.Pid, identities)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := stopRecordedTargetsWithGrace(context.Background(), []recordedAgentTarget{{path: script, observer: observer}}, signalProductionAgent, 250*time.Millisecond, 25*time.Millisecond); err != nil {
		t.Fatalf("stop shell supervisor: %v", err)
	}
	if err := child.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("wait shell supervisor: %v", err)
		}
	}
}

func TestProductionAgentAuthorityAcceptsOnlyRetainedImageInvocations(t *testing.T) {
	observer, err := newScriptSupervisorObserver(
		productionLegacySupervisor,
		[]string{"mister-agent", productionLegacyAgent, "--config", productionLegacyAgentConfig},
		[]string{"mister-agent", productionAgentFATExecutable, "--config", productionLegacyAgentConfig},
	)
	if err != nil {
		t.Fatalf("construct production supervisor observer: %v", err)
	}
	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{name: "legacy rootfs agent", argv: []string{"/bin/sh", productionLegacySupervisor, "mister-agent", productionLegacyAgent, "--config", productionLegacyAgentConfig}, want: true},
		{name: "current FAT agent", argv: []string{"/bin/sh", productionLegacySupervisor, "mister-agent", productionAgentFATExecutable, "--config", productionLegacyAgentConfig}, want: true},
		{name: "successor config is not legacy authority", argv: []string{"/bin/sh", productionLegacySupervisor, "mister-agent", productionLegacyAgent, "--config", "/etc/fogcast/agent.toml"}},
		{name: "unrecognized child", argv: []string{"/bin/sh", productionLegacySupervisor, "mister-agent", "/tmp/mister-agent", "--config", productionLegacyAgentConfig}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := observer.matches(test.argv); got != test.want {
				t.Fatalf("matches(%q)=%t want=%t", test.argv, got, test.want)
			}
		})
	}
}

func TestProductionAgentAuthorityFailsClosedForUnrecognizedInvocation(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "mister-supervise")
	knownAgent := filepath.Join(directory, "known-agent")
	unknownAgent := filepath.Join(directory, "unknown-agent")
	config := filepath.Join(directory, "agent.toml")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.05; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	observer, err := newScriptSupervisorObserver(script, []string{"mister-agent", knownAgent, "--config", config})
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(script, "mister-agent", unknownAgent, "--config", config)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	if _, err := observer.SnapshotContext(context.Background()); err == nil || !strings.Contains(err.Error(), "invocation is not recognized") {
		t.Fatalf("unrecognized authority error=%v", err)
	}
}

func TestProductionAgentAuthorityFailsClosedWithoutInterpreterIdentity(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "mister-supervise")
	agent := filepath.Join(directory, "mister-agent")
	config := filepath.Join(directory, "agent.toml")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.05; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	observer, err := newScriptSupervisorObserver(script, []string{"mister-agent", agent, "--config", config})
	if err != nil {
		t.Fatal(err)
	}
	observer.interpreter = fakeAgentObserver{}
	child := exec.Command(script, "mister-agent", agent, "--config", config)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	if _, err := observer.SnapshotContext(context.Background()); err == nil || !strings.Contains(err.Error(), "interpreter identity is unavailable") {
		t.Fatalf("missing interpreter identity error=%v", err)
	}
}

func TestProductionAgentAuthorityStopsReplacementSupervisor(t *testing.T) {
	original := fpgadev.ProcessIdentity{PID: 643, StartTime: 17, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	replacement := fpgadev.ProcessIdentity{PID: 644, StartTime: 21, Device: 18, Inode: 19, SHA256: strings.Repeat("a", 64)}
	population := []fpgadev.ProcessIdentity{original}
	observer := fakeAgentObserver{snapshot: func(context.Context) ([]fpgadev.ProcessIdentity, error) {
		return append([]fpgadev.ProcessIdentity(nil), population...), nil
	}}
	var signalled []fpgadev.ProcessIdentity
	err := stopRecordedTargetsWithGrace(context.Background(), []recordedAgentTarget{{path: "/usr/sbin/mister-supervise", observer: observer}}, func(_ context.Context, _ recordedAgentObserver, identity fpgadev.ProcessIdentity, signal syscall.Signal) error {
		signalled = append(signalled, identity)
		switch {
		case identity == original && signal == syscall.SIGTERM:
			population = []fpgadev.ProcessIdentity{replacement}
		case identity == replacement && signal == syscall.SIGKILL:
			population = nil
		}
		return nil
	}, 25*time.Millisecond, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("stop replacement supervisor: %v", err)
	}
	if len(signalled) != 3 || signalled[0] != original || signalled[1] != replacement || signalled[2] != replacement {
		t.Fatalf("signalled=%#v, want TERM original then TERM/KILL replacement", signalled)
	}
}

// TestInitializeTaggedInstallCommandsUseConcreteManager exercises the tagged
// command dispatcher against the real journal/lock/source manager.  The
// command boundary is injected with a manager only so the test never invokes
// a target reboot or an ARM-only protected path; all three operations still
// execute the concrete InstallManager state machine and source plan.
func TestInitializeTaggedInstallCommandsUseConcreteManager(t *testing.T) {
	fixture := newConcreteInstallCommandFixture(t)
	runner := &concreteInstallCommandRunner{manager: fixture.manager}

	for _, command := range []string{"install-profile", "recover-install", "uninstall-profile"} {
		var out, errOut bytes.Buffer
		if got := runInstallCommandWithPrivilege([]string{command}, &out, &errOut, runner, func() bool { return true }); got != exitOK {
			t.Fatalf("%s exit=%d out=%q err=%q", command, got, out.String(), errOut.String())
		}
		want := "FOGCAST_FPGA_DEV_INSTALL command=" + command + " code=ok\n"
		if out.String() != want {
			t.Fatalf("%s output=%q want=%q", command, out.String(), want)
		}
		if errOut.Len() != 0 {
			t.Fatalf("%s stderr=%q", command, errOut.String())
		}
	}

	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != fpgadev.InstallStateRestored {
		t.Fatalf("final journal=%#v exists=%v err=%v", record, exists, err)
	}
	for path, want := range map[string][]byte{
		fixture.dispatcher: fixture.dispatcherOriginal,
		fixture.legacy:     fixture.legacyOriginal,
	} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("restored source %s=%q err=%v want=%q", path, got, readErr, want)
		}
	}
	if _, err := os.Lstat(fixture.supervisor); !os.IsNotExist(err) {
		t.Fatalf("supervisor remains after uninstall: %v", err)
	}
}

type concreteInstallCommandRunner struct {
	commandRunner
	manager *fpgadev.InstallManager
}

func (r *concreteInstallCommandRunner) Install(ctx context.Context, packageRoot string) error {
	return r.manager.Install(ctx, packageRoot)
}

func (r *concreteInstallCommandRunner) Recover(ctx context.Context) error {
	return r.manager.Recover(ctx)
}

func (r *concreteInstallCommandRunner) Uninstall(ctx context.Context) error {
	return r.manager.Uninstall(ctx)
}

type concreteInstallCommandFixture struct {
	manager                            *fpgadev.InstallManager
	dispatcher, legacy, supervisor     string
	dispatcherOriginal, legacyOriginal []byte
}

func newConcreteInstallCommandFixture(t *testing.T) concreteInstallCommandFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	supervisor := filepath.Join(root, "supervisor")
	dispatcherOriginal := []byte("dispatcher-original\n")
	legacyOriginal := []byte("legacy-original\n")
	for path, data := range map[string][]byte{dispatcher: dispatcherOriginal, legacy: legacyOriginal} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backupDir := filepath.Join(root, "backups")
	sources, err := fpgadev.CaptureStartSourceInventory([]string{dispatcher, legacy}, backupDir, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := fpgadev.NewSourceMutationPlan(sources, backupDir)
	plan.Supervisor = supervisor
	plan.ExpectedUID = uid

	mainPath := filepath.Join(root, "Main_MiSTer")
	mainBytes := []byte("main\n")
	if err := os.WriteFile(mainPath, mainBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}
	mainDigest := sha256.Sum256(mainBytes)
	main := fpgadev.PathExpectation{Path: mainPath, Kind: "regular", SHA256: hex.EncodeToString(mainDigest[:])}
	fifo := fpgadev.PathExpectation{Path: fifoPath, Kind: "fifo"}
	inventory := fpgadev.InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}

	owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	if err := owner.Replace(hardwareowner.Record{
		Schema: 1, State: hardwareowner.StateNormalMain, BootID: concreteInstallCommandBootID,
		GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1,
		ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain,
		ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone,
		CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone,
		RequestedResources: []string{}, FirstFailure: "",
	}); err != nil {
		t.Fatal(err)
	}
	manager := &fpgadev.InstallManager{
		Journal:       fpgadev.NewInstallJournal(filepath.Join(root, "journal.json"), uid),
		InstallLocker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid),
		OwnerStore:    owner,
		OwnerLocker:   hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		BootID:        func() (string, error) { return concreteInstallCommandBootID, nil },
		PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64),
		Inventory: inventory, Sources: sources, SourcePlan: plan,
		MainReadiness:    func(context.Context) error { return nil },
		StopAgent:        func(context.Context) error { return nil },
		ProveAgentAbsent: func(context.Context) error { return nil },
	}
	manager.ExecSupervisor = func(context.Context, int) error { return nil }
	manager.ChainOriginal = func(context.Context) error { return nil }
	return concreteInstallCommandFixture{
		manager: manager, dispatcher: dispatcher, legacy: legacy, supervisor: supervisor,
		dispatcherOriginal: dispatcherOriginal, legacyOriginal: legacyOriginal,
	}
}

const concreteInstallCommandBootID = "12345678-1234-1234-1234-123456789abc"

type wrappedInstallRunner struct{ commandRunner }

func (r *wrappedInstallRunner) Install(context.Context, string) error {
	return fmt.Errorf("install reboot: %w", fpgadev.ErrRebootRequested)
}
func (r *wrappedInstallRunner) Recover(context.Context) error   { return nil }
func (r *wrappedInstallRunner) Uninstall(context.Context) error { return nil }
