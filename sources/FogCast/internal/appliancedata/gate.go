package appliancedata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
)

// GatePaths are the files GET /v1/update reads to report trial, pending, and
// corrupt. Direct-root images have no factory and are not blocked.
type GatePaths struct {
	Board       string
	Factory     string
	MountInfo   string
	BootJSON    string
	BootIDPath  string
	ReleaseRoot string
}

// ReadProductionUpdateGate uses the immutable image selection shared with the
// target agent. Explicit fixture paths retain their DE10 default below.
func ReadProductionUpdateGate() (Gate, error) {
	factory := "/.fes-bootstrap/etc/fes/factory.json"
	board, err := applianceupdate.ImageBoard(factory, "/etc/fes-board", runtime.GOARCH)
	if err != nil {
		return Gate{}, err
	}
	return ReadUpdateGate(GatePaths{Board: board})
}

// ReadUpdateGate reports the same trial/pending/corrupt conditions that
// GET /v1/update exposes. A missing factory on a direct-root image is clear.
func ReadUpdateGate(paths GatePaths) (Gate, error) {
	if paths.Board == "" {
		paths.Board = release.Board
	}
	if paths.Factory == "" {
		paths.Factory = "/.fes-bootstrap/etc/fes/factory.json"
	}
	if paths.MountInfo == "" {
		paths.MountInfo = "/proc/self/mountinfo"
	}
	if paths.ReleaseRoot == "" {
		paths.ReleaseRoot = appliance.DefaultRoot
	}
	if paths.BootJSON == "" {
		paths.BootJSON = filepath.Join(paths.ReleaseRoot, "boot.json")
	}
	if paths.BootIDPath == "" {
		paths.BootIDPath = "/proc/sys/kernel/random/boot_id"
	}
	info, err := os.Lstat(paths.Factory)
	if errors.Is(err, os.ErrNotExist) {
		if bootstrapMounted(paths.MountInfo) {
			return Gate{}, errors.New("mounted bootstrap has no factory manifest")
		}
		return Gate{}, nil
	}
	if err != nil {
		return Gate{}, err
	}
	if !info.Mode().IsRegular() {
		return Gate{}, errors.New("factory manifest is not a regular file")
	}
	f, err := os.Open(paths.Factory)
	if err != nil {
		return Gate{}, err
	}
	factory, _, err := release.DecodeManifestForBoard(f, paths.Board)
	f.Close()
	if err != nil {
		return Gate{}, err
	}
	boot, err := applianceupdate.ReadBootIdentity(paths.BootJSON, paths.BootIDPath)
	if err != nil {
		return Gate{}, err
	}
	store, err := appliance.NewForBoard(paths.ReleaseRoot, factory, paths.Board)
	if err != nil {
		return Gate{}, err
	}
	trial := boot.Trial
	if trial {
		// Confirmation clears trial on this boot's ticket. A ticket that still
		// says trial, because the rewrite was interrupted or an older confirm
		// left it, is reconciled against the durable store for this exact
		// boot and image. Good alone is insufficient when the same image was
		// retried as trial.
		confirmed, err := store.ConfirmedContext(context.Background(), boot.BootID, boot.ImageSHA256)
		if err != nil {
			return Gate{Trial: true}, err
		}
		trial = !confirmed
	}
	// Read after reconciliation so a pending next image or corrupt state still
	// blocks the data gate even when this boot has already been confirmed.
	st, err := store.Status()
	if err != nil {
		return Gate{Trial: trial, Pending: st.Pending != "", Corrupt: st.Corrupt}, err
	}
	return Gate{Trial: trial, Pending: st.Pending != "", Corrupt: st.Corrupt}, nil
}

func bootstrapMounted(mountInfo string) bool {
	data, err := os.ReadFile(mountInfo)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && fields[4] == "/.fes-bootstrap" {
			return true
		}
	}
	return false
}
