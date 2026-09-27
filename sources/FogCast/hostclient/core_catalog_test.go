package hostclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCoreSetupClientSourceContext(t *testing.T) {
	pid := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/core-catalog/fes.sms/setup" || r.URL.Query().Get("source_id") != "source-two" || r.URL.Query().Get("package_id") != pid {
			t.Error(r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"source_id":"source-two","core_id":"fes.sms","package_id":"` + pid + `","roms":[],"entries":[]}`))
	}))
	defer server.Close()
	c := NewClient(server.URL, server.Client())
	v, err := c.CoreSetup(context.Background(), "source-two", "fes.sms", pid)
	if err != nil || v.SourceID != "source-two" {
		t.Fatalf("%+v %v", v, err)
	}
}
