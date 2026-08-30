//go:build linux && fpgadev

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
	"golang.org/x/sys/unix"
)

type installCommandRunner interface {
	Install(context.Context, string) error
	Recover(context.Context) error
	Uninstall(context.Context) error
}

type recordedAgentObserver interface {
	SnapshotContext(context.Context) ([]fpgadev.ProcessIdentity, error)
}

type recordedAgentTarget struct {
	path     string
	observer recordedAgentObserver
}

type recordedAgentSignaler func(context.Context, recordedAgentObserver, fpgadev.ProcessIdentity, syscall.Signal) error

const (
	productionAgentExecutable     = "/usr/bin/mister-agent"
	productionLegacyAgent         = "/usr/sbin/mister-agent"
	productionAgentFATExecutable  = "/media/fat/mister-remote/mister-agent"
	productionLegacyAgentConfig   = "/media/fat/mister-remote/agent.toml"
	productionLegacySupervisor    = "/usr/sbin/mister-supervise"
	productionAgentTerminateGrace = 2 * time.Second
	productionAgentStableAbsence  = 1500 * time.Millisecond
	productionAgentPollInterval   = 10 * time.Millisecond
)

// stopAgentOnlyWhenAbsent retains the fail-closed single-scan absence rule for
// focused validation. Production stop/proof below strengthens it with actual
// termination and stable absence across every configured executable path.
func stopAgentOnlyWhenAbsent(observer recordedAgentObserver) func(context.Context) error {
	return func(ctx context.Context) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if observer == nil {
			return errors.New("agent observer is unavailable")
		}
		identities, err := observer.SnapshotContext(ctx)
		if err != nil {
			return err
		}
		if len(identities) != 0 {
			return errors.New("agent process remains present")
		}
		return ctx.Err()
	}
}

// stopRecordedAgents first attempts to quiesce the legacy boot authority, then
// terminates every exactly observed agent even when the first authority proof
// failed. Once the agents are absent it retries that proof, allowing a resolved
// observation race to proceed while a persistent authority error remains
// fail-closed. A TERM-resistant process is escalated to KILL; success still
// requires a stable complete-scan absence interval so a supervisor respawn
// cannot pass through a transient empty snapshot.
func stopRecordedAgents(targets []recordedAgentTarget, stopAuthority func(context.Context) error, signal recordedAgentSignaler) func(context.Context) error {
	return func(ctx context.Context) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if stopAuthority == nil || signal == nil {
			return errors.New("agent stop dependency is unavailable")
		}
		if len(targets) == 0 {
			return errors.New("agent observer is unavailable")
		}
		authorityErr := stopAuthority(ctx)
		stopErr := stopRecordedTargets(ctx, targets, signal, productionAgentStableAbsence)
		if authorityErr != nil && stopErr == nil {
			authorityErr = stopAuthority(ctx)
		}
		return errors.Join(authorityErr, stopErr)
	}
}

func proveRecordedAgentsAbsent(targets []recordedAgentTarget) func(context.Context) error {
	return func(ctx context.Context) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(targets) == 0 {
			return errors.New("agent observer is unavailable")
		}
		return waitRecordedTargetsStableAbsent(ctx, targets, productionAgentStableAbsence)
	}
}

func stopRecordedTargets(ctx context.Context, targets []recordedAgentTarget, signal recordedAgentSignaler, stable time.Duration) error {
	return stopRecordedTargetsWithGrace(ctx, targets, signal, productionAgentTerminateGrace, stable)
}

func stopRecordedTargetsWithGrace(ctx context.Context, targets []recordedAgentTarget, signal recordedAgentSignaler, grace, stable time.Duration) error {
	if signal == nil {
		return errors.New("agent stop dependency is unavailable")
	}
	type termination struct {
		firstSeen time.Time
		killed    bool
	}
	terminations := make(map[fpgadev.ProcessIdentity]termination)
	absentSince := time.Time{}
	for {
		population, err := snapshotRecordedTargets(ctx, targets)
		if err != nil {
			return err
		}
		now := time.Now()
		present := false
		for index, identities := range population {
			for _, identity := range identities {
				present = true
				state, seen := terminations[identity]
				switch {
				case !seen:
					if err := signal(ctx, targets[index].observer, identity, syscall.SIGTERM); err != nil {
						return err
					}
					terminations[identity] = termination{firstSeen: now}
				case !state.killed && now.Sub(state.firstSeen) >= grace:
					if err := signal(ctx, targets[index].observer, identity, syscall.SIGKILL); err != nil {
						return err
					}
					state.killed = true
					terminations[identity] = state
				}
			}
		}
		if present {
			absentSince = time.Time{}
		} else if absentSince.IsZero() {
			absentSince = now
		}
		if !absentSince.IsZero() && now.Sub(absentSince) >= stable {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(productionAgentPollInterval):
		}
	}
}

func snapshotRecordedTargets(ctx context.Context, targets []recordedAgentTarget) ([][]fpgadev.ProcessIdentity, error) {
	if len(targets) == 0 {
		return nil, errors.New("agent observer is unavailable")
	}
	population := make([][]fpgadev.ProcessIdentity, len(targets))
	for index, target := range targets {
		if target.path == "" || target.observer == nil {
			return nil, errors.New("agent observer is unavailable")
		}
		identities, err := target.observer.SnapshotContext(ctx)
		if err != nil {
			return nil, err
		}
		population[index] = identities
	}
	return population, nil
}

func waitRecordedTargetsStableAbsent(ctx context.Context, targets []recordedAgentTarget, stable time.Duration) error {
	absentSince := time.Time{}
	for {
		population, err := snapshotRecordedTargets(ctx, targets)
		if err != nil {
			return err
		}
		now := time.Now()
		present := false
		for _, identities := range population {
			present = present || len(identities) != 0
		}
		if present {
			absentSince = time.Time{}
		} else if absentSince.IsZero() {
			absentSince = now
		}
		if !absentSince.IsZero() && now.Sub(absentSince) >= stable {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(productionAgentPollInterval):
		}
	}
}

type productionAgentPathObserver struct {
	path        string
	disk        *fpgadev.Observer
	scanAttempt func(context.Context) ([]fpgadev.ProcessIdentity, bool, error)
}

func newProductionAgentPathObserver(path string) (*productionAgentPathObserver, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("agent path is not absolute and canonical")
	}
	observer := &productionAgentPathObserver{path: path}
	disk, err := fpgadev.NewObserverForExecutable(path)
	if err == nil {
		observer.disk = disk
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return observer, nil
}

func newRecordedAgentTargets(paths ...string) ([]recordedAgentTarget, error) {
	targets := make([]recordedAgentTarget, 0, len(paths))
	for _, path := range paths {
		observer, err := newProductionAgentPathObserver(path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, recordedAgentTarget{path: path, observer: observer})
	}
	return targets, nil
}

func productionAgentTargets() ([]recordedAgentTarget, error) {
	return newRecordedAgentTargets(productionAgentExecutable, productionLegacyAgent, productionAgentFATExecutable)
}

// SnapshotContext covers both the current on-disk executable identity and a
// running deleted executable whose kernel-owned /proc/<pid>/exe link still
// names the canonical path. A matching process that exits during discovery is
// retried; a live inaccessible or changing identity remains fail-closed.
func (o *productionAgentPathObserver) SnapshotContext(ctx context.Context) ([]fpgadev.ProcessIdentity, error) {
	if o == nil || !filepath.IsAbs(o.path) || filepath.Clean(o.path) != o.path {
		return nil, errors.New("agent path observer is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; attempt < 3; attempt++ {
		scan := o.snapshotAttempt
		if o.scanAttempt != nil {
			scan = o.scanAttempt
		}
		identities, retry, err := scan(ctx)
		if err != nil {
			return nil, err
		}
		if !retry {
			return identities, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("agent executable identity changed during scan")
}

func (o *productionAgentPathObserver) snapshotAttempt(ctx context.Context) ([]fpgadev.ProcessIdentity, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	observers := make([]*fpgadev.Observer, 0, 2)
	if o.disk != nil {
		observers = append(observers, o.disk)
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		executable := filepath.Join("/proc", entry.Name(), "exe")
		target, err := os.Readlink(executable)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSuffix(target, " (deleted)") != o.path {
			continue
		}
		observer, err := fpgadev.NewObserverForExecutable(executable)
		if errors.Is(err, os.ErrNotExist) {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, err
		}
		after, err := os.Readlink(executable)
		if errors.Is(err, os.ErrNotExist) {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSuffix(after, " (deleted)") != o.path {
			return nil, false, errors.New("agent executable identity changed during scan")
		}
		observers = append(observers, observer)
	}
	identities := make([]fpgadev.ProcessIdentity, 0)
	seen := make(map[fpgadev.ProcessIdentity]struct{})
	for _, observer := range observers {
		current, err := observer.SnapshotContext(ctx)
		if errors.Is(err, os.ErrNotExist) {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, err
		}
		for _, identity := range current {
			if _, exists := seen[identity]; exists {
				continue
			}
			seen[identity] = struct{}{}
			identities = append(identities, identity)
		}
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].PID != identities[j].PID {
			return identities[i].PID < identities[j].PID
		}
		return identities[i].StartTime < identities[j].StartTime
	})
	return identities, false, ctx.Err()
}

type scriptSupervisorObserver struct {
	script      string
	invocations [][]string
	interpreter recordedAgentObserver
}

const (
	productionScriptInterpreter        = "/bin/sh"
	productionProcessArgumentsMaxBytes = 4096
)

var errProcessArgumentsInvalid = errors.New("process arguments are invalid")

func newScriptSupervisorObserver(script string, invocations ...[]string) (*scriptSupervisorObserver, error) {
	if !filepath.IsAbs(script) || filepath.Clean(script) != script || len(invocations) == 0 {
		return nil, errors.New("script supervisor observer is unavailable")
	}
	exact := make([][]string, 0, len(invocations))
	for _, invocation := range invocations {
		if len(invocation) < 2 || invocation[0] == "" || !filepath.IsAbs(invocation[1]) || filepath.Clean(invocation[1]) != invocation[1] {
			return nil, errors.New("script supervisor invocation is invalid")
		}
		exact = append(exact, append([]string(nil), invocation...))
	}
	interpreter, err := fpgadev.NewObserverForExecutable(productionScriptInterpreter)
	if err != nil {
		return nil, err
	}
	return &scriptSupervisorObserver{script: script, invocations: exact, interpreter: interpreter}, nil
}

func (o *scriptSupervisorObserver) SnapshotContext(ctx context.Context) ([]fpgadev.ProcessIdentity, error) {
	if o == nil || len(o.invocations) == 0 || o.interpreter == nil {
		return nil, errors.New("script supervisor observer is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	interpreters, err := o.interpreter.SnapshotContext(ctx)
	if err != nil {
		return nil, err
	}
	interpreterByPID := make(map[int]fpgadev.ProcessIdentity, len(interpreters))
	for _, identity := range interpreters {
		interpreterByPID[identity.PID] = identity
	}
	identities := make([]fpgadev.ProcessIdentity, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		cmdlinePath := filepath.Join("/proc", entry.Name(), "cmdline")
		argv, err := readProcessArguments(cmdlinePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if errors.Is(err, errProcessArgumentsInvalid) && !o.authorityPrefix(argv) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !o.authorityPrefix(argv) {
			continue
		}
		if !o.matches(argv) {
			return nil, errors.New("script supervisor invocation is not recognized")
		}
		after, err := readProcessArguments(cmdlinePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !equalStrings(argv, after) || !o.matches(after) {
			return nil, errors.New("script supervisor identity changed during scan")
		}
		identity, ok := interpreterByPID[pid]
		if !ok {
			return nil, errors.New("script supervisor interpreter identity is unavailable")
		}
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].PID != identities[j].PID {
			return identities[i].PID < identities[j].PID
		}
		return identities[i].StartTime < identities[j].StartTime
	})
	return identities, ctx.Err()
}

func (o *scriptSupervisorObserver) matches(argv []string) bool {
	if !o.authorityPrefix(argv) {
		return false
	}
	for _, invocation := range o.invocations {
		if equalStrings(argv[2:], invocation) {
			return true
		}
	}
	return false
}

func (o *scriptSupervisorObserver) authorityPrefix(argv []string) bool {
	return len(argv) >= 3 && argv[0] == productionScriptInterpreter && argv[1] == o.script && argv[2] == "mister-agent"
}

func readProcessArguments(path string) ([]string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("process arguments descriptor is unavailable")
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, productionProcessArgumentsMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	invalid := len(raw) > productionProcessArgumentsMaxBytes || raw[len(raw)-1] != 0
	payload := raw
	if raw[len(raw)-1] == 0 {
		payload = raw[:len(raw)-1]
	}
	parts := strings.Split(string(payload), "\x00")
	for _, part := range parts {
		if part == "" {
			invalid = true
		}
	}
	if invalid {
		return parts, errProcessArgumentsInvalid
	}
	return parts, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func signalProductionAgent(ctx context.Context, observer recordedAgentObserver, identity fpgadev.ProcessIdentity, signal syscall.Signal) error {
	if observer == nil || identity.PID <= 0 || identity.StartTime == 0 {
		return errors.New("agent process identity is unavailable")
	}
	pidfd, err := unix.PidfdOpen(identity.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(pidfd) }()
	current, err := observer.SnapshotContext(ctx)
	if err != nil {
		return err
	}
	matched := false
	for _, candidate := range current {
		if candidate == identity {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}
	if err := unix.PidfdSendSignal(pidfd, signal, nil, 0); errors.Is(err, unix.ESRCH) {
		return nil
	} else {
		return err
	}
}

func installCommandName(name string) bool {
	switch name {
	case "install-profile", "recover-install", "uninstall-profile":
		return true
	default:
		return false
	}
}

func runInstallCommand(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	return runInstallCommandWithPrivilege(args, stdout, stderr, runner, func() bool { return os.Geteuid() == 0 })
}

func runInstallCommandWithPrivilege(args []string, stdout, stderr io.Writer, runner commandRunner, privileged func() bool) int {
	packageRoot := ""
	valid := len(args) == 1 && installCommandName(args[0])
	if len(args) == 3 && args[0] == "install-profile" && args[1] == "--package-root" && validPackageRootArg(args[2]) {
		packageRoot, valid = args[2], true
	}
	if privileged == nil || !privileged() || !valid {
		_, _ = io.WriteString(stderr, "usage: mister-fpga-dev install-profile [--package-root PATH]|recover-install|uninstall-profile\n")
		return exitUsage
	}
	manager, ok := runner.(installCommandRunner)
	if !ok || manager == nil {
		var candidate any = productionInstallManager()
		manager, ok = candidate.(installCommandRunner)
	}
	if manager == nil {
		_, _ = fmt.Fprintf(stderr, "FOGCAST_FPGA_DEV_INSTALL code=unavailable detail=%s\n", installFailureDetail(productionInstallPrerequisiteError))
		return exitFailure
	}
	var err error
	switch args[0] {
	case "install-profile":
		err = manager.Install(context.Background(), packageRoot)
	case "recover-install":
		err = manager.Recover(context.Background())
	case "uninstall-profile":
		err = manager.Uninstall(context.Background())
	}
	if err != nil && !errors.Is(err, fpgadev.ErrRebootRequested) {
		_, _ = fmt.Fprintf(stderr, "FOGCAST_FPGA_DEV_INSTALL code=unavailable detail=%s\n", installFailureDetail(err))
		return exitFailure
	}
	_, _ = fmt.Fprintf(stdout, "FOGCAST_FPGA_DEV_INSTALL command=%s code=ok\n", args[0])
	return exitOK
}

func installFailureDetail(err error) string {
	if err == nil {
		return "install failure reason was not recorded"
	}
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, err.Error())
}

func validPackageRootArg(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && path != string(filepath.Separator)
}
