package tenfoot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestHomeFamilyQueryPreservesMachinesAndReportsActualCap(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		machines, titles, max    int
		omitMarker               bool
		wantErr, wantMore        bool
		wantMachines, wantTitles int
	}{
		{name: "below cap", machines: 1, titles: 1, max: 3, wantMachines: 1, wantTitles: 1},
		{name: "exact cap", machines: 1, titles: 2, max: 3, wantMachines: 1, wantTitles: 2},
		{name: "over cap", machines: 1, titles: 3, max: 3, wantMachines: 1, wantTitles: 2, wantMore: true},
		{name: "2000 exact rows over multiple pages", machines: 1, titles: 1999, max: 2000, wantMachines: 1, wantTitles: 1999},
		{name: "2001 rows over multiple pages", machines: 1, titles: 2000, max: 2000, wantMachines: 1, wantTitles: 1999, wantMore: true},
		{name: "machine budget exhausted with titles", machines: 3, titles: 1, max: 3, wantMachines: 3, wantMore: true},
		{name: "too many machines fails closed", machines: 4, max: 3, wantErr: true},
		{name: "old host cannot confirm filter", machines: 1, max: 3, omitMarker: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				kind := r.URL.Query().Get("core_entry_kind")
				count := tc.machines
				if kind == "title" {
					count = tc.titles
				} else if kind != "machine" {
					t.Errorf("unfiltered request: %s", r.URL.RawQuery)
					w.WriteHeader(400)
					return
				}
				start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
				limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
				if limit <= 0 {
					t.Errorf("missing limit")
					w.WriteHeader(400)
					return
				}
				end := min(start+limit, count)
				games := make([]hostclient.Game, 0, end-start)
				for i := start; i < end; i++ {
					games = append(games, hostclient.Game{ID: fmt.Sprintf("%s-%d", kind, i), Title: kind, System: "fpga", CoreID: "fes.zx81", State: "available", RootOnline: true, Launchable: true})
				}
				response := map[string]any{"games": games}
				if end < count {
					response["next_cursor"] = strconv.Itoa(end)
				}
				if !tc.omitMarker {
					response["core_entry_kind"] = kind
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			svc := roomServices{client: NewClient(server.URL, server.Client())}
			games, more, err := svc.QueryGames(context.Background(), hostclient.GameListQuery{Platform: "fpga", CoreIDs: []string{"fes.zx81"}, HomeFamily: true}, tc.max)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				return
			}
			if more != tc.wantMore {
				t.Fatalf("more=%v want %v", more, tc.wantMore)
			}
			m, n := 0, 0
			for _, game := range games {
				switch game.CoreEntryKind {
				case "machine":
					m++
				case "title":
					n++
				default:
					t.Fatalf("missing kind %+v", game)
				}
			}
			if m != tc.wantMachines || n != tc.wantTitles {
				t.Fatalf("machines=%d titles=%d", m, n)
			}
		})
	}
}
