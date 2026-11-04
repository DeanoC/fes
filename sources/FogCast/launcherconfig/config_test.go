package launcherconfig

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	token    = "0123456789abcdef0123456789abcdef"
	targetID = "67c5f4e2-d288-49bb-9049-39ecf39cf6f6"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadProvisionedConfig(t *testing.T) {
	c, err := Load(write(t, `{"api":"http://host:8789/","token":"`+token+`","target_id":"`+targetID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.API != "http://host:8789" || c.Token != token || c.TargetID != targetID {
		t.Fatalf("config = %+v", c)
	}
}

func TestLoadIgnoresRemovedGridKeys(t *testing.T) {
	_, err := Load(write(t, `{"api":"http://host:8789","token":"`+token+`","target_id":"`+targetID+`","kit_ui":"grid","theme":"neon","shelf":"recent","menu_display":true,"framebuffer":"/dev/fb0"}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	for name, body := range map[string]string{
		"endpoint path": `{"api":"http://host:8789/x","token":"` + token + `","target_id":"` + targetID + `"}`,
		"short token":   `{"api":"http://host:8789","token":"short","target_id":"` + targetID + `"}`,
		"target id":     `{"api":"http://host:8789","token":"` + token + `","target_id":"kit2"}`,
		"trailing":      `{"api":"http://host:8789","token":"` + token + `","target_id":"` + targetID + `"} {}`,
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing file accepted")
	}
}
