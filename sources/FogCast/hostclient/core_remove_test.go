package hostclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteCoreEntryVerifiesIdentity(t *testing.T) {
	calls := 0
	response := `{"game_id":"core-pong"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/library/core-entries/core-pong" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	if err := client.DeleteCoreEntry(context.Background(), "core-pong"); err != nil {
		t.Fatal(err)
	}
	response = `{"game_id":"other"}`
	if err := client.DeleteCoreEntry(context.Background(), "core-pong"); err == nil {
		t.Fatal("accepted mismatched response")
	}
	if err := client.DeleteCoreEntry(context.Background(), "../other"); err == nil {
		t.Fatal("accepted invalid ID")
	}
	if calls != 2 {
		t.Fatalf("calls=%d; invalid ID reached host", calls)
	}
}
