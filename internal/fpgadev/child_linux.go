//go:build linux && arm && fpgadev

package fpgadev

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// ChildProcess retains the process handle and (when supported by the running
// kernel) a pidfd for one exact child.  The supervisor never reduces this to a
// numeric PID for cleanup: pidfd signalling is bound to the process instance.
type ChildProcess struct {
	cmd   *exec.Cmd
	proc  *os.Process
	pidfd int
	mu    sync.Mutex
	// startedIdentity is captured while the child is live, before a one-shot
	// readiness writer is allowed to exit. It is evidence for that exact
	// retained process instance; callers that need current liveness must use
	// Attestation or Alive separately.
	startedIdentity ProcessAttestation

	waitOnce  sync.Once
	waitDone  chan struct{}
	waitErr   error
	closeOnce sync.Once
}

// StartChildProcess starts one child with an explicit argv and inherited
// descriptors. It deliberately does not use exec.CommandContext: cancellation
// is owned by the caller so a failed readiness phase can terminate and reap the
// exact retained child before returning.
func StartChildProcess(ctx context.Context, path string, args, env []string, extraFiles []*os.File) (*ChildProcess, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("child executable is empty")
	}
	cmd := exec.Command(path, args...)
	// The supervisor is the lifecycle owner of both children. If it exits
	// abruptly, the kernel must kill the child before it can become an
	// unowned worker under PID 1. Normal failure paths still use the retained
	// pidfd and explicit wait/reap below; this is the crash-only backstop.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if env != nil {
		cmd.Env = append([]string(nil), env...)
	}
	cmd.ExtraFiles = append([]*os.File(nil), extraFiles...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start child: %w", err)
	}
	handle := &ChildProcess{cmd: cmd, proc: cmd.Process, pidfd: -1, waitDone: make(chan struct{})}
	if fd, err := unix.PidfdOpen(cmd.Process.Pid, 0); err == nil {
		handle.pidfd = fd
	}
	identity, identityErr := readLinuxProcessIdentity("/proc", cmd.Process.Pid)
	if identityErr != nil {
		_ = handle.sendSignal(syscall.SIGKILL)
		_ = cmd.Wait()
		handle.closePidfd()
		return nil, fmt.Errorf("attest child: %w", identityErr)
	}
	handle.startedIdentity = ProcessAttestation{PID: uint64(identity.PID), StartTime: identity.StartTime, Device: identity.Device, Inode: identity.Inode, SHA256: identity.SHA256}
	return handle, nil
}

func (p *ChildProcess) PID() int {
	if p == nil || p.proc == nil {
		return 0
	}
	return p.proc.Pid
}

func (p *ChildProcess) startWait() {
	if p == nil || p.cmd == nil {
		return
	}
	p.waitOnce.Do(func() {
		go func() {
			p.waitErr = p.cmd.Wait()
			close(p.waitDone)
			p.closePidfd()
		}()
	})
}

func (p *ChildProcess) Wait(ctx context.Context) error {
	if p == nil || p.cmd == nil {
		return errors.New("child handle is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.startWait()
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TerminateAndReap sends termination through the retained pidfd, escalates to
// SIGKILL only after the bounded grace period, and always waits for this
// handle. A child that already exited is still reaped by Wait.
func (p *ChildProcess) TerminateAndReap(ctx context.Context) error {
	if p == nil || p.proc == nil {
		return errors.New("child handle is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// A completed wait has already reaped this exact child. Treat repeated
	// cleanup as idempotent rather than signalling a potentially reused PID or
	// reporting the child's non-zero exit as a cleanup failure.
	p.startWait()
	select {
	case <-p.waitDone:
		p.closePidfd()
		return nil
	default:
	}
	signalErr := p.sendSignal(syscall.SIGTERM)
	if err := p.Wait(ctx); err != nil {
		if expectedChildTermination(err) {
			return signalErr
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return errors.Join(signalErr, err)
		}
		if killErr := p.sendSignal(syscall.SIGKILL); killErr != nil {
			signalErr = errors.Join(signalErr, killErr)
		}
		// Reaping is not optional after a bounded termination attempt. Once SIGKILL
		// has been issued, the kernel must release the exact child before return.
		if reapErr := p.Wait(context.Background()); reapErr != nil {
			return errors.Join(signalErr, reapErr)
		}
	}
	return signalErr
}

// sendSignal serializes pidfd access with the wait goroutine. In particular,
// it must never issue a syscall through an fd after closePidfd has handed that
// number back to the kernel, where it could have been reused for an unrelated
// object. Falling back to os.Process is safe only while no pidfd is retained.
func (p *ChildProcess) sendSignal(signal syscall.Signal) error {
	if p == nil || p.proc == nil {
		return errors.New("child handle is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var err error
	if p.pidfd >= 0 {
		err = unix.PidfdSendSignal(p.pidfd, signal, nil, 0)
	} else if signal == syscall.SIGKILL {
		err = p.proc.Kill()
	} else {
		err = p.proc.Signal(signal)
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func expectedChildTermination(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ProcessState == nil {
		return false
	}
	status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return false
	}
	signal := status.Signal()
	return signal == syscall.SIGTERM || signal == syscall.SIGKILL
}

func (p *ChildProcess) closePidfd() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		p.mu.Lock()
		fd := p.pidfd
		p.pidfd = -1
		p.mu.Unlock()
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	})
}

// Attestation returns the identity of the live process represented by this
// handle. It fails when the process disappeared or its /proc executable is no
// longer readable; callers must not substitute a fresh PID lookup.
func (p *ChildProcess) Attestation() (ProcessAttestation, error) {
	if p == nil || p.proc == nil || p.proc.Pid <= 0 {
		return ProcessAttestation{}, errors.New("child handle is nil")
	}
	identity, err := readLinuxProcessIdentity("/proc", p.proc.Pid)
	if err != nil {
		return ProcessAttestation{}, err
	}
	return ProcessAttestation{PID: uint64(identity.PID), StartTime: identity.StartTime, Device: identity.Device, Inode: identity.Inode, SHA256: identity.SHA256}, nil
}

// StartAttestation returns the identity captured at successful process start.
// It is intentionally distinct from Attestation: a one-shot readiness child
// may have closed its pipe and exited by the time its bytes are parsed, while
// the receipt still has to bind to the exact process that wrote them.
func (p *ChildProcess) StartAttestation() (ProcessAttestation, error) {
	if p == nil || p.proc == nil || !validProcessAttestation(p.startedIdentity) {
		return ProcessAttestation{}, errors.New("child start identity is unavailable")
	}
	return p.startedIdentity, nil
}

// Alive proves that the retained process has not already been reaped. It does
// not reopen a PID and therefore cannot accidentally adopt a replacement.
func (p *ChildProcess) Alive() error {
	if p == nil || p.proc == nil {
		return errors.New("child handle is nil")
	}
	select {
	case <-p.waitDone:
		return errors.New("child has exited")
	default:
	}
	identity, err := p.Attestation()
	if err != nil {
		return err
	}
	if identity != p.startedIdentity {
		return errors.New("child identity changed")
	}
	return nil
}
