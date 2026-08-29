//go:build fpgadev && linux

package fpgadev

// This file contains the Linux half of the boot-local inventory resolver.
// Every configured path is bound through a descriptor opened beneath a
// trusted root.  The descriptor, rather than a pathname, is the object that
// is checked and hashed.  Process executable and socket endpoint joins are a
// later layer; this tranche deliberately owns only the descriptor and
// lifecycle/holder rules.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type inventoryDescriptorMeta struct {
	Mode  uint32
	UID   uint32
	Nlink uint64
	Dev   uint64
	Ino   uint64
	Rdev  uint64
	Size  int64
}

type inventoryDescriptor struct {
	fd   int
	meta inventoryDescriptorMeta
}

func resolveInventoryPlatform(ctx context.Context, inv InventoryV1, options InventoryResolutionOptions) (ResolvedInventoryV1, error) {
	if err := validateInventoryResolutionOptions(options); err != nil {
		return ResolvedInventoryV1{}, err
	}
	main, err := resolveInventoryPathPlatform(ctx, inv.MainExecutable, true, options)
	if err != nil {
		return ResolvedInventoryV1{}, fmt.Errorf("main executable: %w", err)
	}
	fifo, err := resolveInventoryPathPlatform(ctx, inv.MainFIFO, true, options)
	if err != nil {
		return ResolvedInventoryV1{}, fmt.Errorf("main FIFO: %w", err)
	}
	if options.RequireMainFIFOOwner {
		if err := validateMainFIFOPlatform(fifo); err != nil {
			return ResolvedInventoryV1{}, err
		}
	}
	result := ResolvedInventoryV1{
		Schema:         1,
		MainExecutable: main,
		MainFIFO:       fifo,
		StartSources:   make([]ResolvedSourceV1, 0, len(inv.StartSources)),
	}
	for _, target := range []struct {
		want *PathExpectation
		set  func(*ResolvedPathV1)
	}{
		{inv.CastExecutable, func(p *ResolvedPathV1) { result.CastExecutable = p }},
		{inv.InputUInput, func(p *ResolvedPathV1) { result.InputUInput = p }},
		{inv.CastFramebuffer, func(p *ResolvedPathV1) { result.CastFramebuffer = p }},
		{inv.CastNativeCommand, func(p *ResolvedPathV1) { result.CastNativeCommand = p }},
		{inv.CastTokenFile, func(p *ResolvedPathV1) { result.CastTokenFile = p }},
	} {
		if target.want == nil {
			continue
		}
		resolved, resolveErr := resolveInventoryPathPlatform(ctx, *target.want, false, options)
		if resolveErr != nil {
			return ResolvedInventoryV1{}, resolveErr
		}
		target.set(&resolved)
	}
	result.InputListen = inv.InputListen
	result.CastRTP = inv.CastRTP
	result.CastControl = inv.CastControl
	for _, source := range inv.StartSources {
		resolved, sourceErr := resolveInventorySourcePlatform(ctx, source, options)
		if sourceErr != nil {
			return ResolvedInventoryV1{}, sourceErr
		}
		result.StartSources = append(result.StartSources, resolved)
	}
	mainPIDs := map[int]struct{}(nil)
	if options.RequireProcessEvidence {
		mainPIDs, err = scanInventoryMainProcesses(ctx, options, result.MainExecutable)
		if err != nil {
			return ResolvedInventoryV1{}, err
		}
	}
	if options.ScanProcessDescriptors {
		if err := scanInventoryDescriptorHolders(ctx, options, inv, result, mainPIDs); err != nil {
			return ResolvedInventoryV1{}, err
		}
	}
	if options.RequireNetworkEvidence {
		if err := verifyInventoryNetworkEvidence(ctx, options, inv); err != nil {
			return ResolvedInventoryV1{}, err
		}
	}
	return result, nil
}

func validateInventoryResolutionOptions(options InventoryResolutionOptions) error {
	switch options.Phase {
	case InventoryPhasePreDispatch, InventoryPhasePostMain, InventoryPhaseMainAbsent:
	default:
		return fmt.Errorf("unknown inventory resolution phase %q", options.Phase)
	}
	if options.ProcRoot == "" || !filepath.IsAbs(options.ProcRoot) || filepath.Clean(options.ProcRoot) != options.ProcRoot {
		return errors.New("inventory proc root must be absolute and canonical")
	}
	if options.MainProcess != nil {
		if options.MainProcess.PID <= 0 || options.MainProcess.StartTime == 0 || options.MainProcess.Device == 0 || options.MainProcess.Inode == 0 {
			return errors.New("inventory Main process identity is invalid")
		}
	}
	return nil
}

func resolveInventoryPathPlatform(ctx context.Context, want PathExpectation, required bool, options InventoryResolutionOptions) (ResolvedPathV1, error) {
	if err := want.Validate(required); err != nil {
		return ResolvedPathV1{}, err
	}
	descriptor, err := openInventoryDescriptor(ctx, want.Path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return ResolvedPathV1{Path: want.Path, Kind: want.Kind, State: "expected_absent"}, nil
	}
	if err != nil {
		return ResolvedPathV1{}, fmt.Errorf("bind inventory path: %w", err)
	}
	defer func() { _ = unix.Close(descriptor.fd) }()
	kind := inventoryKindFromUnixMode(descriptor.meta.Mode)
	if kind != want.Kind {
		return ResolvedPathV1{}, fmt.Errorf("resolved inventory path kind %q does not match %q", kind, want.Kind)
	}
	if (want.Kind == "character" || want.Kind == "block") && descriptor.meta.Rdev != want.Rdev {
		return ResolvedPathV1{}, errors.New("resolved inventory device identity does not match")
	}
	if options.RequireMainFIFOOwner && required && want.Kind == "fifo" {
		if descriptor.meta.UID != 0 || descriptor.meta.Mode&0o7777 != 0o600 {
			return ResolvedPathV1{}, errors.New("Main FIFO must be root-owned mode 0600")
		}
	}
	digest := ""
	if want.SHA256 != "" {
		if kind != "regular" {
			return ResolvedPathV1{}, errors.New("hashed inventory path is not regular")
		}
		digest, err = hashInventoryDescriptor(ctx, descriptor)
		if err != nil {
			return ResolvedPathV1{}, err
		}
		if digest != want.SHA256 {
			return ResolvedPathV1{}, errors.New("resolved inventory file hash does not match")
		}
	}
	if err := ctx.Err(); err != nil {
		return ResolvedPathV1{}, err
	}
	return ResolvedPathV1{
		Path:   want.Path,
		Kind:   kind,
		State:  "present",
		Device: descriptor.meta.Dev,
		Inode:  descriptor.meta.Ino,
		Rdev:   descriptor.meta.Rdev,
		SHA256: digest,
	}, nil
}

func resolveInventorySourcePlatform(ctx context.Context, source SourceRecord, options InventoryResolutionOptions) (ResolvedSourceV1, error) {
	if err := source.Validate(); err != nil {
		return ResolvedSourceV1{}, err
	}
	descriptor, err := openInventoryDescriptor(ctx, source.Path)
	if errors.Is(err, os.ErrNotExist) {
		if source.DisabledState != "absent" {
			return ResolvedSourceV1{}, errors.New("required disabled source is absent")
		}
		return ResolvedSourceV1{Path: source.Path, Kind: source.Kind, State: "disabled_absent"}, nil
	}
	if err != nil {
		return ResolvedSourceV1{}, fmt.Errorf("bind disabled source: %w", err)
	}
	defer func() { _ = unix.Close(descriptor.fd) }()
	if source.DisabledState == "absent" {
		return ResolvedSourceV1{}, errors.New("disabled-absent source unexpectedly exists")
	}
	if got := inventoryKindFromUnixMode(descriptor.meta.Mode); got != source.Kind {
		return ResolvedSourceV1{}, errors.New("disabled source kind does not match journal")
	}
	if descriptor.meta.Mode&0o7777 != source.Mode {
		return ResolvedSourceV1{}, errors.New("disabled source mode does not match journal")
	}
	if source.Kind != "regular" {
		return ResolvedSourceV1{}, errors.New("disabled replacement is not regular")
	}
	data, digest, err := readAndHashInventoryDescriptor(ctx, descriptor)
	if err != nil {
		return ResolvedSourceV1{}, err
	}
	if digest != source.DisabledSHA256 {
		return ResolvedSourceV1{}, errors.New("disabled source hash does not match journal")
	}
	if source.DisabledState == "approved_trampoline" && !equalFixedRecoveryTrampoline(data) {
		return ResolvedSourceV1{}, errors.New("approved trampoline bytes are not package-attested")
	}
	state := "disabled_inert"
	if source.DisabledState == "approved_trampoline" {
		state = "approved_trampoline"
	}
	return ResolvedSourceV1{Path: source.Path, Kind: source.Kind, State: state, Device: descriptor.meta.Dev, Inode: descriptor.meta.Ino, SHA256: digest}, nil
}

func equalFixedRecoveryTrampoline(data []byte) bool {
	want := BuildApprovedRecoveryTrampoline()
	if len(data) != len(want) {
		return false
	}
	for index := range want {
		if data[index] != want[index] {
			return false
		}
	}
	return true
}

func validateMainFIFOPlatform(path ResolvedPathV1) error {
	if path.State != "present" || path.Kind != "fifo" {
		return errors.New("Main FIFO is not present")
	}
	// Main's compatibility FIFO is deliberately a root-owned 0600 object. The
	// immutable schema does not persist these volatile metadata checks, so they
	// are enforced at every boot observation.
	return nil
}

func openInventoryDescriptor(ctx context.Context, path string) (inventoryDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return inventoryDescriptor{}, err
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return inventoryDescriptor{}, errors.New("inventory path must be absolute and canonical")
	}
	parent, leaf, err := splitInventoryPath(path)
	if err != nil {
		return inventoryDescriptor{}, err
	}
	rootFD, err := openTrustedRoot()
	if err != nil {
		return inventoryDescriptor{}, err
	}
	if rootFD < 0 {
		return inventoryDescriptor{}, errors.New("trusted inventory root returned an invalid descriptor")
	}
	defer func() { _ = unix.Close(rootFD) }()
	parentFD, err := openTrustedParent(rootFD, parent)
	if err != nil {
		return inventoryDescriptor{}, err
	}
	if parentFD < 0 {
		return inventoryDescriptor{}, errors.New("trusted inventory parent returned an invalid descriptor")
	}
	defer func() { _ = unix.Close(parentFD) }()
	fd, err := bindFIFOFinal(parentFD, leaf)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return inventoryDescriptor{}, os.ErrNotExist
		}
		return inventoryDescriptor{}, err
	}
	if fd < 0 {
		return inventoryDescriptor{}, errors.New("trusted inventory final returned an invalid descriptor")
	}
	meta, err := statInventoryDescriptor(fd)
	if err != nil {
		_ = unix.Close(fd)
		return inventoryDescriptor{}, err
	}
	if meta.Dev == 0 || meta.Ino == 0 {
		_ = unix.Close(fd)
		return inventoryDescriptor{}, errors.New("inventory descriptor identity is invalid")
	}
	if err := ctx.Err(); err != nil {
		_ = unix.Close(fd)
		return inventoryDescriptor{}, err
	}
	return inventoryDescriptor{fd: fd, meta: meta}, nil
}

func splitInventoryPath(path string) (string, string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return "", "", errors.New("inventory path must be absolute and canonical")
	}
	relative := strings.TrimPrefix(path, string(filepath.Separator))
	leaf := filepath.Base(relative)
	parent := filepath.Dir(relative)
	if parent == "" {
		parent = "."
	}
	if leaf == "" || leaf == "." || leaf == string(filepath.Separator) || strings.Contains(leaf, string(filepath.Separator)) {
		return "", "", errors.New("inventory path has no valid final component")
	}
	return parent, leaf, nil
}

func statInventoryDescriptor(fd int) (inventoryDescriptorMeta, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return inventoryDescriptorMeta{}, err
	}
	return inventoryDescriptorMeta{
		Mode:  stat.Mode,
		UID:   stat.Uid,
		Nlink: uint64(stat.Nlink),
		Dev:   uint64(stat.Dev),
		Ino:   uint64(stat.Ino),
		Rdev:  uint64(stat.Rdev),
		Size:  stat.Size,
	}, nil
}

func inventoryKindFromUnixMode(mode uint32) string {
	switch mode & unix.S_IFMT {
	case unix.S_IFREG:
		return "regular"
	case unix.S_IFIFO:
		return "fifo"
	case unix.S_IFCHR:
		return "character"
	case unix.S_IFBLK:
		return "block"
	case unix.S_IFSOCK:
		return "socket"
	case unix.S_IFDIR:
		return "directory"
	default:
		return ""
	}
}

func hashInventoryDescriptor(ctx context.Context, descriptor inventoryDescriptor) (string, error) {
	_, digest, err := readAndHashInventoryDescriptor(ctx, descriptor)
	return digest, err
}

func readAndHashInventoryDescriptor(ctx context.Context, descriptor inventoryDescriptor) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if descriptor.meta.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, "", errors.New("inventory hash descriptor is not regular")
	}
	procFD := filepath.Join("/proc/self/fd", strconv.Itoa(descriptor.fd))
	readFD, err := unix.Open(procFD, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open held inventory descriptor: %w", err)
	}
	readFile := os.NewFile(uintptr(readFD), procFD)
	if readFile == nil {
		_ = unix.Close(readFD)
		return nil, "", errors.New("open held inventory descriptor returned no file")
	}
	defer func() { _ = readFile.Close() }()
	readMeta, err := statInventoryDescriptor(readFD)
	if err != nil {
		return nil, "", err
	}
	if readMeta.Dev != descriptor.meta.Dev || readMeta.Ino != descriptor.meta.Ino || readMeta.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, "", errors.New("held inventory descriptor identity changed before hash")
	}
	if readMeta.Size < 0 || readMeta.Size > ProtectedRegularMaxBytes {
		return nil, "", errors.New("inventory regular file exceeds hash bound")
	}
	data := make([]byte, 0, readMeta.Size)
	reader := io.LimitReader(readFile, ProtectedRegularMaxBytes+1)
	var readErr error
	data, readErr = io.ReadAll(reader)
	if readErr != nil {
		return nil, "", readErr
	}
	if len(data) > ProtectedRegularMaxBytes {
		return nil, "", errors.New("inventory regular file exceeds hash bound")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	finalMeta, err := statInventoryDescriptor(descriptor.fd)
	if err != nil {
		return nil, "", err
	}
	if finalMeta.Dev != descriptor.meta.Dev || finalMeta.Ino != descriptor.meta.Ino || finalMeta.Size != descriptor.meta.Size || finalMeta.Mode != descriptor.meta.Mode {
		return nil, "", errors.New("inventory descriptor changed while hashed")
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), nil
}

type inventoryHolderRule struct {
	Path         string
	MainFIFOOnly bool
}

type inventoryObjectKey struct {
	Device uint64
	Inode  uint64
}

func scanInventoryDescriptorHolders(ctx context.Context, options InventoryResolutionOptions, inv InventoryV1, resolved ResolvedInventoryV1, mainPIDs map[int]struct{}) error {
	rulesByPath := make(map[string]inventoryHolderRule)
	objects := make(map[inventoryObjectKey]inventoryHolderRule)
	add := func(path string, value *ResolvedPathV1, mainFIFO bool) {
		if path == "" {
			return
		}
		rule := inventoryHolderRule{Path: path, MainFIFOOnly: mainFIFO}
		rulesByPath[path] = mergeInventoryHolderRule(rulesByPath[path], rule)
		if value != nil && value.State == "present" {
			key := inventoryObjectKey{Device: value.Device, Inode: value.Inode}
			objects[key] = mergeInventoryHolderRule(objects[key], rule)
		}
	}
	add(inv.MainExecutable.Path, &resolved.MainExecutable, false)
	add(inv.MainFIFO.Path, &resolved.MainFIFO, true)
	addOptionalInventoryHolder(rulesByPath, objects, inv.CastExecutable, resolved.CastExecutable)
	addOptionalInventoryHolder(rulesByPath, objects, inv.InputUInput, resolved.InputUInput)
	addOptionalInventoryHolder(rulesByPath, objects, inv.CastFramebuffer, resolved.CastFramebuffer)
	addOptionalInventoryHolder(rulesByPath, objects, inv.CastNativeCommand, resolved.CastNativeCommand)
	addOptionalInventoryHolder(rulesByPath, objects, inv.CastTokenFile, resolved.CastTokenFile)
	for index, source := range inv.StartSources {
		if index >= len(resolved.StartSources) {
			return errors.New("resolved source population is incomplete")
		}
		value := resolved.StartSources[index]
		rule := inventoryHolderRule{Path: source.Path, MainFIFOOnly: false}
		rulesByPath[source.Path] = mergeInventoryHolderRule(rulesByPath[source.Path], rule)
		if value.State != "disabled_absent" {
			objects[inventoryObjectKey{Device: value.Device, Inode: value.Inode}] = mergeInventoryHolderRule(objects[inventoryObjectKey{Device: value.Device, Inode: value.Inode}], rule)
		}
	}
	return scanInventoryProcFDs(ctx, options, rulesByPath, objects, mainPIDs)
}

func addOptionalInventoryHolder(rules map[string]inventoryHolderRule, objects map[inventoryObjectKey]inventoryHolderRule, want *PathExpectation, got *ResolvedPathV1) {
	if want == nil {
		return
	}
	rule := inventoryHolderRule{Path: want.Path}
	rules[want.Path] = mergeInventoryHolderRule(rules[want.Path], rule)
	if got != nil && got.State == "present" {
		key := inventoryObjectKey{Device: got.Device, Inode: got.Inode}
		objects[key] = mergeInventoryHolderRule(objects[key], rule)
	}
}

func mergeInventoryHolderRule(left, right inventoryHolderRule) inventoryHolderRule {
	if left.Path == "" {
		return right
	}
	// MainFIFOOnly is an allowance, not an additional privilege. Any alias
	// gives the shared object a stricter non-FIFO rule.
	left.MainFIFOOnly = left.MainFIFOOnly && right.MainFIFOOnly
	return left
}

func scanInventoryProcFDs(ctx context.Context, options InventoryResolutionOptions, rules map[string]inventoryHolderRule, objects map[inventoryObjectKey]inventoryHolderRule, mainPIDs map[int]struct{}) error {
	entries, err := os.ReadDir(options.ProcRoot)
	if err != nil {
		return fmt.Errorf("scan inventory proc root: %w", err)
	}
	for _, process := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		pid, ok := parseInventoryPID(process.Name())
		if !ok {
			continue
		}
		fdDir := filepath.Join(options.ProcRoot, process.Name(), "fd")
		fds, readErr := os.ReadDir(fdDir)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return fmt.Errorf("scan inventory process descriptors: %w", readErr)
		}
		for _, fdEntry := range fds {
			if err := ctx.Err(); err != nil {
				return err
			}
			fdPath := filepath.Join(fdDir, fdEntry.Name())
			target, linkErr := os.Readlink(fdPath)
			if errors.Is(linkErr, os.ErrNotExist) {
				continue
			}
			if linkErr != nil {
				return fmt.Errorf("read inventory process descriptor: %w", linkErr)
			}
			normalized := normalizeInventoryDescriptorTarget(target)
			rule, pathMatch := rules[normalized]
			meta, metaErr := openInventoryProcFDMeta(fdPath)
			if errors.Is(metaErr, os.ErrNotExist) {
				if pathMatch {
					return fmt.Errorf("deleted inventoried descriptor holder is not allowed for %s", rule.Path)
				}
				continue
			}
			if metaErr != nil {
				return fmt.Errorf("stat inventory process descriptor: %w", metaErr)
			}
			objectRule, objectMatch := objects[inventoryObjectKey{Device: meta.Dev, Inode: meta.Ino}]
			if !pathMatch && !objectMatch {
				continue
			}
			if pathMatch && objectMatch {
				rule = mergeInventoryHolderRule(rule, objectRule)
			} else if !pathMatch {
				rule = objectRule
			}
			if inventoryHolderAllowed(options, pid, rule, mainPIDs) {
				continue
			}
			return fmt.Errorf("inventoried descriptor holder is not allowed for %s", rule.Path)
		}
	}
	return nil
}

func parseInventoryPID(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	pid, err := strconv.Atoi(value)
	return pid, err == nil && pid > 0
}

func normalizeInventoryDescriptorTarget(target string) string {
	return strings.TrimSuffix(target, " (deleted)")
}

func openInventoryProcFDMeta(path string) (inventoryDescriptorMeta, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH) {
			return inventoryDescriptorMeta{}, os.ErrNotExist
		}
		return inventoryDescriptorMeta{}, err
	}
	defer func() { _ = unix.Close(fd) }()
	return statInventoryDescriptor(fd)
}

func inventoryHolderAllowed(options InventoryResolutionOptions, pid int, rule inventoryHolderRule, mainPIDs map[int]struct{}) bool {
	if options.Phase != InventoryPhasePreDispatch || !rule.MainFIFOOnly {
		return false
	}
	if options.MainProcess != nil {
		return options.MainProcess.PID == pid
	}
	_, ok := mainPIDs[pid]
	return ok
}

func scanInventoryMainProcesses(ctx context.Context, options InventoryResolutionOptions, main ResolvedPathV1) (map[int]struct{}, error) {
	if main.State != "present" || main.Kind != "regular" || main.SHA256 == "" {
		return nil, errors.New("Main executable lacks a process identity binding")
	}
	expected := ExecutableIdentity{Device: main.Device, Inode: main.Inode, SHA256: main.SHA256}
	if !expected.valid() {
		return nil, errors.New("Main executable identity is invalid")
	}
	if options.MainProcess != nil && !options.MainProcess.executable().valid() {
		return nil, errors.New("inventory Main process executable identity is invalid")
	}
	if makeProcessScanner == nil {
		return nil, ErrProcessScannerUnsupported
	}
	scanner := makeProcessScanner(options.ProcRoot)
	if scanner == nil {
		return nil, ErrProcessScannerUnsupported
	}
	records, err := scanner.Scan(ctx, expected)
	if err != nil {
		return nil, err
	}
	mainPIDs := make(map[int]struct{})
	var exact *ProcessIdentity
	for _, record := range records {
		identity := record.Identity
		if !identity.executable().equal(expected) {
			continue
		}
		mainPIDs[identity.PID] = struct{}{}
		if options.MainProcess != nil && options.MainProcess.equal(identity) {
			copy := identity
			exact = &copy
		}
	}
	if options.MainProcess != nil {
		if exact == nil {
			return nil, errors.New("attested Main process identity is absent or reused")
		}
	} else if len(mainPIDs) == 0 {
		return nil, errors.New("attested Main executable process is absent")
	}
	return mainPIDs, nil
}

func readInventoryProcessIdentity(ctx context.Context, root string, pid int) (ProcessIdentity, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, err
	}
	if root == "" {
		root = "/proc"
	}
	processDir := filepath.Join(root, strconv.Itoa(pid))
	statPath := filepath.Join(processDir, "stat")
	before, err := readLinuxProcStat(ctx, statPath, pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	file, err := openLinuxExecutable(ctx, filepath.Join(processDir, "exe"))
	if err != nil {
		return ProcessIdentity{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	sample, err := sampleExecutableFile(file)
	if err != nil {
		return ProcessIdentity{}, err
	}
	device, inode, digest, err := hashLinuxExecutableFileContext(ctx, file, ExecutableIdentity{})
	if err != nil {
		return ProcessIdentity{}, err
	}
	after, err := readLinuxProcStat(ctx, statPath, pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	if before.PID != after.PID || before.StartTime != after.StartTime || sample.Device != device || sample.Inode != inode {
		return ProcessIdentity{}, ErrProcessIdentityChanged
	}
	if err := file.Close(); err != nil {
		closed = true
		return ProcessIdentity{}, err
	}
	closed = true
	return ProcessIdentity{PID: pid, StartTime: after.StartTime, Device: device, Inode: inode, SHA256: digest}, nil
}

func productionLiveProofCheck(ctx context.Context, proof BootProof) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, expected := range []ProcessIdentity{
		{PID: int(proof.SupervisorPID), StartTime: proof.SupervisorStartTime, Device: proof.SupervisorExecutableDevice, Inode: proof.SupervisorExecutableInode, SHA256: proof.SupervisorExecutableSHA256},
		{PID: int(proof.MainPID), StartTime: proof.MainStartTime, Device: proof.MainExecutableDevice, Inode: proof.MainExecutableInode, SHA256: proof.MainExecutableSHA256},
		{PID: int(proof.AgentPID), StartTime: proof.AgentStartTime, Device: proof.AgentExecutableDevice, Inode: proof.AgentExecutableInode, SHA256: proof.AgentExecutableSHA256},
	} {
		if expected.PID <= 0 {
			return errors.New("boot proof process PID is invalid")
		}
		actual, err := readInventoryProcessIdentity(ctx, "/proc", expected.PID)
		if err != nil {
			return err
		}
		if !actual.equal(expected) {
			return errors.New("boot proof live process identity does not match")
		}
	}
	return nil
}

type inventorySocketRow struct {
	Inode uint64
	IP    net.IP
	Port  uint16
}

func verifyInventoryNetworkEvidence(ctx context.Context, options InventoryResolutionOptions, inv InventoryV1) error {
	for _, endpoint := range []*NetworkExpectation{inv.InputListen, inv.CastRTP, inv.CastControl} {
		if endpoint == nil {
			continue
		}
		if err := verifyInventoryNetworkEndpoint(ctx, options.ProcRoot, *endpoint, options.RequireNetworkAbsence); err != nil {
			return err
		}
	}
	return nil
}

func verifyInventoryNetworkEndpoint(ctx context.Context, procRoot string, endpoint NetworkExpectation, expectedAbsent bool) error {
	files, err := inventoryNetworkFiles(endpoint.Network)
	if err != nil {
		return err
	}
	wantIP, wantPort, err := parseInventoryNetworkAddress(endpoint.Address)
	if err != nil {
		return err
	}
	rows := make([]inventorySocketRow, 0)
	for _, file := range files {
		part, readErr := readInventoryProcNetFile(ctx, filepath.Join(procRoot, "net", file))
		if readErr != nil {
			return readErr
		}
		parsed, parseErr := parseInventoryProcNetRows(part)
		if parseErr != nil {
			return fmt.Errorf("parse /proc/net/%s: %w", file, parseErr)
		}
		rows = append(rows, parsed...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fdInodes, err := scanInventorySocketFDs(ctx, procRoot)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Port == wantPort && row.IP.Equal(wantIP) {
			if expectedAbsent {
				return fmt.Errorf("configured %s endpoint is unexpectedly present", endpoint.Address)
			}
			if _, ok := fdInodes[row.Inode]; ok {
				return nil
			}
		}
	}
	if expectedAbsent {
		return nil
	}
	return fmt.Errorf("configured %s endpoint is absent from proc socket join", endpoint.Address)
}

func inventoryNetworkFiles(network string) ([]string, error) {
	switch network {
	case "tcp", "tcp4":
		return []string{"tcp"}, nil
	case "tcp6":
		return []string{"tcp6"}, nil
	case "udp", "udp4":
		return []string{"udp"}, nil
	case "udp6":
		return []string{"udp6"}, nil
	default:
		return nil, fmt.Errorf("unsupported network expectation %q", network)
	}
}

func parseInventoryNetworkAddress(address string) (net.IP, uint16, error) {
	host, portValue, err := net.SplitHostPort(address)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid network address: %w", err)
	}
	port, err := strconv.ParseUint(portValue, 10, 16)
	if err != nil || port == 0 {
		return nil, 0, errors.New("network address port is invalid")
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return nil, 0, errors.New("network address host is invalid")
	}
	return ip, uint16(port), nil
}

func readInventoryProcNetFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, InstallJournalMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > InstallJournalMaxBytes {
		return nil, errors.New("proc network table exceeds bound")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func parseInventoryProcNetRows(raw []byte) ([]inventorySocketRow, error) {
	rows := make([]inventorySocketRow, 0)
	for lineNumber, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "sl" {
			continue
		}
		if len(fields) <= 9 {
			return nil, fmt.Errorf("line %d is incomplete", lineNumber+1)
		}
		ip, port, err := parseInventoryProcEndpoint(fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d endpoint: %w", lineNumber+1, err)
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d inode is invalid", lineNumber+1)
		}
		rows = append(rows, inventorySocketRow{Inode: inode, IP: ip, Port: port})
	}
	return rows, nil
}

func parseInventoryProcEndpoint(value string) (net.IP, uint16, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return nil, 0, errors.New("proc endpoint is malformed")
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return nil, 0, err
	}
	ipBytes, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, 0, err
	}
	switch len(ipBytes) {
	case net.IPv4len:
		return net.IPv4(ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0]), uint16(port), nil
	case net.IPv6len:
		// Linux renders tcp6 addresses as four little-endian 32-bit words.
		for start := 0; start < len(ipBytes); start += 4 {
			ipBytes[start], ipBytes[start+1], ipBytes[start+2], ipBytes[start+3] = ipBytes[start+3], ipBytes[start+2], ipBytes[start+1], ipBytes[start]
		}
		return net.IP(ipBytes), uint16(port), nil
	default:
		return nil, 0, errors.New("proc endpoint address has invalid width")
	}
}

func scanInventorySocketFDs(ctx context.Context, procRoot string) (map[uint64]struct{}, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	inodes := make(map[uint64]struct{})
	for _, process := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := parseInventoryPID(process.Name()); !ok {
			continue
		}
		fdEntries, err := os.ReadDir(filepath.Join(procRoot, process.Name(), "fd"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, fdEntry := range fdEntries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			target, err := os.Readlink(filepath.Join(procRoot, process.Name(), "fd", fdEntry.Name()))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if inode, ok := parseInventorySocketTarget(target); ok {
				inodes[inode] = struct{}{}
			}
		}
	}
	return inodes, nil
}

func parseInventorySocketTarget(target string) (uint64, bool) {
	if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
		return 0, false
	}
	inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]"), 10, 64)
	return inode, err == nil && inode != 0
}
