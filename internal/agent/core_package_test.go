package agent_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type packageRuntime struct {
	fakeRuntime
	activation misterruntime.CoreActivation
	attempted  bool
	packageErr *protocol.APIError
	body       string
	calls      int
}

type confirmingPackageRuntime struct {
	packageRuntime
	idle           bool
	confirmCalls   int
	confirmContext context.Context
}

func (r *confirmingPackageRuntime) ConfirmIdle(ctx context.Context) bool {
	r.confirmCalls++
	r.confirmContext = ctx
	return r.idle
}

func (r *packageRuntime) LoadCoreOwned(_ context.Context, _ context.Context, _ context.Context, size int64, body io.Reader) (misterruntime.CoreActivation, bool, *protocol.APIError) {
	r.calls++
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size {
		return misterruntime.CoreActivation{}, false, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "bad package"}
	}
	r.body = string(data)
	return r.activation, r.attempted, r.packageErr
}

func activeGameStatus() protocol.Status {
	game, system, coreName := "game", protocol.SystemMegaDrive, "MegaDrive"
	return protocol.Status{State: protocol.StateActive, GameID: &game, System: &system, ExpectedCore: &coreName, ObservedCore: &coreName}
}

func TestCoordinatorPreservesActiveSessionForPreMutationPackageFailure(t *testing.T) {
	runtime := &packageRuntime{fakeRuntime: fakeRuntime{reconciled: activeGameStatus()}, packageErr: &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "invalid", Phase: "admission"}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), 0, 0)
	coordinator.Initialize(context.Background())
	before := coordinator.Status()
	status, apiErr := coordinator.LoadCore(context.Background(), 3, strings.NewReader("bad"))
	if apiErr == nil || runtime.calls != 1 || status.State != before.State || status.GameID == nil || *status.GameID != *before.GameID || status.LastError != nil {
		t.Fatalf("status=%#v err=%#v calls=%d", status, apiErr, runtime.calls)
	}
}

func TestCoordinatorPublishesCustomGenerationAndCapabilitiesAfterSuccess(t *testing.T) {
	runtime := &packageRuntime{fakeRuntime: fakeRuntime{health: protocol.Health{Ready: true}}, attempted: true,
		activation: misterruntime.CoreActivation{
			PackageID:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.simple-game", Major: 1}, Build: corepackage.Build{ID: "0123456789abcdef0123456789abcdef"}},
			Generation: 7, ObservedCore: "fes.pong", Gamepad: true,
			ActiveInterfaces: []misterruntime.Protocol2Interface{{ID: "fes.gamepad", Major: 1}},
		},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), 0, 0)
	status, apiErr := coordinator.LoadCore(context.Background(), 7, strings.NewReader("package"))
	if apiErr != nil || status.State != protocol.StateActive || !status.Development || status.CorePackage == nil {
		t.Fatalf("status=%#v err=%#v", status, apiErr)
	}
	if status.CorePackage.PackageID != runtime.activation.PackageID || status.CorePackage.Generation != 7 ||
		!status.CorePackage.Gamepad || status.ObservedCore == nil || *status.ObservedCore != "fes.pong" {
		t.Fatalf("core package status=%#v", status.CorePackage)
	}
}

func TestCoordinatorPublishesPostMutationPackageFailureMetadata(t *testing.T) {
	runtime := &packageRuntime{attempted: true, packageErr: &protocol.APIError{
		Code: protocol.CodeMiSTerUnavailable, Message: "identity mismatch", Phase: "identity",
		Expected: "0123", Observed: "4567",
	}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), 0, 0)
	status, apiErr := coordinator.LoadCore(context.Background(), 7, strings.NewReader("package"))
	if apiErr == nil || status.State != protocol.StateFailed || !status.Development || status.LastError == nil ||
		status.LastError.Phase != "identity" || status.LastError.Expected != "0123" || status.LastError.Observed != "4567" {
		t.Fatalf("status=%#v err=%#v", status, apiErr)
	}
}

func TestCoordinatorPublishesPostMutationPackageFailureOnlyAfterConfirmedIdle(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfirmed", true: "confirmed"}[confirmed], func(t *testing.T) {
			operation := context.WithValue(context.Background(), "scope", "operation")
			request := context.WithValue(context.Background(), "scope", "request")
			runtime := &confirmingPackageRuntime{idle: confirmed, packageRuntime: packageRuntime{
				attempted: true, packageErr: &protocol.APIError{
					Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
					Expected: "0123", Observed: "4567",
				},
			}}
			coordinator := agent.New(runtime, core.DefaultRegistry(), 0, 0, agent.WithOperationContext(operation))
			status, apiErr := coordinator.LoadCore(request, 7, strings.NewReader("package"))
			if apiErr == nil || runtime.confirmCalls != 1 || status.LastError == nil ||
				status.LastError.Code != protocol.CodeUnrecognizedCore || status.LastError.Phase != "identity" ||
				status.LastError.Expected != "0123" || status.LastError.Observed != "4567" ||
				runtime.confirmContext.Value("scope") != "operation" {
				t.Fatalf("status=%#v err=%#v confirms=%d", status, apiErr, runtime.confirmCalls)
			}
			if confirmed && (status.State != protocol.StateIdle || status.Development) {
				t.Fatalf("confirmed idle status=%#v", status)
			}
			if !confirmed && (status.State != protocol.StateFailed || !status.Development) {
				t.Fatalf("unconfirmed failure status=%#v", status)
			}
		})
	}
}
