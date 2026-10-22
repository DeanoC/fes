package kitsoftware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"golang.org/x/sys/unix"
)

// PreparePowerCycle is a local root-only journal action, not a reboot or recovery
// attempt. It never calls an admission mutation, starts/stops/signals a service,
// or changes selection. The existing blocked maintenance marker is untouched.
func PreparePowerCycle(ctx context.Context, req AcknowledgeRequest) (PowerCyclePreparationResult, error) {
	var result PowerCyclePreparationResult
	if os.Geteuid() != 0 {
		return result, errors.New("power-cycle preparation requires root")
	}
	if err := req.validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	repair, err := openOperatorRepair(ctx, req)
	if err != nil {
		return result, err
	}
	defer repair.repo.Close()
	c := &ProcessController{RunDir: acknowledgmentRunDir, Resolve: repair.resolve}
	if err := c.Verify(ctx, req.Selected); err != nil {
		return result, err
	}
	running, err := captureRunningPrior(ctx, req, c)
	if err != nil {
		return result, err
	}
	return preparePowerCycle(ctx, repair.repo, req, func(ctx context.Context, op Operation) (powerCycleEvidence, func(), error) {
		var evidence powerCycleEvidence
		held, unlock, err := lockMaintenanceReservation(ctx, maintenanceLockPath, maintenanceMarkerPath, filepath.Join(repair.control, "admission.key.grant-"+req.Operation), req.Operation, req.Boot, op.Requester, "blocked")
		if err != nil {
			return evidence, nil, err
		}
		fail := func(err error) (powerCycleEvidence, func(), error) { unlock(); return evidence, nil, err }
		if err := acknowledgmentMachine(ctx, req); err != nil {
			return fail(err)
		}
		// Native failure intentionally excludes normal Healthy(): that demands idle
		// and launcher readiness, which cannot prove a reboot-required physical state.
		for pid, start := range running.identities {
			current, err := processStartTime(pid)
			if err != nil {
				return fail(err)
			}
			if current != start {
				return fail(errors.New("supervised process changed during power-cycle proof"))
			}
		}
		if err := sameExecutable(running.supervisor, running.supervisorExecutable); err != nil {
			return fail(err)
		}
		children, err := directChildren(running.supervisor)
		if err != nil {
			return fail(err)
		}
		if len(children) != 3 {
			return fail(errors.New("supervised process membership changed"))
		}
		for _, pid := range children {
			if running.paths[pid] == "" {
				return fail(errors.New("unexpected supervised child"))
			}
			if err := sameExecutable(pid, running.paths[pid]); err != nil {
				return fail(err)
			}
		}
		root, err := c.preparedPath(ctx, req.Selected)
		if err != nil {
			return fail(err)
		}
		current, err := os.Readlink(filepath.Join(acknowledgmentRunDir, "current"))
		if err != nil {
			return fail(err)
		}
		if filepath.Clean(current) != root {
			return fail(errors.New("running release is not selected prior"))
		}
		runtimePID := c.children[0].cmd.Process.Pid
		native, err := readPowerCycleNative(ctx, "/run/mister-runtime.sock", runtimePID, running.identities[runtimePID], running.paths[runtimePID])
		if err != nil {
			return fail(err)
		}
		evidence = powerCycleEvidence{Native: native, Maintenance: held}
		return evidence, unlock, nil
	})
}

// The target agent keeps its maintenance reservation here. Boot clears /run.
const maintenanceLockPath = "/run/fogcast/kit-maintenance.lock"
const maintenanceMarkerPath = "/run/fogcast/kit-maintenance.json"

type powerCycleMarker struct {
	Schema int `json:"schema"`
	MaintenanceStatus
	Token      string `json:"token"`
	StartToken string `json:"start_token"`
}

func readPrivatePowerCycleJSON(path string, out any) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 8192 {
		return errors.New("invalid private maintenance proof file")
	}
	d := json.NewDecoder(io.LimitReader(f, 8193))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing private maintenance proof data")
	}
	return nil
}

// lockMaintenanceReservation holds the agent's marker lock and authenticates the
// stored grant against the persisted reservation of one operation, in one of
// the accepted states. It reads both files and writes neither; the caller
// releases the lock.
func lockMaintenanceReservation(ctx context.Context, lockPath, markerPath, grantPath, operation, boot, owner string, states ...string) (MaintenanceStatus, func(), error) {
	var empty MaintenanceStatus
	fd, err := unix.Open(lockPath, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return empty, nil, err
	}
	closefd := func() { unix.Close(fd) }
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		closefd()
		return empty, nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 {
		closefd()
		return empty, nil, errors.New("invalid maintenance lock identity")
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			closefd()
			return empty, nil, err
		}
		select {
		case <-ctx.Done():
			closefd()
			return empty, nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	unlock := func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }
	fail := func(err error) (MaintenanceStatus, func(), error) { unlock(); return empty, nil, err }
	var linked unix.Stat_t
	if err := unix.Lstat(lockPath, &linked); err != nil {
		return fail(err)
	}
	if st.Ino != linked.Ino || st.Dev != linked.Dev {
		return fail(errors.New("maintenance lock replaced"))
	}
	var marker powerCycleMarker
	var grant maintenanceGrant
	if err := readPrivatePowerCycleJSON(markerPath, &marker); err != nil {
		return fail(err)
	}
	if err := readPrivatePowerCycleJSON(grantPath, &grant); err != nil {
		return fail(err)
	}
	held := marker.MaintenanceStatus
	accepted := strings.Join(states, " or ")
	// A reservation in another state is a state mismatch, not a bad grant.
	if marker.Schema == 1 && held.OperationID == operation && held.BootID == boot && !slices.Contains(states, held.State) {
		return fail(fmt.Errorf("maintenance reservation of this operation is %s, not %s", held.State, accepted))
	}
	if marker.Schema != 1 || !slices.Contains(states, held.State) || held.OperationID != operation || held.BootID != boot || held.Owner != owner || held.Purpose != "kit-software-activation" || !software.ValidID(held.Generation) || !software.ValidID(marker.Token) || !software.ValidID(marker.StartToken) || grant.Status.OperationID != held.OperationID || grant.Status.Generation != held.Generation || grant.Status.BootID != held.BootID || grant.Status.Owner != held.Owner || grant.Status.Purpose != held.Purpose || subtle.ConstantTimeCompare([]byte(marker.Token), []byte(grant.Token)) != 1 {
		return fail(fmt.Errorf("stored grant does not authenticate this %s maintenance reservation", accepted))
	}
	return held, unlock, nil
}
func readPowerCycleNative(ctx context.Context, socket string, pid int, start, executable string) (nativePowerCycleProof, error) {
	status, err := readNativeStatus(ctx, socket, pid, start, executable)
	if err != nil {
		return nativePowerCycleProof{}, err
	}
	if !status.OK || status.State != "reboot_required" || status.Execution != "none" || status.Error == nil || status.Error.Code != "idle_failed" || status.Error.Phase != "recovery" {
		return nativePowerCycleProof{}, errors.New("native runtime does not report idle recovery requiring a physical cycle")
	}
	return nativePowerCycleProof{nativeStatus: status, RuntimePID: pid, RuntimeStartTime: start, RuntimeExecutable: executable}, nil
}
