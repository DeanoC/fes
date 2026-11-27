// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
)

const testProgramBootID = "4facd3fd-7bd9-46a7-a149-a0f7c27b9e0d"

func TestProgramTargetFollowsLocalLaunchReturnAndPark(t *testing.T) {
	f, backend, client := backendFixture(t)
	ctx := context.Background()
	target := programTarget{f.s, f.s.Status().Owner, testProgramBootID}
	lease := programKitLease{target}
	initial := lease.Status()
	boot := target.Health("target")
	if !boot.Ready || boot.BootID != testProgramBootID || boot.APIVersion != "v1" || target.Status().State != protocol.StateIdle || initial.State != "held" || initial.Owner != "native-tenfoot" {
		t.Fatalf("initial observation: %+v %+v", boot, initial)
	}
	otherBoot := programTarget{f.s, target.owner, "9820cfa0-46e3-40d5-b25b-958defcc5f99"}
	if otherBoot.Health("target").BootID == boot.BootID || (programKitLease{otherBoot}).Status().Generation == initial.Generation {
		t.Fatal("a reboot with a repeated firmware owner reused the health or lease identity")
	}
	running, err := client.Launch(ctx, backend.entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := target.Status(); got.State != protocol.StateActive || got.CorePackage == nil ||
		got.CorePackage.PackageID != running.CorePackage.PackageID || got.ObservedCore == nil || *got.ObservedCore != running.CorePackage.CoreID ||
		got.CorePackage.Generation != f.s.Status().Generation || lease.Status().Generation == initial.Generation || target.Health("target").BootID != boot.BootID {
		t.Fatalf("running observation: %+v", got)
	}
	before := f.cpu.calls
	if _, denied := target.Stop(ctx); denied == nil || denied.Code != protocol.CodeKitLeaseDenied || f.cpu.calls != before || target.Status().State != protocol.StateActive {
		t.Fatal("observation client displaced local core")
	}
	if _, err := client.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if target.Status().State != protocol.StateIdle || lease.Status().State != "held" || lease.Status().Generation == initial.Generation {
		t.Fatal("return lost the local lease")
	}
	if _, err := client.Launch(ctx, backend.entries[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Park(ctx, backend.session); err != nil {
		t.Fatal(err)
	}
	if target.Health("target").Ready || target.Status().State != protocol.StateFailed || lease.Status().State != "blocked" {
		t.Fatal("parked target advertised availability")
	}
}

func TestProgramTargetRequiresControlledNetworkAndTrustedBoot(t *testing.T) {
	f := serviceTest(t)
	owner := f.s.Status().Owner
	if _, err := f.s.NewTargetService(nil, agentapi.Provisioning{}, owner, testProgramBootID, nil); !errors.Is(err, ErrProgramSession) {
		t.Fatal("target attempted construction without controlled Ethernet", err)
	}
	nic, controller := newProgramNetwork(t)
	if err := f.s.AttachNetwork(controller); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.NewTargetService(nil, agentapi.Provisioning{}, owner+1, testProgramBootID, nil); !errors.Is(err, ErrProgramSession) {
		t.Fatal("target accepted a foreign boot", err)
	}
	cfg := agentapi.Provisioning{TargetID: "63e03f44-8b14-4d04-bea4-1f0707bb00f7", Token: "controller-secret", Port: 8181}
	endpoint := agentapi.NewLinkEndpoint(nic.link, "02:00:00:00:00:02")
	svc, err := f.s.NewTargetService(endpoint, cfg, owner, testProgramBootID, nil)
	if err != nil {
		t.Fatal(err)
	}
	observation := svc.DiagnosticsSnapshot()
	if observation.TargetID != cfg.TargetID || observation.Addr != "192.0.2.2:8181" || !observation.MDNS.Enabled {
		t.Fatalf("target did not bind the configured pair to its interface: %+v", observation)
	}
	foreign := programTarget{f.s, owner + 1, testProgramBootID}
	if foreign.Health("target").Ready || foreign.Status().State != protocol.StateFailed || (programKitLease{foreign}).Status().State != "blocked" {
		t.Fatal("foreign observation escaped containment")
	}
	if err := f.s.Retire(errors.New("lost ownership")); !errors.Is(err, ErrProgramRetired) {
		t.Fatal(err)
	}
	target := programTarget{f.s, owner, testProgramBootID}
	if target.Health("target").Ready || target.Status().State != protocol.StateFailed || (programKitLease{target}).Status().State != "recovery-required" {
		t.Fatal("retired target advertised an available lease")
	}
}

func TestProgramStartupReadsOptionalAgentUnderTheSDLease(t *testing.T) {
	for _, tc := range []struct {
		name    string
		record  string
		invalid bool
	}{
		{"absent", "", false},
		{"provisioned", `{"target_id":"63e03f44-8b14-4d04-bea4-1f0707bb00f7","token":"controller-secret","port":8181}`, false},
		{"malformed", `{"token":"controller-secret"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
				payload := launcherStaging(t, f)
				if tc.record != "" {
					payload[agentapi.ProvisioningPath] = []byte(tc.record)
				}
				for name, data := range payload {
					f.r.f.files.files[name] = &fstest.MapFile{Data: data}
				}
				startup, err := ReadProgramStartup(context.Background(), f.r.f.store, true, true)
				if tc.invalid {
					if !errors.Is(err, agentapi.ErrInvalidConfig) || startup != nil {
						t.Fatal("accepted malformed provisioning", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if (startup.Agent != nil) != (tc.record != "") {
					t.Fatal("wrong optional agent selection")
				}
				if startup.Agent != nil && (startup.Agent.Port != 8181 || startup.Agent.Token != "controller-secret") {
					t.Fatal("wrong SD agent provisioning")
				}
			})
		})
	}
}
