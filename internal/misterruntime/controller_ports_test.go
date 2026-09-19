package misterruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSetControllerUsesBoundFullSnapshot(t *testing.T) {
	responseLine := fixtureLines(t, "protocol-v2.jsonl")[5]
	fixture := newSequenceSocketFixture(t, []string{responseLine + "\n"})
	request := ControllerRequest{PackageID: fixturePackageID, Generation: 1, Port: 1, Buttons: 128, Keypad: 2048}
	if _, err := NewClient(fixture.path).SetController(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	requests := fixture.wait(t)
	if len(requests) != 1 {
		t.Fatal("incorrect request count")
	}
	raw := requests[0]
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got["operation"] != "set_controller" || got["package_id"] != fixturePackageID || got["expected_generation"] != float64(1) || got["port"] != float64(1) || got["buttons"] != float64(128) || got["keypad"] != float64(2048) || len(got) != 7 {
		t.Fatalf("request %+v", got)
	}
}

type controllerRetirementControl struct {
	Control
	status    Protocol2Response
	statusErr error
	reads     int
}

func (c *controllerRetirementControl) SetController(context.Context, ControllerRequest) (Protocol2Response, error) {
	return Protocol2Response{}, errors.New("rejected generation")
}
func (c *controllerRetirementControl) Protocol2Status(context.Context) (Protocol2Response, error) {
	c.reads++
	return c.status, c.statusErr
}

func TestControllerNeutralRetirementRequiresFreshSafeObservation(t *testing.T) {
	gen := uint64(3)
	active := Protocol2Response{OK: true, State: "running_development", Generation: &gen, ActivePackage: &Protocol2ActivePackage{PackageID: fixturePackageID}}
	for _, tc := range []struct {
		name      string
		status    Protocol2Response
		statusErr error
		buttons   uint8
		success   bool
	}{
		{"clean idle", Protocol2Response{OK: true, State: "idle"}, nil, 0, true},
		{"same generation", active, nil, 0, false},
		{"next generation", active, nil, 0, true},
		{"fault", Protocol2Response{State: "failed"}, nil, 0, false},
		{"reboot", Protocol2Response{OK: true, State: "reboot_required"}, nil, 0, false},
		{"unavailable", Protocol2Response{}, errors.New("unavailable"), 0, false},
		{"nonzero never retired", Protocol2Response{OK: true, State: "idle"}, nil, 16, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := &controllerRetirementControl{status: tc.status, statusErr: tc.statusErr}
			r := NewRuntime(control, "", 0, 0)
			generation := uint64(3)
			if tc.name == "next generation" {
				generation = 2
			}
			err := r.SetController(context.Background(), ControllerRequest{PackageID: fixturePackageID, Generation: generation, Buttons: tc.buttons})
			if (err == nil) != tc.success {
				t.Fatalf("error=%v success=%v", err, tc.success)
			}
			if tc.buttons != 0 && control.reads != 0 {
				t.Fatal("nonzero write reconciled as retired")
			}
		})
	}
}

func TestSetControllerRejectsInvalidRequestAndMismatchedGeneration(t *testing.T) {
	client := NewClient("/not/opened")
	for _, request := range []ControllerRequest{{PackageID: "bad", Generation: 1}, {PackageID: fixturePackageID}, {PackageID: fixturePackageID, Generation: 1, Port: 2}, {PackageID: fixturePackageID, Generation: 1, Keypad: 4096}} {
		if _, err := client.SetController(context.Background(), request); err != errInvalidRuntimeRequest {
			t.Fatalf("invalid %+v: %v", request, err)
		}
	}
	line := strings.Replace(fixtureLines(t, "protocol-v2.jsonl")[5], `"generation":1`, `"generation":2`, 1)
	fixture := newSequenceSocketFixture(t, []string{line + "\n"})
	if _, err := NewClient(fixture.path).SetController(context.Background(), ControllerRequest{PackageID: fixturePackageID, Generation: 1}); err != errInvalidRuntimeResponse {
		t.Fatalf("wrong identity reply %v", err)
	}
}
