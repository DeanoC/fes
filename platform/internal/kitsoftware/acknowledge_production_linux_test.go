package kitsoftware

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type acknowledgmentTransport func(*http.Request) (*http.Response, error)

func (f acknowledgmentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAcknowledgmentReleaseNeverAcquiresMissingGrant(t *testing.T) {
	for _, bad := range []string{"missing", "boot", "operation", "owner"} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			a, err := NewUnixAdmission(filepath.Join(dir, "absent.sock"), filepath.Join(dir, "key"))
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			a.client = &http.Client{Transport: acknowledgmentTransport(func(*http.Request) (*http.Response, error) {
				requests++
				t.Fatal("invalid grant contacted admission")
				return nil, nil
			})}
			r := req()
			g := maintenanceGrant{Token: "private-token", Status: MaintenanceStatus{OperationID: r.ID, Generation: strings.Repeat("a", 64), BootID: a.bootID, Owner: r.Requester, Purpose: "kit-software-activation"}}
			switch bad {
			case "boot":
				g.Status.BootID = "other"
			case "operation":
				g.Status.OperationID = "other"
			case "owner":
				g.Status.Owner = "other"
			}
			if bad != "missing" {
				if err := a.saveGrant(r.ID, g); err != nil {
					t.Fatal(err)
				}
			}
			if err := (acknowledgmentAdmission{admission: a, boot: a.bootID}).Release(context.Background(), r); err == nil {
				t.Fatal("invalid grant released")
			}
			if requests != 0 {
				t.Fatal(requests)
			}
		})
	}
}
func TestRunningPriorProofUsesRealChildrenAndReadiness(t *testing.T) {
	root := filepath.Join(t.TempDir(), "release")
	runDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"mister-runtime", "mister-agent", "fogcast-kit"}
	for _, name := range paths {
		if err := os.WriteFile(filepath.Join(root, "bin", name), bytes, 0555); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := releaseIdentities(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	children := make([]*exec.Cmd, 0, 3)
	for _, name := range paths {
		cmd := exec.Command(filepath.Join(root, "bin", name), "60")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, cmd)
	}
	defer func() {
		for _, cmd := range children {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
	}()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "supervisor.pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(runDir, "current")); err != nil {
		t.Fatal(err)
	}
	a := &UnixAdmission{client: &http.Client{Transport: acknowledgmentTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/status" {
			t.Fatal("proof made mutation", r.URL.Path)
		}
		b, _ := json.Marshal(AdmissionStatus{BundleID: testFactoryID, RawIdleReady: true})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))), Header: make(http.Header)}, nil
	})}}
	c := &ProcessController{RunDir: runDir, Admission: a, prepared: &preparedRelease{id: testFactoryID, root: root, entries: entries}}
	request := AcknowledgeRequest{Selected: testFactoryID}
	// Start() returning implies exec, but wait for proc visibility on busy hosts.
	time.Sleep(10 * time.Millisecond)
	proof, err := captureRunningPriorAt(context.Background(), request, c, runDir, binary)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(runDir, "launcher-ready.json")
	ready := func(pid int) {
		b, _ := json.Marshal(map[string]any{"bundle_id": testFactoryID, "pid": pid})
		if err := os.WriteFile(marker, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := proof.healthy(context.Background(), request, c); err == nil {
		t.Fatal("missing launcher readiness accepted")
	}
	ready(children[1].Process.Pid)
	if err := proof.healthy(context.Background(), request, c); err == nil {
		t.Fatal("wrong readiness PID accepted")
	}
	ready(children[2].Process.Pid)
	if err := proof.healthy(context.Background(), request, c); err != nil {
		t.Fatal("healthy real prior rejected", err)
	}
	if err := children[1].Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = children[1].Wait()
	replacement := exec.Command(filepath.Join(root, "bin", "mister-agent"), "60")
	if err := replacement.Start(); err != nil {
		t.Fatal(err)
	}
	children = append(children, replacement)
	if err := proof.healthy(context.Background(), request, c); err == nil {
		t.Fatal("replacement process accepted by old proof")
	}
}

func TestAcknowledgmentResumeUsesOnlyExactStoredCredential(t *testing.T) {
	for _, scenario := range []string{"ready", "replaced", "bad-token"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			a, err := NewUnixAdmission(filepath.Join(dir, "absent.sock"), filepath.Join(dir, "key"))
			if err != nil {
				t.Fatal(err)
			}
			r := req()
			g := maintenanceGrant{Token: "private-token", Status: MaintenanceStatus{State: "ready", OperationID: r.ID, Generation: strings.Repeat("f", 64), BootID: a.bootID, Owner: r.Requester, Purpose: "kit-software-activation"}}
			if err := a.saveGrant(r.ID, g); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(a.keyPath + ".grant-" + r.ID)
			resumes := 0
			a.client = &http.Client{Transport: acknowledgmentTransport(func(request *http.Request) (*http.Response, error) {
				var out any
				status := 200
				switch request.URL.Path {
				case "/status":
					held := g.Status
					if resumes > 0 && scenario == "replaced" {
						held.Generation = strings.Repeat("e", 64)
					}
					out = AdmissionStatus{Maintenance: held}
				case "/resume":
					resumes++
					var received map[string]string
					if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
						t.Fatal(err)
					}
					if received["credential"] != g.Token || received["expected_generation"] != g.Status.Generation {
						t.Fatal("stored credential not used")
					}
					out = g.Status
					if scenario == "bad-token" {
						status = 403
					}
				default:
					t.Fatal("repair attempted acquisition or another mutation", request.URL.Path)
				}
				b, _ := json.Marshal(out)
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b))), Header: make(http.Header)}, nil
			})}
			err = (acknowledgmentAdmission{admission: a, boot: a.bootID}).Acquire(context.Background(), r)
			if (err == nil) != (scenario == "ready") {
				t.Fatalf("%s: %v", scenario, err)
			}
			after, _ := os.ReadFile(a.keyPath + ".grant-" + r.ID)
			if string(before) != string(after) {
				t.Fatal("repair rewrote private grant")
			}
			if resumes != 1 {
				t.Fatal("resume repeated", resumes)
			}
		})
	}
}
