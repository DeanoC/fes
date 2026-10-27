package hostclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateBlankAtariStDiskPostsTheLibraryAction(t *testing.T) {
	pkg := strings.Repeat("ab", 32)
	media := strings.Repeat("cd", 32)
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/library/atari-st-disks" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"media_id":"`+media+`","size":737280,"name":"WORK","tracks":80,"heads":2,"sectors":9,"created":true,"entry":{"game_id":"atari-desk","title":"Desk","core_id":"fes.atari-st","package_id":"`+pkg+`","media_role":"disk","media_id":"`+media+`"}}`)
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	disk, err := client.CreateBlankAtariStDisk(context.Background(), BlankAtariStRequest{
		Name: "work", Bind: true, GameID: "atari-desk", ExpectedPackageID: pkg,
	})
	if err != nil || !disk.Created || disk.MediaID != media || disk.Entry == nil || disk.Entry.MediaID != media {
		t.Fatalf("disk %+v %v", disk, err)
	}
	if got["name"] != "work" || got["tracks"] != float64(80) || got["bind"] != true || got["expected_media_id"] != "" || got["expected_package_id"] != pkg {
		t.Fatalf("body %#v", got)
	}
	if _, err := client.CreateBlankAtariStDisk(context.Background(), BlankAtariStRequest{Name: "work", Tracks: 1}); err == nil {
		t.Fatal("bad geometry left the client")
	}
}
