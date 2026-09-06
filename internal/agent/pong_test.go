package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/protocol"
)

func TestPongUsesOrdinaryDirectCoordinatorLaunch(t *testing.T) {
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "Pong"}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	status, err := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "pong", System: protocol.SystemPong})
	if err != nil || status.State != protocol.StateActive || status.System == nil || *status.System != protocol.SystemPong || status.ObservedCore == nil || *status.ObservedCore != "Pong" {
		t.Fatalf("launch = %#v %v", status, err)
	}
	if runtime.preparePath != "" || runtime.prepareSpec.ExpectedCore != "Pong" || runtime.launchCalls != 1 {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestPongRejectsCachedLaunchBeforeContentResolution(t *testing.T) {
	coordinator := agent.New(&contentRuntime{}, core.DefaultRegistry(), time.Second, time.Second)
	// A ROMless launch must never dereference a content store.
	controller := agent.NewContentController(coordinator, nil)
	_, err := controller.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "pong", System: protocol.SystemPong, Content: protocol.ContentIdentity{SHA256: cachedDigest, Size: 1, Extension: "bin"}})
	if err == nil || err.Code != protocol.CodeBadRequest {
		t.Fatalf("cached Pong = %v", err)
	}
}
