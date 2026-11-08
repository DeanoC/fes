package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	DiagnosticPongTargetID  = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	DiagnosticPongPackageID = "9be4b59993cfd0ac6b347da42decb340235c570d310901468778d6dad15d0120"
	serviceFogCastKit       = "fogcast-kit"
	serviceTenfoot          = "fogcast-tenfoot"
)

var errNamedRestartSuperseded = errors.New("named service restart was superseded by existing supervisor recovery")

type namedRestartTarget struct {
	child *child
	path  string
	args  []string
	env   []string
}

func (c *ProcessController) RestartNamedService(
	ctx context.Context,
	request ServiceRestartRequest,
	state State,
	admission *UnixAdmission,
	checkpoint func(ServiceRestartRecord) error,
	record ServiceRestartRecord,
) ServiceRestartRecord {
	started := time.Now()
	finish := func(outcome, phase, reason string) ServiceRestartRecord {
		now := time.Now().UTC()
		record.Outcome = outcome
		record.Phase = phase
		record.Reason = reason
		record.UpdatedAt = now
		record.FinishedAt = &now
		record.ElapsedMS = time.Since(started).Milliseconds()
		if checkpoint != nil {
			if err := checkpoint(record); err != nil {
				record.Outcome = "failed"
				record.Phase = "failed"
				record.Reason = appendRestartReason(record.Reason, "persist final restart result: "+err.Error())
			}
		}
		return record
	}
	refuse := func(reason string) ServiceRestartRecord { return finish("refused", "refused", reason) }
	fail := func(reason string) ServiceRestartRecord { return finish("failed", "failed", reason) }

	if request.Service != serviceFogCastKit && request.Service != serviceTenfoot {
		return refuse("service is not allowlisted")
	}
	if checkpoint == nil {
		return fail("service restart result ledger is unavailable")
	}
	if state.Pending != nil || state.Unreleased != nil {
		return refuse("software activation or recovery is pending")
	}
	if state.Selected == "" || c == nil || admission == nil {
		return refuse("active selected bundle or admission status is unavailable")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingGroup != "" || acceptedServiceFailed(c.children) {
		return refuse("service group recovery is active")
	}
	if c.active == "" || c.active != state.Selected || c.prepared == nil || c.prepared.id != c.active {
		return refuse("selected active bundle is not the supervisor's verified running bundle")
	}
	root, err := c.preparedPath(ctx, c.active)
	if err != nil {
		return refuse("active bundle verification failed: " + err.Error())
	}
	current, err := os.Readlink(filepath.Join(c.RunDir, "current"))
	if err != nil || filepath.Clean(current) != root {
		return refuse("current release link does not identify the active selected bundle")
	}
	if len(c.children) != 3 || c.children[0].role != roleRuntime || c.children[1].role != roleAgent || c.children[2].role != roleLauncher {
		return refuse("supervised service membership is incomplete")
	}
	if c.host == nil || c.host.role != roleHost || c.hostPlan == nil {
		return refuse("supervised host service is unavailable")
	}
	if filepath.Clean(c.children[2].path) != filepath.Join(root, "bin/fogcast-tenfoot") ||
		filepath.Clean(c.host.path) != filepath.Join(root, hostExecutable) ||
		filepath.Clean(c.hostPlan.path) != filepath.Join(root, hostExecutable) {
		return refuse("allowlisted service executable is outside the active selected bundle")
	}
	if err := c.ensureNamedSiblingsAlive(nil); err != nil {
		return refuse(err.Error())
	}
	target, err := c.namedRestartTarget(request.Service)
	if err != nil {
		return refuse(err.Error())
	}
	oldIdentity, err := processIdentity(target.child)
	if err != nil {
		return refuse("named service identity is stale: " + err.Error())
	}
	if oldIdentity.ProcessGroupID != oldIdentity.PID {
		return refuse("named service is not in its supervisor-owned process group")
	}
	siblings, err := c.captureNamedSiblings(request.Service)
	if err != nil {
		return refuse(err.Error())
	}
	session, err := c.readNamedRestartSession(ctx, admission, c.active)
	if err != nil {
		return refuse("cannot establish runtime/session status: " + err.Error())
	}
	if err := authorizeNamedRestartSession(request, session); err != nil {
		return refuse(err.Error())
	}
	if request.Service == serviceFogCastKit && c.serviceRestartHostProbe == nil {
		// The production host probe is never optional. This check also catches
		// a missing or unreadable target identity before the old host is stopped.
		if session.TargetID == "" {
			return refuse("target identity is unavailable for host health verification")
		}
	}

	record.BundleID = c.active
	record.Old = &oldIdentity
	record.SessionBefore = &session
	record.Phase = "stopping"
	record.UpdatedAt = time.Now().UTC()
	if err := checkpoint(record); err != nil {
		return fail("persist old service identity before stop: " + err.Error())
	}
	if err := stopChild(ctx, target.child); err != nil {
		record.StopResult = "old-process-exit-or-process-group-empty-not-proven"
		return fail("old service did not stop cleanly: " + err.Error())
	}
	record.StopResult = "old-process-exited-and-process-group-empty"
	record.Phase = "stopped"
	record.UpdatedAt = time.Now().UTC()
	if err := checkpoint(record); err != nil {
		return fail("persist old process exit proof: " + err.Error())
	}
	if err := c.ensureNamedSiblingsAlive(siblings); err != nil {
		return finish("superseded", "superseded", err.Error())
	}
	afterStop, err := c.readNamedRestartSession(ctx, admission, c.active)
	if err != nil || afterStop != session {
		reason := "runtime session changed while stopping named service"
		if err != nil {
			reason += ": " + err.Error()
		}
		return finish("superseded", "superseded", reason)
	}
	if err := authorizeNamedRestartSession(request, afterStop); err != nil {
		return finish("superseded", "superseded", err.Error())
	}
	record.Phase = "starting"
	record.UpdatedAt = time.Now().UTC()
	if err := checkpoint(record); err != nil {
		return fail("persist replacement-start phase: " + err.Error())
	}
	if request.Service == serviceTenfoot {
		if err := os.Remove(filepath.Join(c.RunDir, "launcher-ready.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail("remove stale Tenfoot readiness marker: " + err.Error())
		}
	}
	replacement, err := c.spawn(target.path, target.args, target.env, target.child.role)
	if err != nil {
		target.child.failedReadiness = true
		return fail("replacement process did not start: " + err.Error())
	}
	if request.Service == serviceFogCastKit {
		c.host = replacement
		c.hostRestart = time.Now()
		if c.hostDelay < launcherRestartMin {
			c.hostDelay = launcherRestartMin
		}
	} else {
		c.children[2] = replacement
		c.launcherRestart = time.Now()
		if c.launcherDelay < launcherRestartMin {
			c.launcherDelay = launcherRestartMin
		}
	}
	replacementIdentity, err := processIdentity(replacement)
	if err != nil {
		replacement.failedReadiness = true
		record.ReplacementStart = "started-identity-unavailable"
		_ = stopChild(ctx, replacement)
		return fail("replacement process identity unavailable: " + err.Error())
	}
	record.Replacement = &replacementIdentity
	record.ReplacementStart = "started"
	if replacementIdentity.PID == oldIdentity.PID && replacementIdentity.StartTime == oldIdentity.StartTime {
		replacement.failedReadiness = true
		_ = stopChild(ctx, replacement)
		return fail("replacement process identity did not change")
	}
	if err := c.ensureNamedSiblingsAlive(siblings); err != nil {
		return finish("superseded", "superseded", err.Error())
	}
	if err := checkpoint(func() ServiceRestartRecord {
		record.Phase = "readiness"
		record.UpdatedAt = time.Now().UTC()
		return record
	}()); err != nil {
		replacement.failedReadiness = true
		_ = stopChild(ctx, replacement)
		return fail("persist replacement process identity: " + err.Error())
	}
	probeName := "tenfoot-frame-ack-marker"
	if request.Service == serviceFogCastKit {
		probeName = "fogcast-kit-local-health-api"
	}
	record.ReadinessProbe = probeName
	if err := c.waitNamedServiceReady(ctx, request.Service, replacement, c.active, session, admission, siblings); err != nil {
		if errors.Is(err, errNamedRestartSuperseded) {
			return finish("superseded", "superseded", err.Error())
		}
		replacement.failedReadiness = true
		cleanupCtx, cancel := context.WithTimeout(context.Background(), StopGroupTimeout)
		cleanupErr := stopChild(cleanupCtx, replacement)
		cancel()
		record.ReadinessResult = "failed"
		record.Reason = appendRestartReason(err.Error(), cleanupErrorText(cleanupErr))
		return finish("failed", "failed", record.Reason)
	}
	confirmedReplacement, err := processIdentity(replacement)
	if err != nil || !sameProcessIdentity(replacementIdentity, confirmedReplacement) {
		replacement.failedReadiness = true
		cleanupCtx, cancel := context.WithTimeout(context.Background(), StopGroupTimeout)
		cleanupErr := stopChild(cleanupCtx, replacement)
		cancel()
		reason := "replacement process identity changed after readiness"
		if err != nil {
			reason += ": " + err.Error()
		}
		reason = appendRestartReason(reason, cleanupErrorText(cleanupErr))
		record.ReadinessResult = "failed"
		return finish("failed", "failed", reason)
	}
	finalSession, err := c.readNamedRestartSession(ctx, admission, c.active)
	if err != nil || finalSession != session {
		replacement.failedReadiness = true
		cleanupCtx, cancel := context.WithTimeout(context.Background(), StopGroupTimeout)
		cleanupErr := stopChild(cleanupCtx, replacement)
		cancel()
		reason := "runtime session changed before named-service restart completed"
		if err != nil {
			reason += ": " + err.Error()
		}
		reason = appendRestartReason(reason, cleanupErrorText(cleanupErr))
		return finish("superseded", "superseded", reason)
	}
	if err := c.ensureNamedSiblingsAlive(siblings); err != nil {
		return finish("superseded", "superseded", err.Error())
	}
	record.SessionAfter = &finalSession
	record.SiblingIdentitiesOK = true
	record.ReadinessResult = "positive-functional-readiness-stable"
	return finish("succeeded", "complete", "")
}

func appendRestartReason(left, right string) string {
	if right == "" {
		return left
	}
	if left == "" {
		return right
	}
	return left + "; " + right
}

func cleanupErrorText(err error) string {
	if err == nil {
		return ""
	}
	return "replacement cleanup failed: " + err.Error()
}

func (c *ProcessController) namedRestartTarget(service string) (namedRestartTarget, error) {
	switch service {
	case serviceFogCastKit:
		if c.host == nil || c.hostPlan == nil {
			return namedRestartTarget{}, errors.New("fogcast-kit is not a running supervisor child")
		}
		return namedRestartTarget{child: c.host, path: c.hostPlan.path, args: append([]string(nil), c.hostPlan.args...), env: append([]string(nil), c.hostPlan.env...)}, nil
	case serviceTenfoot:
		if len(c.children) != 3 || c.children[2] == nil || c.children[2].role != roleLauncher {
			return namedRestartTarget{}, errors.New("fogcast-tenfoot is not a running supervisor child")
		}
		launcher := c.children[2]
		return namedRestartTarget{child: launcher, path: launcher.path, args: append([]string(nil), launcher.args...), env: append([]string(nil), launcher.env...)}, nil
	default:
		return namedRestartTarget{}, errors.New("service is not allowlisted")
	}
}

func processIdentity(process *child) (ServiceProcessIdentity, error) {
	if process == nil || process.cmd == nil || process.cmd.Process == nil || process.cmd.Process.Pid <= 1 {
		return ServiceProcessIdentity{}, errors.New("supervisor child handle is incomplete")
	}
	select {
	case <-process.done:
		return ServiceProcessIdentity{}, ErrProcessExited
	default:
	}
	pid := process.cmd.Process.Pid
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ServiceProcessIdentity{}, err
	}
	close := strings.LastIndex(string(stat), ")")
	if close < 0 {
		return ServiceProcessIdentity{}, errors.New("invalid process stat")
	}
	fields := strings.Fields(string(stat)[close+1:])
	if len(fields) < 20 {
		return ServiceProcessIdentity{}, errors.New("short process stat")
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return ServiceProcessIdentity{}, err
	}
	executablePath := fmt.Sprintf("/proc/%d/exe", pid)
	executable, err := os.Stat(executablePath)
	if err != nil {
		return ServiceProcessIdentity{}, err
	}
	expected, err := os.Stat(process.path)
	if err != nil {
		return ServiceProcessIdentity{}, err
	}
	if !os.SameFile(executable, expected) {
		return ServiceProcessIdentity{}, errors.New("supervised executable differs from selected bundle")
	}
	statIdentity, ok := executable.Sys().(*syscall.Stat_t)
	if !ok {
		return ServiceProcessIdentity{}, errors.New("executable identity metadata unavailable")
	}
	startTime := fields[19]
	if startTime == "" {
		return ServiceProcessIdentity{}, errors.New("process start time is unavailable")
	}
	link, err := os.Readlink(executablePath)
	if err != nil {
		return ServiceProcessIdentity{}, err
	}
	return ServiceProcessIdentity{
		Role: process.role, PID: pid, StartTime: startTime, ProcessGroupID: group,
		Executable: link, ExecutableDevice: uint64(statIdentity.Dev), ExecutableInode: statIdentity.Ino,
		LifetimeMS: time.Since(process.started).Milliseconds(),
	}, nil
}

func sameProcessIdentity(left, right ServiceProcessIdentity) bool {
	return left.Role == right.Role && left.PID == right.PID && left.StartTime == right.StartTime &&
		left.ProcessGroupID == right.ProcessGroupID && left.ExecutableDevice == right.ExecutableDevice &&
		left.ExecutableInode == right.ExecutableInode
}

func (c *ProcessController) captureNamedSiblings(service string) (map[*child]ServiceProcessIdentity, error) {
	siblings := make(map[*child]ServiceProcessIdentity, 3)
	for _, process := range c.children {
		if service == serviceTenfoot && process.role == roleLauncher {
			continue
		}
		identity, err := processIdentity(process)
		if err != nil {
			return nil, fmt.Errorf("supervised sibling %s identity is unavailable: %w", process.role, err)
		}
		siblings[process] = identity
	}
	if service == serviceTenfoot {
		identity, err := processIdentity(c.host)
		if err != nil {
			return nil, fmt.Errorf("supervised sibling host identity is unavailable: %w", err)
		}
		siblings[c.host] = identity
	}
	if len(siblings) != 3 {
		return nil, errors.New("supervised sibling set is incomplete")
	}
	return siblings, nil
}

func (c *ProcessController) ensureNamedSiblingsAlive(expected map[*child]ServiceProcessIdentity) error {
	for index, process := range c.children {
		if index > 1 {
			break
		}
		select {
		case <-process.done:
			return fmt.Errorf("%w: %s exited during request", errNamedRestartSuperseded, process.role)
		default:
		}
	}
	if c.pendingGroup != "" {
		return fmt.Errorf("%w: service-group recovery took ownership", errNamedRestartSuperseded)
	}
	if expected == nil {
		return nil
	}
	for process, original := range expected {
		current, err := processIdentity(process)
		if err != nil || !sameProcessIdentity(original, current) {
			return fmt.Errorf("%w: supervised sibling %s identity changed", errNamedRestartSuperseded, original.Role)
		}
	}
	return nil
}

func (c *ProcessController) readNamedRestartSession(ctx context.Context, admission *UnixAdmission, bundle string) (ServiceRestartSession, error) {
	var result ServiceRestartSession
	if len(c.children) != 3 || c.children[0].role != roleRuntime || c.children[1].role != roleAgent {
		return result, errors.New("runtime and agent service processes are unavailable")
	}
	runtime := c.children[0]
	var native nativeStatus
	var err error
	if c.serviceRestartRuntimeProbe != nil {
		native, err = c.serviceRestartRuntimeProbe(ctx, runtime)
	} else {
		identity, identityErr := processIdentity(runtime)
		if identityErr != nil {
			return result, identityErr
		}
		native, err = readNativeStatus(ctx, "/run/mister-runtime.sock", runtime.cmd.Process.Pid, identity.StartTime, runtime.path)
	}
	if err != nil {
		return result, err
	}
	if !native.OK || native.State == "" || native.Execution == "" {
		return result, errors.New("native runtime did not report a positive status")
	}
	activePackageID := ""
	if len(native.ActivePackage) != 0 && string(native.ActivePackage) != "null" {
		var active struct {
			PackageID string `json:"package_id"`
		}
		if err := json.Unmarshal(native.ActivePackage, &active); err != nil {
			return result, errors.New("runtime active package status is malformed")
		}
		activePackageID = active.PackageID
	}
	targetIDPath := c.serviceRestartTargetIDPath
	if targetIDPath == "" {
		targetIDPath = "/media/fat/fogcast/target-id"
	}
	targetIDBytes, err := os.ReadFile(targetIDPath)
	if err != nil {
		return result, fmt.Errorf("read kit target identity: %w", err)
	}
	targetID := strings.TrimSpace(string(targetIDBytes))
	if targetID == "" || strings.ContainsAny(targetID, "\r\n\x00") {
		return result, errors.New("kit target identity is invalid")
	}
	admissionStatus, err := admission.Status(ctx)
	if err != nil {
		return result, err
	}
	if admissionStatus.BundleID != bundle {
		return result, errors.New("admission bundle differs from active selected bundle")
	}
	result = ServiceRestartSession{
		TargetID: targetID, State: native.State, Execution: native.Execution,
		PackageID: activePackageID, LeaseState: admissionStatus.ExecutionLease.State,
		LeaseGeneration: admissionStatus.ExecutionLease.Generation,
		LeaseOwner:      admissionStatus.ExecutionLease.Owner, LeasePurpose: admissionStatus.ExecutionLease.Purpose,
		MaintenanceState: admissionStatus.Maintenance.State, RawIdleReady: admissionStatus.RawIdleReady,
		UpdateBlocked: admissionStatus.UpdateBlocked, SoftwareBundleID: admissionStatus.BundleID,
	}
	if native.Core != nil {
		result.Core = *native.Core
	}
	if native.Generation != nil {
		result.RuntimeGeneration = *native.Generation
	}
	return result, nil
}

func authorizeNamedRestartSession(request ServiceRestartRequest, session ServiceRestartSession) error {
	if session.MaintenanceState != "none" {
		return errors.New("kit maintenance reservation is active or unavailable")
	}
	if session.UpdateBlocked {
		return errors.New("appliance update blocks named-service restart")
	}
	if request.Session == nil {
		if session.State != "idle" || session.Execution != "none" || session.LeaseState != "free" || !session.RawIdleReady {
			return errors.New("active or uncertain session requires the exact Pong diagnostic signature")
		}
		return nil
	}
	expected := request.Session
	if expected.TargetID != DiagnosticPongTargetID || expected.PackageID != DiagnosticPongPackageID || expected.Generation == 0 {
		return errors.New("live-session restart requires the exact diagnostic target, Pong package and runtime generation")
	}
	if session.TargetID != expected.TargetID || session.PackageID != expected.PackageID || session.RuntimeGeneration != expected.Generation ||
		session.Core != "fes.pong" || session.State != "running_development" || session.Execution != "development" {
		return errors.New("runtime session does not match the exact diagnostic Pong target/package/session")
	}
	if session.LeaseState != "held" || session.LeaseGeneration == "" || session.LeaseOwner == "" || session.LeasePurpose == "" {
		return errors.New("diagnostic runtime session has no stable held execution lease")
	}
	return nil
}

func (c *ProcessController) waitNamedServiceReady(
	ctx context.Context,
	service string,
	process *child,
	bundle string,
	session ServiceRestartSession,
	admission *UnixAdmission,
	siblings map[*child]ServiceProcessIdentity,
) error {
	budget := c.serviceRestartBudget
	if budget <= 0 {
		budget = groupRestartTimeout
	}
	stable := c.serviceRestartStable
	if stable <= 0 {
		stable = groupReadyStable
	}
	poll := c.serviceRestartPoll
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	var readySince time.Time
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if lastErr != nil {
				return fmt.Errorf("functional readiness deadline expired: %w", lastErr)
			}
			return errors.New("functional readiness deadline expired")
		default:
		}
		if err := c.ensureNamedSiblingsAlive(siblings); err != nil {
			return err
		}
		select {
		case <-process.done:
			return fmt.Errorf("replacement %s exited before functional readiness", service)
		default:
		}
		identity, err := processIdentity(process)
		if err != nil {
			return fmt.Errorf("replacement process identity changed: %w", err)
		}
		if identity.ProcessGroupID != identity.PID {
			return errors.New("replacement is outside its supervisor-owned process group")
		}
		currentSession, err := c.readNamedRestartSession(ctx, admission, bundle)
		if err != nil {
			lastErr = err
			readySince = time.Time{}
		} else if currentSession != session {
			return fmt.Errorf("%w: runtime session or execution lease changed", errNamedRestartSuperseded)
		} else if err := authorizeNamedRestartSession(ServiceRestartRequest{Session: sessionExpectation(session)}, currentSession); err != nil {
			return fmt.Errorf("%w: %v", errNamedRestartSuperseded, err)
		} else {
			probeErr := c.probeNamedServiceReady(ctx, service, process, bundle, session.TargetID)
			if probeErr != nil {
				lastErr = probeErr
				readySince = time.Time{}
			} else {
				confirmedIdentity, identityErr := processIdentity(process)
				if identityErr != nil || !sameProcessIdentity(identity, confirmedIdentity) {
					return errors.New("replacement process identity changed during readiness probe")
				}
				if readySince.IsZero() {
					readySince = time.Now()
				}
				if time.Since(readySince) >= stable {
					return nil
				}
			}
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-deadline.C:
			timer.Stop()
			if lastErr != nil {
				return fmt.Errorf("functional readiness deadline expired: %w", lastErr)
			}
			return errors.New("functional readiness deadline expired")
		case <-timer.C:
		}
	}
}

func sessionExpectation(session ServiceRestartSession) *DiagnosticSessionExpectation {
	if session.LeaseState != "held" {
		return nil
	}
	return &DiagnosticSessionExpectation{TargetID: session.TargetID, PackageID: session.PackageID, Generation: session.RuntimeGeneration}
}

func (c *ProcessController) probeNamedServiceReady(ctx context.Context, service string, process *child, bundle, targetID string) error {
	switch service {
	case serviceFogCastKit:
		if c.serviceRestartHostProbe != nil {
			return c.serviceRestartHostProbe(ctx, targetID)
		}
		return probeFogCastKitHealth(ctx, targetID)
	case serviceTenfoot:
		if c.serviceRestartLauncherProbe != nil {
			return c.serviceRestartLauncherProbe(ctx, process, bundle)
		}
		return probeTenfootReady(filepath.Join(c.RunDir, "launcher-ready.json"), process, bundle)
	default:
		return errors.New("service is not allowlisted")
	}
}

type fogCastKitHealth struct {
	Ready  bool `json:"ready"`
	Target struct {
		Reachable  bool `json:"reachable"`
		Ready      bool `json:"ready"`
		Connection *struct {
			TargetID string `json:"target_id"`
		} `json:"connection"`
	} `json:"target"`
}

func probeFogCastKitHealth(ctx context.Context, expectedTarget string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8787/api/v1/health", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("host health API returned HTTP %d", response.StatusCode)
	}
	return validateFogCastKitHealth(response.Body, expectedTarget)
}

func validateFogCastKitHealth(body io.Reader, expectedTarget string) error {
	var health fogCastKitHealth
	decoder := json.NewDecoder(io.LimitReader(body, 64<<10))
	if err := decoder.Decode(&health); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("host health API response has trailing data")
	}
	if !health.Ready || !health.Target.Reachable || !health.Target.Ready || health.Target.Connection == nil || health.Target.Connection.TargetID != expectedTarget {
		return errors.New("host health API did not confirm ready host and expected target")
	}
	return nil
}

func probeTenfootReady(path string, process *child, bundle string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 {
		return errors.New("Tenfoot readiness marker is not a private bounded file")
	}
	var ready struct {
		BundleID string `json:"bundle_id"`
		PID      int    `json:"pid"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ready); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("Tenfoot readiness marker has trailing data")
	}
	if ready.BundleID != bundle || ready.PID != process.cmd.Process.Pid {
		return errors.New("Tenfoot frame acknowledgement does not identify replacement bundle and PID")
	}
	return nil
}
