package fogcastcli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStShelfCommands(t *testing.T) {
	pkg, revision := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		io.WriteString(w, `{"game_id":"work-disk"}`)
	}))
	defer server.Close()
	const base = "/api/v1/library/atari-st-shelf"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"st-shelf"}, "GET " + base + " "},
		{[]string{"st-shelf-new", "Work disk 2", pkg}, "POST " + base + ` {"package_id":"` + pkg + `","title":"Work disk 2"}`},
		{[]string{"st-shelf-new", "Side", pkg, "80", "1", "10"}, "POST " + base + ` {"heads":1,"package_id":"` + pkg + `","sectors":10,"title":"Side","tracks":80}`},
		{[]string{"st-shelf-copy", "work-disk", "Spare"}, "POST " + base + `/work-disk/copy {"title":"Spare"}`},
		{[]string{"st-shelf-revert", "work-disk", revision}, "POST " + base + `/work-disk/revert {"expected_revision":"` + revision + `"}`},
		{[]string{"st-shelf-rename", "work-disk", "none", "Letters"}, "POST " + base + `/work-disk/rename {"expected_revision":"","title":"Letters"}`},
		{[]string{"st-shelf-delete", "work-disk", revision}, "POST " + base + `/work-disk/delete {"expected_revision":"` + revision + `"}`},
	} {
		if !validCommand(tc.args) || !strings.Contains(usageText, tc.args[0]) {
			t.Fatalf("%v is not a documented command", tc.args)
		}
		var out, stderr bytes.Buffer
		code := Run(context.Background(), append([]string{"--api", server.URL, "--json"}, tc.args...), &out, &stderr, nil)
		if code != 0 || seen[len(seen)-1] != tc.want || !strings.Contains(out.String(), "work-disk") {
			t.Fatalf("%v: code=%d sent=%q err=%s out=%s", tc.args, code, seen[len(seen)-1], &stderr, &out)
		}
	}
	sent := len(seen)
	for _, args := range [][]string{
		{"st-shelf", "extra"}, {"st-shelf-new", "Work"}, {"st-shelf-new", "Work", pkg, "80", "1"},
		{"st-shelf-copy", "work-disk"}, {"st-shelf-revert", "work-disk"}, {"st-shelf-rename", "work-disk", "Letters"}, {"st-shelf-delete", "work-disk"},
	} {
		if validCommand(args) {
			t.Fatalf("%v admitted", args)
		}
	}
	for _, args := range [][]string{
		{"st-shelf-new", "Work", pkg, "80", "one", "9"}, {"st-shelf-delete", "-bad", "none"},
	} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), append([]string{"--api", server.URL, "--json"}, args...), &out, &stderr, nil); code == 0 {
			t.Fatalf("%v succeeded", args)
		}
	}
	if len(seen) != sent {
		t.Fatalf("invalid commands reached the host: %v", seen[sent:])
	}
}
