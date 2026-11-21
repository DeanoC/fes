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

func TestShelfCommands(t *testing.T) {
	pkg, revision := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		io.WriteString(w, `{"game_id":"work-disk"}`)
	}))
	defer server.Close()
	const st, xt = "/api/v1/library/disk-shelves/fes.atari-st", "/api/v1/library/disk-shelves/fes.pcxt-dos"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"shelf", "fes.atari-st"}, "GET " + st + " "},
		{[]string{"shelf", "fes.pcxt-dos"}, "GET " + xt + " "},
		{[]string{"shelf-new", "fes.atari-st", "Work disk 2", pkg}, "POST " + st + ` {"package_id":"` + pkg + `","title":"Work disk 2"}`},
		{[]string{"shelf-new", "fes.atari-st", "Side", pkg, "80", "1", "10"}, "POST " + st + ` {"heads":1,"package_id":"` + pkg + `","sectors":10,"title":"Side","tracks":80}`},
		{[]string{"shelf-new", "fes.pcxt-dos", "Boot", pkg, "freedos"}, "POST " + xt + ` {"contents":"freedos","package_id":"` + pkg + `","title":"Boot"}`},
		{[]string{"shelf-new", "fes.pcxt-dos", "Work", pkg, "blank"}, "POST " + xt + ` {"contents":"blank","package_id":"` + pkg + `","title":"Work"}`},
		{[]string{"shelf-copy", "fes.atari-st", "work-disk", "Spare"}, "POST " + st + `/work-disk/copy {"title":"Spare"}`},
		{[]string{"shelf-revert", "fes.pcxt-dos", "work-disk", revision}, "POST " + xt + `/work-disk/revert {"expected_revision":"` + revision + `"}`},
		{[]string{"shelf-rename", "fes.atari-st", "work-disk", "none", "Letters"}, "POST " + st + `/work-disk/rename {"expected_revision":"","title":"Letters"}`},
		{[]string{"shelf-delete", "fes.pcxt-dos", "work-disk", revision}, "POST " + xt + `/work-disk/delete {"expected_revision":"` + revision + `"}`},
	} {
		if !validCommand(tc.args) || !strings.Contains(usageText, tc.args[0]+" <core-id>") {
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
		{"shelf"}, {"shelf", "fes.atari-st", "extra"}, {"shelf-new", "fes.atari-st", "Work"},
		{"shelf-new", "fes.atari-st", "Work", pkg, "80", "1"},
		{"shelf-copy", "fes.atari-st", "work-disk"}, {"shelf-revert", "fes.atari-st", "work-disk"},
		{"shelf-rename", "fes.atari-st", "work-disk", "Letters"}, {"shelf-delete", "fes.atari-st", "work-disk"},
		{"st-shelf"},
	} {
		if validCommand(args) {
			t.Fatalf("%v admitted", args)
		}
	}
	for _, args := range [][]string{
		{"shelf-new", "fes.atari-st", "Work", pkg, "80", "one", "9"}, {"shelf-new", "fes.pcxt-dos", "Work", pkg, "dos"},
		{"shelf-delete", "fes.atari-st", "-bad", "none"},
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
