//go:build linux && fpgadev

package fpgadev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
	"golang.org/x/sys/unix"
)

var ErrFaultUnsupported = errors.New("development fault controls are unavailable")

const faultCheckpointReacquireTimeout = time.Second

// Inspection deliberately omits private executable device/inode fields.
type Inspection struct {
	RunID            string
	Session          string
	Generation       uint64
	Phase            string
	PID              int
	StartTime        uint64
	ExecutableSHA256 string
}

type faultController interface {
	Arm(context.Context, string) error
	Inspect(context.Context, string) (Inspection, error)
	Kill(context.Context, string) error
}

type fencedFaultKiller interface {
	KillBound(context.Context, string, string, uint64, hardwareowner.Phase) error
}

type faultCheckpoint interface {
	Checkpoint(context.Context, faultDiagnosticForCheckpoint, hardwareowner.Unlock, func(context.Context) (hardwareowner.Unlock, error)) (hardwareowner.Unlock, error)
}

type faultDiagnosticForCheckpoint struct {
	RunID      string
	Session    string
	Generation uint64
	Phase      hardwareowner.Phase
}

type faultDiagnostic struct {
	RunID            string `json:"run_id"`
	Session          string `json:"session"`
	Generation       uint64 `json:"generation"`
	Phase            string `json:"phase"`
	PID              int    `json:"pid"`
	StartTime        uint64 `json:"start_time"`
	ExecutableSHA256 string `json:"executable_sha256"`
	ExecutableDevice uint64 `json:"executable_device"`
	ExecutableInode  uint64 `json:"executable_inode"`
}

type faultPeerIdentity struct {
	PID       int
	StartTime uint64
	Device    uint64
	Inode     uint64
	SHA256    string
}

type faultRequest struct {
	RunID      string `json:"run_id"`
	Session    string `json:"session"`
	Generation uint64 `json:"generation"`
}

var faultDiagnosticFields = [...]string{
	"run_id", "session", "generation", "phase", "pid", "start_time",
	"executable_sha256", "executable_device", "executable_inode",
}

var faultRequestFields = [...]string{"run_id", "session", "generation"}

type filesystemFaultController struct {
	root                string
	expectedUID         uint32
	diagnostic          func(faultDiagnosticForCheckpoint) (faultDiagnostic, error)
	identity            func(int) (faultPeerIdentity, error)
	processGone         func(context.Context, faultDiagnostic) error
	peerCredentials     func(net.Conn) (int, uint32, error)
	dial                func(context.Context, string, string) (net.Conn, error)
	listen              func(string) (net.Listener, error)
	remove              func(string) error
	syncDirectory       func(string) error
	openSelfExecutable  func() (*os.File, error)
	selfStartTime       func() (uint64, error)
	selfKill            func() error
	afterExecutableOpen func()
}

// readProductionManifest binds manifest validation and parsing bytes to Linux
// descriptors reached beneath a protected staging-directory descriptor. It
// returns that still-open validated descriptor to the private artifact binder,
// so the manifest and top.rbf cannot be taken from different staging paths.
func readProductionManifest(ctx context.Context, staging string, expectedUID uint32, afterOpen func()) ([]byte, *productionStagingDirectory, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	directoryFD, err := openArtifactDirectory(staging, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open manifest staging directory: %w", err)
	}
	directoryFile := os.NewFile(uintptr(directoryFD), staging)
	if directoryFile == nil {
		return nil, nil, errors.Join(ErrFaultUnsupported, unix.Close(directoryFD))
	}
	directory := &productionStagingDirectory{file: directoryFile, path: staging}
	closeWith := func(err error) ([]byte, *productionStagingDirectory, error) {
		return nil, nil, errors.Join(err, directory.close())
	}
	var directoryStat unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryStat); err != nil {
		return closeWith(fmt.Errorf("stat manifest staging directory: %w", err))
	}
	if err := validateDirectoryStat(&directoryStat, expectedUID); err != nil {
		return closeWith(err)
	}

	manifest, err := openProductionManifestAt(directoryFD)
	if err != nil {
		return closeWith(err)
	}
	raw, firstStat, inspectErr := inspectProductionManifest(manifest, expectedUID, afterOpen)
	manifestCloseErr := manifest.Close()
	if err := errors.Join(inspectErr, manifestCloseErr); err != nil {
		return closeWith(err)
	}
	if err := ctx.Err(); err != nil {
		return closeWith(err)
	}

	reopened, err := openProductionManifestAt(directoryFD)
	if err != nil {
		return closeWith(err)
	}
	_, secondStat, inspectErr := inspectProductionManifest(reopened, expectedUID, nil)
	reopenCloseErr := reopened.Close()
	if err := errors.Join(inspectErr, reopenCloseErr); err != nil {
		return closeWith(err)
	}
	if !sameProductionManifestStat(firstStat, secondStat) {
		return closeWith(errors.New("manifest changed during immediate revalidation"))
	}
	if err := ctx.Err(); err != nil {
		return closeWith(err)
	}
	return raw, directory, nil
}

// bindProductionStagingDirectory transfers a validated directory descriptor
// into ArtifactBinding only after top.rbf has been opened and revalidated
// beneath that same descriptor. The caller retains directory ownership on
// every error; a successful binding consumes it.
func (a ArtifactAccess) bindProductionStagingDirectory(manifest Manifest, directory *productionStagingDirectory) (ArtifactBinding, error) {
	if err := validateManifestForBinding(manifest); err != nil {
		return ArtifactBinding{}, fmt.Errorf("validate artifact manifest: %w", err)
	}
	if directory == nil || directory.file == nil {
		return ArtifactBinding{}, errors.New("validated staging directory is unavailable")
	}
	if err := validateArtifactPath(directory.path); err != nil {
		return ArtifactBinding{}, err
	}
	directoryFD := int(directory.file.Fd())
	var directoryStat unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryStat); err != nil {
		return ArtifactBinding{}, fmt.Errorf("restat validated staging directory: %w", err)
	}
	if err := validateDirectoryStat(&directoryStat, a.ExpectedUID); err != nil {
		return ArtifactBinding{}, err
	}

	artifactPath := filepath.Join(directory.path, manifest.ArtifactFilename)
	firstArtifact, firstMetadata, err := openAndInspectArtifact(directoryFD, manifest, a.ExpectedUID, artifactPath)
	if err != nil {
		return ArtifactBinding{}, err
	}
	secondArtifact, secondMetadata, err := openAndInspectArtifact(directoryFD, manifest, a.ExpectedUID, artifactPath)
	if err != nil {
		return ArtifactBinding{}, errors.Join(err, firstArtifact.Close())
	}
	if !sameArtifactMetadata(firstMetadata, secondMetadata) {
		return ArtifactBinding{}, errors.Join(errors.New("artifact changed during immediate revalidation"), firstArtifact.Close(), secondArtifact.Close())
	}
	if err := firstArtifact.Close(); err != nil {
		return ArtifactBinding{}, errors.Join(fmt.Errorf("close initial artifact descriptor: %w", err), secondArtifact.Close())
	}

	directoryFile := directory.file
	directory.file = nil
	secondMetadata.Path = artifactPath
	return ArtifactBinding{
		state: &artifactBindingState{
			dir:         directoryFile,
			artifact:    secondArtifact,
			metadata:    secondMetadata,
			manifest:    manifest,
			expectedUID: a.ExpectedUID,
		},
	}, nil
}

func openProductionManifestAt(directoryFD int) (*os.File, error) {
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(directoryFD, "manifest.json", how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
			return nil, fmt.Errorf("openat2 manifest resolution unsupported: %w", ErrUnsupported)
		}
		return nil, fmt.Errorf("open manifest with openat2: %w", err)
	}
	file := os.NewFile(uintptr(fd), "manifest.json")
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrFaultUnsupported
	}
	return file, nil
}

func inspectProductionManifest(file *os.File, expectedUID uint32, afterOpen func()) ([]byte, unix.Stat_t, error) {
	if file == nil {
		return nil, unix.Stat_t{}, ErrFaultUnsupported
	}
	var before unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &before); err != nil {
		return nil, unix.Stat_t{}, fmt.Errorf("stat manifest: %w", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, unix.Stat_t{}, errors.New("manifest is not a regular file")
	}
	if uint32(before.Uid) != expectedUID {
		return nil, unix.Stat_t{}, errors.New("manifest owner is not authorized")
	}
	if before.Mode&0o7777 != 0o600 {
		return nil, unix.Stat_t{}, errors.New("manifest mode must be 0600")
	}
	if before.Nlink != 1 {
		return nil, unix.Stat_t{}, errors.New("manifest link count must be one")
	}
	if before.Size <= 0 || before.Size > int64(maxStagedManifestBytes) {
		return nil, unix.Stat_t{}, fmt.Errorf("manifest size must be between 1 and %d", maxStagedManifestBytes)
	}
	if afterOpen != nil {
		afterOpen()
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, unix.Stat_t{}, fmt.Errorf("seek manifest: %w", err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maxStagedManifestBytes)+1))
	if err != nil {
		return nil, unix.Stat_t{}, fmt.Errorf("read manifest: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxStagedManifestBytes || int64(len(raw)) != before.Size {
		return nil, unix.Stat_t{}, errors.New("manifest size changed while reading")
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &after); err != nil {
		return nil, unix.Stat_t{}, fmt.Errorf("restat manifest: %w", err)
	}
	if !sameProductionManifestStat(before, after) {
		return nil, unix.Stat_t{}, errors.New("manifest changed while reading")
	}
	return raw, before, nil
}

func sameProductionManifestStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Size == right.Size && left.Mode == right.Mode && left.Uid == right.Uid && left.Nlink == right.Nlink && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func newFilesystemFaultController(root string) faultController {
	return &filesystemFaultController{root: root, expectedUID: 0}
}

// openRunningExecutable opens the process-bound executable descriptor rather
// than resolving a mutable executable pathname. /proc/self/exe remains bound
// to the running image across an atomic replacement of its on-disk name.
func openRunningExecutable() (*os.File, error) { return os.Open("/proc/self/exe") }

func runningProcessStartTime() (uint64, error) {
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, err
	}
	start, parsedPID, err := parseProcStatStartTime(raw)
	if err != nil || parsedPID != os.Getpid() || start == 0 {
		return 0, ErrFaultUnsupported
	}
	return start, nil
}

func (c *filesystemFaultController) runtimeRoot(create bool) error {
	if c == nil || c.root == "" || !filepath.IsAbs(c.root) || filepath.Clean(c.root) != c.root {
		return ErrFaultUnsupported
	}
	if create {
		if err := os.MkdirAll(c.root, 0o700); err != nil {
			return ErrFaultUnsupported
		}
	}
	info, err := os.Lstat(c.root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !exactFileMode(info.Mode(), 0o700) {
		return ErrFaultUnsupported
	}
	uid, ok := fileUID(info)
	if !ok || uid != c.expectedUID {
		return ErrFaultUnsupported
	}
	return nil
}

func fileUID(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

func fileDeviceInode(info os.FileInfo) (uint64, uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return uint64(stat.Dev), uint64(stat.Ino), true
}

func linuxPeerCredentials(connection net.Conn) (pid int, uid uint32, err error) {
	syscallConnection, ok := connection.(syscall.Conn)
	if !ok {
		return 0, 0, ErrFaultUnsupported
	}
	rawConnection, err := syscallConnection.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var operationErr error
	var peerPID int
	var peerUID uint32
	if err := rawConnection.Control(func(fd uintptr) {
		credential, getErr := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if getErr != nil {
			operationErr = getErr
			return
		}
		peerPID = int(credential.Pid)
		peerUID = credential.Uid
	}); err != nil {
		return 0, 0, err
	}
	if operationErr != nil || peerPID <= 0 {
		if operationErr == nil {
			operationErr = ErrFaultUnsupported
		}
		return 0, 0, operationErr
	}
	return peerPID, peerUID, nil
}

func (c *filesystemFaultController) pathFor(runID, suffix string) (string, error) {
	if err := validateRunID(runID); err != nil {
		return "", err
	}
	if err := c.runtimeRoot(false); err != nil {
		return "", err
	}
	name := "fpga-dev-" + runID + suffix
	if suffix == ".sock" {
		return c.socketName(runID), nil
	}
	return filepath.Join(c.root, name), nil
}

func validateFaultEntry(path string, expectedUID uint32) (os.FileInfo, error) {
	file, err := openFaultRegular(path, os.O_RDONLY, expectedUID)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	return info, errors.Join(statErr, closeErr)
}

func openFaultRegular(path string, flags int, expectedUID uint32) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrFaultUnsupported
	}
	info, statErr := file.Stat()
	if statErr != nil || info == nil {
		_ = file.Close()
		return nil, ErrFaultUnsupported
	}
	uid, uidOK := fileUID(info)
	if !uidOK || uid != expectedUID || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !exactFileMode(info.Mode(), 0o600) {
		_ = file.Close()
		return nil, ErrFaultUnsupported
	}
	return file, nil
}

func validateFaultSocket(path string, expectedUID uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 || !exactFileMode(info.Mode(), 0o600) {
		return ErrFaultUnsupported
	}
	uid, ok := fileUID(info)
	if !ok || uid != expectedUID {
		return ErrFaultUnsupported
	}
	return nil
}

func validateFaultDiagnostic(diagnostic faultDiagnostic) error {
	if !manifestRunIDPattern.MatchString(diagnostic.RunID) ||
		!manifestRunIDPattern.MatchString(diagnostic.Session) ||
		diagnostic.Generation == 0 ||
		diagnostic.Phase != string(hardwareowner.PhaseLoadAttempted) ||
		diagnostic.PID <= 0 || diagnostic.StartTime == 0 ||
		!manifestHashPattern.MatchString(diagnostic.ExecutableSHA256) ||
		diagnostic.ExecutableDevice == 0 || diagnostic.ExecutableInode == 0 {
		return ErrFaultUnsupported
	}
	return nil
}

func validateFaultPeer(peer faultPeerIdentity, diagnostic faultDiagnostic) error {
	if err := validateFaultDiagnostic(diagnostic); err != nil {
		return err
	}
	if peer.PID != diagnostic.PID || peer.StartTime != diagnostic.StartTime || peer.Device != diagnostic.ExecutableDevice || peer.Inode != diagnostic.ExecutableInode || peer.SHA256 != diagnostic.ExecutableSHA256 {
		return ErrFaultUnsupported
	}
	return nil
}

func validateFaultDiagnosticFence(diagnostic faultDiagnostic, runID, session string, generation uint64, phase hardwareowner.Phase) error {
	if err := validateFaultDiagnostic(diagnostic); err != nil {
		return err
	}
	if diagnostic.RunID != runID || (session != "" && diagnostic.Session != session) || (generation != 0 && diagnostic.Generation != generation) || (phase != "" && diagnostic.Phase != string(phase)) {
		return ErrFaultUnsupported
	}
	return nil
}

func parseFaultRequest(raw []byte) (faultRequest, error) {
	var request faultRequest
	if err := decodeStrictObject(raw, faultRequestFields[:], func(key string, value []byte) error {
		switch key {
		case "run_id":
			return decodeJSONString(value, &request.RunID)
		case "session":
			return decodeJSONString(value, &request.Session)
		case "generation":
			return decodeJSONUint(value, &request.Generation)
		default:
			return ErrFaultUnsupported
		}
	}); err != nil {
		return faultRequest{}, err
	}
	canonical, err := json.Marshal(request)
	if err != nil || !bytes.Equal(canonical, raw) || !manifestRunIDPattern.MatchString(request.RunID) || !manifestRunIDPattern.MatchString(request.Session) || request.Generation == 0 {
		return faultRequest{}, ErrFaultUnsupported
	}
	return request, nil
}

func syncFaultDirectory(root string) error {
	directory, err := os.Open(root)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	return errors.Join(err, closeErr)
}

func (c *filesystemFaultController) syncRoot() error {
	if c != nil && c.syncDirectory != nil {
		return c.syncDirectory(c.root)
	}
	return syncFaultDirectory(c.root)
}

func (c *filesystemFaultController) removePath(path string) error {
	if c != nil && c.remove != nil {
		return c.remove(path)
	}
	return os.Remove(path)
}

func (c *filesystemFaultController) Arm(ctx context.Context, runID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRunID(runID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.runtimeRoot(true); err != nil {
		return ErrFaultUnsupported
	}
	path := filepath.Join(c.root, "fpga-dev-"+runID+".armed")
	file, err := openFaultRegular(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, c.expectedUID)
	if err != nil {
		return ErrFaultUnsupported
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		_ = c.removePath(path)
		_ = c.syncRoot()
		return ErrFaultUnsupported
	}
	if _, err := validateFaultEntry(path, c.expectedUID); err != nil {
		_ = c.removePath(path)
		_ = c.syncRoot()
		return ErrFaultUnsupported
	}
	if err := c.syncRoot(); err != nil {
		_ = c.removePath(path)
		_ = c.syncRoot()
		return ErrFaultUnsupported
	}
	if err := ctx.Err(); err != nil {
		_ = c.removePath(path)
		_ = c.syncRoot()
		return err
	}
	return nil
}

func (c *filesystemFaultController) Inspect(ctx context.Context, runID string) (Inspection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRunID(runID); err != nil {
		return Inspection{}, ErrFaultUnsupported
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	if err := c.runtimeRoot(false); err != nil {
		return Inspection{}, ErrFaultUnsupported
	}
	marker := filepath.Join(c.root, "fpga-dev-"+runID+".armed")
	if _, err := validateFaultEntry(marker, c.expectedUID); err != nil {
		return Inspection{}, ErrFaultUnsupported
	}
	diagnostic, err := c.readDiagnostic(ctx, runID)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{RunID: diagnostic.RunID, Session: diagnostic.Session, Generation: diagnostic.Generation, Phase: diagnostic.Phase, PID: diagnostic.PID, StartTime: diagnostic.StartTime, ExecutableSHA256: diagnostic.ExecutableSHA256}, nil
}

func (c *filesystemFaultController) readDiagnostic(ctx context.Context, runID string) (faultDiagnostic, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return faultDiagnostic{}, err
	}
	path := filepath.Join(c.root, "fpga-dev-"+runID+".diagnostic")
	file, err := openFaultRegular(path, os.O_RDONLY, c.expectedUID)
	if err != nil {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 16*1024))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	var diagnostic faultDiagnostic
	if err := decodeStrictObject(raw, faultDiagnosticFields[:], func(key string, value []byte) error {
		switch key {
		case "run_id":
			return decodeJSONString(value, &diagnostic.RunID)
		case "session":
			return decodeJSONString(value, &diagnostic.Session)
		case "generation":
			return decodeJSONUint(value, &diagnostic.Generation)
		case "phase":
			return decodeJSONString(value, &diagnostic.Phase)
		case "pid":
			return json.Unmarshal(bytes.TrimSpace(value), &diagnostic.PID)
		case "start_time":
			return decodeJSONUint(value, &diagnostic.StartTime)
		case "executable_sha256":
			return decodeJSONString(value, &diagnostic.ExecutableSHA256)
		case "executable_device":
			return decodeJSONUint(value, &diagnostic.ExecutableDevice)
		case "executable_inode":
			return decodeJSONUint(value, &diagnostic.ExecutableInode)
		default:
			return ErrFaultUnsupported
		}
	}); err != nil {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	canonical, err := json.Marshal(diagnostic)
	if err != nil || !bytes.Equal(append(canonical, '\n'), raw) || diagnostic.RunID != runID {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	if err := validateFaultDiagnostic(diagnostic); err != nil {
		return faultDiagnostic{}, err
	}
	if err := ctx.Err(); err != nil {
		return faultDiagnostic{}, err
	}
	return diagnostic, nil
}

func (c *filesystemFaultController) Kill(ctx context.Context, runID string) error {
	return c.killBound(ctx, runID, "", 0, "")
}

func (c *filesystemFaultController) KillBound(ctx context.Context, runID, session string, generation uint64, phase hardwareowner.Phase) error {
	return c.killBound(ctx, runID, session, generation, phase)
}

func (c *filesystemFaultController) killBound(ctx context.Context, runID, session string, generation uint64, phase hardwareowner.Phase) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRunID(runID); err != nil {
		return ErrFaultUnsupported
	}
	if err := c.runtimeRoot(false); err != nil {
		return ErrFaultUnsupported
	}
	marker := filepath.Join(c.root, "fpga-dev-"+runID+".armed")
	if _, err := validateFaultEntry(marker, c.expectedUID); err != nil {
		return ErrFaultUnsupported
	}
	diagnostic, err := c.readDiagnostic(ctx, runID)
	if err != nil {
		return err
	}
	if err := validateFaultDiagnosticFence(diagnostic, runID, session, generation, phase); err != nil {
		return err
	}
	identityFn := c.identity
	if identityFn == nil {
		identityFn = faultProcessIdentity
	}
	peer, err := identityFn(diagnostic.PID)
	if err != nil || validateFaultPeer(peer, diagnostic) != nil {
		return ErrFaultUnsupported
	}
	socketPath := c.socketName(runID)
	if !strings.HasPrefix(socketPath, "@") {
		if err := validateFaultSocket(socketPath, c.expectedUID); err != nil {
			return ErrFaultUnsupported
		}
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, time.Second)
	dial := c.dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	connection, err := dial(dialCtx, "unixpacket", socketPath)
	if err != nil && c.expectedUID != 0 && c.dial == nil {
		// Fixture roots may use stream sockets when the host kernel lacks
		// SOCK_SEQPACKET. Production always retains the exact packet endpoint.
		connection, err = (&net.Dialer{}).DialContext(dialCtx, "unix", socketPath)
	}
	dialErr := dialCtx.Err()
	dialCancel()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		_ = dialErr
		return ErrFaultUnsupported
	}
	defer connection.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	peerCredentials := c.peerCredentials
	if peerCredentials == nil {
		peerCredentials = linuxPeerCredentials
	}
	peerPID, peerUID, peerErr := peerCredentials(connection)
	requiredUID := uint32(0)
	if c.expectedUID != 0 {
		requiredUID = c.expectedUID
	}
	if peerErr != nil || peerUID != requiredUID || peerPID != diagnostic.PID {
		return ErrFaultUnsupported
	}
	peer, err = identityFn(peerPID)
	if err != nil || validateFaultPeer(peer, diagnostic) != nil {
		return ErrFaultUnsupported
	}
	request, err := json.Marshal(faultRequest{RunID: diagnostic.RunID, Session: diagnostic.Session, Generation: diagnostic.Generation})
	if err != nil {
		return ErrFaultUnsupported
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetWriteDeadline(deadline)
	} else {
		_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
	}
	if _, err := connection.Write(request); err != nil {
		return ErrFaultUnsupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
	} else {
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	}
	var response [1]byte
	readN, readErr := connection.Read(response[:])
	if readN != 0 || readErr == nil || (!errors.Is(readErr, io.EOF) && !errors.Is(readErr, syscall.ECONNRESET) && !errors.Is(readErr, syscall.ENOTCONN)) {
		return ErrFaultUnsupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.processGone != nil {
		return c.processGone(ctx, diagnostic)
	}
	return waitFaultProcessGone(ctx, diagnostic, identityFn)
}

func (c *filesystemFaultController) socketName(runID string) string {
	digest := sha256.Sum256([]byte(runID))
	name := filepath.Join(c.root, "f-"+hex.EncodeToString(digest[:])+".sock")
	if len(name) > 100 {
		// A Unix abstract address avoids truncating the full digest when the
		// configured runtime root itself is close to AF_UNIX's path limit.
		return "@fogcast-f-" + hex.EncodeToString(digest[:])
	}
	return name
}

func waitFaultProcessGone(ctx context.Context, diagnostic faultDiagnostic, identityFn func(int) (faultPeerIdentity, error)) error {
	deadline := time.Now().Add(2 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		peer, err := identityFn(diagnostic.PID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				return nil
			}
			return ErrFaultUnsupported
		}
		if validateFaultPeer(peer, diagnostic) != nil {
			return ErrFaultUnsupported
		}
		if !time.Now().Before(deadline) {
			return ErrFaultUnsupported
		}
		wait := 10 * time.Millisecond
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *filesystemFaultController) Checkpoint(ctx context.Context, checkpoint faultDiagnosticForCheckpoint, unlock hardwareowner.Unlock, acquire func(context.Context) (hardwareowner.Unlock, error)) (hardwareowner.Unlock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return unlock, err
	}
	if checkpoint.Phase != hardwareowner.PhaseLoadAttempted || !manifestRunIDPattern.MatchString(checkpoint.RunID) || !manifestRunIDPattern.MatchString(checkpoint.Session) || checkpoint.Generation == 0 || unlock == nil || acquire == nil {
		return unlock, ErrFaultUnsupported
	}
	if err := c.runtimeRoot(false); err != nil {
		return unlock, err
	}
	marker := filepath.Join(c.root, "fpga-dev-"+checkpoint.RunID+".armed")
	if _, err := validateFaultEntry(marker, c.expectedUID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return unlock, nil
		}
		return unlock, ErrFaultUnsupported
	}
	diagnostic, err := c.diagnosticFor(checkpoint)
	if err != nil {
		return unlock, err
	}
	if err := validateFaultDiagnosticFence(diagnostic, checkpoint.RunID, checkpoint.Session, checkpoint.Generation, checkpoint.Phase); err != nil {
		return unlock, err
	}
	if err := c.writeDiagnostic(checkpoint.RunID, diagnostic); err != nil {
		return unlock, err
	}
	socketPath := c.socketName(checkpoint.RunID)
	abstractSocket := strings.HasPrefix(socketPath, "@")
	if !abstractSocket {
		if _, statErr := os.Lstat(socketPath); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
			return unlock, ErrFaultUnsupported
		}
	}
	listen := c.listen
	if listen == nil {
		listen = func(path string) (net.Listener, error) { return net.Listen("unixpacket", path) }
	}
	listener, err := listen(socketPath)
	if err != nil && c.expectedUID != 0 && c.listen == nil {
		// Some unprivileged CI kernels do not expose the unixpacket network
		// name. Fixture roots use ordinary Unix stream sockets; production
		// always takes the exact SOCK_SEQPACKET path above.
		listener, err = net.Listen("unix", socketPath)
	}
	if err != nil {
		return unlock, ErrFaultUnsupported
	}
	cleanupDone := false
	var cleanupErr error
	cleanupSocket := func() error {
		if cleanupDone {
			return cleanupErr
		}
		cleanupDone = true
		var errs []error
		if err := listener.Close(); err != nil {
			errs = append(errs, err)
		}
		if !abstractSocket {
			if err := c.removePath(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		if err := c.syncRoot(); err != nil {
			errs = append(errs, err)
		}
		cleanupErr = errors.Join(errs...)
		return cleanupErr
	}
	if !abstractSocket {
		if err := os.Chmod(socketPath, 0o600); err != nil {
			return unlock, errors.Join(ErrFaultUnsupported, cleanupSocket())
		}
		if err := validateFaultSocket(socketPath, c.expectedUID); err != nil {
			return unlock, errors.Join(ErrFaultUnsupported, cleanupSocket())
		}
	}
	if err := c.syncRoot(); err != nil {
		return unlock, errors.Join(ErrFaultUnsupported, cleanupSocket())
	}
	if err := unlock(); err != nil {
		// The caller must not invoke an unlock a second time: the original
		// callback has already been consumed (whether or not the underlying
		// unlock syscall completed). Returning nil makes the lifecycle treat
		// this as a lost-lock boundary and prevents unlocked durable writes.
		return nil, errors.Join(err, cleanupSocket())
	}
	for {
		if err := ctx.Err(); err != nil {
			cleanupErr := cleanupSocket()
			reacquired, acquireErr := boundedFaultCheckpointReacquire(acquire)
			if acquireErr != nil || reacquired == nil {
				var reacquiredCleanupErr error
				if reacquired != nil {
					reacquiredCleanupErr = reacquired()
				}
				return nil, errors.Join(err, cleanupErr, acquireErr, reacquiredCleanupErr)
			}
			return reacquired, errors.Join(err, cleanupErr)
		}
		if unixListener, ok := listener.(*net.UnixListener); ok {
			_ = unixListener.SetDeadline(time.Now().Add(50 * time.Millisecond))
		}
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if netErr, ok := acceptErr.(net.Error); ok && netErr.Timeout() {
				continue
			}
			cleanupErr := cleanupSocket()
			reacquired, acquireErr := boundedFaultCheckpointReacquire(acquire)
			if acquireErr != nil || reacquired == nil {
				var reacquiredCleanupErr error
				if reacquired != nil {
					reacquiredCleanupErr = reacquired()
				}
				return nil, errors.Join(acceptErr, cleanupErr, acquireErr, reacquiredCleanupErr)
			}
			return reacquired, errors.Join(acceptErr, cleanupErr)
		}
		peerCredentials := c.peerCredentials
		if peerCredentials == nil {
			peerCredentials = linuxPeerCredentials
		}
		peerPID, peerUID, peerErr := peerCredentials(connection)
		requiredUID := uint32(0)
		if c.expectedUID != 0 {
			requiredUID = c.expectedUID
		}
		if peerErr != nil || peerUID != requiredUID || peerPID <= 0 {
			_ = connection.Close()
			continue
		}
		var raw [512]byte
		_ = connection.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, readErr := connection.Read(raw[:])
		if readErr != nil || n <= 0 || n == len(raw) {
			_ = connection.Close()
			continue
		}
		request, requestErr := parseFaultRequest(raw[:n])
		if requestErr != nil || request.RunID != checkpoint.RunID || request.Session != checkpoint.Session || request.Generation != checkpoint.Generation {
			_ = connection.Close()
			continue
		}
		// Keep the accepted packet connection open through the real self-kill;
		// the client must observe EOF and then confirm process disappearance.
		selfKill := c.selfKill
		if selfKill == nil {
			selfKill = func() error { return syscall.Kill(os.Getpid(), syscall.SIGKILL) }
		}
		if err := selfKill(); err != nil {
			_ = connection.Close()
			cleanupErr := cleanupSocket()
			reacquired, acquireErr := boundedFaultCheckpointReacquire(acquire)
			if acquireErr != nil || reacquired == nil {
				var reacquiredCleanupErr error
				if reacquired != nil {
					reacquiredCleanupErr = reacquired()
				}
				return nil, errors.Join(err, cleanupErr, acquireErr, reacquiredCleanupErr)
			}
			return reacquired, errors.Join(err, cleanupErr)
		}
	}
}

func boundedFaultCheckpointReacquire(acquire func(context.Context) (hardwareowner.Unlock, error)) (hardwareowner.Unlock, error) {
	if acquire == nil {
		return nil, ErrFaultUnsupported
	}
	reacquireCtx, cancel := context.WithTimeout(context.Background(), faultCheckpointReacquireTimeout)
	reacquired, err := acquire(reacquireCtx)
	deadlineErr := reacquireCtx.Err()
	cancel()
	if err == nil && deadlineErr != nil {
		err = deadlineErr
	}
	return reacquired, err
}

func (c *filesystemFaultController) diagnosticFor(checkpoint faultDiagnosticForCheckpoint) (faultDiagnostic, error) {
	if c == nil {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	if c.diagnostic != nil {
		return c.diagnostic(checkpoint)
	}
	openExecutable := c.openSelfExecutable
	if openExecutable == nil {
		openExecutable = openRunningExecutable
	}
	file, err := openExecutable()
	if err != nil || file == nil {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	if c.afterExecutableOpen != nil {
		c.afterExecutableOpen()
	}
	stat, statErr := file.Stat()
	hasher := sha256.New()
	_, hashErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if err := errors.Join(statErr, hashErr, closeErr); err != nil {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	device, inode, ok := fileDeviceInode(stat)
	if !ok || device == 0 || inode == 0 {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	startTime := c.selfStartTime
	if startTime == nil {
		startTime = runningProcessStartTime
	}
	start, startErr := startTime()
	if startErr != nil || start == 0 {
		return faultDiagnostic{}, ErrFaultUnsupported
	}
	return faultDiagnostic{RunID: checkpoint.RunID, Session: checkpoint.Session, Generation: checkpoint.Generation, Phase: string(checkpoint.Phase), PID: os.Getpid(), StartTime: start, ExecutableSHA256: hex.EncodeToString(hasher.Sum(nil)), ExecutableDevice: device, ExecutableInode: inode}, nil
}

func faultProcessIdentity(pid int) (faultPeerIdentity, error) {
	if pid <= 0 {
		return faultPeerIdentity{}, ErrFaultUnsupported
	}
	raw, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "stat"))
	if err != nil {
		return faultPeerIdentity{}, err
	}
	start, parsedPID, err := parseProcStatStartTime(raw)
	if err != nil || parsedPID != pid || start == 0 {
		return faultPeerIdentity{}, ErrFaultUnsupported
	}
	file, err := os.Open(filepath.Join("/proc", fmt.Sprint(pid), "exe"))
	if err != nil {
		return faultPeerIdentity{}, err
	}
	stat, statErr := file.Stat()
	hasher := sha256.New()
	_, hashErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if err := errors.Join(statErr, hashErr, closeErr); err != nil {
		return faultPeerIdentity{}, ErrFaultUnsupported
	}
	device, inode, ok := fileDeviceInode(stat)
	if !ok || device == 0 || inode == 0 {
		return faultPeerIdentity{}, ErrFaultUnsupported
	}
	return faultPeerIdentity{PID: pid, StartTime: start, Device: device, Inode: inode, SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

func (c *filesystemFaultController) writeDiagnostic(runID string, diagnostic faultDiagnostic) error {
	path := filepath.Join(c.root, "fpga-dev-"+runID+".diagnostic")
	file, err := openFaultRegular(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, c.expectedUID)
	if err != nil {
		return ErrFaultUnsupported
	}
	raw, marshalErr := json.Marshal(diagnostic)
	if marshalErr == nil {
		_, marshalErr = file.Write(append(raw, '\n'))
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(marshalErr, syncErr, closeErr); err != nil {
		return ErrFaultUnsupported
	}
	if _, err := validateFaultEntry(path, c.expectedUID); err != nil {
		return ErrFaultUnsupported
	}
	return c.syncRoot()
}

func (r *Runner) afterDispatch(ctx context.Context, d runnerDependencies, prepared *preparedRun, current hardwareowner.Record) error {
	checkpoint, ok := d.fault.(faultCheckpoint)
	if !ok || checkpoint == nil {
		return nil
	}
	if prepared == nil || prepared.unlock == nil {
		return ErrFaultUnsupported
	}
	oldUnlock := prepared.unlock
	prepared.unlock = nil
	newUnlock, err := checkpoint.Checkpoint(ctx, faultDiagnosticForCheckpoint{RunID: current.RunID, Session: current.CandidateSession, Generation: current.CandidateGeneration, Phase: current.Phase}, oldUnlock, func(acquireCtx context.Context) (hardwareowner.Unlock, error) {
		if d.locker == nil || d.store == nil {
			return nil, ErrRunnerConfiguration
		}
		lock, lockErr := d.locker.Lock(acquireCtx)
		if lockErr != nil || lock == nil {
			return lock, lockErr
		}
		record, exists, loadErr := d.store.Load()
		if loadErr != nil || !exists || !sameFencedOwner(record, current) {
			return lock, errors.Join(loadErr, ErrFaultUnsupported)
		}
		inspect, inspectOK := checkpoint.(interface {
			Inspect(context.Context, string) (Inspection, error)
		})
		if !inspectOK {
			return lock, ErrFaultUnsupported
		}
		inspection, inspectErr := inspect.Inspect(acquireCtx, current.RunID)
		if inspectErr != nil {
			return lock, inspectErr
		}
		if inspection.RunID != current.RunID || inspection.Session != current.CandidateSession || inspection.Generation != current.CandidateGeneration || inspection.Phase != string(current.Phase) {
			return lock, ErrFaultUnsupported
		}
		return lock, acquireCtx.Err()
	})
	if newUnlock != nil {
		prepared.unlock = newUnlock
	} else if err == nil {
		prepared.unlock = oldUnlock
	} else {
		prepared.lockLost = true
	}
	return err
}

func sameFencedOwner(left, right hardwareowner.Record) bool {
	return left.Schema == right.Schema && left.State == right.State && left.Phase == right.Phase && left.BootID == right.BootID && left.RunID == right.RunID && left.GenerationHighWater == right.GenerationHighWater && left.ActiveSession == right.ActiveSession && left.ActiveGeneration == right.ActiveGeneration && left.ActiveMode == right.ActiveMode && left.CandidateSession == right.CandidateSession && left.CandidateGeneration == right.CandidateGeneration && left.CandidateMode == right.CandidateMode && left.QuiescingOwner == right.QuiescingOwner && left.CandidateOwner == right.CandidateOwner && left.ActiveOwner == right.ActiveOwner && strings.Join(left.ActiveLeases, "\x00") == strings.Join(right.ActiveLeases, "\x00") && strings.Join(left.RequestedResources, "\x00") == strings.Join(right.RequestedResources, "\x00") && left.FirstFailure == right.FirstFailure
}

func (r *Runner) FaultArm(ctx context.Context, runID string) error {
	d, ok := r.dependencies()
	if !ok || d.fault == nil {
		return ErrFaultUnsupported
	}
	controller, ok := d.fault.(faultController)
	if !ok {
		return ErrFaultUnsupported
	}
	return controller.Arm(ctx, runID)
}

func (r *Runner) FaultInspect(ctx context.Context, runID string) (Inspection, error) {
	d, ok := r.dependencies()
	if !ok || d.fault == nil {
		return Inspection{}, ErrFaultUnsupported
	}
	controller, ok := d.fault.(faultController)
	if !ok {
		return Inspection{}, ErrFaultUnsupported
	}
	return controller.Inspect(ctx, runID)
}

func (r *Runner) FaultKill(ctx context.Context, runID string) (retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d, ok := r.dependencies()
	if !ok || d.fault == nil || d.locker == nil || d.store == nil {
		return ErrFaultUnsupported
	}
	unlock, err := d.locker.Lock(ctx)
	if err != nil || unlock == nil {
		var unlockErr error
		if unlock != nil {
			unlockErr = unlock()
		}
		return errors.Join(ErrFaultUnsupported, err, unlockErr)
	}
	defer func() {
		retErr = errors.Join(retErr, unlock())
	}()
	record, exists, err := d.store.Load()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil || !exists || record.RunID != runID || record.State != hardwareowner.StateRecoveringIntent || record.Phase != hardwareowner.PhaseLoadAttempted {
		return ErrFaultUnsupported
	}
	if err := record.Validate(); err != nil {
		return ErrFaultUnsupported
	}
	if d.bootID == nil {
		return ErrFaultUnsupported
	}
	bootID, bootErr := d.bootID()
	if bootErr != nil || bootID != record.BootID {
		return ErrFaultUnsupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	controller, ok := d.fault.(faultController)
	if !ok {
		return ErrFaultUnsupported
	}
	var killErr error
	if bounded, boundedOK := d.fault.(fencedFaultKiller); boundedOK {
		killErr = bounded.KillBound(ctx, runID, record.CandidateSession, record.CandidateGeneration, record.Phase)
	} else {
		killErr = controller.Kill(ctx, runID)
	}
	return killErr
}

func (r *Runner) RecoveryReboot(ctx context.Context, runID string) (retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRunID(runID); err != nil {
		return err
	}
	d, ok := r.dependencies()
	if !ok || d.maintenance == nil || d.store == nil || d.locker == nil || d.reboot == nil {
		return ErrFaultUnsupported
	}
	maintenanceStatus, maintenanceUnlock, enterErr := d.maintenance.Enter(ctx)
	if enterErr != nil || maintenanceUnlock == nil {
		var maintenanceCleanupErr error
		if maintenanceUnlock != nil {
			maintenanceCleanupErr = maintenanceUnlock.Unlock()
		}
		return errors.Join(ErrFaultUnsupported, enterErr, maintenanceCleanupErr)
	}
	if statusErr := maintenanceStatus.Validate(); statusErr != nil {
		return errors.Join(ErrFaultUnsupported, statusErr, maintenanceUnlock.Unlock())
	}
	unlock, err := d.locker.Lock(ctx)
	if err != nil || unlock == nil {
		var ownerCleanupErr error
		if unlock != nil {
			ownerCleanupErr = unlock()
		}
		return errors.Join(ErrFaultUnsupported, err, ownerCleanupErr, maintenanceUnlock.Unlock())
	}
	defer func() {
		// The durable owner lock is nested inside maintenance. Always release
		// it first, including validation, reboot, and cancellation failures.
		retErr = errors.Join(retErr, unlock(), maintenanceUnlock.Unlock())
	}()
	record, exists, err := d.store.Load()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil || !exists || record.RunID != runID || !validRecoveryRebootRecord(record) {
		return ErrFaultUnsupported
	}
	if err := record.Validate(); err != nil {
		return ErrFaultUnsupported
	}
	if d.bootID == nil {
		return ErrFaultUnsupported
	}
	bootID, bootErr := d.bootID()
	if bootErr != nil || bootID != record.BootID {
		return ErrFaultUnsupported
	}
	rebootCtx, cancel := context.WithTimeout(ctx, rebootTimeout)
	defer cancel()
	if err := rebootCtx.Err(); err != nil {
		return err
	}
	if err := d.reboot.Request(rebootCtx); err != nil {
		return err
	}
	return rebootCtx.Err()
}

func validRecoveryRebootRecord(record hardwareowner.Record) bool {
	switch record.State {
	case hardwareowner.StateRecoveringIntent:
		return record.Phase == hardwareowner.PhaseIntentCommitted || record.Phase == hardwareowner.PhaseLoadAttempted
	case hardwareowner.StateNoOwner:
		return record.Phase == hardwareowner.PhaseMainAbsent
	case hardwareowner.StateFPGADefaultActive:
		switch record.Phase {
		case hardwareowner.PhaseLeaseActive, hardwareowner.PhaseHelloObserved, hardwareowner.PhaseMessagePartial, hardwareowner.PhaseEndAckWritten, hardwareowner.PhaseDoneObserved:
			return true
		default:
			return false
		}
	case hardwareowner.StateRecoveryRequired:
		return true
	default:
		return false
	}
}

func validateRunID(runID string) error {
	if !manifestRunIDPattern.MatchString(runID) {
		return errors.New("run ID is invalid")
	}
	return nil
}
