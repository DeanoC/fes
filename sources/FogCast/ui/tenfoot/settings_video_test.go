package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVideoPreferenceUsesHouseholdPatchAndUpdatesOnlyAfterSuccess(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(map[bool]string{true: "saved", false: "refused"}[succeeds], func(t *testing.T) {
			patched := make(chan struct{}, 1)
			profile := "direct"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/library/settings" || r.Method != "GET" && r.Method != "PATCH" {
					t.Errorf("unexpected lifecycle request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if r.Method == "PATCH" {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 || body["video_profile"] != "scanlines" {
						t.Errorf("patch=%v err=%v", body, err)
					}
					patched <- struct{}{}
					if !succeeds {
						w.WriteHeader(500)
						_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "INTERNAL", "message": "settings unavailable"}})
						return
					}
					profile = "scanlines"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"attract_idle_seconds": 60, "preferred_regions": []string{"usa"}, "video_profile": profile})
			}))
			defer server.Close()
			app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 1)
			now := time.Now()
			app.HandleCommand(CmdSettings, now)
			waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool { return snap.Settings.Open && !snap.Settings.Loading })
			if got := settingsRowByID(t, app, "video-profile").Value; got != "Direct" {
				t.Fatal(got)
			}
			focusSettingsRow(t, app, "video-profile", now)
			app.HandleCommand(CmdSelect, now)
			select {
			case <-patched:
			case <-time.After(2 * time.Second):
				t.Fatal("video preference PATCH missing")
			}
			waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool { return !snap.Settings.Busy })
			want := "Direct"
			if succeeds {
				want = "Scanlines"
			}
			if got := settingsRowByID(t, app, "video-profile").Value; got != want {
				t.Fatalf("value=%q want=%q", got, want)
			}
		})
	}
}
