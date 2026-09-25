package misterruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

func compositionResponse(t *testing.T) (Protocol2Response, expansion.Composition) {
	t.Helper()
	var r Protocol2Response
	if err := json.Unmarshal([]byte(fixtureLines(t, "protocol-v2.jsonl")[5]), &r); err != nil {
		t.Fatal(err)
	}
	active := r.ActivePackage
	active.Descriptor.ABI.ID = "fes.simple-computer"
	active.Observed.ABI.ID = "fes.simple-computer"
	active.Descriptor.Interfaces = append(active.Descriptor.Interfaces, corepackage.Interface{ID: "fes.expansion.zx81-bus", Major: 1, Minor: 0, Required: false})
	for i := range r.Capabilities.ABIs {
		if r.Capabilities.ABIs[i].ID == "fes.simple-game" {
			r.Capabilities.ABIs[i].ID = "fes.simple-computer"
		}
	}
	c := expansion.Composition{PackageID: active.PackageID, ExpansionID: strings.Repeat("c", 64), ShellSHA256: active.Descriptor.Payload.SHA256, PayloadSHA256: strings.Repeat("d", 64), PayloadSize: 40408}
	c.ID, _ = expansion.CompositionID(c.PackageID, c.ExpansionID, c.PayloadSHA256)
	active.Composition = &c
	return r, c
}

func TestColecoComposedStatusRequiresMatchingOptionalBus(t *testing.T) {
	response, _ := compositionResponse(t)
	response.ActivePackage.Descriptor.ABI.ID = "fes.application"
	response.ActivePackage.Observed.ABI.ID = "fes.application"
	response.Capabilities.ABIs[0].ID = "fes.application"
	response.ActivePackage.Descriptor.Interfaces[2].ID = "fes.expansion.coleco-bus"
	if !validProtocol2Response(response) {
		t.Fatal("Coleco composition rejected")
	}
	response.ActivePackage.Descriptor.Interfaces[2].ID = "fes.expansion.zx81-bus"
	if validProtocol2Response(response) {
		t.Fatal("Coleco composition accepted with ZX81 socket")
	}
	response.ActivePackage.Descriptor.Interfaces[2].ID = "fes.expansion.coleco-bus"
	response.ActivePackage.Descriptor.Interfaces = append(response.ActivePackage.Descriptor.Interfaces,
		corepackage.Interface{ID: "fes.expansion.zx81-bus", Major: 1, Minor: 0, Required: false})
	if validProtocol2Response(response) {
		t.Fatal("Coleco composition accepted with both sockets")
	}
}

func TestColecoV2ComposedStatusRequiresVersionedSocket(t *testing.T) {
	response, _ := compositionResponse(t)
	response.ActivePackage.Descriptor.ABI.ID = "fes.application"
	response.ActivePackage.Observed.ABI.ID = "fes.application"
	response.Capabilities.ABIs[0].ID = "fes.application"
	interfaceV2 := &response.ActivePackage.Descriptor.Interfaces[2]
	interfaceV2.ID, interfaceV2.Major = "fes.expansion.coleco-bus", 2
	if !validProtocol2Response(response) {
		t.Fatal("Coleco v2 composition rejected")
	}
	interfaceV2.Major = 1
	if !validProtocol2Response(response) {
		t.Fatal("existing Coleco v1 composition rejected")
	}
	interfaceV2.Major = 3
	if validProtocol2Response(response) {
		t.Fatal("unsupported Coleco socket version accepted")
	}
}

func TestComposedClientChecksExactIdentityAndShape(t *testing.T) {
	response, c := compositionResponse(t)
	for _, mode := range []string{"valid", "wrong tuple", "unknown tuple field", "missing tuple field"} {
		t.Run(mode, func(t *testing.T) {
			current := response
			active := *response.ActivePackage
			current.ActivePackage = &active
			if mode == "wrong tuple" {
				other := c
				other.PayloadSHA256 = strings.Repeat("e", 64)
				other.ID, _ = expansion.CompositionID(other.PackageID, other.ExpansionID, other.PayloadSHA256)
				active.Composition = &other
			}
			encoded, _ := json.Marshal(current)
			if mode == "unknown tuple field" {
				encoded = []byte(strings.Replace(string(encoded), `"payload_size":40408`, `"payload_size":40408,"unknown":0`, 1))
			}
			if mode == "missing tuple field" {
				encoded = []byte(strings.Replace(string(encoded), `,"payload_size":40408`, "", 1))
			}
			fixture := newSequenceSocketFixture(t, []string{fixtureLines(t, "protocol-v2.jsonl")[1] + "\n", string(encoded) + "\n"})
			_, err := NewClient(fixture.path).LoadComposedCore(context.Background(), "/base", c.PackageID, "/expansion", "/composition/linked.rbf", c)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("%s: %v", mode, err)
			}
			requests := fixture.wait(t)
			if len(requests) != 2 || !strings.Contains(requests[1], `"operation":"load_composed_core"`) || !strings.Contains(requests[1], `"composition_id":"`+c.ID+`"`) {
				t.Fatal(requests)
			}
		})
	}
}
func TestCompositionStatusAndAdoptionRequireExactTuple(t *testing.T) {
	response, c := compositionResponse(t)
	if !sameProtocol2RuntimeState(response, response) {
		t.Fatal("same state differs")
	}
	activation := activationFromProtocol2(c.PackageID, response.ActivePackage.Descriptor, response)
	status := corePackageStatus(activation)
	if status.Composition == nil || *status.Composition != c {
		t.Fatal("tuple lost")
	}
	response.ActivePackage.Composition.PayloadSize++
	if *activation.Composition != c {
		t.Fatal("activation aliases wire tuple")
	}
	staged := corepackage.Staged{PackageID: c.PackageID, Descriptor: response.ActivePackage.Descriptor, Composition: &c}
	if matchingAdoptedPackage([]corepackage.Staged{staged}, *response.ActivePackage) != -1 {
		t.Fatal("adopted differing linked payload")
	}
	response.ActivePackage.Composition = &c
	if matchingAdoptedPackage([]corepackage.Staged{staged}, *response.ActivePackage) != 0 {
		t.Fatal("matching composition not adopted")
	}
	plain := response
	copy := *response.ActivePackage
	copy.Composition = nil
	plain.ActivePackage = &copy
	if sameProtocol2RuntimeState(response, plain) {
		t.Fatal("plain and composed confused")
	}
}

type compositionStatusControl struct{ response Protocol2Response }

func (c compositionStatusControl) Protocol2Status(context.Context) (Protocol2Response, error) {
	return c.response, nil
}
func TestLostComposedResponseCannotConfirmAnotherPayload(t *testing.T) {
	response, c := compositionResponse(t)
	staged := corepackage.Staged{PackageID: c.PackageID, Descriptor: response.ActivePackage.Descriptor, Composition: cloneComposition(&c)}
	before := Protocol2Response{State: "idle"}
	r := &Runtime{}
	_, disposition := r.observeLostCoreLoad(context.Background(), compositionStatusControl{response}, staged, before)
	if disposition != coreLoadConfirmed {
		t.Fatal(disposition)
	}
	other := c
	other.PayloadSHA256 = strings.Repeat("e", 64)
	other.ID, _ = expansion.CompositionID(other.PackageID, other.ExpansionID, other.PayloadSHA256)
	response.ActivePackage.Composition = &other
	_, disposition = r.observeLostCoreLoad(context.Background(), compositionStatusControl{response}, staged, before)
	if disposition != coreLoadRecovery {
		t.Fatal("different composition falsely confirmed", disposition)
	}
	response.ActivePackage.Composition = nil
	_, disposition = r.observeLostCoreLoad(context.Background(), compositionStatusControl{response}, staged, before)
	if disposition != coreLoadRecovery {
		t.Fatal("plain shell falsely confirmed", disposition)
	}
}
