package appliancedata

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/internal/appliance"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
)

// GatePaths are the files GET /v1/update reads to report trial, pending, and
// corrupt. Direct-root images have no factory and are not blocked.
type GatePaths struct {
	Factory     string
	MountInfo   string
	BootJSON    string
	BootIDPath  string
	ReleaseRoot string
}

// ReadUpdateGate reports the same trial/pending/corrupt conditions that
// GET /v1/update exposes. A missing factory on a direct-root image is clear.
func ReadUpdateGate(paths GatePaths) (Gate, error) {
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
	factory, err := release.DecodeManifest(f)
	f.Close()
	if err != nil {
		return Gate{}, err
	}
	boot, err := applianceupdate.ReadBootIdentity(paths.BootJSON, paths.BootIDPath)
	if err != nil {
		return Gate{}, err
	}
	store, err := appliance.New(paths.ReleaseRoot, factory)
	if err != nil {
		return Gate{}, err
	}
	st, err := store.Status()
	if err != nil {
		return Gate{Trial: boot.Trial, Pending: st.Pending != "", Corrupt: st.Corrupt}, err
	}
	return Gate{Trial: boot.Trial, Pending: st.Pending != "", Corrupt: st.Corrupt}, nil
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
