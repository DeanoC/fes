package targetclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
)

func softwareClientBundle(t *testing.T) ([]byte, software.Release) {
	t.Helper()
	manifest := software.Manifest{Format: 1, Board: "de10-nano", Architecture: "armv7", BaseContract: "fes-base-v1", Version: "1.0", Interfaces: map[string]string{"runtime": "2"}}
	files := fstest.MapFS{}
	for _, path := range software.RequiredExecutables {
		data := []byte(path)
		manifest.Files = append(manifest.Files, software.File{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data)), Mode: 0555})
		files[path] = &fstest.MapFile{Data: data, Mode: 0755}
	}
	var body bytes.Buffer
	if err := software.WriteBundle(&body, manifest, files); err != nil {
		t.Fatal(err)
	}
	release, err := software.InspectBundle(bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), release
}

func TestKitSoftwareClientUsesBearerWithoutExecutionLease(t *testing.T) {
	archive, release := softwareClientBundle(t)
	requestID := strings.Repeat("a", 64)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get(KitLeaseHeader) != "" {
			t.Error("software request acquired an execution lease or lost authentication")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/kit/software/releases":
			data, _ := io.ReadAll(r.Body)
			if !bytes.Equal(data, archive) || r.ContentLength != int64(len(archive)) {
				t.Error("bundle upload changed")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(release)
		case "GET /v1/kit/software/releases":
			_ = json.NewEncoder(w).Encode([]software.Release{release})
		case "POST /v1/kit/software/activate":
			var request softwarecontrol.Request
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.ID != requestID || request.Candidate != release.ID || request.Generation != 4 {
				t.Errorf("queue lost CAS identity: %+v", request)
			}
			_ = json.NewEncoder(w).Encode(softwarecontrol.Status{State: softwarecontrol.State{Version: 1, Generation: 5, Selected: "factory", Pending: &softwarecontrol.Operation{Request: request, Phase: "queued"}}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	client := NewClient(endpoint, "token", server.Client())
	if _, err := client.InstallKitSoftware(context.Background(), int64(len(archive)), bytes.NewReader(archive), release.ID); err != nil {
		t.Fatal(err)
	}
	if listed, err := client.KitSoftwareReleases(context.Background()); err != nil || len(listed) != 1 || listed[0].ID != release.ID {
		t.Fatal(listed, err)
	}
	status, err := client.QueueKitSoftware(context.Background(), softwarecontrol.Request{ID: requestID, Candidate: release.ID, Requester: "agent", Generation: 4})
	if err != nil || status.Pending == nil || status.Pending.ID != requestID {
		t.Fatal(status, err)
	}
	if calls.Load() != 3 {
		t.Fatal("unexpected retry or lease mutation", calls.Load())
	}
}

type lostSoftwareReply struct {
	calls int
	t     *testing.T
}

func (transport *lostSoftwareReply) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	if request.GetBody != nil {
		transport.t.Error("software mutation exposed transport replay")
	}
	return nil, io.ErrUnexpectedEOF
}

func TestKitSoftwareClientDoesNotReplayLostMutation(t *testing.T) {
	endpoint, _ := url.Parse("http://kit")
	transport := &lostSoftwareReply{t: t}
	client := NewClient(endpoint, "token", &http.Client{Transport: transport})
	_, err := client.QueueKitSoftware(context.Background(), softwarecontrol.Request{ID: strings.Repeat("a", 64), Candidate: strings.Repeat("b", 64), Requester: "agent"})
	if !errors.Is(err, io.ErrUnexpectedEOF) || transport.calls != 1 {
		t.Fatal("lost request replayed", transport.calls, err)
	}
}

func TestKitSoftwareClientRejectsSubstitutedBundleIdentity(t *testing.T) {
	archive, release := softwareClientBundle(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(release) }))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	client := NewClient(endpoint, "token", server.Client())
	if _, err := client.InstallKitSoftware(context.Background(), int64(len(archive)), bytes.NewReader(archive), strings.Repeat("c", 64)); err == nil {
		t.Fatal("substituted installed bundle accepted")
	}
}
