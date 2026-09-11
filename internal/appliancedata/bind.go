// Package appliancedata bind-mounts FogCast mutable directories from the
// leftover-capacity FESDATA3 partition when that partition already exists.
//
// The assembler still ships the fixed 1 GiB FAT plus A2 layout. Creating p3
// is a host-side live-card mutation. This package never grows FAT, never
// moves A2, never formats a partition, and never touches releases,
// credentials, or known-good images.
package appliancedata

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/DeanoC/FogCast/internal/appliance"
)

const (
	DefaultLabel       = "FESDATA3"
	DefaultLabelDevice = "/dev/disk/by-label/" + DefaultLabel
	DefaultMMCDevice   = "/dev/mmcblk0p3"
	DefaultMountPoint  = "/run/fogcast/fesdata3"
	DefaultFATRoot     = "/media/fat/fogcast"
	SkipAbsent         = "absent"
	SkipTrial          = "trial"
	SkipPending        = "pending"
	SkipCorrupt        = "corrupt"
	SkipUpdate         = "update-status"
)

// DefaultBindNames are the FogCast trees that live on p3 after a successful
// bind. Releases, agent.toml, launcher.json and target-id stay on FAT.
var DefaultBindNames = []string{"cache", "saves", "core-data", "launcher-cache", "evidence"}

var forbiddenBindNames = map[string]struct{}{
	"releases":      {},
	"agent.toml":    {},
	"launcher.json": {},
	"target-id":     {},
	"known-good":    {},
}

// Gate is the /v1/update subset that must stay clear before bind.
type Gate struct {
	Trial   bool
	Pending bool
	Corrupt bool
}

func (g Gate) Blocked() (string, bool) {
	if g.Corrupt {
		return SkipCorrupt, true
	}
	if g.Trial {
		return SkipTrial, true
	}
	if g.Pending {
		return SkipPending, true
	}
	return "", false
}

// Result records whether p3 was used. Skipped is a stable reason when bind
// was refused or the partition is absent.
type Result struct {
	Device  string
	Bound   []string
	Skipped string
}

// Mounter is the Linux mount/bind surface. Tests substitute a recorder.
type Mounter interface {
	MountVolume(source, target string) error
	Bind(source, target string) error
}

// Config locates p3, FAT trees, and the update gate. Zero DevicePresent,
// ReadLabel, Mounted, or EvaluateGate values use the production defaults.
type Config struct {
	Devices       []string
	Label         string
	MountPoint    string
	FATRoot       string
	BindNames     []string
	MountInfo     string
	FactoryPath   string
	BootJSON      string
	BootIDPath    string
	ReleaseRoot   string
	EvaluateGate  func() (Gate, error)
	DevicePresent func(string) bool
	ReadLabel     func(string) (string, error)
	Mounted       func(string) bool
	Mounter       Mounter
}

func ProductionConfig() Config {
	return Config{
		Devices:     []string{DefaultLabelDevice, DefaultMMCDevice},
		Label:       DefaultLabel,
		MountPoint:  DefaultMountPoint,
		FATRoot:     DefaultFATRoot,
		BindNames:   append([]string(nil), DefaultBindNames...),
		MountInfo:   "/proc/self/mountinfo",
		FactoryPath: "/.fes-bootstrap/etc/fes/factory.json",
		BootJSON:    filepath.Join(appliance.DefaultRoot, "boot.json"),
		BootIDPath:  "/proc/sys/kernel/random/boot_id",
		ReleaseRoot: appliance.DefaultRoot,
		Mounter:     platformMounter(),
	}
}

// Prepare bind-mounts p3 over the mutable FogCast directories when FESDATA3
// exists and /v1/update is not trial, pending, or corrupt. Missing p3 is a
// no-op. A blocked or unreadable update status refuses the bind without
// mutating mounts.
func Prepare(cfg Config) (Result, error) {
	if err := validateConfig(cfg); err != nil {
		return Result{}, err
	}
	cfg = applyDefaults(cfg)
	device, ok := findDevice(cfg)
	if !ok {
		return Result{Skipped: SkipAbsent}, nil
	}
	gate, err := cfg.EvaluateGate()
	if err != nil {
		return Result{Device: device, Skipped: SkipUpdate}, fmt.Errorf("refusing fesdata3 bind: %w", err)
	}
	if reason, blocked := gate.Blocked(); blocked {
		return Result{Device: device, Skipped: reason}, nil
	}
	if err := os.MkdirAll(cfg.MountPoint, 0o755); err != nil {
		return Result{Device: device}, err
	}
	if !cfg.Mounted(cfg.MountPoint) {
		if err := cfg.Mounter.MountVolume(device, cfg.MountPoint); err != nil {
			return Result{Device: device}, err
		}
	}
	var result Result
	result.Device = device
	var bindErr error
	for _, name := range cfg.BindNames {
		fatPath := filepath.Join(cfg.FATRoot, name)
		dataPath := filepath.Join(cfg.MountPoint, name)
		if cfg.Mounted(fatPath) {
			result.Bound = append(result.Bound, name)
			continue
		}
		if err := os.MkdirAll(dataPath, 0o700); err != nil {
			bindErr = errors.Join(bindErr, err)
			continue
		}
		if err := copyMissing(fatPath, dataPath); err != nil {
			bindErr = errors.Join(bindErr, err)
			continue
		}
		if err := ensureBindTarget(fatPath); err != nil {
			bindErr = errors.Join(bindErr, err)
			continue
		}
		if err := cfg.Mounter.Bind(dataPath, fatPath); err != nil {
			bindErr = errors.Join(bindErr, err)
			continue
		}
		result.Bound = append(result.Bound, name)
	}
	return result, bindErr
}

func validateConfig(cfg Config) error {
	names := cfg.BindNames
	if len(names) == 0 {
		names = DefaultBindNames
	}
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == "" || name != filepath.Base(name) || strings.Contains(name, "/") || name == "." || name == ".." {
			return fmt.Errorf("invalid bind name %q", name)
		}
		if _, blocked := forbiddenBindNames[name]; blocked {
			return fmt.Errorf("refusing to bind protected path %s", name)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate bind name %s", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func applyDefaults(cfg Config) Config {
	if len(cfg.Devices) == 0 {
		cfg.Devices = []string{DefaultLabelDevice, DefaultMMCDevice}
	}
	if cfg.Label == "" {
		cfg.Label = DefaultLabel
	}
	if cfg.MountPoint == "" {
		cfg.MountPoint = DefaultMountPoint
	}
	if cfg.FATRoot == "" {
		cfg.FATRoot = DefaultFATRoot
	}
	if len(cfg.BindNames) == 0 {
		cfg.BindNames = append([]string(nil), DefaultBindNames...)
	}
	if cfg.Mounter == nil {
		cfg.Mounter = platformMounter()
	}
	if cfg.DevicePresent == nil {
		cfg.DevicePresent = pathExists
	}
	if cfg.ReadLabel == nil {
		cfg.ReadLabel = readExt4Label
	}
	if cfg.Mounted == nil {
		cfg.Mounted = func(target string) bool { return mountpointBusy(cfg.MountInfo, target) }
	}
	if cfg.EvaluateGate == nil {
		cfg.EvaluateGate = func() (Gate, error) {
			return ReadUpdateGate(GatePaths{
				Factory:     cfg.FactoryPath,
				MountInfo:   cfg.MountInfo,
				BootJSON:    cfg.BootJSON,
				BootIDPath:  cfg.BootIDPath,
				ReleaseRoot: cfg.ReleaseRoot,
			})
		}
	}
	return cfg
}

func findDevice(cfg Config) (string, bool) {
	for _, path := range cfg.Devices {
		if !cfg.DevicePresent(path) {
			continue
		}
		if strings.HasSuffix(path, "/"+cfg.Label) {
			return path, true
		}
		label, err := cfg.ReadLabel(path)
		if err == nil && label == cfg.Label {
			return path, true
		}
	}
	return "", false
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ensureBindTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("bind target is not a real directory: %s", path)
	}
	return nil
}

func copyMissing(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	info, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(dst, 0o700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("bind source is not a real directory: %s", src)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("refusing path escape from %s", src)
		}
		dest := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(dest, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyRegularMissing(path, dest)
	})
}

func copyRegularMissing(src, dst string) error {
	info, err := os.Lstat(dst)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			return nil
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return nil
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

func mountpointBusy(mountInfo, target string) bool {
	if mountInfo == "" || target == "" {
		return false
	}
	data, err := os.ReadFile(mountInfo)
	if err != nil {
		return false
	}
	target = filepath.Clean(target)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && filepath.Clean(unescapeMount(fields[4])) == target {
			return true
		}
	}
	return false
}

func unescapeMount(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}
