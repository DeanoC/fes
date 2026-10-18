package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/protocol"
)

const testTargetID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"

func TestSoftwareHTTPClientBoundsConnectAndHeader(t *testing.T) {
	client := softwareHTTPClient(15 * time.Minute)
	if client.Timeout != 15*time.Minute {
		t.Fatalf("operation timeout = %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type %T", client.Transport)
	}
	if transport.TLSHandshakeTimeout != softwareConnectTimeout {
		t.Fatalf("TLS handshake timeout = %s", transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout != softwareHeaderTimeout {
		t.Fatalf("response header timeout = %s", transport.ResponseHeaderTimeout)
	}
	if transport.DialContext == nil {
		t.Fatal("dial function is unset")
	}
	if softwareDialer().Timeout != softwareConnectTimeout {
		t.Fatalf("dial timeout = %s", softwareDialer().Timeout)
	}
}

func TestCallReadyRetriesANetworkErrorThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	err := callReady(context.Background(), func(context.Context) error {
		if calls.Add(1) < softwareReadyAttempts {
			return &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNRESET}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != softwareReadyAttempts {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestCallReadyDoesNotRetryAContractError(t *testing.T) {
	var calls atomic.Int32
	err := callReady(context.Background(), func(context.Context) error {
		calls.Add(1)
		return errors.New("configured target identity or API contract does not match")
	})
	if err == nil || strings.Contains(err.Error(), "not ready") {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestRunInstallRetriesResetHealthAndUploadsOnce(t *testing.T) {
	archive, release := softwareCommandBundle(t)
	var healths, installs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
			if healths.Add(1) == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
				return
			}
			_ = json.NewEncoder(w).Encode(protocol.Health{TargetID: testTargetID, APIVersion: "v1", Ready: false})
		case "POST /v1/kit/software/releases":
			installs.Add(1)
			data, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(data, archive) {
				t.Error("bundle upload changed")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(release)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	var out, log bytes.Buffer
	err := run(context.Background(), []string{
		"--config", writeSoftwareConfig(t, server.URL),
		"--action", "install",
		"--bundle", writeBundleFile(t, archive),
		"--timeout", "45s",
	}, &out, &log)
	if err != nil {
		t.Fatal(err)
	}
	if healths.Load() < 2 || installs.Load() != 1 {
		t.Fatalf("healths=%d installs=%d", healths.Load(), installs.Load())
	}
	if !strings.Contains(out.String(), release.ID) {
		t.Fatalf("output = %s", out.String())
	}
}

func TestRunDoesNotRetryADroppedInstall(t *testing.T) {
	archive, _ := softwareCommandBundle(t)
	var installs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
			_ = json.NewEncoder(w).Encode(protocol.Health{TargetID: testTargetID, APIVersion: "v1", Ready: true})
		case "POST /v1/kit/software/releases":
			installs.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	err := run(context.Background(), []string{
		"--config", writeSoftwareConfig(t, server.URL),
		"--action", "install",
		"--bundle", writeBundleFile(t, archive),
		"--timeout", "45s",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || installs.Load() != 1 {
		t.Fatalf("installs=%d err=%v", installs.Load(), err)
	}
	if !strings.Contains(err.Error(), "inspect --action status") || strings.Contains(err.Error(), "kit is not ready") {
		t.Fatal(err)
	}
}

func TestRunReportsKitNotReadyAfterRefusedHealth(t *testing.T) {
	start := time.Now()
	err := run(context.Background(), []string{
		"--config", writeSoftwareConfig(t, "http://127.0.0.1:1"),
		"--action", "status",
		"--timeout", "20s",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "kit is not ready") {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "inspect --action status") {
		t.Fatal(err)
	}
	if elapsed < 2500*time.Millisecond || elapsed > 12*time.Second {
		t.Fatalf("elapsed %s", elapsed)
	}
}

func TestRunDoesNotRetryAnIdentityMismatch(t *testing.T) {
	var healths atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		healths.Add(1)
		_ = json.NewEncoder(w).Encode(protocol.Health{TargetID: "other", APIVersion: "v1", Ready: true})
	}))
	defer server.Close()

	err := run(context.Background(), []string{
		"--config", writeSoftwareConfig(t, server.URL),
		"--action", "status",
		"--timeout", "20s",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not match") || strings.Contains(err.Error(), "not ready") {
		t.Fatal(err)
	}
	if healths.Load() != 1 {
		t.Fatalf("healths = %d", healths.Load())
	}
}

func TestRunRetriesTruncatedSoftwareStatusResponse(t *testing.T) {
	var statuses atomic.Int32
	want := softwarecontrol.Status{State: softwarecontrol.State{Version: 1, Generation: 9, Selected: "factory"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
			_ = json.NewEncoder(w).Encode(protocol.Health{TargetID: testTargetID, APIVersion: "v1", Ready: true})
		case "GET /v1/kit/software":
			if statuses.Add(1) == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 200\r\nContent-Type: application/json\r\n\r\n{\"generation\":")
				_ = conn.Close()
				return
			}
			_ = json.NewEncoder(w).Encode(want)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	err := run(context.Background(), []string{
		"--config", writeSoftwareConfig(t, server.URL),
		"--action", "status",
		"--timeout", "45s",
	}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if statuses.Load() != 2 {
		t.Fatalf("software status calls = %d", statuses.Load())
	}
	var got softwarecontrol.Status
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Generation != want.Generation {
		t.Fatalf("generation = %d, want %d", got.Generation, want.Generation)
	}
}

func TestCallReadyFailsFastOnInvalidTLSCertificate(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request after invalid TLS certificate: %s %s", r.Method, r.URL.Path)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()

	client := softwareHTTPClient(20 * time.Second)
	err := callReady(context.Background(), func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		return response.Body.Close()
	})
	if err == nil || !strings.Contains(err.Error(), "target TLS certificate verification failed") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "kit is not ready") {
		t.Fatalf("certificate error was reported as kit unavailability: %v", err)
	}
	if connections.Load() != 1 {
		t.Fatalf("TLS connections = %d, want 1", connections.Load())
	}
}

func TestRunStatusAllowsABusyKit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
			_ = json.NewEncoder(w).Encode(protocol.Health{TargetID: testTargetID, APIVersion: "v1", Ready: false})
		case "GET /v1/kit/software":
			_ = json.NewEncoder(w).Encode(softwarecontrol.Status{State: softwarecontrol.State{Version: 1, Generation: 7, Selected: "factory"}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	err := run(context.Background(), []string{
		"--config", writeSoftwareConfig(t, server.URL),
		"--action", "status",
		"--timeout", "20s",
	}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var status softwarecontrol.Status
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Generation != 7 {
		t.Fatalf("generation = %d", status.Generation)
	}
}

func writeSoftwareConfig(t *testing.T, address string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf(`selected_target = "kit"
request_timeout_seconds = 5
upload_timeout_seconds = 5

[[targets]]
name = "kit"
enabled = true
target_id = %q
address = %q
agent = "token"
`, testTargetID, address)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBundleFile(t *testing.T, archive []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kit-software.tar")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func softwareCommandBundle(t *testing.T) ([]byte, software.Release) {
	t.Helper()
	manifest := software.Manifest{Format: 1, Board: "de10-nano", Architecture: "armv7", BaseContract: "fes-base-v1", Version: "1.0", Interfaces: map[string]string{"runtime": "2"}}
	files := fstest.MapFS{}
	for _, path := range software.RequiredExecutables {
		data := []byte(path)
		manifest.Files = append(manifest.Files, software.File{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data)), Mode: 0555})
		files[path] = &fstest.MapFile{Data: data, Mode: 0755}
	}
	var body bytes.Buffer
	if err := software.WriteBundle(&body, manifest, files); err != nil {
		t.Fatal(err)
	}
	release, err := software.InspectBundle(bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), release
}
