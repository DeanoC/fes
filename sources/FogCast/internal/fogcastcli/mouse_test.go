package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMouseCLIUsesAttachedSessionAndSendsOneReport(t *testing.T) {
	gets, posts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/session" {
			gets++
			w.Write([]byte(`{"state":"active","core_package":{"package_id":"` + strings.Repeat("a", 64) + `","generation":7,"abi":{"id":"fes.computer","major":1,"minor":0},"active_interfaces":[{"id":"fes.mouse.relative","major":1,"minor":0}]},"input":{"state":"attached","ready":true}}`))
			return
		}
		posts++
		var body struct{ Event remoteinput.Event }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		x, y, buttons, ok := remoteinput.MouseVector(body.Event)
		if !ok || x != -32768 || y != 32767 || buttons != 3 {
			t.Errorf("mouse=%+v", body.Event)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), []string{"--api", server.URL, "--json", "mouse", "-32768", "32767", "3"}, &stdout, &stderr, nil); exit != 0 || gets != 1 || posts != 1 {
		t.Fatalf("exit=%d GET=%d POST=%d out=%s err=%s", exit, gets, posts, stdout.String(), stderr.String())
	}
	for _, args := range [][]string{{"mouse", "32768", "0", "0"}, {"mouse", "0", "-32769", "0"}, {"mouse", "0", "0", "4"}, {"mouse", "1", "2"}, {"mouse", "1.5", "0", "0"}} {
		stdout.Reset()
		stderr.Reset()
		if exit := Run(context.Background(), append([]string{"--api", server.URL}, args...), &stdout, &stderr, nil); exit != 2 || gets != 1 || posts != 1 {
			t.Fatalf("invalid args=%v exit=%d GET=%d POST=%d", args, exit, gets, posts)
		}
	}
}
