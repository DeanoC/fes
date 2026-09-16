package hostclient

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLibrarySettingsPatchRejectsEmptyPayload(t *testing.T) {
	t.Parallel()
	if _, err := (LibrarySettingsPatch{}).payload(); err == nil {
		t.Fatal("empty patch accepted")
	}
}

func TestLibrarySettingsPatchPayloadLibraries(t *testing.T) {
	t.Parallel()
	idle := 60
	raw, err := (LibrarySettingsPatch{AttractIdleSeconds: &idle}).payload()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["libraries"]; ok {
		t.Fatalf("nil libraries included: %#v", raw)
	}
	libraries := []LibraryRoot{{ID: "snes", System: "snes", Root: "/library/snes"}}
	raw, err = (LibrarySettingsPatch{Libraries: &libraries}).payload()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["attract_idle_seconds"]; ok {
		t.Fatalf("libraries patch included idle: %#v", raw)
	}
	if _, ok := raw["targets"]; ok {
		t.Fatalf("libraries patch included targets: %#v", raw)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"libraries":[{"id":"snes","system":"snes","root":"/library/snes"}]}` {
		t.Fatalf("full array = %s", data)
	}
	empty := []LibraryRoot{}
	raw, err = (LibrarySettingsPatch{Libraries: &empty}).payload()
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"libraries":[]}` {
		t.Fatalf("empty array = %s", data)
	}
}

func TestLibrarySettingsPatchPayloadTargets(t *testing.T) {
	t.Parallel()
	idle := 60
	raw, err := (LibrarySettingsPatch{AttractIdleSeconds: &idle}).payload()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["targets"]; ok {
		t.Fatalf("nil targets included: %#v", raw)
	}
	untouched := []LibraryTargetWrite{{
		Name:    "dev",
		Address: "http://192.0.2.10:8182",
		Enabled: true,
	}}
	raw, err = (LibrarySettingsPatch{Targets: &untouched}).payload()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["libraries"]; ok {
		t.Fatalf("targets patch included libraries: %#v", raw)
	}
	if _, ok := raw["selected_target"]; ok {
		t.Fatalf("targets patch included selected_target: %#v", raw)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"targets":[{"name":"dev","address":"http://192.0.2.10:8182","enabled":true}]}` {
		t.Fatalf("omit agent = %s", data)
	}
	if strings.Contains(string(data), `"agent"`) {
		t.Fatalf("untouched agent included: %s", data)
	}
	cleared := ""
	clear := []LibraryTargetWrite{{
		Name:    "dev",
		Address: "",
		Enabled: false,
		Agent:   &cleared,
	}}
	raw, err = (LibrarySettingsPatch{Targets: &clear}).payload()
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"targets":[{"name":"dev","address":"","enabled":false,"agent":""}]}` {
		t.Fatalf("clear agent = %s", data)
	}
	secret := "s3cret"
	set := []LibraryTargetWrite{{
		Name:         "den",
		OriginalName: "dev",
		Address:      "http://192.0.2.10:8182",
		Enabled:      true,
		Agent:        &secret,
	}}
	raw, err = (LibrarySettingsPatch{Targets: &set}).payload()
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"targets":[{"name":"den","original_name":"dev","address":"http://192.0.2.10:8182","enabled":true,"agent":"s3cret"}]}` {
		t.Fatalf("set agent = %s", data)
	}
	empty := []LibraryTargetWrite{}
	raw, err = (LibrarySettingsPatch{Targets: &empty}).payload()
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"targets":[]}` {
		t.Fatalf("empty array = %s", data)
	}
}
