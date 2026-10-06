package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSaveDiskCLIUsesBoundSessionAndSingleMutation(t *testing.T) {
	id := strings.Repeat("a", 64)
	unit := protocol.MediaUnitStatus{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			if r.URL.Path != "/api/v1/session/disk/save" || r.Header.Get(protocol.MediaUnitHeader) != "0" || r.Header.Get(protocol.HostSessionIDHeader) != "session" {
				t.Error(r.URL, r.Header)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "session", "target": "dev", "state": "active", "core_package": map[string]any{"package_id": id, "generation": 9, "abi": map[string]any{"id": "fes.computer", "major": 1, "minor": 0}, "media_units": []protocol.MediaUnitStatus{unit}, "persistence_mode": "persistent", "active_interfaces": []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}}})
	}))
	defer server.Close()
	if result := runLiveMediaCommand(context.Background(), server.URL, []string{"save-disk"}); result.err != nil || posts != 1 {
		t.Fatal(result.err, posts)
	}
	unit.Persistence = nil
	if result := runLiveMediaCommand(context.Background(), server.URL, []string{"save-disk"}); result.err == nil || posts != 1 {
		t.Fatal("volatile disk dispatched")
	}
}

func TestSaveDiskAndStopCLIExposeSaveFailureWithoutRetry(t *testing.T) {
	for _, command := range []string{"save-disk", "stop"} {
		for _, mode := range []string{"human", "json"} {
			t.Run(command+" "+mode, func(t *testing.T) {
				id := strings.Repeat("a", 64)
				unit := protocol.MediaUnitStatus{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}
				path := "/api/v1/session/stop"
				if command == "save-disk" {
					path = "/api/v1/session/disk/save"
				}
				var posts atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						posts.Add(1)
						if r.URL.Path != path {
							t.Errorf("unexpected mutation route: %s", r.URL.Path)
						}
						if command == "save-disk" && (r.Header.Get(protocol.MediaUnitHeader) != "0" || r.Header.Get(protocol.HostSessionIDHeader) != "session") {
							t.Error("save lost its unit/session binding")
						}
						w.WriteHeader(http.StatusInternalServerError)
						_ = json.NewEncoder(w).Encode(protocol.ErrorEnvelope{Error: protocol.APIError{Code: protocol.CodeSaveFailed, Message: "/private/target/data token-secret", Phase: "save"}})
						return
					}
					if r.Method != http.MethodGet || r.URL.Path != "/api/v1/session" {
						t.Errorf("unexpected observation: %s %s", r.Method, r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "session", "target": "dev", "state": "active", "core_package": map[string]any{"package_id": id, "generation": 9, "abi": map[string]any{"id": "fes.computer", "major": 1, "minor": 0}, "media_units": []protocol.MediaUnitStatus{unit}, "persistence_mode": "persistent", "active_interfaces": []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}}})
				}))
				defer server.Close()
				args := []string{"--api", server.URL, command}
				if mode == "json" {
					args = append([]string{"--json"}, args...)
				}
				var stdout, stderr bytes.Buffer
				if exit := Run(context.Background(), args, &stdout, &stderr, failOpen(t)); exit != 1 || posts.Load() != 1 {
					t.Fatalf("exit=%d mutations=%d stdout=%s stderr=%s", exit, posts.Load(), stdout.String(), stderr.String())
				}
				const message = "core data could not be durably written; inspect status before retrying"
				phase, humanCode := "save", "SAVE_FAILED[save]"
				if command == "save-disk" {
					// The existing hostclient HTTP reader projects code/message;
					// Stop's session HTTP reader also preserves the upstream phase.
					phase, humanCode = "", "SAVE_FAILED"
				}
				if mode == "human" {
					if stdout.Len() != 0 || stderr.String() != humanCode+": "+message+"\n" {
						t.Fatalf("lost typed save diagnostic: stdout=%q stderr=%q", stdout.String(), stderr.String())
					}
				} else {
					var response protocol.ErrorEnvelope
					if stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &response) != nil || response.Error.Code != protocol.CodeSaveFailed || response.Error.Phase != phase || response.Error.Message != message {
						t.Fatalf("lost structured save diagnostic: stdout=%q stderr=%q", stdout.String(), stderr.String())
					}
					assertOneJSONValue(t, stdout.Bytes())
				}
				assertPrivateAbsent(t, stdout.String()+stderr.String())
			})
		}
	}
}
