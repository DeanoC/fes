package fogcast

import (
	"context"
	"errors"
	"testing"

	"net/http"

	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

type acquiringMeshClient struct {
	fakeServiceClient
	claims int
	err    error
}

func (c *acquiringMeshClient) AcquireContentPullLease(context.Context) error {
	c.claims++
	if c.err != nil {
		return c.err
	}
	c.meshLeaseGeneration = "gen-acquired"
	return nil
}

type releasingMeshClient struct {
	acquiringMeshClient
	releases int
}

func (c *releasingMeshClient) ReleaseContentPullLease(context.Context) error {
	c.releases++
	c.meshLeaseGeneration = ""
	return nil
}

func TestFreshFPGALaunchAcquiresOnContentPull(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	client := &acquiringMeshClient{}
	service.targetClients["dev"] = client
	service.connection = TargetConnection{
		State: "ready", TargetID: "node-a",
		leaseSeen: true, leaseOwned: false, leaseGeneration: "free-gen",
	}
	service.catalog = &fakeServiceCatalog{gameErr: errors.New("stop after ensure")}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeInternal {
		t.Fatalf("err %v", err)
	}
	if client.claims != 1 || client.meshLeaseGeneration != "gen-acquired" {
		t.Fatalf("claims %d generation %q", client.claims, client.meshLeaseGeneration)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

func TestFreshFPGALaunchClaimFailureDoesNotPull(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{meshcontent.SumSHA256([]byte("source-rom")).String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	client := &acquiringMeshClient{err: &protocol.APIError{Code: "KIT_LEASE_BUSY", Message: "KIT_LEASE_BUSY"}}
	service.targetClients["dev"] = client
	service.connection = TargetConnection{State: "ready", TargetID: "node-a", leaseSeen: true}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
		t.Fatalf("err %v", err)
	}
	if client.claims != 1 || len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("claims %d pulls %+v links %+v", client.claims, exec.pulls, exec.links)
	}
}

func TestForeignHeldKitDoesNotAcquireOrPull(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{meshcontent.SumSHA256([]byte("source-rom")).String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	client := &acquiringMeshClient{}
	service.targetClients["dev"] = client
	service.connection = TargetConnection{State: "busy", Owner: "other-shell", TargetID: "node-a"}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
		t.Fatalf("err %v", err)
	}
	if client.claims != 0 || len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("claims %d pulls %+v links %+v", client.claims, exec.pulls, exec.links)
	}
}

func TestGenerationMismatchDoesNotAcquireOrPull(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{meshcontent.SumSHA256([]byte("source-rom")).String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	client := &acquiringMeshClient{fakeServiceClient: fakeServiceClient{meshLeaseGeneration: "gen-owned"}}
	service.targetClients["dev"] = client
	service.connection = TargetConnection{
		State: "ready", TargetID: "node-a",
		leaseSeen: true, leaseOwned: true, leaseGeneration: "other-gen",
	}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	if !errors.Is(err, meshcontent.ErrLeaseNotFree) {
		t.Fatalf("err %v", err)
	}
	if client.claims != 0 || len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("claims %d pulls %+v links %+v", client.claims, exec.pulls, exec.links)
	}
}

func TestEnsureFailureReleasesGrantClaimedInThisCall(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node: "node-a",
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	client := &releasingMeshClient{}
	service.targetClients["dev"] = client
	service.connection = TargetConnection{State: "ready", TargetID: "node-a", leaseSeen: true}
	service.catalog = &fakeServiceCatalog{gameErr: errors.New("stop after ensure")}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	if !errors.Is(err, meshcontent.ErrContentMissingNoSource) {
		t.Fatalf("err %v", err)
	}
	if client.claims != 1 || client.releases != 1 || len(exec.pulls) != 0 {
		t.Fatalf("claims %d releases %d pulls %+v", client.claims, client.releases, exec.pulls)
	}
}

func TestEnsureFailureKeepsGrantTheSessionAlreadyHeld(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node: "node-a",
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	client := &releasingMeshClient{acquiringMeshClient: acquiringMeshClient{fakeServiceClient: fakeServiceClient{meshLeaseGeneration: "gen-owned"}}}
	service.targetClients["dev"] = client
	service.connection = TargetConnection{
		State: "ready", TargetID: "node-a",
		leaseSeen: true, leaseOwned: true, leaseGeneration: "gen-owned",
	}
	service.catalog = &fakeServiceCatalog{gameErr: errors.New("stop after ensure")}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	if !errors.Is(err, meshcontent.ErrContentMissingNoSource) {
		t.Fatalf("err %v", err)
	}
	if client.claims != 0 || client.releases != 0 {
		t.Fatalf("claims %d releases %d", client.claims, client.releases)
	}
}

func TestExplicitLaunchDoesNotUseSelectedGeneration(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-b",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-b", entry)
	service.targetClients["spare"] = &fakeServiceClient{meshLeaseGeneration: "gen-b"}
	service.connection = TargetConnection{
		State: "ready", TargetID: "node-a",
		leaseSeen: true, leaseOwned: true, leaseGeneration: "gen-a",
	}
	service.catalog = &fakeServiceCatalog{gameErr: errors.New("stop after ensure")}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "spare", nil)
	if errors.Is(err, meshcontent.ErrLeaseNotFree) {
		t.Fatal("selected connection generation blocked the bound kit")
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v err %v", exec.pulls, err)
	}
}

type meshAuthClient struct {
	fakeServiceClient
}

func (meshAuthClient) AuthorizeMutation(*http.Request) error { return nil }

type meshAuthExec struct {
	*meshLaunchExecutor
	auth kitcontent.MutationAuthorizer
}

func (e *meshAuthExec) SetMutationAuthorizer(auth kitcontent.MutationAuthorizer) {
	e.auth = auth
}

func TestMeshSessionAuthorizerIsTheBoundClient(t *testing.T) {
	bound := &meshAuthClient{}
	sibling := &meshAuthClient{}
	exec := &meshAuthExec{meshLaunchExecutor: &meshLaunchExecutor{node: "kit-b"}}
	service := &Service{
		targets: []TargetConfig{
			{Name: "dev", Enabled: true, TargetID: "kit-a"},
			{Name: "other", Enabled: true, TargetID: "kit-b"},
		},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": sibling, "other": bound},
	}
	service.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "kit-b", Executor: exec})
	if exec.auth != kitcontent.MutationAuthorizer(bound) {
		t.Fatalf("authorizer %#v", exec.auth)
	}
}
