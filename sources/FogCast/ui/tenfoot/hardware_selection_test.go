package tenfoot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestRoomServicesProjectsOmittedExpansionSelection(t *testing.T) {
	var mu sync.Mutex
	body := `{"machines":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		io.WriteString(w, body)
	}))
	defer server.Close()
	set := func(next string) {
		mu.Lock()
		body = next
		mu.Unlock()
	}
	project := func(paired bool) *bool {
		t.Helper()
		client := NewClient(server.URL, server.Client())
		client.paired = paired
		snap, err := (roomServices{client: client}).Hardware(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return snap.ExpansionSelection
	}
	if got := project(true); got == nil || *got {
		t.Fatalf("paired older host = %v", got)
	}
	if got := project(false); got == nil || !*got {
		t.Fatalf("unpaired older host = %v", got)
	}
	set(`{"machines":[],"expansion_selection":true}`)
	if got := project(true); got == nil || !*got {
		t.Fatalf("paired supported host = %v", got)
	}
	set(`{"machines":[],"expansion_selection":false}`)
	if got := project(false); got == nil || *got {
		t.Fatalf("unpaired unsupported host = %v", got)
	}
}
