package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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
	paths := []string{"mister-runtime", "mister-agent", "fogcast-tenfoot"}
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

func TestAcknowledgmentHeldOnlyReadsExactReservation(t *testing.T) {
	for _, scenario := range []string{"blocked", "ready", "preparing", "finishing", "replaced", "other-boot", "grant-boot", "bad-token"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			a, err := NewUnixAdmission(filepath.Join(dir, "absent.sock"), filepath.Join(dir, "key"))
			if err != nil {
				t.Fatal(err)
			}
			r := req()
			g := maintenanceGrant{Token: strings.Repeat("a", 64), Status: MaintenanceStatus{State: "ready", OperationID: r.ID, Generation: strings.Repeat("f", 64), BootID: a.bootID, Owner: r.Requester, Purpose: "kit-software-activation"}}
			if scenario == "grant-boot" {
				g.Status.BootID = "other"
			}
			if err := a.saveGrant(r.ID, g); err != nil {
				t.Fatal(err)
			}
			held := g.Status
			held.BootID = a.bootID
			switch scenario {
			case "blocked", "preparing", "finishing":
				held.State = scenario
			case "replaced":
				held.Generation = strings.Repeat("e", 64)
			case "other-boot":
				held.BootID = "22222222-3333-4444-5555-666666666666"
			}
			record := powerCycleMarker{Schema: 1, MaintenanceStatus: held, Token: g.Token, StartToken: strings.Repeat("b", 64)}
			if scenario == "bad-token" {
				record.Token = strings.Repeat("c", 64)
			}
			marker, lock := filepath.Join(dir, "kit-maintenance.json"), filepath.Join(dir, "kit-maintenance.lock")
			mb, _ := json.Marshal(record)
			if err := os.WriteFile(marker, mb, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lock, nil, 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(a.keyPath + ".grant-" + r.ID)
			statuses := 0
			a.client = &http.Client{Transport: acknowledgmentTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet || request.URL.Path != "/status" {
					t.Fatal("reservation check made a mutation", request.URL.Path)
				}
				statuses++
				b, _ := json.Marshal(AdmissionStatus{Maintenance: held})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))), Header: make(http.Header)}, nil
			})}
			err = (acknowledgmentAdmission{admission: a, boot: a.bootID, lockPath: lock, markerPath: marker}).Held(context.Background(), r)
			if (err == nil) != (scenario == "blocked" || scenario == "ready") {
				t.Fatalf("%s: %v", scenario, err)
			}
			if scenario == "bad-token" && err.Error() != "stored grant does not authenticate this blocked or ready maintenance reservation" {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(a.keyPath + ".grant-" + r.ID)
			afterMarker, _ := os.ReadFile(marker)
			if string(before) != string(after) || string(afterMarker) != string(mb) {
				t.Fatal("reservation check rewrote private grant or marker")
			}
			f, err := os.OpenFile(lock, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal("reservation check kept the marker lock", err)
			}
			if scenario == "grant-boot" && statuses != 0 {
				t.Fatal("invalid grant contacted admission")
			}
		})
	}
}

// maintenanceAgent keeps the target agent's reservation on its marker file the
// way the agent does: /resume makes it ready and /end retires it. posts lists
// every mutation.
type maintenanceAgent struct {
	t      *testing.T
	marker string
	posts  []string
}

func (m *maintenanceAgent) RoundTrip(request *http.Request) (*http.Response, error) {
	var record powerCycleMarker
	if err := readPrivatePowerCycleJSON(m.marker, &record); err != nil {
		m.t.Fatal(err)
	}
	var out any = AdmissionStatus{Maintenance: record.MaintenanceStatus}
	if request.Method == http.MethodPost {
		m.posts = append(m.posts, request.URL.Path)
		var received map[string]string
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil || received["credential"] != record.Token || received["expected_generation"] != record.Generation {
			m.t.Fatal("reservation mutation without the stored grant", request.URL.Path, err)
		}
		switch request.URL.Path {
		case "/resume":
			record.State = "ready"
		case "/end":
			record.State = "completed"
		default:
			m.t.Fatal("unexpected admission mutation", request.URL.Path)
		}
		b, _ := json.Marshal(record)
		if err := os.WriteFile(m.marker, b, 0600); err != nil {
			m.t.Fatal(err)
		}
		status := record.MaintenanceStatus
		if status.State == "completed" {
			status.State = "none"
		}
		out = status
	}
	b, _ := json.Marshal(out)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))), Header: make(http.Header)}, nil
}

// Kit B: acknowledge resumed the failed operation's reservation, then found no
// launcher readiness, and prepare-power-cycle refused the reservation it left.
func TestFailedAcknowledgmentLeavesBlockedReservationForPowerCycle(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	dir := t.TempDir()
	a, err := NewUnixAdmission(filepath.Join(dir, "absent.sock"), filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	request.Boot = a.bootID
	state, _ := r.Read()
	failed := *state.Pending
	held := MaintenanceStatus{State: "blocked", OperationID: failed.ID, Generation: strings.Repeat("f", 64), Owner: failed.Requester, Purpose: "kit-software-activation", BootID: a.bootID, Reason: "agent restarted during maintenance; explicit finish required"}
	grant := maintenanceGrant{Status: held, Token: strings.Repeat("a", 64)}
	if err := a.saveGrant(failed.ID, grant); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "kit-maintenance.json")
	lock := filepath.Join(dir, "kit-maintenance.lock")
	blocked, _ := json.Marshal(powerCycleMarker{Schema: 1, MaintenanceStatus: held, Token: grant.Token, StartToken: strings.Repeat("b", 64)})
	if err := os.WriteFile(marker, blocked, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	agent := &maintenanceAgent{t: t, marker: marker}
	a.client = &http.Client{Transport: agent}
	reservation := acknowledgmentAdmission{admission: a, boot: a.bootID, lockPath: lock, markerPath: marker}
	missing := errors.New("open /run/fogcast/software/launcher-ready.json: no such file or directory")
	if _, err := acknowledgePrior(context.Background(), r, reservation, request, func(context.Context) error {
		return fmt.Errorf("prior release cannot be acknowledged: %w", missing)
	}); !errors.Is(err, missing) {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(marker); len(agent.posts) != 0 || string(after) != string(blocked) {
		t.Fatal("failed acknowledgment changed the reservation", agent.posts)
	}
	state, _ = r.Read()
	if state.Pending == nil || *state.Pending != failed || len(state.Receipts) != 0 {
		t.Fatal("failed acknowledgment changed the journal", state)
	}
	got, unlock, err := lockMaintenanceReservation(context.Background(), lock, marker, a.keyPath+".grant-"+failed.ID, request.Operation, request.Boot, failed.Requester, "blocked")
	if err != nil || got.State != "blocked" {
		t.Fatal("power-cycle preparation lost the blocked reservation", got, err)
	}
	unlock()
	// Once the prior group is healthy the same reservation is ended, never resumed.
	result, err := acknowledgePrior(context.Background(), r, reservation, request, func(context.Context) error { return nil })
	if err != nil || !result.Completed {
		t.Fatal(result, err)
	}
	if len(agent.posts) != 1 || agent.posts[0] != "/end" {
		t.Fatal("acknowledgment did more than end the reservation", agent.posts)
	}
	state, _ = r.Read()
	if state.Pending != nil || state.Unreleased != nil || state.Receipts[failed.ID].Result != "recovered" {
		t.Fatal(state)
	}
}
