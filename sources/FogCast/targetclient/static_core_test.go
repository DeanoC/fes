package targetclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/misteross/expansion"
)

func TestStaticComposedReplyHasOneValidReceipt(t *testing.T) {
	for _, mode := range []string{"empty", "ram16k", "changed-id", "mixed", "empty-claim"} {
		t.Run(mode, func(t *testing.T) {
			id := strings.Repeat("a", 64)
			c := expansion.StaticComposition{PackageID: id, BaseSHA256: strings.Repeat("b", 64), BaseSize: 12, PersonaSHA256: strings.Repeat("c", 64), PersonaSize: 26, ControlBuildID: strings.Repeat("d", 32), HardwareID: expansion.EmptyPersonaID}
			if mode == "ram16k" {
				c.ExpansionID = strings.Repeat("e", 64)
				c.HardwareID = expansion.RAM16KPersonaID
			}
			c.ID, _ = expansion.StaticCompositionID(c)
			status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: id, CoreID: "fes.zx81", Generation: 7, PersistenceMode: "volatile", ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, BuildID: strings.Repeat("f", 32), StaticComposition: &c}}
			if mode == "changed-id" {
				c.ID = strings.Repeat("f", 64)
			}
			if mode == "mixed" {
				status.CorePackage.Composition = &expansion.Composition{}
			}
			if mode == "empty-claim" {
				status.CorePackage.ExpansionSocket = protocol.ExpansionSocketEmpty
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; json.NewEncoder(w).Encode(status) }))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := targetclient.NewClient(base, "secret", server.Client())
			_, err := client.LoadComposedCore(context.Background(), 11, strings.NewReader("composition"), id)
			if (err == nil) != (mode == "empty" || mode == "ram16k") {
				t.Fatal("invalid static response outcome", err)
			}
			if calls != 1 {
				t.Fatal("composition request replayed", calls)
			}
		})
	}
}
