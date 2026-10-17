// Package blockprobe decides whether a Linux block device may be opened.
//
// The decision uses only the sysfs attributes size, removable and device/type.
// It does not open the device node. Optical devices (sr* or SCSI type 5),
// removable devices with no media, and any other zero-capacity device are
// refused so callers do not block in SCSI command timeouts.
package blockprobe

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// ReasonOptical is an sr* node or SCSI peripheral type 5.
	ReasonOptical = "optical"
	// ReasonNoMedia is a removable device whose size is zero.
	ReasonNoMedia = "no-media"
	// ReasonZero is any other device whose size is zero.
	ReasonZero = "zero-capacity"

	// OpenTimeout bounds an open and read that passed the sysfs check.
	OpenTimeout = 3 * time.Second
)

// ErrSkip means the device must not be opened, mounted or stated.
var ErrSkip = errors.New("skipping block device")

// Decision is the sysfs classification of one block device.
type Decision struct {
	Name   string
	Allow  bool
	Reason string
}

// Decide reads size, removable and device/type for name under blockDir.
// blockDir is either /sys/class/block or /sys/block. The boolean is false when
// name is not present there and is not an sr* node.
func Decide(blockDir, name string) (Decision, bool) {
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return Decision{}, false
	}
	dir, found := deviceDir(blockDir, name)
	if !found {
		if opticalName(name) {
			return Decision{Name: name, Allow: false, Reason: ReasonOptical}, true
		}
		return Decision{}, false
	}
	return classify(blockDir, dir, name), true
}

// RejectsDevice reports ErrSkip when path is a /dev node that Decide refuses.
// Paths outside /dev, and /dev nodes with no sysfs entry other than sr*, are
// allowed so ordinary files and labelled volumes keep their existing behaviour.
func RejectsDevice(sysfs, path string) error {
	resolved := path
	if followed, err := filepath.EvalSymlinks(path); err == nil {
		resolved = followed
	}
	if !isDevicePath(path) && !isDevicePath(resolved) {
		return nil
	}
	name := filepath.Base(resolved)
	return rejectName(sysfs, name)
}

// RejectsName applies Decide to a basename in a sysfs tree, including names
// that are not under /dev. Tests use it with a fake tree.
func RejectsName(sysfs, name string) error {
	return rejectName(sysfs, filepath.Base(name))
}

// Usable returns sorted device names under blockDir that Decide allows.
// Nested partition directories one level down are included.
func Usable(blockDir string) ([]string, error) {
	entries, err := os.ReadDir(blockDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if decision, ok := Decide(blockDir, name); ok && decision.Allow {
			names = append(names, name)
		}
		if !entry.IsDir() {
			continue
		}
		children, readErr := os.ReadDir(filepath.Join(blockDir, name))
		if readErr != nil {
			continue
		}
		for _, child := range children {
			childName := child.Name()
			if parentDisk(childName) != name {
				continue
			}
			if decision, ok := Decide(blockDir, childName); ok && decision.Allow {
				names = append(names, childName)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// Skips reports whether path must not be stated or opened. A path is skipped
// when it is a refused /dev node, or when the longest mount covering it comes
// from a refused block device.
func Skips(path string) bool {
	return SkipsRoot("/sys", "/proc/self/mountinfo", path)
}

// SkipsRoot is Skips with explicit sysfs and mountinfo locations.
func SkipsRoot(sysfs, mountinfo, path string) bool {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return false
	}
	if isDevicePath(path) && RejectsDevice(sysfs, path) != nil {
		return true
	}
	source, ok := coveringSource(mountinfo, path)
	if !ok || !isDevicePath(source) {
		return false
	}
	return RejectsDevice(sysfs, source) != nil
}

// ReadFullAt reads len(buf) bytes at off. A refused device returns ErrSkip
// without being opened. An allowed open or read that exceeds timeout returns
// an error while the stuck call is abandoned.
func ReadFullAt(path string, buf []byte, off int64, timeout time.Duration) error {
	if err := RejectsDevice("/sys", path); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = OpenTimeout
	}
	type result struct {
		file *os.File
		err  error
	}
	done := make(chan result, 1)
	go func() {
		file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			done <- result{err: err}
			return
		}
		// The open was non-blocking. The read of an admitted device is the
		// same blocking read callers used before, still bounded by timeout.
		_ = unix.SetNonblock(int(file.Fd()), false)
		_, err = file.ReadAt(buf, off)
		if err != nil && !errors.Is(err, io.EOF) {
			done <- result{file: file, err: err}
			return
		}
		if err != nil {
			done <- result{file: file, err: io.ErrUnexpectedEOF}
			return
		}
		done <- result{file: file}
	}()
	select {
	case got := <-done:
		if got.file != nil {
			_ = got.file.Close()
		}
		return got.err
	case <-time.After(timeout):
		return fmt.Errorf("reading %s timed out after %s", path, timeout)
	}
}

func rejectName(sysfs, name string) error {
	for _, dir := range []string{
		filepath.Join(sysfs, "class", "block"),
		filepath.Join(sysfs, "block"),
	} {
		decision, ok := Decide(dir, name)
		if !ok {
			continue
		}
		if !decision.Allow {
			return fmt.Errorf("%w: %s (%s)", ErrSkip, name, decision.Reason)
		}
		return nil
	}
	if opticalName(name) {
		return fmt.Errorf("%w: %s (%s)", ErrSkip, name, ReasonOptical)
	}
	return nil
}

func classify(blockDir, dir, name string) Decision {
	if opticalName(name) || scsiType(blockDir, dir, name) == 5 {
		return Decision{Name: name, Allow: false, Reason: ReasonOptical}
	}
	size, sizeOK := readUint(filepath.Join(dir, "size"))
	removable := readTrim(filepath.Join(dir, "removable")) == "1"
	if !removable {
		if parent := parentDisk(name); parent != "" {
			if parentDir, ok := deviceDir(blockDir, parent); ok {
				removable = readTrim(filepath.Join(parentDir, "removable")) == "1"
			}
		}
	}
	if sizeOK && size == 0 && removable {
		return Decision{Name: name, Allow: false, Reason: ReasonNoMedia}
	}
	if sizeOK && size == 0 {
		return Decision{Name: name, Allow: false, Reason: ReasonZero}
	}
	return Decision{Name: name, Allow: true}
}

func scsiType(blockDir, dir, name string) int {
	if value, ok := readInt(filepath.Join(dir, "device", "type")); ok {
		return value
	}
	parent := parentDisk(name)
	if parent == "" {
		return -1
	}
	parentDir, ok := deviceDir(blockDir, parent)
	if !ok {
		return -1
	}
	if value, ok := readInt(filepath.Join(parentDir, "device", "type")); ok {
		return value
	}
	return -1
}

func deviceDir(blockDir, name string) (string, bool) {
	direct := filepath.Join(blockDir, name)
	if info, err := os.Lstat(direct); err == nil && info.IsDir() {
		return direct, true
	}
	if parent := parentDisk(name); parent != "" {
		nested := filepath.Join(blockDir, parent, name)
		if info, err := os.Lstat(nested); err == nil && info.IsDir() {
			return nested, true
		}
	}
	return "", false
}

func opticalName(name string) bool {
	rest, ok := strings.CutPrefix(name, "sr")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parentDisk(name string) string {
	if strings.HasPrefix(name, "mmcblk") || strings.HasPrefix(name, "nvme") {
		index := strings.LastIndex(name, "p")
		if index <= 0 || !digits(name[index+1:]) {
			return ""
		}
		return name[:index]
	}
	end := len(name)
	for end > 0 && name[end-1] >= '0' && name[end-1] <= '9' {
		end--
	}
	if end == len(name) || end == 0 {
		return ""
	}
	base := name[:end]
	switch base {
	case "sd", "sr", "hd", "vd", "xvd", "loop":
		return ""
	default:
		return base
	}
}

func digits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isDevicePath(path string) bool {
	cleaned := filepath.Clean(path)
	return cleaned == "/dev" || strings.HasPrefix(cleaned, "/dev/")
}

func coveringSource(mountinfo, path string) (string, bool) {
	if !filepath.IsAbs(path) {
		return "", false
	}
	data, err := os.ReadFile(mountinfo)
	if err != nil {
		return "", false
	}
	bestLen := -1
	source := ""
	for _, line := range strings.Split(string(data), "\n") {
		mountpoint, dev, ok := parseMount(line)
		if !ok {
			continue
		}
		if !mountCovers(mountpoint, path) {
			continue
		}
		if len(mountpoint) > bestLen {
			bestLen = len(mountpoint)
			source = dev
		}
	}
	if bestLen < 0 {
		return "", false
	}
	return source, true
}

// mountCovers reports whether mountpoint contains path. The root mount is
// stored as "/" and must cover every absolute path; appending a slash would
// look for the prefix "//" and miss every child.
func mountCovers(mountpoint, path string) bool {
	if path == mountpoint || mountpoint == "/" {
		return true
	}
	return strings.HasPrefix(path, mountpoint+"/")
}

func parseMount(line string) (mountpoint, source string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return "", "", false
	}
	separator := -1
	for i := 6; i < len(fields); i++ {
		if fields[i] == "-" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+2 >= len(fields) {
		return "", "", false
	}
	return unescape(fields[4]), unescape(fields[separator+2]), true
}

func unescape(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}

func readTrim(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readUint(path string) (uint64, bool) {
	text := readTrim(path)
	if text == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func readInt(path string) (int, bool) {
	value, ok := readUint(path)
	if !ok || value > 255 {
		return -1, false
	}
	return int(value), true
}
