package fpgadev

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Attempt describes how far a FIFO dispatch got. NotInvoked means that no
// descriptor was successfully opened. Invoked means that a descriptor was
// opened or an operation was attempted, so the command may have been
// consumed even when an error was reported. Completed is reserved for a
// complete write followed by a successful close.
type Attempt uint8

const (
	NotInvoked Attempt = iota
	Invoked
	Completed
)

func (a Attempt) String() string {
	switch a {
	case NotInvoked:
		return "NotInvoked"
	case Invoked:
		return "Invoked"
	case Completed:
		return "Completed"
	default:
		return "Attempt(?)"
	}
}

// FIFO is the synchronous compatibility Main command FIFO adapter. The
// default owner is root; tests for temporary FIFOs can set ExpectedUID to the
// test process UID explicitly.
type FIFO struct {
	Path        string
	ExpectedUID uint32
	// PollInterval bounds the sleep between nonblocking open attempts and
	// retries of a temporarily unwritable FIFO.
	PollInterval time.Duration

	// ops is an in-package seam used by hostile syscall/error tests. Production
	// operations use the descriptor-bound defaults below.
	ops *fifoOperations
}

// NewFIFO constructs a FIFO adapter. The optional owner UID is intended for
// software tests; production callers should omit it so root ownership is
// required.
func NewFIFO(path string, expectedUID ...uint32) *FIFO {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &FIFO{Path: path, ExpectedUID: uid}
}

type fifoDescriptor struct {
	Device uint64
	Inode  uint64
	Mode   uint32
	UID    uint32
}

type fifoOperations struct {
	// open is retained as a legacy injected seam. Supplying it selects the
	// legacy path-only test adapter; production defaults never use it.
	lstat      func(string) (os.FileInfo, error)
	open       func(string, int, uint32) (int, error)
	openRoot   func() (int, error)
	openParent func(int, string) (int, error)
	bindFinal  func(int, string) (int, error)
	openFinal  func(int, string, int, uint32) (int, error)
	fstat      func(int) (fifoDescriptor, error)
	poll       func([]unix.PollFd, int) (int, error)
	write      func(int, []byte) (int, error)
	close      func(int) error
}

var fifoDefaultOps = fifoOperations{
	openRoot:   openTrustedRoot,
	openParent: openTrustedParent,
	bindFinal:  bindFIFOFinal,
	openFinal:  openBoundFIFO,
	fstat:      statFIFODescriptor,
	poll:       unix.Poll,
	write:      unix.Write,
	close:      unix.Close,
}

type fifoError struct {
	label string
	err   error
}

func (e fifoError) Error() string { return e.label }
func (e fifoError) Unwrap() error { return e.err }

func wrapFIFO(label string, err error) error {
	if err == nil {
		return errors.New(label)
	}
	return fifoError{label: label, err: err}
}

func (f FIFO) operations() fifoOperations {
	ops := fifoDefaultOps
	if f.ops == nil {
		return ops
	}
	if f.ops.open != nil {
		ops.open = f.ops.open
		if f.ops.lstat != nil {
			ops.lstat = f.ops.lstat
		} else {
			ops.lstat = os.Lstat
		}
	} else if f.ops.lstat != nil {
		ops.lstat = f.ops.lstat
	}
	if f.ops.openRoot != nil {
		ops.openRoot = f.ops.openRoot
	}
	if f.ops.openParent != nil {
		ops.openParent = f.ops.openParent
	}
	if f.ops.bindFinal != nil {
		ops.bindFinal = f.ops.bindFinal
	}
	if f.ops.openFinal != nil {
		ops.openFinal = f.ops.openFinal
	}
	if f.ops.fstat != nil {
		ops.fstat = f.ops.fstat
	}
	if f.ops.poll != nil {
		ops.poll = f.ops.poll
	}
	if f.ops.write != nil {
		ops.write = f.ops.write
	}
	if f.ops.close != nil {
		ops.close = f.ops.close
	}
	return ops
}

func (f FIFO) usesLegacyOperations() bool {
	return f.ops != nil && f.ops.open != nil
}

// Dispatch writes exactly one validated command synchronously. There is no
// detached operation: when this method returns, all descriptors are closed
// and no later write can occur.
func (f FIFO) Dispatch(ctx context.Context, command string) (Attempt, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateFIFOCommand(command); err != nil {
		return NotInvoked, err
	}
	if err := validateFIFOPath(f.Path); err != nil {
		return NotInvoked, err
	}
	if err := ctx.Err(); err != nil {
		return NotInvoked, err
	}
	ops := f.operations()
	if f.usesLegacyOperations() {
		return f.dispatchLegacy(ctx, command, ops)
	}
	return f.dispatchBound(ctx, command, ops)
}

func (f FIFO) dispatchLegacy(ctx context.Context, command string, ops fifoOperations) (Attempt, error) {
	if err := validateFIFOAncestors(f.Path, ops.lstat); err != nil {
		return NotInvoked, err
	}
	info, err := ops.lstat(f.Path)
	if err != nil {
		return NotInvoked, wrapFIFO("lstat FIFO failed", err)
	}
	if err := validateFIFOInfo(info, f.ExpectedUID); err != nil {
		return NotInvoked, err
	}

	fd, opened, err := f.openLegacy(ctx, ops)
	if err != nil {
		if opened {
			return f.closeAfterOpened(fd, -1, -1, ops, err)
		}
		return NotInvoked, err
	}
	descriptor, statErr := ops.fstat(fd)
	if statErr != nil {
		return f.closeAfterOpened(fd, -1, -1, ops, wrapFIFO("stat FIFO descriptor failed", statErr))
	}
	if err := validateFIFODescriptor(descriptor, f.ExpectedUID); err != nil {
		return f.closeAfterOpened(fd, -1, -1, ops, err)
	}
	if err := ctx.Err(); err != nil {
		return f.closeAfterOpened(fd, -1, -1, ops, err)
	}
	if err := f.write(ctx, ops, fd, []byte(command)); err != nil {
		return f.closeAfterOpened(fd, -1, -1, ops, err)
	}
	if err := ops.close(fd); err != nil {
		return Invoked, wrapFIFO("close FIFO failed", err)
	}
	return Completed, nil
}

func (f FIFO) dispatchBound(ctx context.Context, command string, ops fifoOperations) (Attempt, error) {
	parent, leaf, err := splitFIFOPath(f.Path)
	if err != nil {
		return NotInvoked, err
	}
	rootFD, err := ops.openRoot()
	if err != nil {
		if rootFD >= 0 {
			_ = ops.close(rootFD)
		}
		return NotInvoked, wrapFIFO("open FIFO root failed", err)
	}
	if rootFD < 0 {
		return NotInvoked, errors.New("open FIFO root returned an invalid descriptor")
	}
	if err := ctx.Err(); err != nil {
		_ = ops.close(rootFD)
		return NotInvoked, err
	}
	parentFD, err := ops.openParent(rootFD, parent)
	rootCloseErr := ops.close(rootFD)
	if err != nil {
		if parentFD >= 0 {
			_ = ops.close(parentFD)
		}
		if rootCloseErr != nil {
			err = errors.Join(wrapFIFO("open FIFO parent failed", err), wrapFIFO("close FIFO root failed", rootCloseErr))
		}
		return NotInvoked, wrapFIFO("open FIFO parent failed", err)
	}
	if parentFD < 0 {
		return NotInvoked, errors.New("open FIFO parent returned an invalid descriptor")
	}
	if rootCloseErr != nil {
		_ = ops.close(parentFD)
		return NotInvoked, wrapFIFO("close FIFO root failed", rootCloseErr)
	}

	// Bind the name through the held parent descriptor before any writer
	// retries. The bind descriptor is no-follow and supplies the immutable
	// dev/inode/type/mode/UID tuple for final revalidation.
	bindFD, err := ops.bindFinal(parentFD, leaf)
	if err != nil {
		if bindFD >= 0 {
			_ = ops.close(bindFD)
		}
		_ = ops.close(parentFD)
		return NotInvoked, wrapFIFO("bind FIFO failed", err)
	}
	if bindFD < 0 {
		_ = ops.close(parentFD)
		return NotInvoked, errors.New("bind FIFO returned an invalid descriptor")
	}
	bound, statErr := ops.fstat(bindFD)
	if statErr != nil {
		_ = ops.close(bindFD)
		_ = ops.close(parentFD)
		return NotInvoked, wrapFIFO("stat bound FIFO failed", statErr)
	}
	if err := validateFIFODescriptor(bound, f.ExpectedUID); err != nil {
		_ = ops.close(bindFD)
		_ = ops.close(parentFD)
		return NotInvoked, err
	}

	fd, opened, err := f.openBound(ctx, ops, parentFD, leaf)
	if err != nil {
		if opened {
			return f.closeAfterOpened(fd, bindFD, parentFD, ops, err)
		}
		_ = ops.close(bindFD)
		_ = ops.close(parentFD)
		return NotInvoked, err
	}
	actual, statErr := ops.fstat(fd)
	if statErr != nil {
		return f.closeAfterOpened(fd, bindFD, parentFD, ops, wrapFIFO("stat FIFO descriptor failed", statErr))
	}
	if err := validateFIFODescriptor(actual, f.ExpectedUID); err != nil {
		return f.closeAfterOpened(fd, bindFD, parentFD, ops, err)
	}
	if !sameFIFODescriptor(bound, actual) {
		return f.closeAfterOpened(fd, bindFD, parentFD, ops, errors.New("FIFO descriptor changed during open"))
	}
	if err := ctx.Err(); err != nil {
		return f.closeAfterOpened(fd, bindFD, parentFD, ops, err)
	}
	if err := f.write(ctx, ops, fd, []byte(command)); err != nil {
		return f.closeAfterOpened(fd, bindFD, parentFD, ops, err)
	}
	var closeErr error
	if err := ops.close(fd); err != nil {
		closeErr = wrapFIFO("close FIFO failed", err)
	}
	if err := ops.close(bindFD); err != nil {
		closeErr = errors.Join(closeErr, wrapFIFO("close bound FIFO failed", err))
	}
	if err := ops.close(parentFD); err != nil {
		closeErr = errors.Join(closeErr, wrapFIFO("close FIFO parent failed", err))
	}
	if closeErr != nil {
		return Invoked, closeErr
	}
	return Completed, nil
}

func (f FIFO) openLegacy(ctx context.Context, ops fifoOperations) (int, bool, error) {
	flags := unix.O_WRONLY | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOFOLLOW
	for {
		if err := ctx.Err(); err != nil {
			return -1, false, err
		}
		fd, err := ops.open(f.Path, flags, 0)
		if err == nil {
			if fd < 0 {
				return -1, false, errors.New("open FIFO returned an invalid descriptor")
			}
			return fd, true, nil
		}
		if fd >= 0 {
			_ = ops.close(fd)
			return -1, false, wrapFIFO("open FIFO failed after descriptor allocation", err)
		}
		if err == unix.EINTR {
			continue
		}
		if err != unix.ENXIO && err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			return -1, false, wrapFIFO("open FIFO failed", err)
		}
		if err := f.pollContext(ctx, ops); err != nil {
			return -1, false, err
		}
	}
}

func (f FIFO) openBound(ctx context.Context, ops fifoOperations, parentFD int, leaf string) (int, bool, error) {
	flags := unix.O_WRONLY | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOFOLLOW
	for {
		if err := ctx.Err(); err != nil {
			return -1, false, err
		}
		fd, err := ops.openFinal(parentFD, leaf, flags, 0)
		if err == nil {
			if fd < 0 {
				return -1, false, errors.New("open FIFO returned an invalid descriptor")
			}
			return fd, true, nil
		}
		if fd >= 0 {
			_ = ops.close(fd)
			return -1, false, wrapFIFO("open FIFO failed after descriptor allocation", err)
		}
		if err == unix.EINTR {
			continue
		}
		if err != unix.ENXIO && err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			return -1, false, wrapFIFO("open FIFO failed", err)
		}
		if err := f.pollContext(ctx, ops); err != nil {
			return -1, false, err
		}
	}
}

func (f FIFO) write(ctx context.Context, ops fifoOperations, fd int, command []byte) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := ops.write(fd, command)
		if n < 0 || n > len(command) {
			return errors.New("FIFO write returned an invalid byte count")
		}
		if err == unix.EINTR && n == 0 {
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			if n != 0 {
				return errors.Join(io.ErrShortWrite, wrapFIFO("FIFO write was partial", err))
			}
			if pollErr := f.pollContext(ctx, ops, fd); pollErr != nil {
				return pollErr
			}
			continue
		}
		if err != nil {
			return wrapFIFO("write FIFO failed", err)
		}
		if n != len(command) {
			return io.ErrShortWrite
		}
		return nil
	}
}

func (f FIFO) closeAfterOpened(fd, bindFD, parentFD int, ops fifoOperations, cause error) (Attempt, error) {
	var closeErr error
	if fd >= 0 {
		if err := ops.close(fd); err != nil {
			closeErr = wrapFIFO("close FIFO failed", err)
		}
	}
	if bindFD >= 0 {
		if err := ops.close(bindFD); err != nil {
			closeErr = errors.Join(closeErr, wrapFIFO("close bound FIFO failed", err))
		}
	}
	if parentFD >= 0 {
		if err := ops.close(parentFD); err != nil {
			closeErr = errors.Join(closeErr, wrapFIFO("close FIFO parent failed", err))
		}
	}
	if closeErr != nil {
		return Invoked, errors.Join(cause, closeErr)
	}
	return Invoked, cause
}

func (f FIFO) pollContext(ctx context.Context, ops fifoOperations, descriptor ...int) error {
	interval := f.PollInterval
	if interval <= 0 {
		interval = 10 * time.Millisecond
	}
	// Keep cancellation observation bounded even if a caller supplies a
	// pathological interval. unix.Poll has millisecond granularity.
	if interval > 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	timeout := int(interval / time.Millisecond)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var fds []unix.PollFd
		if len(descriptor) != 0 {
			fds = []unix.PollFd{{Fd: int32(descriptor[0]), Events: unix.POLLOUT}}
		}
		n, err := ops.poll(fds, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return wrapFIFO("poll FIFO failed", err)
		}
		if n < 0 {
			return errors.New("poll FIFO returned an invalid event count")
		}
		if len(fds) != 0 && n > 0 {
			revents := fds[0].Revents
			if revents&(unix.POLLNVAL|unix.POLLERR) != 0 {
				return errors.New("poll FIFO reported a descriptor error")
			}
			if revents&unix.POLLHUP != 0 {
				return errors.New("poll FIFO reported a closed reader")
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
}

func validateFIFOCommand(command string) error {
	if len(command) < 2 || command[len(command)-1] != '\n' {
		return errors.New("FIFO command must be a non-empty newline-terminated line")
	}
	if !utf8.ValidString(command) {
		return errors.New("FIFO command is not valid UTF-8")
	}
	for index, value := range command {
		if value == '\n' {
			if index != len(command)-1 {
				return errors.New("FIFO command contains an embedded newline")
			}
			continue
		}
		if unicode.IsControl(value) {
			return errors.New("FIFO command contains a control byte")
		}
	}
	return nil
}

func validateFIFOPath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return errors.New("FIFO path must be an absolute canonical path")
	}
	return nil
}

func splitFIFOPath(path string) (parent, leaf string, err error) {
	if err := validateFIFOPath(path); err != nil {
		return "", "", err
	}
	relative := strings.TrimPrefix(path, string(filepath.Separator))
	leaf = filepath.Base(relative)
	parent = filepath.Dir(relative)
	if parent == "" {
		parent = "."
	}
	if leaf == "." || leaf == string(filepath.Separator) || strings.Contains(leaf, string(filepath.Separator)) {
		return "", "", errors.New("FIFO path has no valid final component")
	}
	return parent, leaf, nil
}

func validateFIFOAncestors(path string, lstat func(string) (os.FileInfo, error)) error {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := lstat(current)
		if err != nil {
			return wrapFIFO("lstat FIFO parent failed", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("FIFO parent must not be a symlink")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func validateFIFOInfo(info os.FileInfo, expectedUID uint32) error {
	if info == nil {
		return errors.New("FIFO lstat returned no entry")
	}
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		return errors.New("FIFO path must not be a symlink")
	}
	if mode&os.ModeNamedPipe == 0 {
		return errors.New("FIFO path is not a named pipe")
	}
	if mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || !allowedFIFOPermissions(uint32(mode.Perm())) {
		return errors.New("FIFO mode must be 0600 or 0644")
	}
	uid, ok := fileInfoUID(info)
	if !ok || uid != expectedUID {
		return errors.New("FIFO owner is not authorized")
	}
	return nil
}

func validateFIFODescriptor(stat fifoDescriptor, expectedUID uint32) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFIFO {
		return errors.New("FIFO descriptor is not a named pipe")
	}
	if !allowedFIFOPermissions(stat.Mode & 0o7777) {
		return errors.New("FIFO descriptor mode must be 0600 or 0644")
	}
	if stat.UID != expectedUID {
		return errors.New("FIFO descriptor owner is not authorized")
	}
	if stat.Device == 0 || stat.Inode == 0 {
		return errors.New("FIFO descriptor identity is invalid")
	}
	return nil
}

func allowedFIFOPermissions(mode uint32) bool {
	// Compatibility Main publishes 0644 on the DE10-Nano: only its owner can
	// write commands. Keep the allowlist exact so no other mode is implied.
	return mode == 0o600 || mode == 0o644
}

func sameFIFODescriptor(left, right fifoDescriptor) bool {
	return left.Device == right.Device && left.Inode == right.Inode && left.Mode == right.Mode && left.UID == right.UID
}

func statFIFODescriptor(fd int) (fifoDescriptor, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fifoDescriptor{}, err
	}
	return fifoDescriptor{Device: uint64(stat.Dev), Inode: uint64(stat.Ino), Mode: uint32(stat.Mode), UID: uint32(stat.Uid)}, nil
}

func fileInfoUID(info os.FileInfo) (uint32, bool) {
	if info == nil || info.Sys() == nil {
		return 0, false
	}
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	field := v.FieldByName("Uid")
	if !field.IsValid() || !field.CanUint() {
		return 0, false
	}
	return uint32(field.Uint()), true
}

// Linux openat2 constants are kept local so this file remains compilable on
// Darwin. The runtime guard below is the only path that invokes the Linux raw
// syscall; Darwin returns a fail-closed unsupported error.
const (
	linuxOpenat2Syscall    = uintptr(437)
	linuxOPath             = uint64(0x200000)
	linuxResolveNoSymlinks = uint64(0x04)
	linuxResolveBeneath    = uint64(0x08)
)

type linuxOpenHow struct {
	Flags   uint64
	Mode    uint64
	Resolve uint64
}

func openTrustedRoot() (int, error) {
	if runtime.GOOS != "linux" {
		return -1, ErrUnsupported
	}
	return unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
}

func openTrustedParent(rootFD int, parent string) (int, error) {
	return openat2FIFO(rootFD, parent, linuxOPath|uint64(unix.O_DIRECTORY)|uint64(unix.O_CLOEXEC), 0, linuxResolveBeneath|linuxResolveNoSymlinks)
}

func bindFIFOFinal(parentFD int, leaf string) (int, error) {
	return openat2FIFO(parentFD, leaf, linuxOPath|uint64(unix.O_CLOEXEC)|uint64(unix.O_NOFOLLOW), 0, linuxResolveBeneath|linuxResolveNoSymlinks)
}

func openBoundFIFO(parentFD int, leaf string, flags int, mode uint32) (int, error) {
	return openat2FIFO(parentFD, leaf, uint64(flags), uint64(mode), linuxResolveBeneath|linuxResolveNoSymlinks)
}

func openat2FIFO(dirfd int, path string, flags, mode, resolve uint64) (int, error) {
	if runtime.GOOS != "linux" {
		return -1, ErrUnsupported
	}
	pathBytes, err := unix.BytePtrFromString(path)
	if err != nil {
		return -1, err
	}
	how := linuxOpenHow{Flags: flags, Mode: mode, Resolve: resolve}
	rawFD, _, errno := unix.Syscall6(linuxOpenat2Syscall, uintptr(dirfd), uintptr(unsafe.Pointer(pathBytes)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
	if errno != 0 {
		return -1, errno
	}
	return int(rawFD), nil
}
