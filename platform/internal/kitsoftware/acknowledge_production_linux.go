package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
)

const acknowledgmentRunDir = "/run/fogcast/software"

// AcknowledgeRunningPrior is a root-only local operator repair. It never starts,
// stops or signals any service and cannot deploy an image or select a bundle.
// Its process proof deliberately supports a still-running older supervisor.
func AcknowledgeRunningPrior(ctx context.Context, req AcknowledgeRequest) (AcknowledgeResult, error) {
	var result AcknowledgeResult
	if os.Geteuid() != 0 {
		return result, errors.New("software acknowledgment requires root")
	}
	if err := req.validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := acknowledgmentMachine(ctx, req); err != nil {
		return result, err
	}
	raw, err := os.ReadFile("/etc/fes/software-base.json")
	if err != nil {
		return result, err
	}
	var base software.Compatibility
	if err := json.Unmarshal(raw, &base); err != nil {
		return result, err
	}
	store, err := software.OpenPersistent(base)
	if err != nil {
		return result, err
	}
	control := filepath.Join(store.Root(), "control")
	// An operator repair can never initialize missing state or mint a new key.
	for _, name := range []string{initializedMarker, "state.json", "admission.key"} {
		info, err := os.Lstat(filepath.Join(control, name))
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() {
			return result, errors.New("repair state is not regular")
		}
	}
	repo, err := OpenRepository(control, req.Selected)
	if err != nil {
		return result, err
	}
	defer repo.Close()
	admission, err := NewUnixAdmission("/run/fogcast/software-admission.sock", filepath.Join(control, "admission.key"))
	if err != nil {
		return result, err
	}
	const factoryPath = "/usr/share/fes/software"
	manifestFile, err := os.Open(filepath.Join(factoryPath, "manifest.json"))
	if err != nil {
		return result, err
	}
	manifest, err := software.DecodeManifest(io.LimitReader(manifestFile, software.MaxManifestSize+1))
	manifestFile.Close()
	if err != nil {
		return result, err
	}
	factoryID, err := manifest.ID()
	if err != nil {
		return result, err
	}
	factory := software.Release{ID: factoryID, Manifest: manifest}
	controller := &ProcessController{RunDir: acknowledgmentRunDir, Admission: admission, BaseIdentity: req.Base, Resolve: func(ctx context.Context, id string) (string, error) {
		return ResolveRelease(ctx, id, factoryPath, factory, base, store)
	}}
	if err := controller.Verify(ctx, req.Selected); err != nil {
		return result, err
	}
	proof, err := captureRunningPrior(ctx, req, controller)
	if err != nil {
		return result, err
	}
	// The worker lock is acquired before any grant resume or journal write. The
	// running group may not be replaced between samples or during acknowledgment.
	return acknowledgePrior(ctx, repo, acknowledgmentAdmission{admission: admission, boot: req.Boot}, req, func(ctx context.Context) error {
		return stableAcknowledgment(ctx, func(ctx context.Context) error {
			if err := acknowledgmentMachine(ctx, req); err != nil {
				return err
			}
			return proof.healthy(ctx, req, controller)
		})
	})
}
func acknowledgmentMachine(ctx context.Context, req AcknowledgeRequest) error {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(boot)) != req.Boot {
		return errors.New("acknowledgment boot identity changed")
	}
	image, err := exec.CommandContext(ctx, "/usr/sbin/fes-kit-data", "identity").Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(image)) != req.Base {
		return errors.New("acknowledgment base identity changed")
	}
	raw, err := exec.CommandContext(ctx, "/usr/sbin/fes-kit-data", "status").Output()
	if err != nil {
		return err
	}
	var gate struct{ Trial, Pending, Corrupt bool }
	if err := json.Unmarshal(raw, &gate); err != nil {
		return err
	}
	if gate.Trial || gate.Pending || gate.Corrupt {
		return errors.New("appliance update blocks acknowledgment")
	}
	return nil
}

type runningPriorProof struct {
	supervisorExecutable string
	runDir               string
	supervisor           int
	identities           map[int]string
	paths                map[int]string
}

func directChildren(parent int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var children []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		end := strings.LastIndex(string(raw), ")")
		if end < 0 {
			return nil, errors.New("invalid process stat")
		}
		fields := strings.Fields(string(raw)[end+1:])
		if len(fields) < 2 {
			return nil, errors.New("short process stat")
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, err
		}
		if ppid == parent {
			children = append(children, pid)
		}
	}
	return children, nil
}
func sameExecutable(pid int, path string) error {
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	expected, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(actual, expected) {
		return errors.New("running executable differs from verified release")
	}
	return nil
}
func captureRunningPrior(ctx context.Context, req AcknowledgeRequest, c *ProcessController) (runningPriorProof, error) {
	return captureRunningPriorAt(ctx, req, c, acknowledgmentRunDir, "/usr/sbin/fes-kit-software")
}

func captureRunningPriorAt(ctx context.Context, req AcknowledgeRequest, c *ProcessController, runDir, supervisorExecutable string) (runningPriorProof, error) {
	proof := runningPriorProof{runDir: runDir, supervisorExecutable: supervisorExecutable, identities: map[int]string{}, paths: map[int]string{}}
	raw, err := os.ReadFile(filepath.Join(runDir, "supervisor.pid"))
	if err != nil {
		return proof, err
	}
	proof.supervisor, err = strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || proof.supervisor <= 1 {
		return proof, errors.New("invalid supervisor PID")
	}
	if err := sameExecutable(proof.supervisor, supervisorExecutable); err != nil {
		return proof, err
	}
	root, err := c.preparedPath(ctx, req.Selected)
	if err != nil {
		return proof, err
	}
	children, err := directChildren(proof.supervisor)
	if err != nil {
		return proof, err
	}
	if len(children) != 3 {
		return proof, errors.New("supervisor does not own exactly three services")
	}
	expected := []string{"mister-runtime", "mister-agent", "fogcast-kit", "fogcast-tenfoot"}
	matched := map[string]int{}
	for _, pid := range children {
		name := ""
		for _, candidate := range expected {
			path := filepath.Join(root, "bin", candidate)
			if sameExecutable(pid, path) == nil {
				name = candidate
				proof.paths[pid] = path
				break
			}
		}
		if name == "" || matched[name] != 0 {
			return proof, errors.New("unknown or duplicate supervised process")
		}
		matched[name] = pid
	}
	launcher := matched["fogcast-kit"]
	if launcher == 0 {
		launcher = matched["fogcast-tenfoot"]
	}
	if matched["mister-runtime"] == 0 || matched["mister-agent"] == 0 || launcher == 0 {
		return proof, errors.New("prior service group incomplete")
	}
	for _, pid := range append(children, proof.supervisor) {
		identity, err := processStartTime(pid)
		if err != nil {
			return proof, err
		}
		proof.identities[pid] = identity
	}
	c.active = req.Selected
	for _, pid := range []int{matched["mister-runtime"], matched["mister-agent"], launcher} {
		// These are observations, never owned child handles: no Wait or Stop is
		// called on this controller. Healthy reads /proc and the readiness marker.
		c.children = append(c.children, &child{cmd: &exec.Cmd{Process: &os.Process{Pid: pid}}, path: proof.paths[pid], done: make(chan struct{})})
	}
	return proof, nil
}
func (p runningPriorProof) healthy(ctx context.Context, req AcknowledgeRequest, c *ProcessController) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(p.runDir, "supervisor.pid"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) != strconv.Itoa(p.supervisor) {
		return errors.New("supervisor PID publication changed")
	}
	for pid, identity := range p.identities {
		current, err := processStartTime(pid)
		if err != nil {
			return err
		}
		if current != identity {
			return errors.New("prior process identity changed")
		}
	}
	if err := sameExecutable(p.supervisor, p.supervisorExecutable); err != nil {
		return err
	}
	children, err := directChildren(p.supervisor)
	if err != nil {
		return err
	}
	if len(children) != 3 {
		return errors.New("prior process membership changed")
	}
	for _, pid := range children {
		if p.paths[pid] == "" {
			return errors.New("prior process parent identity changed")
		}
	}
	root, err := c.preparedPath(ctx, req.Selected)
	if err != nil {
		return err
	}
	current, err := os.Readlink(filepath.Join(p.runDir, "current"))
	if err != nil {
		return err
	}
	if filepath.Clean(current) != root {
		return errors.New("supervisor current release differs from selected prior")
	}
	return c.Healthy(ctx, req.Selected)
}

// Do not turn a missing or unrelated maintenance reservation into new operator
// authority. Only the existing durable grant from this exact failed operation
// may be resumed. The grant never leaves this process or enters public output.
type acknowledgmentAdmission struct {
	admission *UnixAdmission
	boot      string
}

func (a acknowledgmentAdmission) Acquire(ctx context.Context, r Request) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	grant, err := a.admission.loadGrant(r.ID)
	if err != nil {
		return err
	}
	if grant.Token == "" || grant.Status.OperationID != r.ID || grant.Status.BootID != a.boot || !software.ValidID(grant.Status.Generation) || grant.Status.Owner != r.Requester || grant.Status.Purpose != "kit-software-activation" {
		return errors.New("exact failed maintenance grant unavailable for acknowledgment resume")
	}
	matches := func(held MaintenanceStatus) bool {
		return held.OperationID == r.ID && held.Generation == grant.Status.Generation && held.BootID == a.boot && held.Owner == r.Requester && held.Purpose == "kit-software-activation"
	}
	status, err := a.admission.Status(ctx)
	if err != nil {
		return err
	}
	if !matches(status.Maintenance) {
		return errors.New("failed operation no longer owns this boot's maintenance reservation")
	}
	if state := status.Maintenance.State; state != "blocked" && state != "ready" && state != "preparing" {
		return errors.New("failed maintenance reservation is not resumable")
	}
	// Even ready reservations must authenticate the stored token. Resume cannot
	// allocate or replace a grant; it rejects stale generations and credentials.
	var resumed MaintenanceStatus
	if err := a.admission.call(ctx, "/resume", completion(grant), &resumed); err != nil {
		return err
	}
	if !matches(resumed) {
		return errors.New("resumed maintenance identity changed")
	}
	for {
		status, err = a.admission.Status(ctx)
		if err != nil {
			return err
		}
		held := status.Maintenance
		if !matches(held) {
			return errors.New("resumed maintenance identity changed")
		}
		if held.State == "ready" {
			return nil
		}
		if held.State != "preparing" {
			return errors.New("resumed maintenance did not become ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (a acknowledgmentAdmission) Release(ctx context.Context, r Request) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	grant, err := a.admission.loadGrant(r.ID)
	if err != nil {
		return err
	}
	if grant.Token == "" || grant.Status.OperationID != r.ID || grant.Status.BootID != a.boot || grant.Status.Generation == "" || grant.Status.Owner != r.Requester || grant.Status.Purpose != "kit-software-activation" {
		return errors.New("exact failed maintenance grant unavailable for acknowledgment release")
	}
	for {
		var status MaintenanceStatus
		if err := a.admission.call(ctx, "/end", completion(grant), &status); err != nil {
			return err
		}
		if status.State == "none" || status.State == "completed" {
			return nil
		}
		if status.State != "finishing" || status.OperationID != r.ID || status.Generation != grant.Status.Generation || status.BootID != a.boot {
			return errors.New("acknowledgment completion identity changed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
