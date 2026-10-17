package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdmissionUncertainDistinguishesStatus(t *testing.T) {
	if AdmissionUncertain(nil) {
		t.Fatal("nil error")
	}
	if AdmissionUncertain(&admissionResponseError{Path: "/fence", Status: 500, Body: "rejected"}) {
		t.Fatal("HTTP rejection is a known result")
	}
	busy := &busyAdmissionError{status: &admissionResponseError{Path: "/cancel", Status: 409, Body: "busy"}}
	if AdmissionUncertain(busy) || !errors.Is(busy, ErrBusy) {
		t.Fatal("busy HTTP status", AdmissionUncertain(busy), busy)
	}
	if !AdmissionUncertain(errors.New("timeout")) {
		t.Fatal("transport error")
	}
}

func TestAdmissionPreparingResumeAndLostEndReply(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "admission.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	phase := "preparing"
	calls := 0
	completed := false
	failReply := true
	bootBytes, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	boot := strings.TrimSpace(string(bootBytes))
	id := strings.Repeat("a", 64)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/acquire":
			if completed {
				http.Error(w, "completed cannot acquire", 409)
				return
			}
			calls++
			if calls == 2 {
				phase = "blocked"
			}
			json.NewEncoder(w).Encode(maintenanceGrant{Status: MaintenanceStatus{State: phase, OperationID: id, Generation: "gen", BootID: boot}, Token: "token"})
		case "/resume":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			if req["credential"] != "token" || req["operation_id"] != id || req["expected_generation"] != "gen" {
				t.Error("resume lost grant identity")
			}
			phase = "ready"
			json.NewEncoder(w).Encode(MaintenanceStatus{State: "preparing"})
		case "/end":
			completed = true
			if failReply {
				failReply = false
				http.Error(w, "lost completion response", 503)
				return
			}
			json.NewEncoder(w).Encode(MaintenanceStatus{State: "none"})
		default:
			http.NotFound(w, r)
		}
	})
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	defer server.Close()
	key := filepath.Join(dir, "admission.key")
	a, err := NewUnixAdmission(socket, key)
	if err != nil {
		t.Fatal(err)
	}
	r := Request{ID: id, Requester: "host", Candidate: "candidate"}
	if err := a.Acquire(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(context.Background(), r); err == nil {
		t.Fatal("lost response not reported")
	}
	restarted, err := NewUnixAdmission(socket, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Release(context.Background(), r); err != nil {
		t.Fatal("private receipt did not recover completed end", err)
	}
	if a.Claim(r).Credential != restarted.Claim(r).Credential {
		t.Fatal("credential did not survive restart")
	}
}
func TestStateInvalidOwnerNeverQueues(t *testing.T) {
	r, _, _, _ := setup(t)
	request := req()
	request.Requester = "bad\towner"
	if Queue(r, request) == nil {
		t.Fatal("unadmittable owner accepted")
	}
	s, _ := r.Read()
	if s.Pending != nil {
		t.Fatal("bad request persisted")
	}
}
func TestBootRecoveryNeverRetriesSpentAttempt(t *testing.T) {
	r, e, c, _ := setup(t)
	Queue(r, req())
	r.Update(func(s *State) error { s.Pending.Phase = "recovering"; return nil })
	if err := e.Boot(context.Background(), req()); err == nil {
		t.Fatal("spent recovery boot accepted")
	}
	if c.starts != 0 {
		t.Fatal("spent recovery restarted processes")
	}
}
func TestBootInterruptedCandidateConsumesRecovery(t *testing.T) {
	r, e, c, _ := setup(t)
	Queue(r, req())
	r.Update(func(s *State) error { s.Pending.Phase = "applying"; return nil })
	if err := e.Boot(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if c.active != testFactoryID || c.starts != 1 || s.Pending != nil || s.Receipts[req().ID].Result != "recovered" {
		t.Fatal(s, c)
	}
}
func TestStopConfirmsProcessExit(t *testing.T) {
	dir := t.TempDir()
	c := &ProcessController{RunDir: dir}
	if err := c.start("/bin/sleep", []string{"60"}, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.children) != 0 {
		t.Fatal("stopped children retained")
	}
}
func TestReleaseDoesNotIgnoreCorruptGrant(t *testing.T) {
	dir := t.TempDir()
	a, err := NewUnixAdmission(filepath.Join(dir, "none.sock"), filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Release(context.Background(), req()); err == nil || errors.Is(err, ErrBusy) {
		t.Fatal("missing grant silently released", err)
	}
}

func TestAcceptedProcessDeathReportsFaultWithoutRestart(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir()}
	if err := c.start("/bin/true", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	c.active = "accepted"
	<-c.children[0].done
	if !strings.Contains(c.ServiceFault(), "explicit supervisor restart") {
		t.Fatal(c.ServiceFault())
	}
	if c.Active() != "accepted" || len(c.children) != 1 {
		t.Fatal("service death changed accepted selection")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownFitsApplianceWatchdogBudget(t *testing.T) {
	if 3*(StopTermGrace+StopKillGrace) >= StopGroupTimeout {
		t.Fatal("group deadline does not cover every member")
	}
	if StopGroupTimeout >= StopCommandTimeout {
		t.Fatal("stop command deadline too short")
	}
	if StopCommandTimeout+5*time.Second >= 30*time.Second {
		t.Fatal("software and other init stops exceed watchdog fallback budget")
	}
}
