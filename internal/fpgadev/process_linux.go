//go:build linux

package fpgadev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

const processFlagKThread uint64 = 0x00200000

func init() {
	makeProcessScanner = func(root string) ProcessScanner {
		return linuxProcessScanner{root: root}
	}
	executableIdentityForPath = linuxExecutableIdentity
}

type linuxExecutableHash func(context.Context, string, ExecutableIdentity) (uint64, uint64, string, error)
type linuxHeldExecutableHash func(context.Context, *os.File, ExecutableIdentity) (uint64, uint64, string, error)
type linuxExecutableOpener func(context.Context, string) (*os.File, error)
type linuxProcStatReader func(context.Context, string, int) (procStatInfo, error)

type procStatInfo struct {
	PID       int
	State     byte
	Flags     uint64
	StartTime uint64
}

type executableSample struct {
	Device uint64
	Inode  uint64
	Size   int64
}

type linuxProcessScanner struct {
	root               string
	hashExecutable     linuxExecutableHash
	hashHeldExecutable linuxHeldExecutableHash
	openExecutable     linuxExecutableOpener
	readStat           linuxProcStatReader
}

type processScanError struct {
	operation string
	err       error
}

func (e processScanError) Error() string { return e.operation }
func (e processScanError) Unwrap() error { return e.err }

func processError(operation string, err error) error {
	if err == nil {
		return errors.New(operation)
	}
	// Keep the public diagnostic path-free while preserving errors.Is for
	// callers that need to classify a stable errno.
	return processScanError{operation: operation, err: err}
}

func (s linuxProcessScanner) Scan(ctx context.Context, expected ExecutableIdentity) ([]ProcessRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !expected.valid() {
		return nil, errors.New("expected executable identity is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := s.root
	if root == "" {
		root = "/proc"
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, processError("read proc population failed", err)
	}
	records := make([]ProcessRecord, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, ok := processPID(entry.Name())
		if !ok {
			continue
		}
		identity, present, err := s.readIdentity(ctx, root, pid, expected)
		if err != nil {
			return nil, err
		}
		if present {
			records = append(records, ProcessRecord{Identity: identity})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func processPID(name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	for _, value := range name {
		if value < '0' || value > '9' {
			return 0, false
		}
	}
	pid, err := strconv.Atoi(name)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func (s linuxProcessScanner) readIdentity(ctx context.Context, root string, pid int, expected ExecutableIdentity) (ProcessIdentity, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		identity, present, retry, err := s.readIdentityAttempt(ctx, root, pid, expected)
		if err != nil {
			return ProcessIdentity{}, false, err
		}
		if !retry {
			return identity, present, nil
		}
		if err := ctx.Err(); err != nil {
			return ProcessIdentity{}, false, err
		}
	}
	return ProcessIdentity{}, false, processError("process identity changed during scan", nil)
}

func (s linuxProcessScanner) readIdentityAttempt(ctx context.Context, root string, pid int, expected ExecutableIdentity) (ProcessIdentity, bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}
	processDir := filepath.Join(root, strconv.Itoa(pid))
	statPath := filepath.Join(processDir, "stat")
	exePath := filepath.Join(processDir, "exe")
	statBefore, err := s.readStatValue(ctx, statPath, pid)
	if err != nil {
		if transientProcessError(err) {
			return ProcessIdentity{}, false, false, nil
		}
		return ProcessIdentity{}, false, false, err
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}

	opener := s.openExecutable
	if opener == nil {
		opener = openLinuxExecutable
	}
	held, err := opener(ctx, exePath)
	if err != nil {
		if held != nil {
			_ = held.Close()
		}
		return s.classifyMissingExecutable(ctx, statPath, pid, statBefore, err)
	}
	if held == nil {
		return ProcessIdentity{}, false, false, processError("open process executable returned no descriptor", nil)
	}
	closed := false
	defer func() {
		if !closed {
			_ = held.Close()
		}
	}()

	first, err := sampleExecutableFile(held)
	if err != nil {
		return ProcessIdentity{}, false, false, processError("stat process executable failed", err)
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}
	statMiddle, err := s.readStatValue(ctx, statPath, pid)
	if err != nil {
		if transientProcessError(err) {
			return ProcessIdentity{}, false, false, nil
		}
		return ProcessIdentity{}, false, false, err
	}
	if statMiddle.StartTime != statBefore.StartTime || statMiddle.PID != statBefore.PID {
		return ProcessIdentity{}, false, true, nil
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}

	digest := ""
	if !expected.valid() || (first.Device == expected.Device && first.Inode == expected.Inode) {
		var hashDevice, hashInode uint64
		var hashErr error
		if s.hashExecutable != nil {
			hashDevice, hashInode, digest, hashErr = s.hashExecutable(ctx, exePath, expected)
		} else {
			hasher := s.hashHeldExecutable
			if hasher == nil {
				hasher = hashLinuxExecutableFileContext
			}
			hashDevice, hashInode, digest, hashErr = hasher(ctx, held, expected)
		}
		if hashErr != nil {
			if transientProcessError(hashErr) {
				return s.classifyMissingExecutable(ctx, statPath, pid, statMiddle, hashErr)
			}
			return ProcessIdentity{}, false, false, hashErr
		}
		if hashDevice != first.Device || hashInode != first.Inode {
			return ProcessIdentity{}, false, true, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}

	second, secondErr := opener(ctx, exePath)
	if secondErr != nil {
		if second != nil {
			_ = second.Close()
		}
		return s.classifyMissingExecutable(ctx, statPath, pid, statMiddle, secondErr)
	}
	if second == nil {
		return ProcessIdentity{}, false, false, processError("open process executable returned no descriptor", nil)
	}
	secondSample, sampleErr := sampleExecutableFile(second)
	secondCloseErr := second.Close()
	if sampleErr != nil {
		return ProcessIdentity{}, false, false, processError("stat process executable failed", sampleErr)
	}
	if secondCloseErr != nil {
		return ProcessIdentity{}, false, false, processError("close process executable failed", secondCloseErr)
	}
	if first.Device != secondSample.Device || first.Inode != secondSample.Inode {
		return ProcessIdentity{}, false, true, nil
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}
	statAfter, statErr := s.readStatValue(ctx, statPath, pid)
	if statErr != nil {
		if transientProcessError(statErr) {
			return ProcessIdentity{}, false, false, nil
		}
		return ProcessIdentity{}, false, false, statErr
	}
	if statAfter.PID != statBefore.PID || statAfter.StartTime != statBefore.StartTime {
		return ProcessIdentity{}, false, true, nil
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}
	if err := held.Close(); err != nil {
		closed = true
		return ProcessIdentity{}, false, false, processError("close process executable failed", err)
	}
	closed = true
	return ProcessIdentity{PID: pid, StartTime: statAfter.StartTime, Device: first.Device, Inode: first.Inode, SHA256: digest}, true, false, nil
}

func (s linuxProcessScanner) classifyMissingExecutable(ctx context.Context, statPath string, pid int, before procStatInfo, openErr error) (ProcessIdentity, bool, bool, error) {
	if !transientProcessError(openErr) {
		return ProcessIdentity{}, false, false, processError("open process executable failed", openErr)
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, false, false, err
	}
	after, err := s.readStatValue(ctx, statPath, pid)
	if err != nil {
		if transientProcessError(err) {
			return ProcessIdentity{}, false, false, nil
		}
		return ProcessIdentity{}, false, false, err
	}
	if after.PID != before.PID || after.StartTime != before.StartTime {
		return ProcessIdentity{}, false, true, nil
	}
	if after.Flags&processFlagKThread != 0 || after.State == 'Z' || after.State == 'X' || after.State == 'x' {
		return ProcessIdentity{}, false, false, nil
	}
	return ProcessIdentity{}, false, false, processError("live process executable is unavailable", openErr)
}

func (s linuxProcessScanner) readStatValue(ctx context.Context, path string, pid int) (procStatInfo, error) {
	if s.readStat != nil {
		return s.readStat(ctx, path, pid)
	}
	return readLinuxProcStat(ctx, path, pid)
}

func openLinuxExecutable(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(path)
}

func sampleExecutableFile(file *os.File) (executableSample, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return executableSample{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return executableSample{}, processError("process executable is not regular", nil)
	}
	return executableSample{Device: uint64(stat.Dev), Inode: uint64(stat.Ino), Size: stat.Size}, nil
}

func transientProcessError(err error) bool {
	return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH)
}

func readLinuxProcStat(ctx context.Context, path string, pid int) (procStatInfo, error) {
	if err := ctx.Err(); err != nil {
		return procStatInfo{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return procStatInfo{}, processError("read process stat failed", err)
	}
	start, parsedPID, state, flags, err := parseProcStatFields(raw)
	if err != nil {
		return procStatInfo{}, processError("parse process stat failed", err)
	}
	if parsedPID != pid {
		return procStatInfo{}, processError("process stat PID mismatch", nil)
	}
	if start == 0 {
		return procStatInfo{}, processError("process stat start time is invalid", nil)
	}
	return procStatInfo{PID: parsedPID, State: state, Flags: flags, StartTime: start}, nil
}

func executableDeviceInode(ctx context.Context, path string) (uint64, uint64, error) {
	file, err := openLinuxExecutable(ctx, path)
	if err != nil {
		return 0, 0, processError("open process executable failed", err)
	}
	defer file.Close()
	sample, err := sampleExecutableFile(file)
	if err != nil {
		return 0, 0, processError("stat process executable failed", err)
	}
	return sample.Device, sample.Inode, nil
}

func readLinuxProcessIdentity(root string, pid int) (ProcessIdentity, error) {
	identity, present, err := (linuxProcessScanner{root: root}).readIdentity(context.Background(), root, pid, ExecutableIdentity{})
	if err != nil {
		return ProcessIdentity{}, err
	}
	if !present {
		return ProcessIdentity{}, processError("process disappeared during scan", unix.ENOENT)
	}
	return identity, nil
}

func readProcessStartInfo(path string, pid int) (uint64, byte, error) {
	info, err := readLinuxProcStat(context.Background(), path, pid)
	if err != nil {
		return 0, 0, err
	}
	return info.StartTime, info.State, nil
}

func readProcessStartTime(path string, pid int) (uint64, error) {
	start, _, err := readProcessStartInfo(path, pid)
	return start, err
}

func hashLinuxExecutable(path string) (device, inode uint64, digest string, err error) {
	return hashLinuxExecutableContext(context.Background(), path, ExecutableIdentity{})
}

func hashLinuxExecutableContext(ctx context.Context, path string, expected ExecutableIdentity) (device, inode uint64, digest string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	file, err := openLinuxExecutable(ctx, path)
	if err != nil {
		return 0, 0, "", processError("open process executable failed", err)
	}
	defer file.Close()
	return hashLinuxExecutableFileContext(ctx, file, expected)
}

func hashLinuxExecutableFileContext(ctx context.Context, file *os.File, expected ExecutableIdentity) (device, inode uint64, digest string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, "", err
	}
	first, err := sampleExecutableFile(file)
	if err != nil {
		return 0, 0, "", processError("stat process executable failed", err)
	}
	if expected.valid() && (first.Device != expected.Device || first.Inode != expected.Inode) {
		return 0, 0, "", processError("process executable changed during scan", nil)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, 0, "", processError("rewind process executable failed", err)
	}
	hasher := sha256.New()
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, "", err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			if _, err := hasher.Write(buffer[:n]); err != nil {
				return 0, 0, "", processError("hash process executable failed", err)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return 0, 0, "", processError("read process executable failed", readErr)
		}
		if n == 0 {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, "", err
	}
	last, err := sampleExecutableFile(file)
	if err != nil {
		return 0, 0, "", processError("restat process executable failed", err)
	}
	if first.Device != last.Device || first.Inode != last.Inode || first.Size != last.Size {
		return 0, 0, "", processError("process executable changed during scan", nil)
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, "", err
	}
	return first.Device, first.Inode, hex.EncodeToString(hasher.Sum(nil)), nil
}

func linuxExecutableIdentity(path string) (ExecutableIdentity, error) {
	device, inode, digest, err := hashLinuxExecutable(path)
	if err != nil {
		return ExecutableIdentity{}, err
	}
	return ExecutableIdentity{Device: device, Inode: inode, SHA256: digest}, nil
}
