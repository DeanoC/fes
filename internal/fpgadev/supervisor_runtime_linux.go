//go:build linux && fpgadev

package fpgadev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// SupervisorRuntimeConfig is the closed target composition for the successor
// boot supervisor. Paths and argv are supplied by the protected target image,
// not by a network/API request. Host tests may provide temporary fixtures.
type SupervisorRuntimeConfig struct {
	// StartChild is an injected host-test seam. ARM production leaves it nil,
	// selecting the real ARM child/PDEATHSIG adapter.
	StartChild      func(context.Context, string, []string, []string, []*os.File) (*ChildProcess, error)
	MainExecutable  string
	MainArguments   []string
	MainEnvironment []string
	// MainProcessScanner is an optional observer seam for host fixtures. The
	// ARM production constructor leaves it nil: readiness is bound directly to
	// the retained Main handle and never performs a process-population scan.
	MainProcessScanner ProcessScanner
	MainFIFO           string
	// RequireRootFIFO is enabled by the ARM production composition. Host
	// fixtures intentionally use their unprivileged temporary directory while
	// retaining the exact type and mode checks.
	RequireRootFIFO  bool
	FPGAManagerState string
	MenuPath         string
	CoreNameFile     string
	// CoreNameFallbackFile is the alternate compatibility-Main publication
	// path. Production pairs the configured /tmp/CORENAME and stock
	// /media/fat/CORENAME locations; host fixtures may provide temporary paths.
	CoreNameFallbackFile string

	AgentExecutable  string
	AgentArguments   []string
	AgentEnvironment []string
	ProfileSHA256    string

	PollInterval time.Duration
}

// SupervisorRuntime owns the retained Main/agent handles and the one-use
// readiness pipe. It is deliberately stateful: calling StartMain or
// StartAgent twice is a configuration error, never a restart policy.
type SupervisorRuntime struct {
	config SupervisorRuntimeConfig

	mu            sync.Mutex
	mainStarted   bool
	main          *ChildProcess
	agent         *ChildProcess
	receiptReader *os.File
	agentRead     bool
	observer      *Observer
	mainReady     *ProcessIdentity
}

// CompatibilityMainReadiness observes an already-running compatibility Main;
// it never requires SupervisorRuntime.StartMain and uses one cumulative
// caller deadline for all gates.
type CompatibilityMainReadiness struct {
	runtime  *SupervisorRuntime
	observer *Observer
}

func NewCompatibilityMainReadiness(runtime *SupervisorRuntime, observer *Observer) *CompatibilityMainReadiness {
	return &CompatibilityMainReadiness{runtime: runtime, observer: observer}
}

func (r *CompatibilityMainReadiness) Verify(ctx context.Context) error {
	if r == nil || r.runtime == nil || r.observer == nil {
		return ErrRunnerConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return waitRuntimeCondition(bounded, r.runtime.config.PollInterval, func() error {
		identities, err := r.observer.SnapshotContext(bounded)
		if err != nil {
			return err
		}
		if len(identities) != 1 {
			return errors.New("compatibility Main is not uniquely present")
		}
		if err := r.runtime.CommandFIFOReady(); err != nil {
			return err
		}
		if err := r.runtime.FPGAManagerReady(); err != nil {
			return err
		}
		return r.runtime.menuReady(bounded)
	})
}

// WaitProgrammed waits for positive post-dispatch evidence: the FPGA manager
// remains operational and every present canonical CORENAME publisher has
// stably left MENU. Main process absence is deliberately not part of this
// predicate because fpga_load_rbf app_restart may immediately replace Main.
func (r *CompatibilityMainReadiness) WaitProgrammed(ctx context.Context) error {
	if r == nil || r.runtime == nil {
		return ErrRunnerConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return waitRuntimeCondition(ctx, r.runtime.config.PollInterval, func() error {
		if err := r.runtime.FPGAManagerReady(); err != nil {
			return err
		}
		if err := r.runtime.coreProgrammed(ctx); err != nil {
			return err
		}
		return r.runtime.FPGAManagerReady()
	})
}

func NewSupervisorRuntime(config SupervisorRuntimeConfig) (*SupervisorRuntime, error) {
	if config.MainExecutable == "" {
		config.MainExecutable = "/media/fat/MiSTer"
	}
	if config.MainFIFO == "" {
		config.MainFIFO = "/dev/MiSTer_cmd"
	}
	if config.FPGAManagerState == "" {
		config.FPGAManagerState = "/sys/class/fpga_manager/fpga0/state"
	}
	if config.MenuPath == "" {
		config.MenuPath = "/media/fat/menu.rbf"
	}
	if config.CoreNameFile == "" {
		config.CoreNameFile = "/media/fat/CORENAME"
	}
	if config.CoreNameFallbackFile == "" {
		switch config.CoreNameFile {
		case "/media/fat/CORENAME":
			config.CoreNameFallbackFile = "/tmp/CORENAME"
		case "/tmp/CORENAME":
			config.CoreNameFallbackFile = "/media/fat/CORENAME"
		}
	}
	if config.AgentExecutable == "" {
		config.AgentExecutable = "/usr/bin/mister-agent"
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 10 * time.Millisecond
	}
	for name, path := range map[string]string{
		"Main executable":    config.MainExecutable,
		"Main FIFO":          config.MainFIFO,
		"FPGA manager state": config.FPGAManagerState,
		"Menu":               config.MenuPath,
		"core name":          config.CoreNameFile,
		"agent executable":   config.AgentExecutable,
	} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, fmt.Errorf("%s path is not absolute and canonical", name)
		}
	}
	if config.CoreNameFallbackFile != "" && (!filepath.IsAbs(config.CoreNameFallbackFile) || filepath.Clean(config.CoreNameFallbackFile) != config.CoreNameFallbackFile) {
		return nil, errors.New("fallback core name path is not absolute and canonical")
	}
	if config.ProfileSHA256 != "" && !manifestHashPattern.MatchString(config.ProfileSHA256) {
		return nil, errors.New("profile hash is not canonical")
	}
	return &SupervisorRuntime{config: config}, nil
}

func (r *SupervisorRuntime) StartMain(ctx context.Context) (*ChildProcess, error) {
	if r == nil {
		return nil, ErrRunnerConfiguration
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mainStarted {
		return nil, errors.New("Main may only be started once")
	}
	r.mainStarted = true
	starter := r.config.StartChild
	if starter == nil {
		starter = StartChildProcess
	}
	child, err := starter(ctx, r.config.MainExecutable, r.config.MainArguments, r.config.MainEnvironment, nil)
	if err != nil {
		return nil, err
	}
	r.main = child
	return child, nil
}

func (r *SupervisorRuntime) Main() *ChildProcess {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.main
}

func (r *SupervisorRuntime) StartAgent(ctx context.Context) (*ChildProcess, error) {
	if r == nil {
		return nil, ErrRunnerConfiguration
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.main == nil {
		return nil, errors.New("agent cannot start before Main")
	}
	if r.agent != nil || r.receiptReader != nil {
		return nil, errors.New("agent may only be started once")
	}
	reader, writer, err := NewReadinessPipe()
	if err != nil {
		return nil, err
	}
	args := append([]string(nil), r.config.AgentArguments...)
	for _, arg := range args {
		if arg == "--readiness-fd" || strings.HasPrefix(arg, "--readiness-fd=") {
			_ = reader.Close()
			_ = writer.Close()
			return nil, errors.New("agent readiness fd is supervisor-owned")
		}
	}
	args = append(args, "--readiness-fd", "3")
	starter := r.config.StartChild
	if starter == nil {
		starter = StartChildProcess
	}
	child, err := starter(ctx, r.config.AgentExecutable, args, r.config.AgentEnvironment, []*os.File{writer})
	closeWriterErr := writer.Close()
	if err != nil {
		_ = reader.Close()
		return nil, errors.Join(err, closeWriterErr)
	}
	if closeWriterErr != nil {
		_ = reader.Close()
		_ = child.TerminateAndReap(context.Background())
		return nil, closeWriterErr
	}
	r.agent = child
	r.receiptReader = reader
	return child, nil
}

func (r *SupervisorRuntime) Agent() *ChildProcess {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agent
}

// WaitChildren waits for the first retained Main or agent handle to exit. It
// never scans a PID or adopts a replacement; both waits are bound to the
// process handles returned by StartMain/StartAgent. A normal exit is still a
// lifecycle failure because a live supervisor requires both children.
func (r *SupervisorRuntime) WaitChildren(ctx context.Context) error {
	if r == nil {
		return ErrRunnerConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	main, agent := r.main, r.agent
	r.mu.Unlock()
	if main == nil && agent == nil {
		return ErrRunnerConfiguration
	}
	type waitResult struct {
		name string
		err  error
	}
	results := make(chan waitResult, 2)
	wait := func(name string, child *ChildProcess) {
		if child == nil {
			return
		}
		err := child.Wait(ctx)
		if err == nil {
			err = ErrSupervisorChildExited
		} else {
			err = errors.Join(ErrSupervisorChildExited, err)
		}
		results <- waitResult{name: name, err: err}
	}
	count := 0
	if main != nil {
		count++
		go wait("Main", main)
	}
	if agent != nil {
		count++
		go wait("agent", agent)
	}
	select {
	case result := <-results:
		return fmt.Errorf("%s: %w", result.name, result.err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CloseReceipt closes the supervisor's inherited receipt reader without
// touching either child. Failure fencing uses this before removing proof and
// committing recovery_required so a writer cannot remain attached to a stale
// readiness phase while durable state changes.
func (r *SupervisorRuntime) CloseReceipt() error {
	if r == nil {
		return ErrRunnerConfiguration
	}
	r.mu.Lock()
	reader := r.receiptReader
	r.receiptReader = nil
	r.mu.Unlock()
	if reader == nil {
		return nil
	}
	return reader.Close()
}

// TerminateChildren closes the parent side of the one-use receipt pipe and
// terminates/reaps the exact retained children in reverse start order. It is
// safe to call after a partial start or repeatedly after a failure.
func (r *SupervisorRuntime) TerminateChildren(ctx context.Context) error {
	if r == nil {
		return ErrRunnerConfiguration
	}
	r.mu.Lock()
	reader := r.receiptReader
	r.receiptReader = nil
	agent, main := r.agent, r.main
	r.mu.Unlock()
	var errs []error
	if reader != nil {
		if err := reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if agent != nil {
		errs = append(errs, terminateRuntimeChild(agent))
	}
	if main != nil {
		errs = append(errs, terminateRuntimeChild(main))
	}
	return errors.Join(errs...)
}

func (r *SupervisorRuntime) ReadinessReceipt(ctx context.Context) (ReadinessReceipt, error) {
	if r == nil {
		return ReadinessReceipt{}, ErrRunnerConfiguration
	}
	r.mu.Lock()
	if r.agent == nil || r.receiptReader == nil || r.agentRead {
		r.mu.Unlock()
		return ReadinessReceipt{}, errors.New("readiness receipt is not available")
	}
	reader := r.receiptReader
	agent := r.agent
	r.agentRead = true
	r.receiptReader = nil
	r.mu.Unlock()
	startedIdentity, identityErr := agent.StartAttestation()
	if identityErr != nil {
		_ = reader.Close()
		return ReadinessReceipt{}, errors.Join(identityErr, terminateRuntimeChild(agent))
	}
	receipt, err := ReadReadinessReceiptFromPipe(ctx, reader)
	closeErr := reader.Close()
	if err != nil {
		// The failed receipt path owns termination and reaping of this exact child.
		return ReadinessReceipt{}, errors.Join(err, closeErr, terminateRuntimeChild(agent))
	}
	if closeErr != nil {
		return ReadinessReceipt{}, errors.Join(closeErr, terminateRuntimeChild(agent))
	}
	if receipt.PID != startedIdentity.PID || receipt.StartTime != startedIdentity.StartTime || receipt.ExecutableDevice != startedIdentity.Device || receipt.ExecutableInode != startedIdentity.Inode || receipt.ExecutableSHA256 != startedIdentity.SHA256 {
		return ReadinessReceipt{}, errors.Join(errors.New("readiness receipt identity does not match retained child"), terminateRuntimeChild(agent))
	}
	return receipt, nil
}

func terminateRuntimeChild(child *ChildProcess) error {
	if child == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return child.TerminateAndReap(cleanupCtx)
}

func (r *SupervisorRuntime) AgentAttestation() (ProcessAttestation, error) {
	if r == nil {
		return ProcessAttestation{}, ErrRunnerConfiguration
	}
	r.mu.Lock()
	agent := r.agent
	r.mu.Unlock()
	if agent == nil {
		return ProcessAttestation{}, errors.New("agent has not started")
	}
	return agent.Attestation()
}

// MainAttestation returns the live identity of the exact retained Main child.
// It never reopens a numeric PID, so proof publication remains bound to the
// handle returned by StartMain rather than to a replacement process.
func (r *SupervisorRuntime) MainAttestation() (ProcessAttestation, error) {
	if r == nil {
		return ProcessAttestation{}, ErrRunnerConfiguration
	}
	r.mu.Lock()
	main := r.main
	r.mu.Unlock()
	if main == nil {
		return ProcessAttestation{}, errors.New("Main has not started")
	}
	return main.Attestation()
}

// AgentAlive checks the retained agent handle without reopening a PID. It is
// called after receipt consumption and before any boot proof is published.
func (r *SupervisorRuntime) AgentAlive() error {
	if r == nil {
		return ErrRunnerConfiguration
	}
	r.mu.Lock()
	agent := r.agent
	r.mu.Unlock()
	if agent == nil {
		return errors.New("agent has not started")
	}
	return agent.Alive()
}

// WaitMainReady checks all four readiness observations under one cumulative
// context. Each condition is polled until it is true or the caller's deadline
// expires; no condition receives a fresh timeout.
func (r *SupervisorRuntime) WaitMainReady(ctx context.Context) error {
	if r == nil {
		return ErrRunnerConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	checks := []func() error{func() error { return r.mainExecutableReady(ctx) }, r.CommandFIFOReady, r.FPGAManagerReady, func() error { return r.menuReady(ctx) }}
	for _, check := range checks {
		if err := waitRuntimeCondition(ctx, r.config.PollInterval, check); err != nil {
			return err
		}
	}
	return nil
}

func (r *SupervisorRuntime) MainExecutableReady() error {
	return r.mainExecutableReady(context.Background())
}

func (r *SupervisorRuntime) mainExecutableReady(ctx context.Context) error {
	if r == nil {
		return ErrRunnerConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Readiness is bound to the retained child returned by StartMain.  A
	// process-table scan could select an unrelated or replacement process, so
	// it is deliberately not part of production admission.  The optional
	// scanner remains available only to host fixtures that need to model an
	// observer independently of the child handle.
	r.mu.Lock()
	main := r.main
	scanner := r.config.MainProcessScanner
	r.mu.Unlock()
	if main == nil {
		return errors.New("Main has not started")
	}
	identity, err := main.Attestation()
	if err != nil {
		return err
	}
	observed := ProcessIdentity{PID: int(identity.PID), StartTime: identity.StartTime, Device: identity.Device, Inode: identity.Inode, SHA256: identity.SHA256}
	if scanner != nil {
		identities, scanErr := scanner.Scan(ctx, observed.executable())
		if scanErr != nil {
			return scanErr
		}
		if len(identities) != 1 || !identities[0].Identity.equal(observed) {
			return fmt.Errorf("Main process set does not contain retained Main")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mainReady != nil && (r.mainReady.PID != observed.PID || r.mainReady.StartTime != observed.StartTime) {
		return errors.New("Main identity changed during readiness")
	}
	r.mainReady = &observed
	return nil
}

func (r *SupervisorRuntime) CommandFIFOReady() error {
	info, err := os.Lstat(r.config.MainFIFO)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Main command FIFO is a symlink")
	}
	if info.Mode()&os.ModeType != os.ModeNamedPipe {
		return errors.New("Main command FIFO has the wrong type")
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || !allowedFIFOPermissions(uint32(info.Mode().Perm())) {
		return fmt.Errorf("Main command FIFO mode is %o, want 600 or 644", info.Mode().Perm())
	}
	if r.config.RequireRootFIFO {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return errors.New("Main command FIFO is not root-owned")
		}
		if stat.Nlink != 1 {
			return errors.New("Main command FIFO link count is not one")
		}
	}
	return nil
}

func (r *SupervisorRuntime) FPGAManagerReady() error {
	raw, err := os.ReadFile(r.config.FPGAManagerState)
	if err != nil {
		return err
	}
	if len(raw) > 128 || string(raw) != "operating\n" {
		return errors.New("FPGA manager is not operating")
	}
	return nil
}

func (r *SupervisorRuntime) MenuReady() error {
	return r.menuReady(context.Background())
}

func (r *SupervisorRuntime) menuReady(ctx context.Context) error {
	first, err := r.menuSnapshot(ctx)
	if err != nil {
		return err
	}
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	second, err := r.menuSnapshot(ctx)
	if err != nil {
		return err
	}
	if len(first) != len(second) {
		return errors.New("CORENAME changed during readiness")
	}
	for index := range first {
		if first[index] != second[index] {
			return errors.New("CORENAME changed during readiness")
		}
	}
	return nil
}

func (r *SupervisorRuntime) coreProgrammed(ctx context.Context) error {
	first, err := r.coreNameSnapshot(ctx)
	if err != nil {
		return err
	}
	for _, publisher := range first {
		if publisher.value == "MENU" {
			return errors.New("CORENAME remains MENU")
		}
	}
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	second, err := r.coreNameSnapshot(ctx)
	if err != nil {
		return err
	}
	if len(first) != len(second) {
		return errors.New("CORENAME changed during handoff")
	}
	for index := range first {
		if first[index] != second[index] || second[index].value == "MENU" {
			return errors.New("CORENAME changed during handoff")
		}
	}
	return nil
}

type coreNamePublisher struct {
	path  string
	value string
}

func (r *SupervisorRuntime) coreNameSnapshot(ctx context.Context) ([]coreNamePublisher, error) {
	paths := []string{r.config.CoreNameFile}
	if fallback := r.config.CoreNameFallbackFile; fallback != "" && fallback != r.config.CoreNameFile {
		paths = append(paths, fallback)
	}
	publishers := make([]coreNamePublisher, 0, len(paths))
	for _, path := range paths {
		value, err := readCoreNamePublisher(ctx, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		publishers = append(publishers, coreNamePublisher{path: path, value: value})
	}
	if len(publishers) == 0 {
		return nil, errors.New("CORENAME is unavailable")
	}
	for index := 1; index < len(publishers); index++ {
		if publishers[index].value != publishers[0].value {
			return nil, errors.New("CORENAME publishers disagree")
		}
	}
	return publishers, nil
}

// menuSnapshot accepts only a non-empty, stable set of publishers containing
// exactly MENU, with or without a single trailing newline. A missing alternate
// is allowed, but every present candidate must publish MENU so a stale or
// malformed CORENAME cannot be masked by the other location.
func (r *SupervisorRuntime) menuSnapshot(ctx context.Context) ([]string, error) {
	paths := []string{r.config.CoreNameFile}
	if fallback := r.config.CoreNameFallbackFile; fallback != "" && fallback != r.config.CoreNameFile {
		paths = append(paths, fallback)
	}
	ready := make([]string, 0, len(paths))
	for _, path := range paths {
		present, err := readMenuPublisher(ctx, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if present {
			ready = append(ready, path)
		}
	}
	if len(ready) == 0 {
		return nil, errors.New("CORENAME is unavailable")
	}
	return ready, nil
}

func readMenuPublisher(ctx context.Context, path string) (bool, error) {
	value, err := readCoreNamePublisher(ctx, path)
	if err != nil {
		return false, err
	}
	if value != "MENU" {
		return false, fmt.Errorf("CORENAME at %s is not MENU", path)
	}
	return true, nil
}

func readCoreNamePublisher(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return "", errors.New("CORENAME descriptor is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("CORENAME at %s is not a regular file", path)
	}
	raw, err := io.ReadAll(io.LimitReader(file, 130))
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > 129 {
		return "", fmt.Errorf("CORENAME at %s has invalid length", path)
	}
	value := string(raw)
	if strings.HasSuffix(value, "\n") {
		value = strings.TrimSuffix(value, "\n")
	}
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("CORENAME at %s is malformed", path)
	}
	for _, character := range []byte(value) {
		if character < 0x20 || character > 0x7e {
			return "", fmt.Errorf("CORENAME at %s is malformed", path)
		}
	}
	return value, nil
}

func waitRuntimeCondition(ctx context.Context, interval time.Duration, check func() error) error {
	if check == nil {
		return ErrRunnerConfiguration
	}
	if interval <= 0 {
		interval = 10 * time.Millisecond
	}
	var last error
	for {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return errors.Join(last, err)
			}
			return err
		}
		if err := check(); err == nil {
			return nil
		} else {
			last = err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}

func verifyRuntimePath(path string, wanted os.FileMode, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", label)
	}
	if wanted != 0 && info.Mode()&os.ModeType != wanted {
		return fmt.Errorf("%s has the wrong type", label)
	}
	if wanted == 0 && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not regular", label)
	}
	return nil
}

// NewReadinessPipe returns the private one-use channel used by the supervisor
// to receive the agent's canonical receipt.
func NewReadinessPipe() (reader, writer *os.File, err error) { return os.Pipe() }

// ReadReadinessReceiptFromPipe polls a Unix pipe so context cancellation is
// observed without leaving a detached reader goroutine behind.
func ReadReadinessReceiptFromPipe(ctx context.Context, reader *os.File) (ReadinessReceipt, error) {
	if reader == nil {
		return ReadinessReceipt{}, errors.New("readiness pipe is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var raw []byte
	buffer := make([]byte, 256)
	for {
		if err := ctx.Err(); err != nil {
			return ReadinessReceipt{}, err
		}
		poll := []unix.PollFd{{Fd: int32(reader.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		timeout := 50
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				if err := ctx.Err(); err != nil {
					return ReadinessReceipt{}, err
				}
				return ReadinessReceipt{}, context.DeadlineExceeded
			}
			if ms := int(remaining / time.Millisecond); ms < timeout {
				timeout = ms
			}
			if timeout < 1 {
				timeout = 1
			}
		}
		n, err := unix.Poll(poll, timeout)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return ReadinessReceipt{}, err
		}
		if n == 0 {
			continue
		}
		readN, readErr := reader.Read(buffer)
		if readN > 0 {
			raw = append(raw, buffer[:readN]...)
			if len(raw) > ReadinessReceiptMaxBytes {
				return ReadinessReceipt{}, errors.New("readiness receipt exceeds size bound")
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return ParseReadinessReceipt(raw)
			}
			return ReadinessReceipt{}, readErr
		}
		if poll[0].Revents&(unix.POLLERR) != 0 {
			return ReadinessReceipt{}, errors.New("readiness pipe reported an error")
		}
	}
}

func profileHashForRuntime(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func mkfifoRuntimeTest(path string) error {
	return unix.Mkfifo(path, 0o600)
}

var _ = profileHashForRuntime
var _ = strings.TrimSpace
