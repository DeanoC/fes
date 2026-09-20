package tenfoot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot/attractvideo"
)

func TestOpenFetchedAttractVideoSkipsFetchWhenUnavailable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("FetchVideoFile should not run on stub platforms")
	}))
	t.Cleanup(server.Close)

	handle := strings.Repeat("ab", 32)
	player, err := openFetchedAttractVideo(context.Background(), NewClient(server.URL, server.Client()), handle, false, func(string) (attractvideo.Player, error) {
		t.Fatal("Open should not run on stub platforms")
		return nil, errAttractVideoUnavailable
	})
	if player != nil {
		player.Close()
		t.Fatal("expected nil player")
	}
	if err != errAttractVideoUnavailable {
		t.Fatalf("err = %v", err)
	}
}
