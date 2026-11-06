// Package appliancedata bind-mounts FogCast mutable directories and the game
// library from the leftover-capacity FESDATA3 partition when it exists.
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

	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"github.com/DeanoC/FogCast/appliance/store"
)

const (
	DefaultLabel           = "FESDATA3"
	DefaultLabelDevice     = "/dev/disk/by-label/" + DefaultLabel
	DefaultMMCDevice       = "/dev/mmcblk0p3"
	DefaultMountPoint      = "/run/fogcast/fesdata3"
	DefaultFATRoot         = "/media/fat/fogcast"
	DefaultLibraryFATRoot  = "/media/fat/games"
	DefaultLibraryDataRoot = DefaultMountPoint + "/games"
	SkipAbsent             = "absent"
	SkipTrial              = "trial"
	SkipPending            = "pending"
	SkipCorrupt            = "corrupt"
	SkipUpdate             = "update-status"
)

// gamesMigrationMarker is stored at the p3 root, outside the bound game tree.
const gamesMigrationMarker = ".fes-games-migrated-v1"

// gamesFallbackMarker is stored beside the FAT game tree while p3 is
// unavailable or its bind is gated. It lets the next successful bind copy
// game files added during that FAT-only interval without repeating migration
// on every normal startup.
const gamesFallbackMarker = ".fes-games-fallback-v1"

// DefaultBindNames are the FogCast trees that live on p3 after a successful
// bind. The game library tree is mounted separately from /media/fat/games.
// Releases, agent.toml, launcher.json and target-id stay on FAT.
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
	Unbind(volumeRoot, target string) error
}

// Config locates p3, FAT trees, and the update gate. Zero DevicePresent,
// ReadLabel, Mounted, or EvaluateGate values use the production defaults.
type Config struct {
	Devices         []string
	Label           string
	MountPoint      string
	FATRoot         string
	LibraryFATRoot  string
	LibraryDataRoot string
	BindNames       []string
	MountInfo       string
	FactoryPath     string
	BootJSON        string
	BootIDPath      string
	ReleaseRoot     string
	EvaluateGate    func() (Gate, error)
	DevicePresent   func(string) bool
	// Probe refuses a candidate before it is opened. Nil uses the sysfs check
	// that skips optical devices and disks with no usable media.
	Probe       func(string) error
	ReadLabel   func(string) (string, error)
	Mounted     func(string) bool
	BindMatches func(source, target string) (bool, error)
	Mounter     Mounter
}

func ProductionConfig() Config {
	return Config{
		Devices:         []string{DefaultLabelDevice, DefaultMMCDevice},
		Label:           DefaultLabel,
		MountPoint:      DefaultMountPoint,
		FATRoot:         DefaultFATRoot,
		LibraryFATRoot:  DefaultLibraryFATRoot,
		LibraryDataRoot: DefaultLibraryDataRoot,
		BindNames:       append([]string(nil), DefaultBindNames...),
		MountInfo:       "/proc/self/mountinfo",
		FactoryPath:     "/.fes-bootstrap/etc/fes/factory.json",
		BootJSON:        filepath.Join(appliance.DefaultRoot, "boot.json"),
		BootIDPath:      "/proc/sys/kernel/random/boot_id",
		ReleaseRoot:     appliance.DefaultRoot,
		Mounter:         platformMounter(),
	}
}

// SelectedDevice is the block device Prepare would mount. It uses the same
// candidate order and label rule as Prepare and does not mount or format.
// A path ending in the volume label is accepted when present, so a labelled
// USB partition matches /dev/disk/by-label/FESDATA3. Any other candidate must
// carry that ext4 label. The second result is false when no candidate is present.
func SelectedDevice(cfg Config) (string, bool) {
	cfg = applyDefaults(cfg)
	return findDevice(cfg)
}

// Prepare bind-mounts p3 over the mutable FogCast directories when FESDATA3
// exists and /v1/update is not trial, pending, or corrupt. Missing p3 is a
// no-op. A blocked or unreadable update status refuses the bind without
// mutating mounts.
// Mount prepares only the ext4 mount. It never copies or bind-mounts mutable
// application data, and obeys the same trial/pending/corruption gate as Prepare.
func Mount(cfg Config) (Result, error) {
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
	return Result{Device: device}, nil
}

func Prepare(cfg Config) (Result, error) {
	mounted, err := Mount(cfg)
	cfg = applyDefaults(cfg)
	if mounted.Skipped != "" {
		if markErr := markGamesFallback(cfg.LibraryFATRoot); markErr != nil {
			err = errors.Join(err, markErr)
		}
		return mounted, err
	}
	if err != nil {
		return mounted, err
	}
	device := mounted.Device

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
	bindGames := true
	repairGames := false
	if cfg.Mounted(cfg.LibraryFATRoot) {
		matches, err := cfg.BindMatches(cfg.LibraryDataRoot, cfg.LibraryFATRoot)
		if err != nil {
			bindErr = errors.Join(bindErr, fmt.Errorf("verify game library bind: %w", err))
			bindGames = false
		} else if matches {
			result.Bound = append(result.Bound, "games")
			bindGames = false
		} else if err := cfg.Mounter.Unbind(cfg.MountPoint, cfg.LibraryFATRoot); err != nil {
			bindErr = errors.Join(bindErr, fmt.Errorf("remove stale game library bind: %w", err))
			bindGames = false
		} else {
			repairGames = true
		}
	}
	if bindGames {
		marker := filepath.Join(cfg.MountPoint, gamesMigrationMarker)
		fallbackPending, err := gamesFallbackPending(cfg.LibraryFATRoot)
		if err != nil {
			bindErr = errors.Join(bindErr, err)
		} else {
			var gameErr error
			if repairGames || fallbackPending {
				gameErr = repairGamesOnce(cfg.LibraryFATRoot, cfg.LibraryDataRoot, marker)
			} else {
				gameErr = migrateGamesOnce(cfg.LibraryFATRoot, cfg.LibraryDataRoot, marker)
			}
			if gameErr != nil {
				bindErr = errors.Join(bindErr, gameErr)
			} else {
				if fallbackPending {
					if err := clearGamesFallback(cfg.LibraryFATRoot); err != nil {
						bindErr = errors.Join(bindErr, err)
					}
				}
				if err := ensureBindTarget(cfg.LibraryFATRoot); err != nil {
					bindErr = errors.Join(bindErr, err)
				} else if err := cfg.Mounter.Bind(cfg.LibraryDataRoot, cfg.LibraryFATRoot); err != nil {
					bindErr = errors.Join(bindErr, err)
				} else {
					result.Bound = append(result.Bound, "games")
				}
			}
		}
	}
	return result, bindErr
}

func gamesFallbackMarkerPath(libraryFATRoot string) string {
	return filepath.Join(filepath.Dir(libraryFATRoot), gamesFallbackMarker)
}

func gamesFallbackPending(libraryFATRoot string) (bool, error) {
	marker := gamesFallbackMarkerPath(libraryFATRoot)
	info, err := os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("games fallback marker is not a regular file: %s", marker)
	}
	return true, nil
}

func markGamesFallback(libraryFATRoot string) error {
	marker := gamesFallbackMarkerPath(libraryFATRoot)
	pending, err := gamesFallbackPending(libraryFATRoot)
	if err != nil || pending {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	if err := copyMarkerAtomically(strings.NewReader("pending\n"), marker); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(marker))
}

func clearGamesFallback(libraryFATRoot string) error {
	marker := gamesFallbackMarkerPath(libraryFATRoot)
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(filepath.Dir(marker))
}

func validateConfig(cfg Config) error {
	for _, path := range []string{cfg.LibraryFATRoot, cfg.LibraryDataRoot} {
		if path != "" && !filepath.IsAbs(path) {
			return fmt.Errorf("library bind path must be absolute: %s", path)
		}
	}
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
	if cfg.LibraryFATRoot == "" {
		cfg.LibraryFATRoot = DefaultLibraryFATRoot
	}
	if cfg.LibraryDataRoot == "" {
		cfg.LibraryDataRoot = filepath.Join(cfg.MountPoint, "games")
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
	if cfg.Probe == nil {
		cfg.Probe = productionProbe
	}
	if cfg.ReadLabel == nil {
		cfg.ReadLabel = readExt4Label
	}
	if cfg.Mounted == nil {
		cfg.Mounted = func(target string) bool { return mountpointBusy(cfg.MountInfo, target) }
	}
	if cfg.BindMatches == nil {
		cfg.BindMatches = sameBindSource
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

func productionProbe(path string) error {
	return blockprobe.RejectsDevice("/sys", path)
}

func findDevice(cfg Config) (string, bool) {
	for _, path := range cfg.Devices {
		if !cfg.DevicePresent(path) {
			continue
		}
		if err := cfg.Probe(path); errors.Is(err, blockprobe.ErrSkip) {
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

func sameBindSource(source, target string) (bool, error) {
	sourceInfo, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.IsDir() {
		return false, fmt.Errorf("bind source is not a real directory: %s", source)
	}
	targetInfo, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(sourceInfo, targetInfo), nil
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

func migrateGamesOnce(src, dst, marker string) error {
	return migrateGamesOnceWithSync(src, dst, marker, syncDirectory)
}

func migrateGamesOnceWithSync(src, dst, marker string, syncDir func(string) error) error {
	complete, err := migrationComplete(marker)
	if err != nil {
		return err
	}
	if complete {
		info, err := os.Lstat(dst)
		if errors.Is(err, os.ErrNotExist) {
			if err := copyMissing(src, dst); err != nil {
				return err
			}
			return syncDestinationDirectories(src, dst, syncDir)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("game data root is not a real directory: %s", dst)
		}
		return nil
	}
	if err := copyMissing(src, dst); err != nil {
		return err
	}
	if err := syncDestinationDirectories(src, dst, syncDir); err != nil {
		return err
	}
	if err := copyFileAtomically(strings.NewReader("complete\n"), marker); err != nil {
		return err
	}
	complete, err = migrationComplete(marker)
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("games migration marker was not published: %s", marker)
	}
	return syncDir(filepath.Dir(marker))
}

func repairGamesOnce(src, dst, marker string) error {
	complete, err := migrationComplete(marker)
	if err != nil {
		return err
	}
	if !complete {
		return migrateGamesOnce(src, dst, marker)
	}
	info, err := os.Lstat(dst)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return fmt.Errorf("game data root is not a real directory: %s", dst)
	}
	if err := copyMissing(src, dst); err != nil {
		return err
	}
	return syncDestinationDirectories(src, dst, syncDirectory)
}

func syncDestinationDirectories(src, dst string, syncDir func(string) error) error {
	directories := []string{dst}
	info, err := os.Lstat(src)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return fmt.Errorf("bind source is not a real directory: %s", src)
	}
	if err == nil {
		if err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() && path != src {
				rel, err := filepath.Rel(src, path)
				if err != nil {
					return err
				}
				directories = append(directories, filepath.Join(dst, rel))
			}
			return nil
		}); err != nil {
			return err
		}
	}
	// Sync deepest directories first so both their file entries and the
	// directory entries in their parents are durable before the marker appears.
	for i := len(directories) - 1; i >= 0; i-- {
		if err := syncDir(directories[i]); err != nil {
			return fmt.Errorf("sync game destination directory %s: %w", directories[i], err)
		}
	}
	// copyMissing may have created the destination root itself.
	parent := filepath.Dir(dst)
	if err := syncDir(parent); err != nil {
		return fmt.Errorf("sync game destination parent directory %s: %w", parent, err)
	}
	return nil
}

func migrationComplete(marker string) (bool, error) {
	info, err := os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("games migration marker is not a regular file: %s", marker)
	}
	return true, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
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
	return copyFileAtomically(in, dst)
}

func copyFileAtomically(in io.Reader, dst string) error {
	return copyFileAtomicallyWithPublisher(in, dst, func(temp, dst string) error {
		// Link publishes the complete temporary without replacing an entry that
		// another startup or process may have created after the initial Lstat.
		err := os.Link(temp, dst)
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	})
}

func copyMarkerAtomically(in io.Reader, dst string) error {
	return copyMarkerAtomicallyWithOps(in, dst, os.Link, os.Rename)
}

func copyMarkerAtomicallyWithOps(in io.Reader, dst string, link, rename func(string, string) error) error {
	return copyFileAtomicallyWithPublisher(in, dst, func(temp, dst string) error {
		linkErr := link(temp, dst)
		if linkErr == nil || errors.Is(linkErr, os.ErrExist) {
			return nil
		}
		if renameErr := rename(temp, dst); renameErr != nil {
			return errors.Join(fmt.Errorf("link fallback marker: %w", linkErr), fmt.Errorf("rename fallback marker: %w", renameErr))
		}
		// The fallback marker is an idempotent sentinel. A same-directory rename
		// publishes it atomically on filesystems such as VFAT that lack hard links.
		return nil
	})
}

func copyFileAtomicallyWithPublisher(in io.Reader, dst string, publish func(temp, dst string) error) error {
	out, err := os.CreateTemp(filepath.Dir(dst), ".fes-copy-*")
	if err != nil {
		return err
	}
	temp := out.Name()
	defer os.Remove(temp)

	if _, err = io.Copy(out, in); err != nil {
		return errors.Join(err, out.Close())
	}
	if err = out.Sync(); err != nil {
		return errors.Join(err, out.Close())
	}
	if err = out.Close(); err != nil {
		return err
	}
	return publish(temp, dst)
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
