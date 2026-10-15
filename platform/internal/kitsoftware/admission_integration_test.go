package kitsoftware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The fixture is compiled from FogCast/internal/softwareagent so this integration
// crosses the actual module/internal boundary through its production Unix API.
// Build with: (cd sources/FogCast && go test -c ./internal/softwareagent -o /tmp/fes-software-agent-fixture)
// FES_SOFTWARE_AGENT_FIXTURE optionally reuses a prebuilt fixture; otherwise the
// normal test lane compiles the repository fixture itself.
func TestRealAgentAdmissionAcrossRestart(t *testing.T) {
	fixture := os.Getenv("FES_SOFTWARE_AGENT_FIXTURE")
	if fixture == "" {
		fixture = filepath.Join(t.TempDir(), "software-agent-fixture")
		build := exec.Command("go", "test", "-c", "./internal/softwareagent", "-o", fixture)
		build.Dir = filepath.Join("..", "..", "..", "sources", "FogCast")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "GOWORK=") {
				build.Env = append(build.Env, value)
			}
		}
		build.Env = append(build.Env, "GOWORK=off")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build real agent fixture: %v\n%s", err, output)
		}
	}
	dir, err := os.MkdirTemp("", "fes-admission-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "agent.sock")
	var process *exec.Cmd
	stop := func() {
		if process != nil {
			_ = process.Process.Signal(syscall.SIGTERM)
			if err := process.Wait(); err != nil {
				t.Errorf("fixture stop: %v", err)
			}
			process = nil
		}
	}
	defer stop()
	start := func() {
		process = exec.Command(fixture, "-test.run=^TestSupervisorFixture$")
		process.Env = append(os.Environ(), "FES_SOFTWARE_TEST_SOCKET="+socket, "FES_SOFTWARE_TEST_ROOT="+filepath.Join(dir, "marker"), "FES_SOFTWARE_TEST_BUNDLE_ID=candidate")
		process.Stdout = os.Stderr
		process.Stderr = os.Stderr
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
	}
	start()
	a, err := NewUnixAdmission(socket, filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := a.Status(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real agent did not become available")
		}
		time.Sleep(20 * time.Millisecond)
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	call := func(path string, in, out any, token string) int {
		b, _ := json.Marshal(in)
		req, _ := http.NewRequest(http.MethodPost, "http://unix"+path, bytes.NewReader(b))
		req.Header.Set("X-FogCast-Kit-Lease", token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil && resp.StatusCode == 200 {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode
	}
	var host struct {
		Token string `json:"token"`
	}
	hostReq := map[string]string{"request_id": strings.Repeat("b", 64), "owner": "competing-host", "purpose": "play"}
	// Lease cleanup settles independently of the status endpoint being available.
	for {
		code := call("/test/claim", hostReq, &host, "")
		if code == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("host claim HTTP %d", code)
		}
		time.Sleep(20 * time.Millisecond)
	}
	req := Request{ID: strings.Repeat("a", 64), Candidate: "candidate", Requester: "updater"}
	if err := a.Queue(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if err := a.Acquire(context.Background(), req); !errors.Is(err, ErrBusy) {
		t.Fatalf("live owner was not preserved: %v", err)
	}
	if code := call("/test/release", map[string]string{}, nil, host.Token); code != 200 {
		t.Fatal("host release", code)
	}
	for {
		err = a.Acquire(context.Background(), req)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrBusy) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	status, err := a.Status(context.Background())
	if err != nil || status.Maintenance.State != "ready" {
		t.Fatal(status, err)
	}
	stop()
	start()
	deadline = time.Now().Add(5 * time.Second)
	for {
		err = a.Acquire(context.Background(), req)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("replacement failed to resume reservation", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := a.Release(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	// An /end reply can be lost across a supervisor crash. Persisted private
	// generation/token proof completes idempotently without replaying /acquire.
	restarted, err := NewUnixAdmission(socket, filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Release(context.Background(), req); err != nil {
		t.Fatal("completed end could not replay safely", err)
	}
	status, err = restarted.Status(context.Background())
	if err != nil || status.Maintenance.State != "none" {
		t.Fatal(fmt.Sprint(status), err)
	}
}
