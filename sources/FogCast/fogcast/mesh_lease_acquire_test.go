package fogcast

import (
	"context"
	"errors"
	"testing"

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
	client := &acquiringMeshClient{err: errors.New("kit held")}
	service.targetClients["dev"] = client
	service.connection = TargetConnection{State: "ready", TargetID: "node-a", leaseSeen: true}

	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	if err == nil || err.Error() != "kit held" {
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
