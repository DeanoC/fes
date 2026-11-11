package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

const fixtureSessionJSON = `{"id":"sess-1","game_id":"fixture","state":"active","execution":"fpga","core_package":{"package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"abi":{"id":"fes.simple-game","major":1,"minor":0},"build_id":"0123456789abcdef0123456789abcdef","active_interfaces":[{"id":"fes.gamepad","major":1,"minor":0}],"gamepad":true},"input":{"state":"attached","ready":true}}` + "\n"

func failOpen(t *testing.T) OpenService {
	t.Helper()
	return func(context.Context, fogcast.Paths) (Service, error) {
		t.Fatal("opened a FogCast service")
		return nil, nil
	}
}

func TestLaunchStopStatusUsePersistentHostSessionAPI(t *testing.T) {
	var mu sync.Mutex
	var requests []recordedSessionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, recordedSessionRequest{Method: r.Method, Path: r.URL.Path, Body: string(body), ContentType: r.Header.Get("Content-Type")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fixtureSessionJSON)
	}))
	defer server.Close()

	for _, test := range []struct {
		name   string
		args   []string
		method string
		path   string
		body   string
	}{
		{name: "launch", args: []string{"launch", "fixture"}, method: http.MethodPost, path: "/api/v1/session/launch", body: `{"game_id":"fixture"}`},
		{name: "stop", args: []string{"stop"}, method: http.MethodPost, path: "/api/v1/session/stop"},
		{name: "status", args: []string{"status"}, method: http.MethodGet, path: "/api/v1/session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mu.Lock()
			requests = nil
			mu.Unlock()
			var stdout, stderr bytes.Buffer
			args := append([]string{"--json", "--api", server.URL}, test.args...)
			exit := Run(context.Background(), args, &stdout, &stderr, failOpen(t))
			mu.Lock()
			got := append([]recordedSessionRequest(nil), requests...)
			mu.Unlock()
			if exit != 0 || stderr.Len() != 0 || len(got) != 1 {
				t.Fatalf("exit=%d stderr=%q requests=%+v stdout=%s", exit, stderr.String(), got, stdout.String())
			}
			if got[0].Method != test.method || got[0].Path != test.path || got[0].Body != test.body {
				t.Fatalf("request=%+v want %s %s %q", got[0], test.method, test.path, test.body)
			}
			if test.method == http.MethodPost && test.body != "" && got[0].ContentType != "application/json" {
				t.Fatalf("content-type=%q", got[0].ContentType)
			}
			assertHostSessionJSON(t, stdout.Bytes())
		})
	}
}

func TestSessionCommandsDoNotOpenServiceOrRetryOnFailure(t *testing.T) {
	private := "/Volumes/private/token-secret"
	t.Run("error envelope", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/api/v1/session/launch" || r.Method != http.MethodPost {
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"BUSY","message":"`+private+`","phase":"request"}}`)
		}))
		defer server.Close()
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"--json", "--api", server.URL, "launch", "fixture"}, &stdout, &stderr, failOpen(t))
		if exit != 1 || calls != 1 || stderr.Len() != 0 ||
			!strings.Contains(stdout.String(), `"code":"BUSY"`) ||
			strings.Contains(stdout.String()+stderr.String(), private) {
			t.Fatalf("exit=%d calls=%d stdout=%q stderr=%q", exit, calls, stdout.String(), stderr.String())
		}
	})
	t.Run("malformed success", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			_, _ = io.WriteString(w, `{"state":`)
		}))
		defer server.Close()
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"--json", "--api", server.URL, "status"}, &stdout, &stderr, failOpen(t))
		if exit != 1 || calls != 1 || !strings.Contains(stdout.String(), `"code":"INTERNAL"`) {
			t.Fatalf("exit=%d calls=%d stdout=%q stderr=%q", exit, calls, stdout.String(), stderr.String())
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}))
		defer server.Close()
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"--json", "--api", server.URL, "stop"}, &stdout, &stderr, failOpen(t))
		if exit != 1 || calls != 1 || !strings.Contains(stdout.String(), `"code":"MISTER_UNAVAILABLE"`) {
			t.Fatalf("exit=%d calls=%d stdout=%q stderr=%q", exit, calls, stdout.String(), stderr.String())
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr bytes.Buffer
		exit := Run(ctx, []string{"--json", "--api", "http://127.0.0.1:1", "launch", "fixture"}, &stdout, &stderr, failOpen(t))
		if exit != 1 || !strings.Contains(stdout.String(), `"code":"CANCELED"`) {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
		}
	})
	t.Run("connection failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("server should be closed")
		}))
		url := server.URL
		server.Close()
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"--json", "--api", url, "status"}, &stdout, &stderr, failOpen(t))
		if exit != 1 || !strings.Contains(stdout.String(), `"code":"MISTER_UNAVAILABLE"`) {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
		}
	})
}

func TestSessionCommandsSanitizeHostJSONAndRejectUnknownState(t *testing.T) {
	private := "/Volumes/private/token-secret"
	oversized := strings.Repeat("a", 4096)
	t.Run("hostile optional fields", func(t *testing.T) {
		hostile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"id":"sess-1","state":"active","game_id":%q,"system":%q,"secret":%q,"content":{"path":%q}}`, oversized, private, private, private)
		}))
		defer hostile.Close()
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"--json", "--api", hostile.URL, "status"}, &stdout, &stderr, failOpen(t))
		if exit != 0 || stderr.Len() != 0 || stdout.String() != "{\"id\":\"sess-1\",\"state\":\"active\"}\n" {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
		}
		assertOneJSONValue(t, stdout.Bytes())
		if strings.Contains(stdout.String()+stderr.String(), private) || strings.Contains(stdout.String(), oversized) {
			t.Fatalf("leaked hostile session: %q", stdout.String())
		}
	})
	t.Run("unknown state", func(t *testing.T) {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"state":%q}`, private)
		}))
		defer bad.Close()
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"--api", bad.URL, "stop"}, &stdout, &stderr, failOpen(t))
		if exit != 1 || stdout.Len() != 0 || stderr.String() != "INTERNAL[recovery]: FogCast operation failed internally\n" {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
		}
		assertPrivateAbsent(t, stdout.String()+stderr.String())
	})
}

func TestSessionCommandsHumanOutputUsesHostSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop" {
			_, _ = io.WriteString(w, `{"id":"sess-1","state":"idle"}`)
			return
		}
		_, _ = io.WriteString(w, fixtureSessionJSON)
	}))
	defer server.Close()
	var launchOut, launchErr bytes.Buffer
	if exit := Run(context.Background(), []string{"--api", server.URL, "launch", "fixture"}, &launchOut, &launchErr, failOpen(t)); exit != 0 || launchErr.Len() != 0 ||
		launchOut.String() != "active: fixture package=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa abi=fes.simple-game/1.0 build=0123456789abcdef0123456789abcdef generation=1\n" {
		t.Fatalf("launch exit=%d stdout=%q stderr=%q", exit, launchOut.String(), launchErr.String())
	}
	var stopOut, stopErr bytes.Buffer
	if exit := Run(context.Background(), []string{"--api", server.URL, "stop"}, &stopOut, &stopErr, failOpen(t)); exit != 0 || stopErr.Len() != 0 || stopOut.String() != "idle\n" {
		t.Fatalf("stop exit=%d stdout=%q stderr=%q", exit, stopOut.String(), stopErr.String())
	}
}

func TestHealthAndCatalogStillUseInjectedService(t *testing.T) {
	service := &fakeService{health: protocol.Health{Ready: true}}
	var stdout bytes.Buffer
	exit := Run(context.Background(), []string{"health"}, &stdout, io.Discard, openFake(service))
	if exit != 0 || service.closeCalls != 1 || stdout.String() != "ready\n" {
		t.Fatalf("health still must use the injected service: exit=%d close=%d stdout=%q", exit, service.closeCalls, stdout.String())
	}
}

type recordedSessionRequest struct {
	Method      string
	Path        string
	Body        string
	ContentType string
}

func assertHostSessionJSON(t *testing.T, data []byte) {
	t.Helper()
	assertOneJSONValue(t, data)
	var session map[string]any
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "game_id", "state", "execution", "core_package", "input"} {
		if _, ok := session[key]; !ok {
			t.Fatalf("session JSON missing %s: %s", key, data)
		}
	}
	if session["state"] != "active" || session["game_id"] != "fixture" || session["id"] != "sess-1" {
		t.Fatalf("session JSON=%s", data)
	}
	if _, ok := session["status"]; ok {
		t.Fatalf("legacy launch status wrapper present: %s", data)
	}
	if _, ok := session["content"]; ok {
		t.Fatalf("legacy launch content wrapper present: %s", data)
	}
}
