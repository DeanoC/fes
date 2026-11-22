package hostclient

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestDecodeSessionRuntimeInputRequiresCoherentObservation(t *testing.T) {
	instance, pkg := strings.Repeat("a", 32), strings.Repeat("b", 64)
	identity := &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: instance, PackageID: pkg, Generation: 4}
	decode := func(body map[string]any) (SessionResult, error) {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return DecodeSession(200, raw)
	}
	body := map[string]any{"state": "active", "core_package": map[string]any{"package_id": pkg, "generation": 4}, "runtime_input": identity}
	body["input_agent_instance"] = strings.Repeat("c", 32)
	result, err := decode(body)
	if err != nil || result.RuntimeInput == nil || *result.RuntimeInput != *identity || result.InputAgentInstance != strings.Repeat("c", 32) {
		t.Fatalf("identity=%+v err=%v", result.RuntimeInput, err)
	}
	body["input_agent_instance"] = "invalid"
	if _, err := decode(body); err == nil {
		t.Fatal("invalid agent incarnation accepted")
	}
	body["input_agent_instance"] = strings.Repeat("c", 32)
	body["core_package"] = map[string]any{"package_id": pkg, "generation": 5}
	if _, err := decode(body); err == nil {
		t.Fatal("mismatched generation accepted")
	}
	body["state"] = "idle"
	if _, err := decode(body); err == nil {
		t.Fatal("idle identity accepted")
	}
	dev := map[string]any{"state": "active", "execution": "fpga_development", "runtime_input": &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: instance, PackageID: "", Generation: 7}}
	result, err = decode(dev)
	if err != nil || result.RuntimeInput == nil || result.RuntimeInput.Kind != protocol.RuntimeInputDevelopment || result.CorePackage != nil {
		t.Fatalf("direct development identity=%+v err=%v", result.RuntimeInput, err)
	}
	delete(dev, "execution")
	if _, err := decode(dev); err == nil {
		t.Fatal("untyped empty-package session accepted")
	}
	delete(dev, "runtime_input")
	if result, err := decode(dev); err != nil || result.RuntimeInput != nil {
		t.Fatalf("missing identity not unavailable: %+v %v", result.RuntimeInput, err)
	}
}
