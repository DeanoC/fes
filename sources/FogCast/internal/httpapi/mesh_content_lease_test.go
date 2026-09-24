package httpapi_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

type countingMesh struct {
	pulls int
	links int
}

func (countingMesh) NodeID() string { return "kit-a" }
func (countingMesh) EligibleABIs() []meshcontent.EligibleABI {
	return nil
}
func (countingMesh) Slot(meshcontent.ContentID) meshcontent.SlotState {
	return meshcontent.StateMissing
}
func (countingMesh) SourceAdvertises(meshcontent.ContentID) bool { return false }
func (c *countingMesh) Pull(context.Context, meshcontent.ContentID) (meshcontent.SlotState, error) {
	c.pulls++
	return meshcontent.StatePresent, nil
}
func (c *countingMesh) LinkExpansion(string, meshcontent.ContentID) error {
	c.links++
	return nil
}

func TestMeshContentPullAndLinkRequireTheKitLease(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	holder := claimKit(t, manager)
	executor := &countingMesh{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithMeshContent(executor))
	id := "sha256:" + strings.Repeat("ab", 32)
	pull := "/v1/mesh/content/pull?id=" + id
	linkBody := `{"content_id":"` + id + `"}`

	for _, token := range []string{"", "foreign"} {
		for _, call := range []struct {
			path string
			body string
		}{
			{path: pull},
			{path: "/v1/mesh/content/link?name=port", body: linkBody},
		} {
			request := httptest.NewRequest(http.MethodPost, call.path, strings.NewReader(call.body))
			request.Header.Set("Authorization", "Bearer bearer")
			if token != "" {
				request.Header.Set(httpapi.KitLeaseHeader, token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "KIT_LEASE_REQUIRED") {
				t.Fatalf("%s token %q: %d %s", call.path, token, response.Code, response.Body.String())
			}
		}
	}
	if executor.pulls != 0 || executor.links != 0 {
		t.Fatalf("rejected mutations pulled %d linked %d", executor.pulls, executor.links)
	}

	node := httptest.NewRequest(http.MethodGet, "/v1/mesh/content/node", nil)
	node.Header.Set("Authorization", "Bearer bearer")
	nodeResponse := httptest.NewRecorder()
	handler.ServeHTTP(nodeResponse, node)
	if nodeResponse.Code != http.StatusOK || !strings.Contains(nodeResponse.Body.String(), `"node_id":"kit-a"`) {
		t.Fatalf("node read: %d %s", nodeResponse.Code, nodeResponse.Body.String())
	}

	ownedPull := httptest.NewRequest(http.MethodPost, pull, nil)
	ownedPull.Header.Set("Authorization", "Bearer bearer")
	ownedPull.Header.Set(httpapi.KitLeaseHeader, holder.Token)
	ownedPullResponse := httptest.NewRecorder()
	handler.ServeHTTP(ownedPullResponse, ownedPull)
	if ownedPullResponse.Code != http.StatusOK || !strings.Contains(ownedPullResponse.Body.String(), `"state":"present"`) || executor.pulls != 1 {
		t.Fatalf("owned pull: %d %s pulls %d", ownedPullResponse.Code, ownedPullResponse.Body.String(), executor.pulls)
	}
	ownedLink := httptest.NewRequest(http.MethodPost, "/v1/mesh/content/link?name=port", strings.NewReader(linkBody))
	ownedLink.Header.Set("Authorization", "Bearer bearer")
	ownedLink.Header.Set("Content-Type", "application/json")
	ownedLink.Header.Set(httpapi.KitLeaseHeader, holder.Token)
	ownedLinkResponse := httptest.NewRecorder()
	handler.ServeHTTP(ownedLinkResponse, ownedLink)
	if ownedLinkResponse.Code != http.StatusOK || executor.links != 1 {
		t.Fatalf("owned link: %d %s links %d", ownedLinkResponse.Code, ownedLinkResponse.Body.String(), executor.links)
	}
}

func TestMeshContentHostlessOwnerIsDenied(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	deadline := time.Now().Add(time.Second)
	var grant kitlease.Grant
	for {
		var err error
		grant, err = manager.Claim(kitlease.ClaimRequest{
			RequestID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Owner:     kitlease.HostlessOwner,
			Purpose:   kitlease.HostlessPurpose,
		})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	executor := &countingMesh{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithMeshContent(executor))
	id := "sha256:" + strings.Repeat("cd", 32)
	for _, path := range []string{
		"/v1/mesh/content/pull?id=" + id,
		"/v1/mesh/content/link?name=port",
	} {
		var body *strings.Reader
		if strings.Contains(path, "/link") {
			body = strings.NewReader(`{"content_id":"` + id + `"}`)
		} else {
			body = strings.NewReader("")
		}
		request := httptest.NewRequest(http.MethodPost, path, body)
		request.Header.Set("Authorization", "Bearer bearer")
		request.Header.Set(httpapi.KitLeaseHeader, grant.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "KIT_LEASE_DENIED") {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
	if executor.pulls != 0 || executor.links != 0 {
		t.Fatalf("hostless wrote pulls %d links %d", executor.pulls, executor.links)
	}
}

func TestCanceledMeshPullIsLogged(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	executor := &blockingPull{started: make(chan struct{})}
	handler := httpapi.New(&fakeController{}, "bearer", "test", logger, httpapi.WithMeshContent(executor))
	ctx, cancel := context.WithCancel(context.Background())
	id := "sha256:" + strings.Repeat("ab", 32)
	request := httptest.NewRequest(http.MethodPost, "/v1/mesh/content/pull?id="+id, nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer bearer")
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
		close(done)
	}()
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("pull did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled pull did not return")
	}
	if !strings.Contains(logs.String(), "mesh content pull canceled") {
		t.Fatalf("log %s", logs.String())
	}
}

type blockingPull struct {
	started chan struct{}
}

func (blockingPull) NodeID() string                          { return "kit-a" }
func (blockingPull) EligibleABIs() []meshcontent.EligibleABI { return nil }
func (blockingPull) Slot(meshcontent.ContentID) meshcontent.SlotState {
	return meshcontent.StateMissing
}
func (blockingPull) SourceAdvertises(meshcontent.ContentID) bool { return false }
func (b *blockingPull) Pull(ctx context.Context, _ meshcontent.ContentID) (meshcontent.SlotState, error) {
	close(b.started)
	<-ctx.Done()
	return "", ctx.Err()
}
func (blockingPull) LinkExpansion(string, meshcontent.ContentID) error { return nil }
