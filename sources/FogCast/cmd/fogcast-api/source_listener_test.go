package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

type apiSourceService struct{ compositionService }

func (*apiSourceService) LibrarySourceEntries(context.Context) ([]catalog.CoreEntry, error) {
	return []catalog.CoreEntry{{GameID: "source-game", Title: "Cassette"}}, nil
}
func (*apiSourceService) LibrarySourceContentAdvertises(context.Context, meshcontent.ContentID) (bool, error) {
	return false, nil
}
func (*apiSourceService) OpenLibrarySourceContent(context.Context, meshcontent.ContentID) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}

func TestSourceListenerServesAndShutsDown(t *testing.T) {
	token := "source-test-token-0123456789abcdef"
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := port.Addr().String()
	port.Close()
	configPath := filepath.Join(t.TempDir(), "host.toml")
	if err := os.WriteFile(configPath, []byte("base_url = \"http://127.0.0.1:8182\"\ntoken = \"test\"\nlibrary_source_token = \""+token+"\"\nrequest_timeout_seconds = 12\nupload_timeout_seconds = 60\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- runWithComposer(ctx, []string{"--config", configPath, "--listen", "127.0.0.1:0", "--source-listen", address}, io.Discard, io.Discard,
			func(context.Context, fogcast.Paths) (service, error) { return &apiSourceService{}, nil },
			func(service, fogcast.Config, bridgeStarterFactory) (http.Handler, func() error, error) {
				return http.NewServeMux(), func() error { return nil }, nil
			})
	}()
	client := &http.Client{Timeout: time.Second}
	url := "http://" + address + "/api/v1/library/core-entries"
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, requestErr := http.NewRequest(http.MethodGet, url, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "source-game") {
				t.Fatalf("source response %d: %s", response.StatusCode, body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("source did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit=%d", code)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("listener did not stop")
	}
	if response, err := client.Get(url); err == nil {
		response.Body.Close()
		t.Fatal("source listener remained open")
	}
}

func TestSourceListenerRefusesMissingToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "host.toml")
	if err := os.WriteFile(configPath, []byte("base_url = \"http://127.0.0.1:8182\"\ntoken = \"test\"\nrequest_timeout_seconds = 12\nupload_timeout_seconds = 60\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opened := false
	code := runWithComposer(context.Background(), []string{"--config", configPath, "--listen", "127.0.0.1:0", "--source-listen", "0.0.0.0:8790"}, io.Discard, io.Discard,
		func(context.Context, fogcast.Paths) (service, error) { opened = true; return &apiSourceService{}, nil },
		func(service, fogcast.Config, bridgeStarterFactory) (http.Handler, func() error, error) {
			return http.NewServeMux(), func() error { return nil }, nil
		})
	if code != 2 || opened {
		t.Fatalf("missing listener token: exit=%d service_opened=%v", code, opened)
	}
}
