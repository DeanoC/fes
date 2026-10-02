package fogcast_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

// The discovered origin is not the enrolled [[targets]] address. A library
// GET must not send the agent token there, including when placement would
// have preferred that origin.
func TestLibraryTitlesGetSendsNoBearerToDiscoveredOrigin(t *testing.T) {
	const kitID = "01234567-89ab-cdef-0123-456789abcdef"
	var discoveredAuth atomic.Int32
	discovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			discoveredAuth.Add(1)
		}
		http.Error(w, "discovered origin", http.StatusForbidden)
	}))
	defer discovered.Close()
	discoveredHost := strings.TrimPrefix(discovered.URL, "http://")
	base := discovered.Client().Transport
	var mu sync.Mutex
	var seen []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		seen = append(seen, r.URL.Host+" "+auth)
		mu.Unlock()
		if r.URL.Host == discoveredHost {
			if auth != "" {
				discoveredAuth.Add(1)
			}
			return base.RoundTrip(r)
		}
		return nil, fmt.Errorf("refusing %s", r.URL.Host)
	})}
	service := fogcast.NewLibraryRouteTestService()
	service.SetLibraryRouteFixture([]fogcast.MeshNode{{
		NodeID: kitID, TargetID: kitID, Address: discovered.URL, Mesh: discovery.MeshProtocol,
		Capabilities: discovery.KitCapabilities(),
	}}, []fogcast.TargetConfig{{
		Name: "kit", Enabled: true, TargetID: kitID, Address: "http://192.0.2.1:8182", Agent: "fixture-token",
	}}, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/library/titles", nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	hostapi.New(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"titles"`) {
		t.Fatalf("library GET: %d %s", response.Code, response.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if discoveredAuth.Load() != 0 {
		t.Fatalf("discovered origin received Authorization: %d %v", discoveredAuth.Load(), seen)
	}
	for _, hit := range seen {
		host, auth, _ := strings.Cut(hit, " ")
		if host == discoveredHost && auth != "" {
			t.Fatalf("bearer sent to discovered origin: %v", seen)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
