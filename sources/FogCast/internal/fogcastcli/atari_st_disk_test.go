package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateBlankStDiskCommand(t *testing.T) {
	pkg := strings.Repeat("ab", 32)
	media := strings.Repeat("cd", 32)
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/library/atari-st-disks" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"media_id":"`+media+`","size":409600,"name":"SIDE","tracks":80,"heads":1,"sectors":10,"created":true,"entry":{"game_id":"atari-desk","package_id":"`+pkg+`","media_role":"disk","media_id":"`+media+`"}}`)
	}))
	defer server.Close()
	if !validCommand([]string{"create-blank-st-disk", "side", "80", "1", "10", "bind", "atari-desk", pkg, "none"}) || validCommand([]string{"create-blank-st-disk"}) {
		t.Fatal("arity")
	}
	if !strings.Contains(usageText, "create-blank-st-disk ") {
		t.Fatal("usage omits blank disk")
	}
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--api", server.URL, "--json", "create-blank-st-disk", "side", "80", "1", "10", "bind", "atari-desk", pkg, "none"}, &out, &stderr, nil)
	if code != 0 || got["name"] != "side" || got["tracks"] != float64(80) || got["heads"] != float64(1) || got["sectors"] != float64(10) || got["expected_media_id"] != "" || got["bind"] != true {
		t.Fatalf("code=%d body=%#v err=%s out=%s", code, got, &stderr, &out)
	}
	if !strings.Contains(out.String(), media) {
		t.Fatalf("output %s", &out)
	}
}
