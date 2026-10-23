// fes-kit-software is part of the base OS, never the replaceable bundle.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/fes/platform/internal/kitsoftware"
	"golang.org/x/sys/unix"
)

const runDir = "/run/fogcast/software"
const socket = "/run/fogcast/software.sock"
const factoryPath = "/usr/share/fes/software"
const helper = "/usr/sbin/fes-kit-data"

func main() {
	action := "run"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}
	var err error
	switch action {
	case "start":
		cmd := exec.Command(os.Args[0], "run")
		var null io.Closer
		null, err = detachDaemon(cmd)
		if err == nil {
			err = cmd.Start()
			_ = null.Close()
		}
	case "stop":
		err = stop()
	case "status":
		err = status()
	case "acknowledge":
		err = acknowledge(os.Args[2:])
	case "prepare-power-cycle":
		err = preparePowerCycle(os.Args[2:])
	case "select-factory":
		err = selectFactory(os.Args[2:])
	case "run":
		err = run()
	default:
		err = errors.New("usage: fes-kit-software start|stop|run|status|acknowledge|prepare-power-cycle|select-factory")
	}
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func stop() error {
	b, err := os.ReadFile(filepath.Join(runDir, "supervisor.pid"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return errors.New("invalid software supervisor PID")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	running, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	self, err := os.Stat(os.Args[0])
	if err != nil {
		return err
	}
	if !os.SameFile(running, self) {
		return errors.New("software supervisor PID identity changed")
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return err
	}
	polls := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(polls, int(kitsoftware.StopCommandTimeout.Milliseconds()))
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("software supervisor did not stop; process identity retained")
	}
	return kitsoftware.CheckNoManagedProcesses()
}
func helperCommand(ctx context.Context, command string) error {
	b, err := exec.CommandContext(ctx, helper, command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kit data %s: %w: %s", command, err, b)
	}
	return nil
}

// detachDaemon starts the supervisor away from the caller's stdio. Setsid
// drops the controlling terminal. /dev/null replaces stdin, stdout and
// stderr so a closed SSH pipe cannot raise SIGPIPE on the next log mirror.
func detachDaemon(cmd *exec.Cmd) (io.Closer, error) {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	return null, nil
}

// ignoreClosedPipe keeps a broken stderr from killing the supervisor. Go
// turns SIGPIPE on fd 2 into process death unless it is ignored; the tmpfs
// log is the record, and a closed mirror must not take the group down.
func ignoreClosedPipe() {
	signal.Ignore(syscall.SIGPIPE)
}

func run() error {
	ignoreClosedPipe()
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return err
	}
	logs, err := kitsoftware.OpenSupervisorLog(filepath.Join(runDir, kitsoftware.SupervisorLogName), os.Stderr)
	if err != nil {
		return err
	}
	defer logs.Close()
	log.SetOutput(logs)
	lock, err := os.OpenFile(filepath.Join(runDir, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); errors.Is(err, unix.EWOULDBLOCK) {
		log.Print("software supervisor already running")
		return nil
	} else if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "supervisor.pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return kitsoftware.SuperviseUntilSignal(ctx, func(ctx context.Context) error {
		return supervise(ctx, logs)
	}, waitSupervisorBackoff)
}
func waitSupervisorBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func supervise(ctx context.Context, output io.Writer) error {
	log.Print("software supervisor starting")
	var repo *kitsoftware.FileRepository
	defer func() {
		if repo != nil {
			_ = repo.Close()
		}
	}()
	if err := kitsoftware.StopOrphans(ctx); err != nil {
		return err
	}
	baseBytes, err := os.ReadFile("/etc/fes/software-base.json")
	if err != nil {
		return err
	}
	var base software.Compatibility
	if err := json.Unmarshal(baseBytes, &base); err != nil {
		return err
	}
	factory, err := software.VerifyDirectory(ctx, factoryPath)
	if err != nil {
		return err
	}
	if err := factory.Manifest.Compatible(base); err != nil {
		return err
	}
	bootIDBytes, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	bootRequest := kitsoftware.Request{ID: operationID("software-boot-" + strings.TrimSpace(string(bootIDBytes)) + hex.EncodeToString(nonce)), Candidate: factory.ID, Requester: "kit-software-supervisor"}
	identityBytes, err := exec.CommandContext(ctx, helper, "identity").Output()
	if err != nil {
		return err
	}
	baseIdentity := strings.TrimSpace(string(identityBytes))
	if baseIdentity == "" {
		return errors.New("missing base image identity")
	}
	server := &kitsoftware.Server{BaseIdentity: baseIdentity, Factory: &factory}
	var store *software.Store
	var admission *kitsoftware.UnixAdmission
	controller := &kitsoftware.ProcessController{RunDir: runDir, BaseIdentity: baseIdentity, Output: output}
	server.Controller = controller
	controller.Resolve = func(ctx context.Context, id string) (string, error) {
		return kitsoftware.ResolveRelease(ctx, id, factoryPath, factory, base, store)
	}
	// The helper refuses trial/pending/corrupt images and absent/mismounted p3.
	// Never create a durable state directory on the temporary mountpoint.
	openPersistent := func() error {
		if err := helperCommand(ctx, "prepare"); err != nil {
			return err
		}
		next, err := software.OpenPersistent(base)
		if err != nil {
			return err
		}
		if err := kitsoftware.RetainFactory(ctx, next, factory, factoryPath); err != nil {
			return err
		}
		statePath := filepath.Join(next.Root(), "control")
		nextRepo, err := kitsoftware.OpenRepository(statePath, factory.ID)
		if err != nil {
			return err
		}
		nextAdmission, err := kitsoftware.NewUnixAdmission("/run/fogcast/software-admission.sock", filepath.Join(statePath, "admission.key"))
		if err != nil {
			nextRepo.Close()
			return err
		}
		store, repo, admission = next, nextRepo, nextAdmission
		server.Store = store
		server.Repository = repo
		server.Admission = admission
		controller.Admission = admission
		server.StorageError = ""
		return nil
	}
	gate := func(ctx context.Context) error { return helperCommand(ctx, "mount") }
	server.Gate = func() error { return gate(ctx) }
	if err := openPersistent(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || (exit.ExitCode() != 3 && exit.ExitCode() != 4) {
			return fmt.Errorf("persistent software state blocked: %w", err)
		}
		server.StorageError = err.Error()
		admission, err = kitsoftware.NewUnixAdmission("/run/fogcast/software-admission.sock", filepath.Join(runDir, "bootstrap.key"))
		if err != nil {
			return err
		}
		controller.Admission = admission
		server.Admission = admission
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}
	server.PublishStatus()
	httpServer := &http.Server{Handler: server, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("software API: %v", err)
		}
	}()
	defer httpServer.Close()
	engine := &kitsoftware.Engine{Repository: repo, Controller: controller, Admission: admission, Gate: gate, Factory: factory.ID}
	// The engine holds back a selection this image cannot run and starts the
	// factory software in its place; status reports which selection and why.
	publish := func() {
		server.HeldBack, server.HeldBackReason = engine.HeldBack, engine.HeldBackReason
		server.PublishStatus()
	}
	var startupErr error
	var startupDelay time.Duration
	var startupRetryAt time.Time
	server.Mu.Lock()
	if repo != nil {
		startupErr = engine.Boot(ctx, bootRequest)
	} else {
		controller.PermitUpdateBlocked = true
		startupErr = engine.StartFactory(ctx, factory.ID)
		controller.PermitUpdateBlocked = false
	}
	if startupErr != nil {
		log.Printf("software startup blocked: %v", startupErr)
		server.StorageError = startupErr.Error()
		startupDelay = kitsoftware.NextSupervisorBackoff(0)
		startupRetryAt = time.Now().Add(startupDelay)
	}
	publish()
	server.Mu.Unlock()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var trialHold *kitsoftware.UnixAdmission
	var trialRequest kitsoftware.Request
	var trialSuccessor kitsoftware.SuccessorHold

	for {
		select {
		case <-ctx.Done():
			// Keep the singleton alive if a controller remains. The stop caller
			// times out with failure rather than assuming hardware is free.
			for {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), kitsoftware.StopGroupTimeout)
				err := controller.Stop(stopCtx)
				stopCancel()
				if err == nil {
					return nil
				}
				log.Printf("software stop blocked: %v", err)
				time.Sleep(time.Second)
			}
		case <-ticker.C:
			server.Mu.Lock()
			if startupErr != nil {
				if !time.Now().Before(startupRetryAt) {
					if err := controller.Stop(ctx); err != nil {
						log.Printf("software startup cleanup: %v", err)
						startupErr = err
					} else if repo != nil {
						startupErr = engine.Boot(ctx, bootRequest)
					} else {
						controller.PermitUpdateBlocked = true
						startupErr = engine.StartFactory(ctx, factory.ID)
						controller.PermitUpdateBlocked = false
					}
					if startupErr != nil {
						log.Printf("software startup blocked: %v", startupErr)
						server.StorageError = startupErr.Error()
						startupDelay = kitsoftware.NextSupervisorBackoff(startupDelay)
						startupRetryAt = time.Now().Add(startupDelay)
					} else {
						log.Print("software startup recovered")
						server.StorageError = ""
						startupDelay = 0
					}
				}
				publish()
				server.Mu.Unlock()
				continue
			}
			pending := kitsoftware.Request{}
			queueKnown := true
			if repo != nil {
				state, readErr := repo.Read()
				pending, queueKnown = kitsoftware.GroupRestartRequest(state, readErr)
			}
			if err := controller.RecoverAcceptedExits(ctx, pending, queueKnown); err != nil {
				log.Printf("service restart: %v", err)
			}
			if repo == nil && startupErr == nil && trialHold == nil {
				// Confirmed trials reconcile through the ordinary queue. The
				// factory session is never stopped outside atomic admission.
				if err := gate(ctx); err == nil {
					reconcile := bootRequest
					reconcile.ID = operationID(reconcile.ID + "-reconcile")
					if err := admission.Queue(ctx, reconcile); err == nil {
						if err := admission.Acquire(ctx, reconcile); err == nil {
							oldAdmission := admission
							if err := openPersistent(); err != nil {
								server.StorageError = err.Error()
								_ = oldAdmission.Release(ctx, reconcile)
							} else {
								engine = &kitsoftware.Engine{Repository: repo, Controller: controller, Admission: admission, Gate: gate, Factory: factory.ID}
								trialHold = oldAdmission
								trialRequest = reconcile
							}
						}
					}
				}
			}
			if trialHold != nil {
				// Install the persisted queue before ending the boot reservation.
				// A failure leaves that credential held so claims stay closed.
				// Once the reservation has finished, confirm the same successor
				// and retry release; do not require the reservation to accept
				// another held install.
				restore := bootRequest
				restore.ID = operationID(restore.ID + "-restore-selection")
				err := kitsoftware.HandoffTrial(ctx, repo, restore, &trialSuccessor, func(ctx context.Context, request kitsoftware.Request) error {
					return admission.QueueWhileHeld(ctx, trialHold, trialRequest, request)
				}, func(ctx context.Context, request kitsoftware.Request) error {
					return admission.Queue(ctx, request)
				}, func(ctx context.Context) (bool, error) {
					status, err := trialHold.Status(ctx)
					if err != nil {
						return false, err
					}
					return kitsoftware.ReservationStillHeld(status, trialRequest.ID), nil
				}, func(ctx context.Context) error {
					return trialHold.Release(ctx, trialRequest)
				})
				if err != nil {
					server.StorageError = err.Error()
				} else {
					trialHold = nil
					trialSuccessor = kitsoftware.SuccessorHold{}
					server.StorageError = ""
				}
			}
			if trialHold == nil && repo != nil && startupErr == nil {
				if err := server.ReconcileExecutionFence(ctx); err != nil {
					server.StorageError = err.Error()
				}
				state, err := repo.Read()
				if err == nil && state.Pending != nil {
					_ = admission.Queue(ctx, state.Pending.Request)
				}
				if err := engine.Run(ctx); err != nil && !errors.Is(err, kitsoftware.ErrBusy) {
					log.Printf("software activation: %v", err)
				}
			}
			publish()
			server.Mu.Unlock()
		}
	}
}
func operationID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func status() error {
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	resp, err := client.Get("http://unix/v1/kit/software")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("software status HTTP %d", resp.StatusCode)
	}
	return nil
}
