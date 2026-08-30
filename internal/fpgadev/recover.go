//go:build fpgadev

package fpgadev

// Recovery and installation are intentionally kept in this package instead
// of the command binaries.  The command layer only selects a mode and passes
// validated paths; the manager owns lock ordering, journal checkpoints, and
// the fail-closed source mutation protocol.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
	"golang.org/x/sys/unix"
)

var (
	ErrInstallJournalAbsent = errors.New("install journal is absent")
	ErrInstallNotTerminal   = errors.New("install journal is not terminal")
	ErrInstallOwnerInvalid  = errors.New("owner is not canonical normal_main")
	ErrRebootRequested      = errors.New("successor boot is required")
)

const (
	approvedRecoveryTrampoline  = "#!/bin/sh\nset -eu\nexec /usr/bin/mister-fpga-dev recover-install\n"
	approvedInertSource         = "#!/bin/sh\nset -eu\nexit 0\n"
	defaultSupervisorExecutable = "/usr/bin/fogcast-dev-supervisor"
	defaultInstallStageRoot     = "/var/lib/fogcast/fpgadev-staging"
	InstallStageRoot            = defaultInstallStageRoot
)

// SourceMutationPlan is the concrete, descriptor-bound source transition
// used by production installation. Sources are captured before the prepared
// journal is committed; every later operation is idempotent and verifies the
// recorded bytes/metadata before it advances the journal.
type SourceMutationPlan struct {
	Sources         []SourceRecord
	BackupDir       string
	Dispatcher      string
	Supervisor      string
	SupervisorBytes []byte
	InertBytes      []byte
	// TrampolineBytes is filled by InstallManager after the persistent staged
	// recovery helper has been verified. A nil value retains the historical
	// static fixture payload for callers that exercise SourceMutationPlan on
	// its own.
	TrampolineBytes []byte
	MutationHook    func(string) error
	ExpectedUID     uint32
}

// NewSourceMutationPlan constructs the protected source adapter. A nil
// SupervisorBytes selects the fixed supervisor launcher; a nil InertBytes
// selects the fixed inert replacement. The manager copies Sources into the
// immutable journal after Prepare has captured all backup hashes.
func NewSourceMutationPlan(sources []SourceRecord, backupDir string) *SourceMutationPlan {
	return &SourceMutationPlan{
		Sources: append([]SourceRecord(nil), sources...), BackupDir: backupDir,
		SupervisorBytes: []byte("#!/bin/sh\nset -eu\nexec /usr/bin/fogcast-dev-supervisor --inherited-fd 3\n"),
		InertBytes:      []byte(approvedInertSource), ExpectedUID: uint32(os.Getuid()),
	}
}

// BuildApprovedRecoveryTrampoline returns the exact fixed dispatcher payload.
// The command is deliberately argument-free: recover-install opens and holds
// the protected lock itself, then hands that descriptor to the supervisor.
func BuildApprovedRecoveryTrampoline() []byte { return []byte(approvedRecoveryTrampoline) }

// BuildRecoveryTrampoline returns the boot-first dispatcher payload bound to
// one persistent staged helper. The helper path and digest are embedded in the
// script so a boot cannot accidentally execute a not-yet-installed fixed-path
// binary. The script intentionally starts no Main or agent itself.
func BuildRecoveryTrampoline(stagePath, helperSHA256, diagnosticPath string) []byte {
	if stagePath == "" || helperSHA256 == "" {
		return BuildApprovedRecoveryTrampoline()
	}
	helper := filepath.Join(stagePath, "bin", "mister-fpga-dev")
	if diagnosticPath == "" {
		diagnosticPath = filepath.Join(filepath.Dir(stagePath), "recovery.diagnostic")
	}
	return []byte("#!/bin/sh\nset -eu\n" +
		"helper=" + shellQuote(helper) + "\n" +
		"diagnostic=" + shellQuote(diagnosticPath) + "\n" +
		"expected=" + shellQuote(helperSHA256) + "\n" +
		"if [ ! -f \"$helper\" ] || [ \"$(sha256sum \"$helper\" | awk '{print $1}')\" != \"$expected\" ]; then\n" +
		"  umask 077\n" +
		"  printf '%s\\n' 'recovery helper missing or mismatched; target remains fenced' >\"$diagnostic\"\n" +
		"  exit 1\n" +
		"fi\n" +
		"exec \"$helper\" recover-install\n")
}

func shellQuote(value string) string {
	// All generated paths are absolute canonical paths, but shell-quoting the
	// complete value keeps this helper safe if a fixture deliberately contains
	// punctuation. A single quote is represented by the standard close/escape/
	// reopen sequence.
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// CaptureStartSourceInventory captures protected regular launch sources and
// assigns exactly one approved dispatcher. It is intended for the installer,
// not for boot-time path discovery; all returned identities are stable source
// bytes/mode only and contain no reboot-volatile inode values.
func CaptureStartSourceInventory(paths []string, backupDir, dispatcher string) ([]SourceRecord, error) {
	if len(paths) == 0 || backupDir == "" || !filepath.IsAbs(backupDir) || filepath.Clean(backupDir) != backupDir {
		return nil, errors.New("source inventory inputs are invalid")
	}
	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)
	seen := make(map[string]struct{}, len(ordered))
	result := make([]SourceRecord, 0, len(ordered))
	for index, path := range ordered {
		if _, ok := seen[path]; ok || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, errors.New("source inventory is not canonical and duplicate-free")
		}
		seen[path] = struct{}{}
		if dispatcher == "" {
			return nil, errors.New("approved dispatcher is invalid")
		}
		kind, mode, digest, err := inspectProtectedRegular(path)
		if err != nil {
			return nil, err
		}
		backup := filepath.Join(backupDir, fmt.Sprintf("%02d-%s", index, filepath.Base(path)))
		state := "absent"
		disabledDigest := ""
		if path == dispatcher {
			state = "approved_trampoline"
			digestSum := sha256.Sum256([]byte(approvedRecoveryTrampoline))
			disabledDigest = hex.EncodeToString(digestSum[:])
		}
		result = append(result, SourceRecord{Path: path, Kind: kind, Mode: mode, SHA256: digest, BackupPath: backup, BackupSHA256: digest, DisabledState: state, DisabledSHA256: disabledDigest})
	}
	if dispatcher == "" {
		return nil, errors.New("source inventory requires one approved dispatcher")
	}
	approved := false
	for _, source := range result {
		if source.Path == dispatcher {
			approved = true
			break
		}
	}
	if !approved {
		return nil, errors.New("approved dispatcher is not inventoried")
	}
	return result, nil
}

// InventoryFromProtectedConfig derives the closed v1 inventory from the
// strictly parsed prior agent config and pinned Main/FIFO/source authority.
// It rejects a non-development config or any configured value that cannot be
// represented as a protected path/network expectation.
func InventoryFromProtectedConfig(configPath, mainExecutable, mainFIFO, backupDir, dispatcher string, startSources []string) (InventoryV1, error) {
	cfg, err := loadProtectedAgentConfig(configPath)
	if err != nil {
		return InventoryV1{}, err
	}
	if err := cfg.ValidateDevelopmentInventory(); err != nil {
		return InventoryV1{}, err
	}
	if dispatcher == "" {
		dispatcher = cfg.FPGADevBootDispatcher
	} else if dispatcher != cfg.FPGADevBootDispatcher {
		return InventoryV1{}, errors.New("boot dispatcher does not match protected FPGA authority")
	}
	if len(startSources) == 0 {
		startSources = append([]string(nil), cfg.FPGADevStartSources...)
	} else if !sameStringSlice(startSources, cfg.FPGADevStartSources) {
		return InventoryV1{}, errors.New("start sources do not match protected FPGA authority")
	}
	main, err := inspectPathExpectation(mainExecutable, true)
	if err != nil {
		return InventoryV1{}, err
	}
	if main.Kind != "regular" || main.SHA256 == "" {
		return InventoryV1{}, errors.New("Main executable must be a hashed regular file")
	}
	fifo, err := inspectPathExpectation(mainFIFO, true)
	if err != nil {
		return InventoryV1{}, err
	}
	if fifo.Kind != "fifo" {
		return InventoryV1{}, errors.New("Main command path must be a FIFO")
	}
	sources, err := CaptureStartSourceInventory(startSources, backupDir, dispatcher)
	if err != nil {
		return InventoryV1{}, err
	}
	result := InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}
	for _, item := range []struct {
		path   string
		target **PathExpectation
	}{
		{cfg.InputUInputPath, &result.InputUInput},
	} {
		if item.path == "" {
			continue
		}
		value, pathErr := inspectPathExpectation(item.path, true)
		if pathErr != nil {
			return InventoryV1{}, pathErr
		}
		*item.target = &value
	}
	if cfg.CastBinary != "" {
		cast, castErr := inspectPathExpectation(cfg.CastBinary, true)
		if castErr != nil {
			return InventoryV1{}, castErr
		}
		if cast.Kind != "regular" || cast.SHA256 == "" {
			return InventoryV1{}, errors.New("cast executable must be a hashed regular file")
		}
		result.CastExecutable = &cast
		for _, item := range []struct {
			path   string
			target **PathExpectation
		}{
			{cfg.CastFramebuffer, &result.CastFramebuffer},
			{cfg.CastNativeCmd, &result.CastNativeCommand},
			{cfg.CastTokenFile, &result.CastTokenFile},
		} {
			if item.path == "" {
				continue
			}
			value, pathErr := inspectPathExpectation(item.path, true)
			if pathErr != nil {
				return InventoryV1{}, pathErr
			}
			*item.target = &value
		}
		result.CastRTP = &NetworkExpectation{Network: "udp", Address: cfg.CastRTPAddress}
		result.CastControl = &NetworkExpectation{Network: "tcp", Address: cfg.CastControlAddress}
	}
	if cfg.InputListenAddress != "" {
		result.InputListen = &NetworkExpectation{Network: "tcp", Address: cfg.InputListenAddress}
	}
	if err := result.Validate(); err != nil {
		return InventoryV1{}, err
	}
	return result, nil
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateProtectedConfig(path string) error {
	file, info, err := openProtectedMetadata(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("prior agent config is not a protected 0600 regular file")
	}
	if uid, ok := journalUID(info); ok && uid != uint32(os.Getuid()) {
		return errors.New("prior agent config owner is not the current protected uid")
	}
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return errors.New("prior agent config link count is not one")
	}
	return nil
}

// loadProtectedAgentConfig parses the bytes read through the already-held
// O_PATH descriptor. The metadata check and the parse therefore observe one
// inode, even if an attacker swaps the pathname after the initial open.
func loadProtectedAgentConfig(path string) (agentconfig.Config, error) {
	cfg, _, err := loadProtectedAgentConfigSnapshot(path)
	return cfg, err
}

// LoadProtectedAgentConfig returns the validated protected configuration and
// the SHA-256 of the exact bytes read from its held descriptor. Production
// constructors use the digest to bind the install journal to the same
// operator-approved configuration snapshot.
func LoadProtectedAgentConfig(path string) (agentconfig.Config, string, error) {
	cfg, data, err := loadProtectedAgentConfigSnapshot(path)
	if err != nil {
		return agentconfig.Config{}, "", err
	}
	digest := sha256.Sum256(data)
	return cfg, hex.EncodeToString(digest[:]), nil
}

func loadProtectedAgentConfigSnapshot(path string) (agentconfig.Config, []byte, error) {
	file, info, err := openProtectedMetadata(path)
	if err != nil {
		return agentconfig.Config{}, nil, err
	}
	defer file.Close()
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return agentconfig.Config{}, nil, errors.New("prior agent config is not a protected 0600 regular file")
	}
	if uid, ok := journalUID(info); ok && uid != uint32(os.Getuid()) {
		return agentconfig.Config{}, nil, errors.New("prior agent config owner is not the current protected uid")
	}
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return agentconfig.Config{}, nil, errors.New("prior agent config link count is not one")
	}
	readable, err := protectedReadableDescriptor(path, file, info)
	if err != nil {
		return agentconfig.Config{}, nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(readable, InstallJournalMaxBytes+1))
	closeErr := readable.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return agentconfig.Config{}, nil, err
	}
	if len(data) > InstallJournalMaxBytes {
		return agentconfig.Config{}, nil, errors.New("prior agent config exceeds size bound")
	}
	last, err := file.Stat()
	if err != nil {
		return agentconfig.Config{}, nil, err
	}
	if !os.SameFile(info, last) || last.Size() != int64(len(data)) || last.Mode().Perm() != info.Mode().Perm() {
		return agentconfig.Config{}, nil, errors.New("prior agent config changed while being read")
	}
	cfg, err := agentconfig.Parse(data)
	if err != nil {
		return agentconfig.Config{}, nil, err
	}
	return cfg, data, nil
}

// BuildInventoryFromAgentConfig is a descriptive alias used by installers.
func BuildInventoryFromAgentConfig(configPath, mainExecutable, mainFIFO, backupDir, dispatcher string, startSources []string) (InventoryV1, error) {
	return InventoryFromProtectedConfig(configPath, mainExecutable, mainFIFO, backupDir, dispatcher, startSources)
}

func (p *SourceMutationPlan) validate() error {
	if p == nil || p.BackupDir == "" || !filepath.IsAbs(p.BackupDir) || filepath.Clean(p.BackupDir) != p.BackupDir {
		return errors.New("source mutation plan is invalid")
	}
	if len(p.Sources) == 0 || len(p.Sources) > 16 {
		return errors.New("source mutation plan has invalid source count")
	}
	if p.ExpectedUID == 0 && os.Getuid() != 0 {
		// A zero UID is the production policy. Host fixtures should select their
		// own uid explicitly rather than silently weakening a protected plan.
		return errors.New("source mutation plan requires an explicit non-root fixture uid")
	}
	approved := 0
	for index, source := range p.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("source[%d]: %w", index, err)
		}
		if index > 0 && p.Sources[index-1].Path >= source.Path {
			return errors.New("source mutation plan is not path sorted")
		}
		if source.DisabledState == "approved_trampoline" {
			approved++
			if p.Dispatcher != "" && p.Dispatcher != source.Path {
				return errors.New("source mutation dispatcher does not match approved source")
			}
		}
	}
	if approved != 1 {
		return errors.New("source mutation plan requires one approved trampoline")
	}
	if p.Dispatcher == "" {
		for _, source := range p.Sources {
			if source.DisabledState == "approved_trampoline" {
				p.Dispatcher = source.Path
				break
			}
		}
	}
	if !filepath.IsAbs(p.Dispatcher) || filepath.Clean(p.Dispatcher) != p.Dispatcher {
		return errors.New("source mutation dispatcher is invalid")
	}
	if p.Supervisor != "" && (!filepath.IsAbs(p.Supervisor) || filepath.Clean(p.Supervisor) != p.Supervisor) {
		return errors.New("source mutation supervisor path is invalid")
	}
	if p.Supervisor != "" {
		for _, source := range p.Sources {
			if source.Path == p.Supervisor {
				return errors.New("source mutation supervisor path collides with a launch source")
			}
		}
	}
	if err := ensureNoSymlinkComponents(filepath.Dir(p.BackupDir)); err != nil {
		return err
	}
	return nil
}

// ValidateForProduction exposes the plan admission check to the ARM command
// composition without exposing any mutation primitive. The install command
// validates the complete plan before it returns a manager to its dispatcher.
func (p *SourceMutationPlan) ValidateForProduction() error { return p.validate() }

// ProtectedInstallManagerOptions describes the immutable target composition
// needed by install-profile, recover-install, and uninstall-profile. The
// option set is intentionally path-oriented so the ARM command can supply
// its protected layout while host tests substitute a private temporary root.
// Runtime callbacks are injected by the platform adapter; the manager still
// owns the journal, source plan, locks, and recovery handoff.
type ProtectedInstallManagerOptions struct {
	ConfigPath         string
	MainExecutable     string
	MainFIFO           string
	BackupDir          string
	JournalPath        string
	InstallLockPath    string
	OwnerPath          string
	OwnerLockPath      string
	Dispatcher         string
	StartSources       []string
	SupervisorSource   string
	StageRoot          string
	FixedMembers       map[string]string
	RecoveryDiagnostic string

	SupervisorExecutable string
	SupervisorArguments  []string
	PackageSHA256        string
	ExpectedUID          uint32
	BootID               func() (string, error)
	InheritedLockFD      int
	ExecSupervisor       func(context.Context, int) error

	MainReadiness    any
	MainObserver     any
	StopAgent        any
	ProveAgentAbsent any
	RequestReboot    any
}

// NewProtectedInstallManager constructs the concrete protected install
// manager used by the ARM command. It reads one protected config snapshot and
// retains only static path/lock/stage authority; live launch-source inventory
// is deferred until Install holds both locks and has confirmed that the
// journal is absent. A successor Recover or Uninstall binds its source plan
// from the durable journal instead of recapturing mutated live files. The
// config digest is read again before returning so a path swap between those
// reads fails closed.
func NewProtectedInstallManager(options ProtectedInstallManagerOptions) (*InstallManager, error) {
	if options.ConfigPath == "" || options.SupervisorSource == "" || options.PackageSHA256 == "" {
		return nil, errors.New("protected install manager requires config, supervisor source, and package hash")
	}
	if !manifestHashPattern.MatchString(options.PackageSHA256) {
		return nil, errors.New("package hash is not canonical")
	}
	if options.MainExecutable == "" {
		options.MainExecutable = "/media/fat/MiSTer"
	}
	if options.JournalPath == "" {
		options.JournalPath = InstallJournalPath
	}
	if options.InstallLockPath == "" {
		options.InstallLockPath = InstallLockPath
	}
	if options.BootID == nil {
		options.BootID = readKernelBootID
	}
	if options.SupervisorExecutable == "" {
		options.SupervisorExecutable = defaultSupervisorExecutable
	}
	if len(options.SupervisorArguments) != 0 {
		return nil, errors.New("protected supervisor handoff accepts only the fixed inherited-fd argument")
	}
	if options.MainReadiness == nil || options.StopAgent == nil || options.ProveAgentAbsent == nil {
		return nil, errors.New("protected install manager requires Main readiness and agent absence proof")
	}
	cfg, configHash, err := LoadProtectedAgentConfig(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.ValidateDevelopmentInventory(); err != nil {
		return nil, err
	}
	if options.MainFIFO != "" && options.MainFIFO != cfg.CommandPipe {
		return nil, errors.New("Main FIFO does not match protected agent config")
	}
	if options.OwnerPath != "" && options.OwnerPath != cfg.HardwareOwnerPath {
		return nil, errors.New("owner store path does not match protected agent config")
	}
	if options.OwnerLockPath != "" && options.OwnerLockPath != cfg.HardwareOwnerLock {
		return nil, errors.New("owner lock path does not match protected agent config")
	}
	mainFIFO := options.MainFIFO
	if mainFIFO == "" {
		mainFIFO = cfg.CommandPipe
	}
	backupDir := options.BackupDir
	if backupDir == "" {
		backupDir = InstallBackupDir
	}
	if options.Dispatcher != "" && options.Dispatcher != cfg.FPGADevBootDispatcher {
		return nil, errors.New("boot dispatcher does not match protected FPGA authority")
	}
	if len(options.StartSources) != 0 && !sameStringSlice(options.StartSources, cfg.FPGADevStartSources) {
		return nil, errors.New("start sources do not match protected FPGA authority")
	}
	if !filepath.IsAbs(options.MainExecutable) || filepath.Clean(options.MainExecutable) != options.MainExecutable {
		return nil, errors.New("Main executable path is invalid")
	}
	if !filepath.IsAbs(mainFIFO) || filepath.Clean(mainFIFO) != mainFIFO {
		return nil, errors.New("Main FIFO path is invalid")
	}
	if !filepath.IsAbs(backupDir) || filepath.Clean(backupDir) != backupDir {
		return nil, errors.New("backup directory path is invalid")
	}
	if !filepath.IsAbs(options.SupervisorSource) || filepath.Clean(options.SupervisorSource) != options.SupervisorSource {
		return nil, errors.New("supervisor source path is invalid")
	}
	if options.StageRoot != "" && (!filepath.IsAbs(options.StageRoot) || filepath.Clean(options.StageRoot) != options.StageRoot) {
		return nil, errors.New("persistent stage root path is invalid")
	}
	if options.RecoveryDiagnostic != "" {
		if !filepath.IsAbs(options.RecoveryDiagnostic) || filepath.Clean(options.RecoveryDiagnostic) != options.RecoveryDiagnostic {
			return nil, errors.New("recovery diagnostic path is invalid")
		}
		diagnosticRoot := options.StageRoot
		if diagnosticRoot == "" {
			diagnosticRoot = defaultInstallStageRoot
		}
		if pathWithin(options.RecoveryDiagnostic, filepath.Clean(diagnosticRoot)) {
			return nil, errors.New("recovery diagnostic path must be outside the persistent stage root")
		}
	}
	for member, target := range options.FixedMembers {
		if member == "" || target == "" || !filepath.IsAbs(target) || filepath.Clean(target) != target {
			return nil, fmt.Errorf("fixed install target %q is invalid", member)
		}
	}
	_, finalConfigHash, err := LoadProtectedAgentConfig(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	if finalConfigHash != configHash {
		return nil, errors.New("protected agent config changed during install-manager composition")
	}
	plan := NewSourceMutationPlan(nil, backupDir)
	plan.Dispatcher = cfg.FPGADevBootDispatcher
	plan.Supervisor = options.SupervisorSource
	plan.ExpectedUID = options.ExpectedUID
	journal := NewInstallJournal(options.JournalPath, options.ExpectedUID)
	journal.BackupDir = backupDir
	ownerPath := options.OwnerPath
	if ownerPath == "" {
		ownerPath = cfg.HardwareOwnerPath
	}
	ownerLockPath := options.OwnerLockPath
	if ownerLockPath == "" {
		ownerLockPath = cfg.HardwareOwnerLock
	}
	if ownerPath == "" || ownerLockPath == "" {
		return nil, errors.New("protected install manager requires owner store and lock paths")
	}
	manager := NewInstallManager(journal,
		NewInstallLocker(options.InstallLockPath, options.ExpectedUID),
		hardwareowner.NewStore(ownerPath, options.ExpectedUID),
		NewInstallLocker(ownerLockPath, options.ExpectedUID),
		options.BootID,
	)
	manager.PackageSHA256 = options.PackageSHA256
	manager.PreviousConfigSHA256 = configHash
	manager.ProfilePath = options.ConfigPath
	manager.InstallSourceConfig = &InstallSourceConfig{
		ConfigPath: options.ConfigPath, MainExecutable: options.MainExecutable,
		MainFIFO: mainFIFO, BackupDir: backupDir, Dispatcher: cfg.FPGADevBootDispatcher,
		StartSources: append([]string(nil), cfg.FPGADevStartSources...), Supervisor: options.SupervisorSource,
	}
	manager.configureChainFromJournal = true
	manager.SourcePlan = plan
	manager.StageRoot = options.StageRoot
	manager.FixedMembers = cloneStringMap(options.FixedMembers)
	manager.RecoveryDiagnostic = options.RecoveryDiagnostic
	manager.MainReadiness = options.MainReadiness
	manager.MainObserver = options.MainObserver
	manager.StopAgent = options.StopAgent
	manager.ProveAgentAbsent = options.ProveAgentAbsent
	// Install already owns both locks for its complete coarse transition. The
	// initializer therefore uses the held owner lock directly; the exported
	// InitializeOwnerUnderInstallLock helper remains for callers that hold only
	// the install lock (for example a retained recovery descriptor).
	manager.OwnerInitializer = func(ctx context.Context) error {
		bootID, bootErr := manager.BootID()
		if bootErr != nil {
			return bootErr
		}
		return manager.initializeOwnerHeld(ctx, bootID)
	}
	manager.RequestReboot = options.RequestReboot
	manager.SupervisorExecutable = options.SupervisorExecutable
	manager.SupervisorArguments = append([]string(nil), options.SupervisorArguments...)
	manager.InheritedLockFD = options.InheritedLockFD
	manager.ExecSupervisor = options.ExecSupervisor
	manager.preJournalAuthorityPath = PreJournalRecoveryAuthorityPath(journal.Path)
	journalExists := false
	if info, statErr := os.Lstat(journal.Path); statErr == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("protected install journal is not a regular file")
		}
		record, exists, loadErr := journal.Load()
		if loadErr != nil {
			return nil, loadErr
		}
		if exists {
			journalExists = true
			if err := manager.configureChainOriginal(record); err != nil {
				return nil, err
			}
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if manager.preJournalAuthorityPath == "" {
		return nil, errors.New("pre-journal recovery authority path is invalid")
	}
	if !journalExists {
		authority, authorityExists, authorityErr := loadPreJournalRecoveryAuthority(manager.preJournalAuthorityPath, journal.ExpectedUID)
		if authorityErr != nil {
			return nil, authorityErr
		}
		if authorityExists {
			if err := manager.validatePreJournalRecoveryAuthority(authority); err != nil {
				return nil, err
			}
			manager.preJournalAuthority = &authority
		}
	}
	return manager, nil
}

func loadPreJournalRecoveryAuthority(path string, expectedUID uint32) (PreJournalRecoveryAuthority, bool, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return PreJournalRecoveryAuthority{}, false, errors.New("pre-journal recovery authority path is invalid")
	}
	parent := filepath.Dir(path)
	if err := validateSecureJournalDir(parent, expectedUID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PreJournalRecoveryAuthority{}, false, nil
		}
		return PreJournalRecoveryAuthority{}, false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return PreJournalRecoveryAuthority{}, false, nil
	}
	if err != nil {
		return PreJournalRecoveryAuthority{}, false, err
	}
	if err := validateJournalFileInfo(info, expectedUID); err != nil {
		return PreJournalRecoveryAuthority{}, true, err
	}
	raw, err := readRegularFileNoFollow(path, info)
	if err != nil {
		return PreJournalRecoveryAuthority{}, true, err
	}
	authority, err := ParsePreJournalRecoveryAuthority(raw)
	if err != nil {
		return PreJournalRecoveryAuthority{}, true, err
	}
	return authority, true, nil
}

func writePreJournalRecoveryAuthority(path string, authority PreJournalRecoveryAuthority, expectedUID uint32) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("pre-journal recovery authority path is invalid")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return err
	}
	if err := validateSecureJournalDir(parent, expectedUID); err != nil {
		return err
	}
	raw, err := authority.MarshalCanonical()
	if err != nil {
		return err
	}
	if err := atomicReplaceProtected(path, raw, 0o600); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return validateJournalFileInfo(info, expectedUID)
}

func removePreJournalRecoveryAuthority(path string, expectedUID uint32) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("pre-journal recovery authority path is invalid")
	}
	parent := filepath.Dir(path)
	if err := validateSecureJournalDir(parent, expectedUID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateJournalFileInfo(info, expectedUID); err != nil {
		return err
	}
	return removeProtectedPath(path, expectedUID)
}

func (m *InstallManager) validatePreJournalRecoveryAuthority(authority PreJournalRecoveryAuthority) error {
	if m == nil || m.Journal == nil {
		return ErrRunnerConfiguration
	}
	if err := authority.Validate(); err != nil {
		return err
	}
	stageRoot := filepath.Clean(m.stageRoot())
	if !pathWithin(authority.StagePath, stageRoot) {
		return errors.New("pre-journal stage is outside the protected stage root")
	}
	dispatcher := ""
	if m.InstallSourceConfig != nil {
		dispatcher = m.InstallSourceConfig.Dispatcher
	}
	if dispatcher == "" && m.SourcePlan != nil {
		dispatcher = m.SourcePlan.Dispatcher
	}
	if dispatcher == "" || authority.DispatcherPath != dispatcher {
		return errors.New("pre-journal dispatcher does not match protected authority")
	}
	backupDir := m.Journal.BackupDir
	if m.SourcePlan != nil && m.SourcePlan.BackupDir != "" {
		backupDir = m.SourcePlan.BackupDir
	}
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(m.Journal.Path), "fpgadev-install-v1-backups")
	}
	if !pathWithin(authority.DispatcherBackupPath, filepath.Clean(backupDir)) {
		return errors.New("pre-journal dispatcher backup escapes protected backup directory")
	}
	if filepath.Join(authority.StagePath, "bin", "mister-fpga-dev") == authority.DispatcherPath {
		return errors.New("pre-journal helper collides with dispatcher")
	}
	return nil
}

func (m *InstallManager) makePreJournalRecoveryAuthority(stage string) (PreJournalRecoveryAuthority, error) {
	if m == nil || m.Journal == nil || m.SourcePlan == nil {
		return PreJournalRecoveryAuthority{}, ErrRunnerConfiguration
	}
	if stage == "" {
		return PreJournalRecoveryAuthority{}, errors.New("pre-journal stage is unavailable")
	}
	var dispatcher SourceRecord
	for _, source := range m.SourcePlan.Sources {
		if source.DisabledState == "approved_trampoline" {
			dispatcher = source
			break
		}
	}
	if dispatcher.Path == "" {
		return PreJournalRecoveryAuthority{}, errors.New("pre-journal dispatcher source is unavailable")
	}
	authority := PreJournalRecoveryAuthority{
		Schema:                 1,
		StagePath:              filepath.Clean(stage),
		StageManifestSHA256:    filepath.Base(filepath.Clean(stage)),
		DispatcherPath:         dispatcher.Path,
		DispatcherBackupPath:   dispatcher.BackupPath,
		DispatcherBackupSHA256: dispatcher.BackupSHA256,
		DispatcherMode:         dispatcher.Mode,
		RecoveryHelperSHA256:   filepath.Base(filepath.Clean(stage)),
	}
	// The helper hash is the staged member digest, not the stage-directory
	// name. It is populated by the package-aware caller below.
	if pkg, err := validatePersistentStage(stage, m.Journal.ExpectedUID); err == nil {
		authority.RecoveryHelperSHA256 = pkg.MemberSHA256["bin/mister-fpga-dev"]
	} else {
		return PreJournalRecoveryAuthority{}, err
	}
	if err := m.validatePreJournalRecoveryAuthority(authority); err != nil {
		return PreJournalRecoveryAuthority{}, err
	}
	return authority, nil
}

func (m *InstallManager) persistPreJournalRecoveryAuthority(stage string) error {
	if m == nil || m.Journal == nil {
		return ErrRunnerConfiguration
	}
	authority, err := m.makePreJournalRecoveryAuthority(stage)
	if err != nil {
		return err
	}
	path := m.preJournalAuthorityPath
	if path == "" {
		path = PreJournalRecoveryAuthorityPath(m.Journal.Path)
	}
	if err := writePreJournalRecoveryAuthority(path, authority, m.Journal.ExpectedUID); err != nil {
		return err
	}
	m.preJournalAuthorityPath = path
	m.preJournalAuthority = &authority
	return nil
}

func (m *InstallManager) recoverPreJournalAuthority(ctx context.Context) (bool, error) {
	if m == nil || m.Journal == nil {
		return false, ErrRunnerConfiguration
	}
	path := m.preJournalAuthorityPath
	if path == "" {
		path = PreJournalRecoveryAuthorityPath(m.Journal.Path)
	}
	authority, exists, err := loadPreJournalRecoveryAuthority(path, m.Journal.ExpectedUID)
	if err != nil {
		return exists, err
	}
	if !exists {
		return false, nil
	}
	if err := m.validatePreJournalRecoveryAuthority(authority); err != nil {
		return true, err
	}
	m.preJournalAuthorityPath = path
	m.preJournalAuthority = &authority
	original, err := readProtectedBackup(authority.DispatcherBackupPath, m.Journal.ExpectedUID)
	if err != nil {
		return true, m.fencePreJournal(ctx, authority, fmt.Errorf("pre-journal dispatcher backup is unavailable: %w", err))
	}
	digest := sha256.Sum256(original)
	if hex.EncodeToString(digest[:]) != authority.DispatcherBackupSHA256 {
		return true, m.fencePreJournal(ctx, authority, errors.New("pre-journal dispatcher backup hash does not match authority"))
	}
	if err := verifyProtectedBytes(authority.DispatcherPath, original, authority.DispatcherMode, m.Journal.ExpectedUID); err == nil {
		// The dispatcher is already restored. This covers a retry after the
		// stage-cleanup or original-chain operation failed.
	} else {
		stageRoot := filepath.Clean(m.stageRoot())
		if !pathWithin(authority.StagePath, stageRoot) {
			return true, err
		}
		pkg, stageErr := validatePersistentStage(authority.StagePath, m.Journal.ExpectedUID)
		if stageErr != nil {
			return true, m.fencePreJournal(ctx, authority, stageErr)
		}
		if pkg.MemberSHA256["bin/mister-fpga-dev"] != authority.RecoveryHelperSHA256 {
			return true, m.fencePreJournal(ctx, authority, errors.New("pre-journal staged helper hash does not match authority"))
		}
		trampoline := BuildRecoveryTrampoline(authority.StagePath, authority.RecoveryHelperSHA256, m.diagnosticPath(authority.StagePath))
		if trampolineErr := verifyProtectedBytes(authority.DispatcherPath, trampoline, 0o755, m.Journal.ExpectedUID); trampolineErr != nil {
			return true, m.fencePreJournal(ctx, authority, fmt.Errorf("pre-journal dispatcher is neither original nor bound trampoline: %w", trampolineErr))
		}
		if err := atomicReplaceProtected(authority.DispatcherPath, original, os.FileMode(authority.DispatcherMode)); err != nil {
			return true, err
		}
		if err := verifyProtectedBytes(authority.DispatcherPath, original, authority.DispatcherMode, m.Journal.ExpectedUID); err != nil {
			return true, err
		}
	}
	m.StagePath = authority.StagePath
	m.StageManifestSHA256 = authority.StageManifestSHA256
	if err := m.removePersistentStage(); err != nil {
		return true, m.fencePreJournal(ctx, authority, err)
	}
	chain := m.ChainOriginal
	if chain == nil {
		source := SourceRecord{Path: authority.DispatcherPath, Kind: "regular", Mode: authority.DispatcherMode, SHA256: authority.DispatcherBackupSHA256, BackupPath: authority.DispatcherBackupPath, BackupSHA256: authority.DispatcherBackupSHA256, DisabledState: "approved_trampoline", DisabledSHA256: hashBytes(BuildRecoveryTrampoline(authority.StagePath, authority.RecoveryHelperSHA256, m.diagnosticPath(authority.StagePath)))}
		chain = chainOriginalFromBackup(authority.DispatcherPath, source, m.Journal.ExpectedUID)
	}
	if err := invokeMutation(ctx, chain); err != nil {
		return true, err
	}
	if err := removePreJournalRecoveryAuthority(path, m.Journal.ExpectedUID); err != nil {
		return true, err
	}
	m.preJournalAuthority = nil
	return true, nil
}

func (m *InstallManager) fencePreJournal(ctx context.Context, authority PreJournalRecoveryAuthority, cause error) error {
	if cause == nil {
		cause = errors.New("pre-journal recovery authority rejected")
	}
	diagnostic := ""
	if m != nil {
		diagnostic = m.diagnosticPath(authority.StagePath)
	}
	if diagnostic == "" {
		return cause
	}
	return errors.Join(cause, writeRecoveryDiagnostic(diagnostic, cause.Error()))
}

func hashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Prepare creates/validates separately protected backups without changing any
// launch source. It is safe to call again after a crash: an existing backup is
// accepted only when its bytes, mode, owner, and link count match the journal
// binding.
func (p *SourceMutationPlan) Prepare(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := p.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(p.BackupDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(p.BackupDir, 0o700); err != nil {
		return err
	}
	for index := range p.Sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		source := &p.Sources[index]
		held, sourceInfo, infoErr := openProtectedMetadata(source.Path)
		if infoErr != nil {
			if source.DisabledState == "absent" && errors.Is(infoErr, os.ErrNotExist) {
				if err := validateSourceBackup(*source, p.ExpectedUID); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("open source %s: %w", source.Path, infoErr)
		}
		uid, uidKnown := journalUID(sourceInfo)
		nlink, nlinkKnown := journalNlink(sourceInfo)
		closeErr := held.Close()
		if err := errors.Join(closeErr); err != nil {
			return err
		}
		if uidKnown && uid != p.ExpectedUID {
			return errors.New("launch source owner does not match protected uid")
		}
		if nlinkKnown && nlink != 1 {
			return errors.New("launch source link count is not one")
		}
		kind, mode, digest, data, err := readProtectedSource(source.Path)
		if err != nil {
			// A crash after an already durable source transition leaves the
			// prepared record behind. Accept only the exact recorded disabled
			// representation; every other disappearance is a source swap.
			if source.DisabledState == "absent" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read source %s: %w", source.Path, err)
		}
		if source.Kind != kind || source.Mode != mode || source.SHA256 != digest {
			alreadyDisabled := false
			switch source.DisabledState {
			case "approved_trampoline":
				alreadyDisabled = verifyProtectedBytes(source.Path, p.trampolineBytes(), 0o755, p.ExpectedUID) == nil
			case "inert_replacement":
				alreadyDisabled = verifyProtectedBytes(source.Path, p.inertBytes(), source.Mode, p.ExpectedUID) == nil
			}
			if !alreadyDisabled {
				return errors.New("launch source changed since inventory capture")
			}
			if err := validateSourceBackup(*source, p.ExpectedUID); err != nil {
				return err
			}
			continue
		}
		if source.BackupPath == "" {
			source.BackupPath = filepath.Join(p.BackupDir, fmt.Sprintf("%02d-%s", index, filepath.Base(source.Path)))
		}
		if !pathWithin(source.BackupPath, p.BackupDir) {
			return errors.New("source backup escapes protected backup directory")
		}
		if source.BackupSHA256 != source.SHA256 {
			return errors.New("source backup hash does not bind original bytes")
		}
		if err := writeProtectedBackup(source.BackupPath, data, mode, p.ExpectedUID); err != nil {
			return fmt.Errorf("write backup %s: %w", source.BackupPath, err)
		}
	}
	if err := syncDirectory(p.BackupDir); err != nil {
		return fmt.Errorf("sync backup directory %s: %w", p.BackupDir, err)
	}
	return nil
}

func validateSourceBackup(source SourceRecord, expectedUID uint32) error {
	data, err := readProtectedBackup(source.BackupPath, expectedUID)
	if err != nil {
		return fmt.Errorf("read source backup %s: %w", source.BackupPath, err)
	}
	digest := sha256.Sum256(data)
	if source.BackupSHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("source backup hash does not match journal")
	}
	return nil
}

func (p *SourceMutationPlan) InstallTrampoline(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := p.mutationHook("before-trampoline"); err != nil {
		return err
	}
	data := p.trampolineBytes()
	var source SourceRecord
	for _, candidate := range p.Sources {
		if candidate.Path == p.Dispatcher {
			source = candidate
			break
		}
	}
	if source.Path == "" {
		return errors.New("approved dispatcher source is missing")
	}
	if err := verifyProtectedBytes(p.Dispatcher, data, 0o755, p.ExpectedUID); err == nil {
		return nil
	}
	// A first attempt may replace only the original, descriptor-bound source;
	// an unknown current file is a source-swap failure, not an invitation to
	// overwrite it. The backup is the byte-identical original authority.
	original, err := readProtectedBackup(source.BackupPath, p.ExpectedUID)
	if err != nil {
		return err
	}
	if err := verifyProtectedBytes(p.Dispatcher, original, source.Mode, p.ExpectedUID); err != nil {
		return err
	}
	if err := atomicReplaceProtected(p.Dispatcher, data, 0o755); err != nil {
		return err
	}
	if err := verifyProtectedBytes(p.Dispatcher, data, 0o755, p.ExpectedUID); err != nil {
		return err
	}
	return p.mutationHook("after-trampoline")
}

func (p *SourceMutationPlan) DisableSources(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}
	for _, source := range p.Sources {
		if err := contextErr(ctx); err != nil {
			return err
		}
		if err := p.mutationHook("before-source-disable:" + source.Path); err != nil {
			return err
		}
		switch source.DisabledState {
		case "approved_trampoline":
			expectedHash := sha256.Sum256(p.trampolineBytes())
			if source.DisabledSHA256 != hex.EncodeToString(expectedHash[:]) {
				return errors.New("approved trampoline hash is not fixed")
			}
			if err := verifyProtectedBytes(source.Path, p.trampolineBytes(), 0o755, p.ExpectedUID); err != nil {
				return err
			}
		case "absent":
			if info, statErr := os.Lstat(source.Path); statErr == nil {
				original, backupErr := readProtectedBackup(source.BackupPath, p.ExpectedUID)
				if backupErr != nil {
					return backupErr
				}
				if verifyErr := verifyProtectedBytes(source.Path, original, source.Mode, p.ExpectedUID); verifyErr != nil {
					return verifyErr
				}
				_ = info
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			if err := removeProtectedPath(source.Path, p.ExpectedUID); err != nil {
				return err
			}
		case "inert_replacement":
			data := p.inertBytes()
			expectedHash := sha256.Sum256(data)
			if source.DisabledSHA256 != hex.EncodeToString(expectedHash[:]) {
				return errors.New("inert replacement hash is not fixed")
			}
			if err := verifyProtectedBytes(source.Path, data, source.Mode, p.ExpectedUID); err == nil {
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				// The source may still be the original captured bytes on the first
				// pass; any other replacement is rejected below by the preimage
				// check before the atomic write.
				original, backupErr := readProtectedBackup(source.BackupPath, p.ExpectedUID)
				if backupErr != nil {
					return backupErr
				}
				if verifyErr := verifyProtectedBytes(source.Path, original, source.Mode, p.ExpectedUID); verifyErr != nil {
					return verifyErr
				}
			}
			if err := atomicReplaceProtected(source.Path, data, os.FileMode(source.Mode)); err != nil {
				return err
			}
			if err := verifyProtectedBytes(source.Path, data, source.Mode, p.ExpectedUID); err != nil {
				return err
			}
		}
		if err := p.mutationHook("after-source-disable:" + source.Path); err != nil {
			return err
		}
	}
	return nil
}

func (p *SourceMutationPlan) InstallSupervisor(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if p.Supervisor == "" {
		return errors.New("supervisor source path is required")
	}
	if err := p.mutationHook("before-supervisor-install"); err != nil {
		return err
	}
	data := p.supervisorBytes()
	if err := atomicReplaceProtected(p.Supervisor, data, 0o755); err != nil {
		return err
	}
	if err := verifyProtectedBytes(p.Supervisor, data, 0o755, p.ExpectedUID); err != nil {
		return err
	}
	return p.mutationHook("after-supervisor-install")
}

func (p *SourceMutationPlan) DisableSupervisor(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if p.Supervisor == "" {
		return errors.New("supervisor source path is required")
	}
	if err := p.mutationHook("before-supervisor-disable"); err != nil {
		return err
	}
	if err := removeProtectedPath(p.Supervisor, p.ExpectedUID); err != nil {
		return err
	}
	return p.mutationHook("after-supervisor-disable")
}

// RestoreSources restores every original source except the dispatcher. The
// dispatcher is deliberately left under the recovery trampoline until
// RemoveDispatcher is called last by Uninstall.
func (p *SourceMutationPlan) RestoreSources(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}
	for _, source := range p.Sources {
		if source.Path == p.Dispatcher {
			continue
		}
		if err := contextErr(ctx); err != nil {
			return err
		}
		if err := p.mutationHook("before-source-restore:" + source.Path); err != nil {
			return err
		}
		data, err := readProtectedBackup(source.BackupPath, p.ExpectedUID)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if source.BackupSHA256 != hex.EncodeToString(digest[:]) {
			return errors.New("source backup hash does not match journal")
		}
		if err := atomicReplaceProtected(source.Path, data, os.FileMode(source.Mode)); err != nil {
			return err
		}
		if err := verifyProtectedBytes(source.Path, data, source.Mode, p.ExpectedUID); err != nil {
			return err
		}
		if err := p.mutationHook("after-source-restore:" + source.Path); err != nil {
			return err
		}
	}
	return nil
}

// RemoveDispatcher restores the original dispatcher only after all other
// sources and the supervisor have been disabled.
func (p *SourceMutationPlan) RemoveDispatcher(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := p.mutationHook("before-dispatcher-restore"); err != nil {
		return err
	}
	for _, source := range p.Sources {
		if source.Path != p.Dispatcher {
			continue
		}
		data, err := readProtectedBackup(source.BackupPath, p.ExpectedUID)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if source.BackupSHA256 != hex.EncodeToString(digest[:]) {
			return errors.New("source backup hash does not match journal")
		}
		if err := atomicReplaceProtected(source.Path, data, os.FileMode(source.Mode)); err != nil {
			return err
		}
		if err := verifyProtectedBytes(source.Path, data, source.Mode, p.ExpectedUID); err != nil {
			return err
		}
		return p.mutationHook("after-dispatcher-restore")
	}
	return errors.New("approved dispatcher backup is missing")
}

func (p *SourceMutationPlan) trampolineBytes() []byte {
	if p != nil && len(p.TrampolineBytes) != 0 {
		return append([]byte(nil), p.TrampolineBytes...)
	}
	return []byte(approvedRecoveryTrampoline)
}

func (p *SourceMutationPlan) inertBytes() []byte {
	if len(p.InertBytes) != 0 {
		return append([]byte(nil), p.InertBytes...)
	}
	return []byte(approvedInertSource)
}

func (p *SourceMutationPlan) supervisorBytes() []byte {
	if len(p.SupervisorBytes) != 0 {
		return append([]byte(nil), p.SupervisorBytes...)
	}
	return []byte("#!/bin/sh\nset -eu\nexec /usr/bin/fogcast-dev-supervisor --inherited-fd 3\n")
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" || !filepath.IsAbs(path) || !filepath.IsAbs(root) {
		return false
	}
	path, root = filepath.Clean(path), filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// InstallPackage is the verified, private transfer-stage package. Its
// Members map contains only the regular files listed by manifest.sha256;
// callers must not mutate the map after validation.
type InstallPackage struct {
	Root           string
	ManifestSHA256 string
	Manifest       []byte
	Members        map[string][]byte
	MemberSHA256   map[string]string
}

var installPackageMembers = []string{
	"bin/fogcast-dev-supervisor",
	"bin/mister-agent",
	"bin/mister-fpga-dev",
	"deploy/fpgadev/agent.toml.example",
	"deploy/fpgadev/start.sh",
}

// ValidateInstallPackage verifies the deterministic software transfer stage
// without touching any fixed target path. It accepts the package shape emitted
// by scripts/package-fpgadev.sh and rejects unknown, duplicate, malformed, or
// symlinked members.
func ValidateInstallPackage(root string) (InstallPackage, error) {
	return validateInstallPackage(root, 0o644)
}

func validatePersistentInstallPackage(root string) (InstallPackage, error) {
	return validateInstallPackage(root, 0o600)
}

func validateInstallPackage(root string, nonExecutableMode os.FileMode) (InstallPackage, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return InstallPackage{}, errors.New("install package root is not absolute and canonical")
	}
	if err := ensureNoSymlinkComponents(root); err != nil {
		return InstallPackage{}, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return InstallPackage{}, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 || rootInfo.Mode()&os.ModeSymlink != 0 {
		return InstallPackage{}, errors.New("install package root must be a protected 0700 directory")
	}
	if uid, ok := journalUID(rootInfo); ok && uid != uint32(os.Getuid()) {
		return InstallPackage{}, errors.New("install package root owner is not the current protected uid")
	}
	manifestPath := filepath.Join(root, "manifest.sha256")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return InstallPackage{}, err
	}
	if err := validatePackageFileInfo(manifestInfo, nonExecutableMode); err != nil {
		return InstallPackage{}, fmt.Errorf("manifest.sha256: %w", err)
	}
	if uid, ok := journalUID(manifestInfo); ok && uid != uint32(os.Getuid()) {
		return InstallPackage{}, errors.New("software manifest owner is not the current protected uid")
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return InstallPackage{}, err
	}
	if len(manifest) == 0 || len(manifest) > InstallJournalMaxBytes || !bytes.HasSuffix(manifest, []byte{'\n'}) {
		return InstallPackage{}, errors.New("software manifest is empty, oversized, or not newline terminated")
	}
	entries, err := parseInstallManifest(manifest)
	if err != nil {
		return InstallPackage{}, err
	}
	if len(entries) != len(installPackageMembers) {
		return InstallPackage{}, fmt.Errorf("software manifest contains %d members; want %d", len(entries), len(installPackageMembers))
	}
	for index, member := range installPackageMembers {
		if entries[index].Name != member {
			return InstallPackage{}, fmt.Errorf("software manifest member %q is out of canonical order", entries[index].Name)
		}
	}
	if err := validateInstallPackageTree(root); err != nil {
		return InstallPackage{}, err
	}
	manifestDigest := sha256.Sum256(manifest)
	result := InstallPackage{Root: root, ManifestSHA256: hex.EncodeToString(manifestDigest[:]), Manifest: append([]byte(nil), manifest...), Members: make(map[string][]byte, len(entries)), MemberSHA256: make(map[string]string, len(entries))}
	for _, entry := range entries {
		memberPath := filepath.Join(root, filepath.FromSlash(entry.Name))
		if !pathWithin(memberPath, root) {
			return InstallPackage{}, errors.New("software manifest member escapes package root")
		}
		if err := ensureNoSymlinkComponents(filepath.Dir(memberPath)); err != nil {
			return InstallPackage{}, fmt.Errorf("software member %s parent: %w", entry.Name, err)
		}
		info, statErr := os.Lstat(memberPath)
		if statErr != nil {
			return InstallPackage{}, fmt.Errorf("software member %s: %w", entry.Name, statErr)
		}
		wantMode := nonExecutableMode
		if strings.HasPrefix(entry.Name, "bin/") || entry.Name == "deploy/fpgadev/start.sh" {
			wantMode = 0o755
		}
		if err := validatePackageFileInfo(info, wantMode); err != nil {
			return InstallPackage{}, fmt.Errorf("software member %s: %w", entry.Name, err)
		}
		if uid, ok := journalUID(info); ok && uid != uint32(os.Getuid()) {
			return InstallPackage{}, fmt.Errorf("software member %s owner is not the current protected uid", entry.Name)
		}
		data, readErr := os.ReadFile(memberPath)
		if readErr != nil {
			return InstallPackage{}, readErr
		}
		digest := sha256.Sum256(data)
		got := hex.EncodeToString(digest[:])
		if got != entry.Hash {
			return InstallPackage{}, fmt.Errorf("software member %s hash mismatch", entry.Name)
		}
		result.Members[entry.Name] = data
		result.MemberSHA256[entry.Name] = got
	}
	return result, nil
}

func validateInstallPackageTree(root string) error {
	expectedFiles := map[string]struct{}{"manifest.sha256": {}}
	for _, member := range installPackageMembers {
		expectedFiles[member] = struct{}{}
	}
	expectedDirs := map[string]struct{}{"": {}, "bin": {}, "deploy": {}, "deploy/fpgadev": {}}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("software package contains symlink %q", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if _, ok := expectedDirs[rel]; !ok {
				return fmt.Errorf("software package contains unknown directory %q", rel)
			}
			return nil
		}
		if _, ok := expectedFiles[rel]; !ok {
			return fmt.Errorf("software package contains unknown member %q", rel)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("software package member %q has unsafe type", rel)
		}
		return nil
	})
}

type installManifestEntry struct {
	Hash string
	Name string
}

func parseInstallManifest(raw []byte) ([]installManifestEntry, error) {
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "" {
		return nil, errors.New("software manifest must contain one entry per line")
	}
	entries := make([]installManifestEntry, 0, len(lines)-1)
	seen := make(map[string]bool, len(lines)-1)
	for _, line := range lines[:len(lines)-1] {
		fields := strings.Split(line, "  ")
		if len(fields) != 2 || !manifestHashPattern.MatchString(fields[0]) || fields[1] == "" || strings.Contains(fields[1], "\\") || filepath.IsAbs(fields[1]) || filepath.Clean(filepath.FromSlash(fields[1])) != filepath.FromSlash(fields[1]) {
			return nil, fmt.Errorf("malformed software manifest line")
		}
		name := filepath.ToSlash(fields[1])
		if seen[name] {
			return nil, fmt.Errorf("duplicate software manifest member %q", name)
		}
		seen[name] = true
		entries = append(entries, installManifestEntry{Hash: fields[0], Name: name})
	}
	return entries, nil
}

func validatePackageFileInfo(info os.FileInfo, mode os.FileMode) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != mode.Perm() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("software package member has unsafe type or mode")
	}
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return errors.New("software package member link count is not one")
	}
	return nil
}

func (m *InstallManager) stageRoot() string {
	if m != nil && m.StageRoot != "" {
		return m.StageRoot
	}
	return defaultInstallStageRoot
}

func (m *InstallManager) diagnosticPath(stage string) string {
	if m != nil && m.RecoveryDiagnostic != "" {
		candidate := filepath.Clean(m.RecoveryDiagnostic)
		if filepath.IsAbs(m.RecoveryDiagnostic) && candidate == m.RecoveryDiagnostic && !pathWithin(candidate, filepath.Clean(m.stageRoot())) {
			return candidate
		}
	}
	if m != nil && m.Journal != nil {
		return filepath.Join(filepath.Dir(m.Journal.Path), "recovery.diagnostic")
	}
	return ""
}

func secureInstallDirectory(path string, uid uint32) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("install directory path is invalid")
	}
	if err := ensureNoSymlinkComponents(filepath.Dir(path)); err != nil {
		return err
	}
	// MkdirAll and Chmod follow the final component. Reject an existing
	// symlink before either operation so a swapped stage root cannot redirect
	// writes into an attacker-selected directory.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("install directory is not a regular directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return validateSecureJournalDir(path, uid)
}

func copyInstallPackage(pkg InstallPackage, stage string, uid uint32) error {
	return copyInstallPackageWithSync(pkg, stage, uid, syncProtectedFile)
}

func copyInstallPackageWithSync(pkg InstallPackage, stage string, uid uint32, syncFile func(string) error) error {
	if syncFile == nil {
		return errors.New("persistent stage sync function is unavailable")
	}
	if stage == "" || !filepath.IsAbs(stage) || filepath.Clean(stage) != stage ||
		!manifestHashPattern.MatchString(filepath.Base(stage)) || filepath.Base(stage) != pkg.ManifestSHA256 {
		return errors.New("persistent stage name is not bound to the software manifest")
	}
	if err := secureInstallDirectory(filepath.Dir(stage), uid); err != nil {
		return err
	}
	if err := secureInstallDirectory(stage, uid); err != nil {
		return err
	}
	for _, member := range append(append([]string(nil), installPackageMembers...), "manifest.sha256") {
		data := pkg.Manifest
		mode := os.FileMode(0o600)
		if member != "manifest.sha256" {
			data = pkg.Members[member]
			if strings.HasPrefix(member, "bin/") || member == "deploy/fpgadev/start.sh" {
				mode = 0o755
			}
		}
		dst := filepath.Join(stage, filepath.FromSlash(member))
		if !pathWithin(dst, stage) {
			return errors.New("staged software member escapes persistent stage")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := ensureNoSymlinkComponents(filepath.Dir(dst)); err != nil {
			return err
		}
		if info, err := os.Lstat(dst); err == nil {
			if err := validatePackageFileInfo(info, mode); err != nil {
				return err
			}
			got, readErr := os.ReadFile(dst)
			if readErr != nil || !bytes.Equal(got, data) {
				if readErr != nil {
					return readErr
				}
				return fmt.Errorf("persistent staged member %s differs", member)
			}
			if err := syncFile(dst); err != nil {
				return fmt.Errorf("sync matching staged member %s: %w", member, err)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := writeProtectedFile(dst, data, mode, uid); err != nil {
			return fmt.Errorf("stage %s: %w", member, err)
		}
	}
	// Persist child directory entries before the stage directory itself. The
	// individual members were fsynced by writeProtectedFile; syncing each
	// containing directory closes the rename/create durability chain.
	dirs := []string{
		filepath.Join(stage, "bin"),
		filepath.Join(stage, "deploy", "fpgadev"),
		filepath.Join(stage, "deploy"),
	}
	for _, dir := range dirs {
		if err := syncDirectory(dir); err != nil {
			return err
		}
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(stage))
}

func writeProtectedFile(path string, data []byte, mode os.FileMode, uid uint32) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || mode.Perm() == 0 {
		return errors.New("protected file path is invalid")
	}
	if err := ensureNoSymlinkComponents(filepath.Dir(path)); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("protected file descriptor is invalid")
	}
	writeErr := writeAll(file, data)
	if writeErr == nil {
		writeErr = file.Chmod(mode.Perm())
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := validatePackageFileInfo(info, mode); err != nil {
		return err
	}
	if uid != 0 {
		if got, ok := journalUID(info); ok && got != uid {
			return errors.New("protected file owner does not match expected uid")
		}
	}
	return nil
}

// openProtectedMetadata obtains an O_PATH descriptor without following the
// final component. Parent components are checked for symlinks before open;
// callers always fstat this descriptor and never use a path-only identity for
// a source transition.
func openProtectedMetadata(path string) (*os.File, os.FileInfo, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, errors.New("protected path is not absolute and canonical")
	}
	if err := ensureNoSymlinkComponents(filepath.Dir(path)); err != nil {
		return nil, nil, err
	}
	fd, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, errors.New("protected path descriptor is invalid")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func protectedReadableDescriptor(path string, held *os.File, heldInfo os.FileInfo) (*os.File, error) {
	if held == nil || heldInfo == nil {
		return nil, errors.New("protected path descriptor is unavailable")
	}
	// Opening /proc/self/fd/N follows the already-held O_PATH descriptor rather
	// than resolving the mutable source pathname a second time. The resulting
	// descriptor is still checked against the held device/inode.
	readPath := filepath.Join("/proc/self/fd", strconv.FormatUint(uint64(held.Fd()), 10))
	fd, err := unix.Open(readPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	readable := os.NewFile(uintptr(fd), path)
	if readable == nil {
		_ = unix.Close(fd)
		return nil, errors.New("protected readable descriptor is invalid")
	}
	readInfo, err := readable.Stat()
	if err != nil {
		_ = readable.Close()
		return nil, err
	}
	if !os.SameFile(heldInfo, readInfo) {
		_ = readable.Close()
		return nil, errors.New("protected path changed while opening readable descriptor")
	}
	return readable, nil
}

func inspectProtectedRegular(path string) (string, uint32, string, error) {
	kind, mode, digest, _, err := readProtectedSource(path)
	return kind, mode, digest, err
}

func readProtectedSource(path string) (kind string, mode uint32, digest string, data []byte, err error) {
	held, info, err := openProtectedMetadata(path)
	if err != nil {
		return "", 0, "", nil, err
	}
	defer held.Close()
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, "", nil, errors.New("launch source is not a regular file")
	}
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return "", 0, "", nil, errors.New("launch source link count is not one")
	}
	readable, err := protectedReadableDescriptor(path, held, info)
	if err != nil {
		return "", 0, "", nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(readable, ProtectedRegularMaxBytes+1))
	closeErr := readable.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", 0, "", nil, err
	}
	if len(data) > ProtectedRegularMaxBytes {
		return "", 0, "", nil, errors.New("launch source exceeds size bound")
	}
	last, err := held.Stat()
	if err != nil {
		return "", 0, "", nil, err
	}
	if !os.SameFile(info, last) || last.Size() != int64(len(data)) || last.Mode().Perm() != info.Mode().Perm() {
		return "", 0, "", nil, errors.New("launch source changed while being read")
	}
	digestSum := sha256.Sum256(data)
	return "regular", uint32(info.Mode().Perm()), hex.EncodeToString(digestSum[:]), data, nil
}

func inspectPathExpectation(path string, required bool) (PathExpectation, error) {
	if path == "" {
		if required {
			return PathExpectation{}, errors.New("required protected path is empty")
		}
		return PathExpectation{}, nil
	}
	held, info, err := openProtectedMetadata(path)
	if err != nil {
		return PathExpectation{}, err
	}
	defer held.Close()
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return PathExpectation{}, errors.New("protected path link count is not one")
	}
	kind := protectedKind(info.Mode())
	if kind == "" {
		return PathExpectation{}, errors.New("protected path type is unsupported")
	}
	want := PathExpectation{Path: path, Kind: kind}
	if kind == "character" || kind == "block" {
		want.Rdev, _ = journalStatField(info, "Rdev", "Dev")
	}
	if kind == "regular" {
		readable, readErr := protectedReadableDescriptor(path, held, info)
		if readErr != nil {
			return PathExpectation{}, readErr
		}
		data, ioErr := io.ReadAll(io.LimitReader(readable, ProtectedRegularMaxBytes+1))
		closeErr := readable.Close()
		if err := errors.Join(ioErr, closeErr); err != nil {
			return PathExpectation{}, err
		}
		if len(data) > ProtectedRegularMaxBytes {
			return PathExpectation{}, errors.New("protected regular path exceeds size bound")
		}
		digest := sha256.Sum256(data)
		want.SHA256 = hex.EncodeToString(digest[:])
	}
	return want, nil
}

func protectedKind(mode os.FileMode) string {
	switch {
	case mode.IsRegular():
		return "regular"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	case mode&os.ModeCharDevice != 0:
		return "character"
	case mode&os.ModeDevice != 0:
		return "block"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode.IsDir():
		return "directory"
	default:
		return ""
	}
}

func writeProtectedBackup(path string, data []byte, sourceMode uint32, expectedUID uint32) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("backup path is invalid")
	}
	if err := ensureNoSymlinkComponents(filepath.Dir(path)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if err := validateBackupInfo(info, expectedUID); err != nil {
			return err
		}
		got, err := readRegularFileNoFollow(path, info)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, data) {
			return errors.New("protected backup bytes do not match original")
		}
		if err := syncProtectedFile(path); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("backup descriptor is invalid")
	}
	writeErr := writeAll(file, data)
	if writeErr == nil {
		writeErr = file.Chmod(0o600)
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := validateBackupInfo(info, expectedUID); err != nil {
		return err
	}
	if sourceMode == 0 {
		return errors.New("backup source mode is empty")
	}
	return syncDirectory(filepath.Dir(path))
}

func validateBackupInfo(info os.FileInfo, expectedUID uint32) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("backup is not a protected regular 0600 file")
	}
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return errors.New("backup link count is not one")
	}
	if uid, ok := journalUID(info); ok && uid != expectedUID {
		return errors.New("backup owner does not match protected uid")
	}
	return nil
}

func readProtectedBackup(path string, expectedUID uint32) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := validateBackupInfo(info, expectedUID); err != nil {
		return nil, err
	}
	return readRegularFileNoFollow(path, info)
}

func readRegularFileNoFollow(path string, info os.FileInfo) ([]byte, error) {
	held, heldInfo, err := openProtectedMetadata(path)
	if err != nil {
		return nil, err
	}
	defer held.Close()
	if info != nil && !os.SameFile(info, heldInfo) {
		return nil, errors.New("protected file changed while opening")
	}
	readable, err := protectedReadableDescriptor(path, held, heldInfo)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(readable, ProtectedRegularMaxBytes+1))
	closeErr := readable.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(data) > ProtectedRegularMaxBytes {
		return nil, errors.New("protected file exceeds size bound")
	}
	return data, nil
}

func writeAll(file *os.File, data []byte) error {
	for len(data) != 0 {
		n, err := file.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func atomicReplaceProtected(path string, data []byte, mode os.FileMode) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || mode.Perm() == 0 {
		return errors.New("atomic protected path is invalid")
	}
	parent := filepath.Dir(path)
	if err := ensureNoSymlinkComponents(parent); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("atomic protected target is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".fogcast-source-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode.Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := writeAll(tmp, data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	keep = true
	return syncDirectory(parent)
}

func verifyProtectedBytes(path string, want []byte, mode uint32, expectedUID uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != os.FileMode(mode).Perm() {
		return errors.New("protected replacement metadata does not match")
	}
	if uid, ok := journalUID(info); ok && uid != expectedUID {
		return errors.New("protected replacement owner does not match")
	}
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return errors.New("protected replacement link count is not one")
	}
	got, err := readRegularFileNoFollow(path, info)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("protected replacement bytes do not match")
	}
	return nil
}

func removeProtectedPath(path string, expectedUID uint32) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("protected source is not a regular file")
	}
	if uid, ok := journalUID(info); ok && uid != expectedUID {
		return errors.New("protected source owner does not match")
	}
	if nlink, ok := journalNlink(info); ok && nlink != 1 {
		return errors.New("protected source link count is not one")
	}
	parent := filepath.Dir(path)
	if err := ensureNoSymlinkComponents(parent); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".fogcast-disabled-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	if err := os.Rename(path, tmpPath); err != nil {
		return err
	}
	moved, statErr := os.Lstat(tmpPath)
	if statErr != nil {
		return statErr
	}
	if !os.SameFile(info, moved) {
		return errors.New("protected source changed during atomic disable")
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func syncDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("directory path is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	return errors.Join(err, closeErr)
}

func syncProtectedFile(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("protected file path is invalid")
	}
	if err := ensureNoSymlinkComponents(filepath.Dir(path)); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("protected file descriptor is invalid")
	}
	info, statErr := file.Stat()
	if statErr == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		statErr = errors.New("protected file is not regular")
	}
	syncErr := error(nil)
	if statErr == nil {
		syncErr = file.Sync()
	}
	closeErr := file.Close()
	return errors.Join(statErr, syncErr, closeErr)
}

// InstallManager is the policy object used by install-profile,
// recover-install, and uninstall-profile.  Callback fields are the only
// mutation seams; an unconfigured manager fails closed before touching a
// journal or launch source.
type InstallManager struct {
	Journal       *InstallJournalStore
	InstallLocker ownerLocker
	OwnerStore    ownerStore
	OwnerLocker   ownerLocker
	BootID        func() (string, error)

	PackageSHA256        string
	PreviousConfigSHA256 string
	Inventory            InventoryV1
	Sources              []SourceRecord
	ProfilePath          string

	// InstallSourceConfig is the immutable, path-only composition retained by
	// the production constructor.  It is resolved into source identities only
	// after install has acquired both locks and established that the journal is
	// absent.  Recovery and uninstall bind exclusively to journal Sources and
	// never re-inventory live launch files.
	InstallSourceConfig *InstallSourceConfig

	// PackageRoot is the private transfer-stage package supplied by the host.
	// StageRoot is injectable for typed filesystem fixtures and defaults to the
	// protected target location required by the amendment. FixedMembers maps
	// verified package member names to their target paths; an empty map keeps
	// the package contents staged without guessing a target layout.
	PackageRoot         string
	StageRoot           string
	StageManifestSHA256 string
	StagePath           string
	FixedMembers        map[string]string
	RecoveryDiagnostic  string
	ChainOriginal       func(context.Context) error
	InstallMemberMode   os.FileMode
	FailureHook         func(string) error

	MainReadiness     any
	MainObserver      any
	OwnerInitializer  any
	StopAgent         any
	ProveAgentAbsent  any
	InstallTrampoline any
	DisableSources    any
	DisableSupervisor any
	InstallSupervisor any
	RestoreSources    any
	RemoveDispatcher  any
	RequestReboot     any
	Reset             any

	// SourcePlan is the concrete protected source adapter used by the ARM
	// production constructor. Callback fields remain available for anonymous
	// host state-machine fixtures, but production no longer depends on them.
	SourcePlan *SourceMutationPlan

	sourceHookBound bool

	// Recover uses these fields only after durable journal repair. The real
	// adapter replaces itself with the supervisor through ExecSupervisor while
	// retaining the exact install-lock descriptor.
	SupervisorExecutable      string
	SupervisorArguments       []string
	ExecSupervisor            func(context.Context, int) error
	InheritedLockFD           int
	configureChainFromJournal bool

	// preJournalAuthority is loaded from a protected sidecar before a coarse
	// journal exists. It is never used as an install state; it only binds the
	// staged helper to the exact original dispatcher backup for the one
	// pre-prepared recovery seam.
	preJournalAuthority     *PreJournalRecoveryAuthority
	preJournalAuthorityPath string
}

// InstallSourceConfig describes the one-time installer inventory inputs. It
// deliberately contains no captured source bytes or identities: those belong
// to the durable journal and must not be recaptured by a rebooted recovery or
// uninstall process after launch files have been mutated.
type InstallSourceConfig struct {
	ConfigPath     string
	MainExecutable string
	MainFIFO       string
	BackupDir      string
	Dispatcher     string
	StartSources   []string
	Supervisor     string
}

// bindSourcePlan installs the one concrete source implementation behind the
// manager's phase callbacks. Anonymous callback fixtures remain supported,
// but a production manager cannot accidentally proceed with an unimplemented
// mutation seam when it has a SourceMutationPlan.
func (m *InstallManager) bindSourcePlan() error {
	if m == nil || m.SourcePlan == nil {
		return nil
	}
	plan := m.SourcePlan
	if !m.sourceHookBound {
		priorHook := plan.MutationHook
		plan.MutationHook = func(event string) error {
			if priorHook != nil {
				if err := priorHook(event); err != nil {
					return err
				}
			}
			return m.failureHook(event)
		}
		m.sourceHookBound = true
	}
	if len(plan.Sources) == 0 && len(m.Sources) != 0 {
		plan.Sources = append([]SourceRecord(nil), m.Sources...)
	}
	if len(m.Sources) == 0 && len(plan.Sources) != 0 {
		m.Sources = append([]SourceRecord(nil), plan.Sources...)
	}
	if len(m.Sources) != 0 && len(plan.Sources) != 0 {
		if len(m.Sources) != len(plan.Sources) {
			return errors.New("source mutation plan and manager source sets differ")
		}
		for index := range m.Sources {
			if m.Sources[index] != plan.Sources[index] {
				return errors.New("source mutation plan and manager source sets differ")
			}
		}
	}
	if m.InstallTrampoline == nil {
		m.InstallTrampoline = func(ctx context.Context) error { return plan.InstallTrampoline(ctx) }
	}
	if m.DisableSources == nil {
		m.DisableSources = func(ctx context.Context) error { return plan.DisableSources(ctx) }
	}
	if m.InstallSupervisor == nil {
		m.InstallSupervisor = func(ctx context.Context) error { return plan.InstallSupervisor(ctx) }
	}
	if m.DisableSupervisor == nil {
		m.DisableSupervisor = func(ctx context.Context) error { return plan.DisableSupervisor(ctx) }
	}
	if m.RestoreSources == nil {
		m.RestoreSources = func(ctx context.Context) error { return plan.RestoreSources(ctx) }
	}
	if m.RemoveDispatcher == nil {
		m.RemoveDispatcher = func(ctx context.Context) error { return plan.RemoveDispatcher(ctx) }
	}
	return nil
}

func (m *InstallManager) needsInstallSourceComposition() bool {
	return m != nil && m.InstallSourceConfig != nil && len(m.Sources) == 0 &&
		(m.SourcePlan == nil || len(m.SourcePlan.Sources) == 0)
}

// composeInstallSourcePlan is intentionally reachable only from Install after
// the journal has been loaded and both operation locks are held.  It is the
// sole path allowed to inspect pristine live launch sources.  A successor
// Recover or Uninstall process instead supplies bindJournalRecord with the
// exact durable inventory and backup bindings.
func (m *InstallManager) composeInstallSourcePlan() error {
	if m == nil || m.InstallSourceConfig == nil {
		return nil
	}
	config := m.InstallSourceConfig
	inventory, err := InventoryFromProtectedConfig(config.ConfigPath, config.MainExecutable, config.MainFIFO, config.BackupDir, config.Dispatcher, config.StartSources)
	if err != nil {
		return err
	}
	_, configHash, err := LoadProtectedAgentConfig(config.ConfigPath)
	if err != nil {
		return err
	}
	if configHash != m.PreviousConfigSHA256 {
		return errors.New("protected agent config changed during install-manager composition")
	}
	if m.SourcePlan == nil {
		m.SourcePlan = NewSourceMutationPlan(nil, config.BackupDir)
	}
	m.SourcePlan.Sources = cloneSourceRecords(inventory.StartSources)
	m.SourcePlan.BackupDir = config.BackupDir
	m.SourcePlan.Dispatcher = config.Dispatcher
	m.SourcePlan.Supervisor = config.Supervisor
	m.SourcePlan.ExpectedUID = m.Journal.ExpectedUID
	m.Inventory = inventory
	m.Sources = cloneSourceRecords(inventory.StartSources)
	if err := m.bindSourcePlan(); err != nil {
		return err
	}
	if err := m.SourcePlan.ValidateForProduction(); err != nil {
		return err
	}
	return nil
}

func cloneSourceRecords(sources []SourceRecord) []SourceRecord {
	return append([]SourceRecord(nil), sources...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

// NewInstallManager accepts optional dependencies to keep the public
// constructor useful to host fixtures while preserving fixed production
// paths when no overrides are supplied.
func NewInstallManager(args ...any) *InstallManager {
	m := &InstallManager{
		Journal:       NewProductionInstallJournal(),
		InstallLocker: hardwareowner.NewLocker(InstallLockPath),
		OwnerStore:    hardwareowner.NewProductionStore(),
		OwnerLocker:   hardwareowner.NewProductionLocker(),
		BootID:        readKernelBootID,
	}
	customLockers := 0
	for _, arg := range args {
		switch value := arg.(type) {
		case *InstallJournalStore:
			m.Journal = value
		case ownerStore:
			m.OwnerStore = value
		case ownerLocker:
			if customLockers == 0 {
				m.InstallLocker = value
			} else {
				m.OwnerLocker = value
			}
			customLockers++
		case func() (string, error):
			m.BootID = value
		case InventoryV1:
			m.Inventory = value
		case []SourceRecord:
			m.Sources = append([]SourceRecord(nil), value...)
		}
	}
	return m
}

func (m *InstallManager) normalize() error {
	if m == nil || m.Journal == nil || m.InstallLocker == nil || m.OwnerStore == nil || m.OwnerLocker == nil || m.BootID == nil {
		return ErrRunnerConfiguration
	}
	if err := m.bindSourcePlan(); err != nil {
		return err
	}
	if !manifestHashPattern.MatchString(m.PackageSHA256) {
		return errors.New("package hash is not canonical")
	}
	if !manifestHashPattern.MatchString(m.PreviousConfigSHA256) {
		return errors.New("previous config hash is not canonical")
	}
	if m.needsInstallSourceComposition() {
		if m.StopAgent == nil || m.ProveAgentAbsent == nil {
			return errors.New("install mutation and old-agent proof callbacks are required")
		}
		return nil
	}
	if err := m.Inventory.Validate(); err != nil {
		return err
	}
	if m.Sources == nil {
		m.Sources = append([]SourceRecord(nil), m.Inventory.StartSources...)
	}
	if len(m.Sources) == 0 {
		return errors.New("install requires at least one launch source")
	}
	if m.StopAgent == nil || m.ProveAgentAbsent == nil || m.InstallTrampoline == nil || m.DisableSources == nil || m.InstallSupervisor == nil {
		return errors.New("install mutation and old-agent proof callbacks are required")
	}
	return validateSourceSet(m.Sources)
}

func validateSourceSet(sources []SourceRecord) error {
	seen := map[string]bool{}
	approved := 0
	for i, source := range sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if seen[source.Path] {
			return errors.New("duplicate launch source")
		}
		seen[source.Path] = true
		if i > 0 && sources[i-1].Path >= source.Path {
			return errors.New("launch sources are not path sorted")
		}
		if source.DisabledState == "approved_trampoline" {
			approved++
		}
	}
	if approved > 1 {
		return errors.New("multiple approved recovery trampolines")
	}
	return nil
}

// InitializeOwner performs the internal install-time migration while the
// compatibility Main is still healthy. It is intentionally variadic so
// package-local callers can provide either a configured InstallManager or no
// arguments and receive a deterministic configuration failure.
func InitializeOwner(ctx context.Context, args ...any) error {
	var manager *InstallManager
	for _, arg := range args {
		if value, ok := arg.(*InstallManager); ok {
			manager = value
		}
	}
	if manager == nil {
		manager = NewInstallManager(args...)
	}
	return manager.InitializeOwner(ctx)
}

func (m *InstallManager) InitializeOwner(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.normalizeForOwner(); err != nil {
		return err
	}
	installUnlock, err := m.InstallLocker.Lock(ctx)
	if err != nil {
		if installUnlock != nil {
			return errors.Join(err, installUnlock())
		}
		return err
	}
	if installUnlock == nil {
		return ErrRunnerConfiguration
	}
	defer func() { _ = installUnlock() }()
	ownerUnlock, err := m.OwnerLocker.Lock(ctx)
	if err != nil {
		var ownerErr error
		if ownerUnlock != nil {
			ownerErr = ownerUnlock()
		}
		return errors.Join(err, ownerErr, installUnlock())
	}
	if ownerUnlock == nil {
		return ErrRunnerConfiguration
	}
	defer func() { _ = ownerUnlock() }()
	bootID, err := m.BootID()
	if err != nil {
		return err
	}
	if err := m.observeHealthyMain(ctx); err != nil {
		return err
	}
	record, exists, err := m.OwnerStore.Load()
	if err != nil {
		return err
	}
	if exists {
		if err := record.Validate(); err != nil {
			return err
		}
		if record.BootID != bootID || record.State != hardwareowner.StateNormalMain {
			return ErrInstallOwnerInvalid
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.OwnerStore == nil {
		return ErrRunnerConfiguration
	}
	session, err := randomSession()
	if err != nil {
		return err
	}
	record = hardwareowner.Record{
		Schema: hardwareowner.SchemaVersion, State: hardwareowner.StateNormalMain,
		BootID: bootID, GenerationHighWater: 1, ActiveSession: session,
		ActiveGeneration: 1, ActiveMode: hardwareowner.ModeFPGANative,
		ActiveOwner: hardwareowner.OwnerCompatMain, ActiveLeases: hardwareowner.NormalLeases(),
		CandidateMode: hardwareowner.ModeNone, CandidateOwner: hardwareowner.OwnerNone,
		QuiescingOwner: hardwareowner.OwnerNone, CandidateSession: "", CandidateGeneration: 0,
		RequestedResources: []string{}, RunID: "", Phase: "", FirstFailure: "",
	}
	return m.OwnerStore.Replace(record)
}

// InitializeOwnerUnderInstallLock performs the install-time owner migration
// while the caller already holds the shared install lock. It acquires only
// the owner lock, preserving the global install-then-owner order and avoiding
// a same-process second flock on the retained install descriptor during
// recover-install.
func (m *InstallManager) InitializeOwnerUnderInstallLock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.normalizeForOwner(); err != nil {
		return err
	}
	ownerUnlock, err := m.OwnerLocker.Lock(ctx)
	if err != nil {
		if ownerUnlock != nil {
			return errors.Join(err, ownerUnlock())
		}
		return err
	}
	if ownerUnlock == nil {
		return ErrRunnerConfiguration
	}
	defer func() { _ = ownerUnlock() }()
	bootID, err := m.BootID()
	if err != nil {
		return err
	}
	return m.initializeOwnerHeld(ctx, bootID)
}

func (m *InstallManager) normalizeForOwner() error {
	if m == nil || m.InstallLocker == nil || m.OwnerStore == nil || m.OwnerLocker == nil || m.BootID == nil {
		return ErrRunnerConfiguration
	}
	return nil
}

func (m *InstallManager) observeHealthyMain(ctx context.Context) error {
	if m.MainReadiness == nil {
		return ErrRunnerConfiguration
	}
	if err := invokeReadiness(ctx, m.MainReadiness); err != nil {
		return err
	}
	if m.MainObserver == nil {
		return nil
	}
	return invokeSingleMainObservation(ctx, m.MainObserver)
}

func invokeSingleMainObservation(ctx context.Context, value any) error {
	if observer, ok := value.(contextProcessSnapshotter); ok {
		instances, err := observer.SnapshotContext(ctx)
		if err != nil {
			return err
		}
		if len(instances) != 1 {
			return errors.New("Main process set is ambiguous")
		}
		return nil
	}
	if observer, ok := value.(interface {
		Snapshot() ([]ProcessIdentity, error)
	}); ok {
		instances, err := observer.Snapshot()
		if err != nil {
			return err
		}
		if len(instances) != 1 {
			return errors.New("Main process set is ambiguous")
		}
		return nil
	}
	if fn, ok := value.(func(context.Context) ([]ProcessIdentity, error)); ok {
		instances, err := fn(ctx)
		if err != nil {
			return err
		}
		if len(instances) != 1 {
			return errors.New("Main process set is ambiguous")
		}
		return nil
	}
	if fn, ok := value.(func(context.Context) (int, error)); ok {
		count, err := fn(ctx)
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("Main process set is ambiguous")
		}
		return nil
	}
	if fn, ok := value.(func(context.Context) (bool, error)); ok {
		ready, err := fn(ctx)
		if err != nil {
			return err
		}
		if !ready {
			return errors.New("Main process set is not healthy")
		}
		return nil
	}
	return invokeReadiness(ctx, value)
}

func (m *InstallManager) failureHook(event string) error {
	if m != nil && m.FailureHook != nil {
		return m.FailureHook(event)
	}
	return nil
}

// replaceJournalCheckpoint exposes the only crash seams that matter to boot:
// the hook immediately before the journal file/parent fsync sequence and the
// hook after that sequence has completed. An after-hook error deliberately
// leaves the new state durable so the next invocation resumes idempotently.
func (m *InstallManager) replaceJournalCheckpoint(record InstallJournalRecord, state InstallState) error {
	if err := m.failureHook("before-" + string(state) + "-fsync"); err != nil {
		return err
	}
	if err := m.Journal.Replace(record); err != nil {
		return err
	}
	return m.failureHook("after-" + string(state) + "-fsync")
}

func (p *SourceMutationPlan) mutationHook(event string) error {
	if p != nil && p.MutationHook != nil {
		return p.MutationHook(event)
	}
	return nil
}

func lockInstallThenOwner(ctx context.Context, install, owner ownerLocker) (hardwareowner.Unlock, hardwareowner.Unlock, error) {
	if install == nil || owner == nil {
		return nil, nil, ErrRunnerConfiguration
	}
	installUnlock, err := install.Lock(ctx)
	if err != nil {
		if installUnlock != nil {
			return nil, nil, errors.Join(err, installUnlock())
		}
		return nil, nil, err
	}
	if installUnlock == nil {
		return nil, nil, ErrRunnerConfiguration
	}
	ownerUnlock, err := owner.Lock(ctx)
	if err != nil {
		var ownerErr error
		if ownerUnlock != nil {
			ownerErr = ownerUnlock()
		}
		return nil, nil, errors.Join(err, ownerErr, installUnlock())
	}
	if ownerUnlock == nil {
		return nil, nil, errors.Join(ErrRunnerConfiguration, installUnlock())
	}
	return installUnlock, ownerUnlock, nil
}

func releaseInstallOwner(installUnlock, ownerUnlock hardwareowner.Unlock) error {
	// The owner is always released first. This preserves the same ordering as
	// hardwareowner.Gate and prevents a new installer from racing a stale owner
	// record during the unlock window.
	if ownerUnlock == nil && installUnlock == nil {
		return nil
	}
	var ownerErr, installErr error
	if ownerUnlock != nil {
		ownerErr = ownerUnlock()
	}
	if installUnlock != nil {
		installErr = installUnlock()
	}
	return errors.Join(ownerErr, installErr)
}

func (m *InstallManager) requestReboot(ctx context.Context) error {
	if m == nil || m.RequestReboot == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rebootCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return invokeMutation(rebootCtx, m.RequestReboot)
}

func (m *InstallManager) chainOriginal(ctx context.Context) error {
	if m == nil || m.ChainOriginal == nil {
		return ErrRunnerConfiguration
	}
	return m.ChainOriginal(ctx)
}

// chainOriginalFromBackup verifies the durable dispatcher backup and the
// restored live dispatcher before handing control back to the original
// launcher.  The exec function is injectable for host tests; production uses
// syscall.Exec so no child process is created.
func chainOriginalFromBackup(dispatcher string, source SourceRecord, expectedUID uint32) func(context.Context) error {
	return chainOriginalFromBackupWithExec(dispatcher, source, expectedUID, syscall.Exec)
}

func chainOriginalFromBackupWithExec(dispatcher string, source SourceRecord, expectedUID uint32, execFn func(string, []string, []string) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if dispatcher == "" || source.Path != dispatcher || source.BackupPath == "" || !manifestHashPattern.MatchString(source.BackupSHA256) || execFn == nil {
			return ErrRunnerConfiguration
		}
		original, err := readProtectedBackup(source.BackupPath, expectedUID)
		if err != nil {
			return fmt.Errorf("original dispatcher backup is unavailable: %w", err)
		}
		digest := sha256.Sum256(original)
		if hex.EncodeToString(digest[:]) != source.BackupSHA256 {
			return errors.New("original dispatcher backup hash does not match journal")
		}
		if err := verifyProtectedBytes(dispatcher, original, source.Mode, expectedUID); err != nil {
			return fmt.Errorf("original dispatcher was not restored: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return execFn(dispatcher, []string{dispatcher}, os.Environ())
	}
}

func (m *InstallManager) journalRecord(state InstallState) InstallJournalRecord {
	return InstallJournalRecord{
		Schema: 1, State: state, InstallBootID: m.installBootID(), PackageSHA256: m.PackageSHA256,
		PreviousConfigSHA256: m.PreviousConfigSHA256, Inventory: m.Inventory,
		Sources: cloneSourceRecords(m.Sources),
	}
}

func (m *InstallManager) installBootID() string {
	if m == nil || m.BootID == nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	id, err := m.BootID()
	if err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	return id
}

func (m *InstallManager) setDynamicTrampoline(stage string, helperSHA string) error {
	if m == nil || m.SourcePlan == nil {
		return nil
	}
	if stage == "" || !pathWithin(filepath.Clean(stage), filepath.Clean(m.stageRoot())) || !manifestHashPattern.MatchString(filepath.Base(filepath.Clean(stage))) || !manifestHashPattern.MatchString(helperSHA) {
		return errors.New("persistent recovery stage binding is invalid")
	}
	data := BuildRecoveryTrampoline(stage, helperSHA, m.diagnosticPath(stage))
	m.SourcePlan.TrampolineBytes = data
	digest := sha256.Sum256(data)
	for index := range m.SourcePlan.Sources {
		if m.SourcePlan.Sources[index].DisabledState == "approved_trampoline" {
			m.SourcePlan.Sources[index].DisabledSHA256 = hex.EncodeToString(digest[:])
		}
	}
	m.Sources = cloneSourceRecords(m.SourcePlan.Sources)
	m.Inventory.StartSources = cloneSourceRecords(m.SourcePlan.Sources)
	return nil
}

func (m *InstallManager) preparePersistentStage(ctx context.Context, packageRoot string) (InstallPackage, string, error) {
	if packageRoot == "" {
		return InstallPackage{}, "", nil
	}
	if err := contextErr(ctx); err != nil {
		return InstallPackage{}, "", err
	}
	pkg, err := ValidateInstallPackage(packageRoot)
	if err != nil {
		return InstallPackage{}, "", err
	}
	stageRoot := m.stageRoot()
	if err := secureInstallDirectory(stageRoot, m.Journal.ExpectedUID); err != nil {
		return InstallPackage{}, "", err
	}
	stage := filepath.Join(stageRoot, pkg.ManifestSHA256)
	if err := m.failureHook("before-persistent-stage"); err != nil {
		return InstallPackage{}, "", err
	}
	if err := copyInstallPackage(pkg, stage, m.Journal.ExpectedUID); err != nil {
		return InstallPackage{}, "", err
	}
	if err := m.failureHook("after-persistent-stage"); err != nil {
		return InstallPackage{}, "", err
	}
	m.StageManifestSHA256 = pkg.ManifestSHA256
	m.StagePath = stage
	if err := m.setDynamicTrampoline(stage, pkg.MemberSHA256["bin/mister-fpga-dev"]); err != nil {
		return InstallPackage{}, "", err
	}
	return pkg, stage, nil
}

func (m *InstallManager) installFixedMembers(pkg InstallPackage, stage string) error {
	if m == nil || len(m.FixedMembers) == 0 {
		return nil
	}
	members := make([]string, 0, len(m.FixedMembers))
	for member := range m.FixedMembers {
		members = append(members, member)
	}
	sort.Strings(members)
	for _, member := range members {
		target := m.FixedMembers[member]
		data, ok := pkg.Members[member]
		if !ok {
			return fmt.Errorf("fixed install member %q is not in software package", member)
		}
		if target == "" || !filepath.IsAbs(target) || filepath.Clean(target) != target {
			return fmt.Errorf("fixed install target %q is invalid", target)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(member, "bin/") || member == "deploy/fpgadev/start.sh" {
			mode = 0o755
		}
		if err := atomicReplaceProtected(target, data, mode); err != nil {
			return fmt.Errorf("install fixed member %s: %w", member, err)
		}
		if err := verifyProtectedBytes(target, data, uint32(mode.Perm()), m.Journal.ExpectedUID); err != nil {
			return err
		}
	}
	return nil
}

func (m *InstallManager) bindJournalRecord(record InstallJournalRecord) error {
	if m == nil {
		return ErrRunnerConfiguration
	}
	m.PackageSHA256 = record.PackageSHA256
	m.PreviousConfigSHA256 = record.PreviousConfigSHA256
	m.Inventory = record.Inventory
	m.Sources = cloneSourceRecords(record.Sources)
	if m.SourcePlan == nil {
		backupDir := m.Journal.BackupDir
		if backupDir == "" {
			backupDir = filepath.Join(filepath.Dir(m.Journal.Path), "fpgadev-install-v1-backups")
		}
		plan := NewSourceMutationPlan(record.Sources, backupDir)
		plan.ExpectedUID = m.Journal.ExpectedUID
		for _, source := range record.Sources {
			if source.DisabledState == "approved_trampoline" {
				plan.Dispatcher = source.Path
				break
			}
		}
		m.SourcePlan = plan
	}
	if len(m.SourcePlan.Sources) == 0 {
		m.SourcePlan.Sources = cloneSourceRecords(record.Sources)
	} else {
		if len(m.SourcePlan.Sources) != len(record.Sources) {
			return errors.New("recovery source set differs from journal")
		}
		for index := range record.Sources {
			if !sameSourceAuthority(m.SourcePlan.Sources[index], record.Sources[index]) {
				return errors.New("recovery source set differs from journal")
			}
		}
		// The journal carries the durable replacement hash. The in-memory plan
		// may have been composed from the original config before the dynamic
		// trampoline was published, so use the journal's exact source records
		// after checking their immutable authority fields above.
		m.SourcePlan.Sources = cloneSourceRecords(record.Sources)
	}
	m.SourcePlan.ExpectedUID = m.Journal.ExpectedUID
	if m.SourcePlan.BackupDir == "" {
		m.SourcePlan.BackupDir = m.Journal.BackupDir
		if m.SourcePlan.BackupDir == "" {
			m.SourcePlan.BackupDir = filepath.Join(filepath.Dir(m.Journal.Path), "fpgadev-install-v1-backups")
		}
	}
	var journalDispatcher string
	for _, source := range record.Sources {
		if source.DisabledState == "approved_trampoline" {
			journalDispatcher = source.Path
			break
		}
	}
	if journalDispatcher == "" {
		return errors.New("install journal has no approved dispatcher")
	}
	if m.SourcePlan.Dispatcher != "" && m.SourcePlan.Dispatcher != journalDispatcher {
		return errors.New("recovery dispatcher differs from journal")
	}
	m.SourcePlan.Dispatcher = journalDispatcher
	// A successor recovery process has no in-memory trampoline payload. Bind
	// the exact published bytes before any source operation so installed-state
	// retries validate the recorded disabled hash instead of falling back to
	// the legacy fixture payload.
	if m.SourcePlan.Dispatcher != "" {
		if current, readErr := os.ReadFile(m.SourcePlan.Dispatcher); readErr == nil && bytes.Contains(current, []byte("expected='")) {
			m.SourcePlan.TrampolineBytes = append([]byte(nil), current...)
		}
	}
	if m.ChainOriginal == nil && m.configureChainFromJournal {
		if err := m.configureChainOriginal(record); err != nil {
			return err
		}
	}
	if err := m.bindSourcePlan(); err != nil {
		return err
	}
	return nil
}

func (m *InstallManager) configureChainOriginal(record InstallJournalRecord) error {
	if m == nil || m.Journal == nil {
		return ErrRunnerConfiguration
	}
	for _, source := range record.Sources {
		if source.DisabledState == "approved_trampoline" {
			m.ChainOriginal = chainOriginalFromBackup(source.Path, source, m.Journal.ExpectedUID)
			return nil
		}
	}
	return errors.New("install journal has no original dispatcher binding")
}

func sameSourceAuthority(left, right SourceRecord) bool {
	return left.Path == right.Path && left.Kind == right.Kind && left.Mode == right.Mode &&
		left.SHA256 == right.SHA256 && left.BackupPath == right.BackupPath &&
		left.BackupSHA256 == right.BackupSHA256 && left.DisabledState == right.DisabledState
}

func (m *InstallManager) locatePersistentStage() (InstallPackage, string, error) {
	if m == nil || m.Journal == nil {
		return InstallPackage{}, "", ErrRunnerConfiguration
	}
	root := m.stageRoot()
	// Recovery discovers stages from disk after a reboot. Validate the stage
	// root itself before ReadDir so a swapped-in symlink or writable directory
	// cannot steer helper selection or cleanup.
	if err := validateSecureJournalDir(root, m.Journal.ExpectedUID); err != nil {
		return InstallPackage{}, "", err
	}
	if m.StagePath != "" {
		stage := filepath.Clean(m.StagePath)
		if !pathWithin(stage, filepath.Clean(root)) || !manifestHashPattern.MatchString(filepath.Base(stage)) {
			return InstallPackage{}, "", errors.New("persistent install stage path is outside the protected stage root")
		}
		pkg, err := validatePersistentStage(stage, m.Journal.ExpectedUID)
		if err == nil && filepath.Base(stage) != pkg.ManifestSHA256 {
			return InstallPackage{}, stage, errors.New("persistent install stage name does not match its manifest")
		}
		return pkg, stage, err
	}
	if m.StageManifestSHA256 != "" {
		if !manifestHashPattern.MatchString(m.StageManifestSHA256) {
			return InstallPackage{}, "", errors.New("persistent install stage manifest binding is invalid")
		}
		stage := filepath.Join(root, m.StageManifestSHA256)
		pkg, err := validatePersistentStage(stage, m.Journal.ExpectedUID)
		if err == nil && filepath.Base(stage) != pkg.ManifestSHA256 {
			return InstallPackage{}, stage, errors.New("persistent install stage name does not match its manifest")
		}
		return pkg, stage, err
	}
	// The rebooted recovery process does not retain in-memory manager fields.
	// Select the one stage whose manifest digest is self-consistent and whose
	// package hash matches the trampoline binding, rejecting ambiguity.
	entries, err := os.ReadDir(root)
	if err != nil {
		return InstallPackage{}, "", err
	}
	var found InstallPackage
	var foundPath string
	var candidatePath string
	var candidateErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Only a manifest-digest basename can be an install stage. Unknown
		// private directories are ignored rather than allowing an unrelated
		// directory to steer recovery or cleanup.
		if !manifestHashPattern.MatchString(entry.Name()) {
			continue
		}
		stage := filepath.Join(root, entry.Name())
		pkg, validateErr := validatePersistentStage(stage, m.Journal.ExpectedUID)
		if validateErr != nil {
			// Keep one malformed candidate's path so recoverPrepared can still
			// distinguish a valid helper plus damaged payload (rollback) from a
			// missing/mismatched helper (fence). A rebooted process otherwise has
			// no in-memory StagePath to retain this evidence.
			if candidatePath != "" {
				return InstallPackage{}, "", errors.New("multiple persistent install stages are present")
			}
			candidatePath, candidateErr = stage, validateErr
			continue
		}
		if filepath.Base(stage) != pkg.ManifestSHA256 {
			continue
		}
		if foundPath != "" {
			return InstallPackage{}, "", errors.New("multiple persistent install stages are present")
		}
		found, foundPath = pkg, stage
	}
	if foundPath != "" && candidatePath != "" {
		return InstallPackage{}, "", errors.New("multiple persistent install stages are present")
	}
	if foundPath == "" && candidatePath != "" {
		return InstallPackage{}, candidatePath, candidateErr
	}
	if foundPath == "" {
		return InstallPackage{}, "", os.ErrNotExist
	}
	m.StageManifestSHA256 = found.ManifestSHA256
	m.StagePath = foundPath
	return found, foundPath, nil
}

func validatePersistentStage(stage string, expectedUID ...uint32) (InstallPackage, error) {
	if stage == "" || !filepath.IsAbs(stage) || filepath.Clean(stage) != stage {
		return InstallPackage{}, errors.New("persistent install stage path is invalid")
	}
	pkg, err := validatePersistentInstallPackage(stage)
	if err != nil {
		return InstallPackage{}, err
	}
	if len(expectedUID) == 0 {
		return pkg, nil
	}
	uid := expectedUID[0]
	checkOwner := func(path string) error {
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}
		if got, ok := journalUID(info); ok && got != uid {
			return errors.New("persistent install stage member owner is not authorized")
		}
		return nil
	}
	if err := checkOwner(stage); err != nil {
		return InstallPackage{}, err
	}
	if err := checkOwner(filepath.Join(stage, "manifest.sha256")); err != nil {
		return InstallPackage{}, err
	}
	for member := range pkg.Members {
		if err := checkOwner(filepath.Join(stage, filepath.FromSlash(member))); err != nil {
			return InstallPackage{}, err
		}
	}
	return pkg, nil
}

func (m *InstallManager) restoreNonDispatcher(ctx context.Context) error {
	if m == nil {
		return ErrRunnerConfiguration
	}
	if m.RestoreSources != nil {
		return invokeMutation(ctx, m.RestoreSources)
	}
	if m.SourcePlan != nil {
		return m.SourcePlan.RestoreSources(ctx)
	}
	return nil
}

func (m *InstallManager) restoreDispatcher(ctx context.Context) error {
	if m == nil {
		return ErrRunnerConfiguration
	}
	if m.RemoveDispatcher != nil {
		return invokeMutation(ctx, m.RemoveDispatcher)
	}
	if m.SourcePlan != nil {
		return m.SourcePlan.RemoveDispatcher(ctx)
	}
	return nil
}

func (m *InstallManager) disableSupervisor(ctx context.Context) error {
	if m == nil {
		return ErrRunnerConfiguration
	}
	if m.DisableSupervisor != nil {
		return invokeMutation(ctx, m.DisableSupervisor)
	}
	if m.SourcePlan != nil {
		return m.SourcePlan.DisableSupervisor(ctx)
	}
	return nil
}

func (m *InstallManager) disableSources(ctx context.Context) error {
	if m == nil {
		return ErrRunnerConfiguration
	}
	if m.DisableSources != nil {
		return invokeMutation(ctx, m.DisableSources)
	}
	if m.SourcePlan != nil {
		return m.SourcePlan.DisableSources(ctx)
	}
	return nil
}

func (m *InstallManager) removePersistentStage() error {
	if m == nil {
		return nil
	}
	if m.StagePath == "" {
		// A successor process does not retain StagePath in memory. Discover the
		// one manifest-bound stage only for package-aware managers; legacy
		// callback fixtures intentionally have no persistent package authority.
		if m.StageRoot == "" && m.PackageRoot == "" && len(m.FixedMembers) == 0 {
			return nil
		}
		_, stage, err := m.locatePersistentStage()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		m.StagePath = stage
	}
	root := filepath.Clean(m.stageRoot())
	stage := filepath.Clean(m.StagePath)
	if !pathWithin(stage, root) || !manifestHashPattern.MatchString(filepath.Base(stage)) {
		return errors.New("persistent install stage path is outside the protected stage root")
	}
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := ensureNoSymlinkComponents(root); err != nil {
		return err
	}
	if info, err := os.Lstat(stage); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("persistent install stage is not a directory")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	} else {
		return err
	}
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(stage))
}

// Install advances the coarse journal while retaining install then owner
// locks for the complete operation. packageRoot is the verified private
// transfer-stage directory; an empty root is reserved for callback-only host
// fixtures that exercise the journal without fixed package members.
func (m *InstallManager) Install(ctx context.Context, packageRoot string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.normalize(); err != nil {
		return err
	}
	root := m.PackageRoot
	if packageRoot != "" {
		root = packageRoot
	}
	installUnlock, ownerUnlock, err := lockInstallThenOwner(ctx, m.InstallLocker, m.OwnerLocker)
	if err != nil {
		return err
	}
	defer func() { _ = releaseInstallOwner(installUnlock, ownerUnlock) }()

	bootID, err := m.BootID()
	if err != nil {
		return err
	}
	if err := m.validateProfileSnapshot(); err != nil {
		return err
	}
	current, exists, err := m.Journal.Load()
	if err != nil {
		return err
	}
	if !exists && root == "" && (m.StageRoot != "" || len(m.FixedMembers) != 0) {
		return errors.New("package-aware install requires a verified package root")
	}
	if !exists && m.needsInstallSourceComposition() {
		if err := m.composeInstallSourcePlan(); err != nil {
			return err
		}
		if err := m.normalize(); err != nil {
			return err
		}
	}
	if err := m.stopAndProveAgent(ctx); err != nil {
		return err
	}
	if exists {
		if err := m.bindJournalRecord(current); err != nil {
			return err
		}
		switch current.State {
		case InstallStateTerminal, InstallStateRestored:
			return ErrRebootRequested
		case InstallStatePrepared, InstallStateInstalled:
			// Resume the exact durable transition. The helper/payload validation
			// happens before any fixed path is touched.
			return m.completeInstallLocked(ctx, current, bootID)
		default:
			return ErrInstallNotTerminal
		}
	}

	pkg, stage, err := m.preparePersistentStage(ctx, root)
	if err != nil {
		return err
	}
	if m.SourcePlan != nil {
		// Stage first so a retry after a crash immediately after trampoline
		// publication can bind the dynamic payload before Prepare verifies the
		// already-published dispatcher. Staging is private and cannot launch
		// anything, so it is safe to precede the backup capture.
		if err := m.SourcePlan.Prepare(ctx); err != nil {
			return err
		}
		m.Sources = cloneSourceRecords(m.SourcePlan.Sources)
		m.Inventory.StartSources = cloneSourceRecords(m.SourcePlan.Sources)
	}
	if stage == "" && m.SourcePlan != nil {
		// Keep the static fixture payload and its canonical hash when there is
		// no package root. Real installations always take the dynamic branch.
		data := m.SourcePlan.trampolineBytes()
		digest := sha256.Sum256(data)
		for index := range m.SourcePlan.Sources {
			if m.SourcePlan.Sources[index].DisabledState == "approved_trampoline" {
				m.SourcePlan.Sources[index].DisabledSHA256 = hex.EncodeToString(digest[:])
			}
		}
		m.Sources = cloneSourceRecords(m.SourcePlan.Sources)
		m.Inventory.StartSources = cloneSourceRecords(m.SourcePlan.Sources)
	}
	if stage != "" {
		if err := m.failureHook("before-pre-journal-authority"); err != nil {
			return err
		}
		if err := m.persistPreJournalRecoveryAuthority(stage); err != nil {
			return err
		}
		if err := m.failureHook("after-pre-journal-authority"); err != nil {
			return err
		}
	}
	if err := m.failureHook("before-trampoline-publication"); err != nil {
		return err
	}
	if err := invokeMutation(ctx, m.InstallTrampoline); err != nil {
		return err
	}
	if err := m.failureHook("after-trampoline-publication"); err != nil {
		return err
	}
	// Package staging and pre-journal source work can outlast the initial
	// agent proof. Reconcile once more immediately before publishing the
	// prepared checkpoint so a replacement agent cannot survive into Prepare.
	if err := m.stopAndProveAgent(ctx); err != nil {
		return err
	}
	record := InstallJournalRecord{Schema: 1, State: InstallStatePrepared, InstallBootID: bootID, PackageSHA256: m.PackageSHA256, PreviousConfigSHA256: m.PreviousConfigSHA256, Inventory: m.Inventory, Sources: cloneSourceRecords(m.Sources)}
	if err := m.replaceJournalCheckpoint(record, InstallStatePrepared); err != nil {
		return err
	}
	if stage != "" {
		if err := removePreJournalRecoveryAuthority(m.preJournalAuthorityPath, m.Journal.ExpectedUID); err != nil {
			return err
		}
		m.preJournalAuthority = nil
	}
	_ = pkg
	return m.completeInstallLocked(ctx, record, bootID)
}

// completeInstallLocked is called only while install then owner locks are
// held. It advances prepared -> installed -> terminal and leaves the current
// checkpoint intact on every error so the next boot can retry or roll back.
func (m *InstallManager) completeInstallLocked(ctx context.Context, record InstallJournalRecord, bootID string) error {
	var pkg InstallPackage
	var stage string
	var err error
	if m.StagePath != "" || m.StageRoot != "" || m.PackageRoot != "" {
		pkg, stage, err = m.locatePersistentStage()
		if err != nil {
			// A package-root-free compatibility fixture has no stage. A journal
			// created by a real package install never takes this branch.
			if m.PackageRoot != "" || m.StagePath != "" || m.StageManifestSHA256 != "" {
				return err
			}
		}
	}
	if record.State == InstallStatePrepared {
		if stage != "" {
			if err := m.installFixedMembers(pkg, stage); err != nil {
				return err
			}
		}
		if m.InstallSupervisor != nil {
			if err := invokeMutation(ctx, m.InstallSupervisor); err != nil {
				return err
			}
		} else if m.SourcePlan != nil {
			if err := m.SourcePlan.InstallSupervisor(ctx); err != nil {
				return err
			}
		}
		if err := m.failureHook("before-owner-initialization"); err != nil {
			return err
		}
		if m.OwnerInitializer != nil {
			if err := invokeMutation(ctx, m.OwnerInitializer); err != nil {
				return err
			}
		} else if m.OwnerStore != nil {
			if err := m.initializeOwnerHeld(ctx, bootID); err != nil {
				return err
			}
		}
		record.State = InstallStateInstalled
		if err := m.replaceJournalCheckpoint(record, InstallStateInstalled); err != nil {
			return err
		}
	}
	if record.State == InstallStateInstalled {
		if err := m.disableSources(ctx); err != nil {
			return err
		}
		record.State = InstallStateTerminal
		if err := m.replaceJournalCheckpoint(record, InstallStateTerminal); err != nil {
			return err
		}
	}
	if err := m.requestReboot(ctx); err != nil {
		return err
	}
	return ErrRebootRequested
}

func (m *InstallManager) initializeOwnerHeld(ctx context.Context, bootID string) error {
	if err := m.observeHealthyMain(ctx); err != nil {
		return err
	}
	record, exists, err := m.OwnerStore.Load()
	if err != nil {
		return err
	}
	if !exists {
		if m.MainObserver == nil {
			return hardwareowner.ErrOwnerRecordAbsent
		}
		return m.writeInitialOwner(bootID)
	}
	if err := record.Validate(); err != nil || record.BootID != bootID || record.State != hardwareowner.StateNormalMain {
		return ErrInstallOwnerInvalid
	}
	return nil
}

func (m *InstallManager) writeInitialOwner(bootID string) error {
	if m.OwnerStore == nil {
		return ErrRunnerConfiguration
	}
	session, err := randomSession()
	if err != nil {
		return err
	}
	record := hardwareowner.Record{
		Schema: hardwareowner.SchemaVersion, State: hardwareowner.StateNormalMain,
		BootID: bootID, GenerationHighWater: 1, ActiveSession: session,
		ActiveGeneration: 1, ActiveMode: hardwareowner.ModeFPGANative,
		ActiveOwner: hardwareowner.OwnerCompatMain, ActiveLeases: hardwareowner.NormalLeases(),
		CandidateMode: hardwareowner.ModeNone, CandidateOwner: hardwareowner.OwnerNone,
		QuiescingOwner: hardwareowner.OwnerNone, CandidateSession: "", CandidateGeneration: 0,
		RequestedResources: []string{}, RunID: "", Phase: "", FirstFailure: "",
	}
	return m.OwnerStore.Replace(record)
}

func (m *InstallManager) stopAndProveAgent(ctx context.Context) error {
	if m.StopAgent != nil {
		if err := invokeMutation(ctx, m.StopAgent); err != nil {
			return err
		}
	}
	if m.ProveAgentAbsent != nil {
		return invokeReadiness(ctx, m.ProveAgentAbsent)
	}
	return nil
}

// validateProfileSnapshot binds a composed manager to the protected config
// bytes at the point where install is about to mutate launch state. The
// constructor performs the initial and final composition reads; this check
// closes the interval between composition and command execution.
func (m *InstallManager) validateProfileSnapshot() error {
	if m == nil || m.ProfilePath == "" {
		return nil
	}
	_, digest, err := LoadProtectedAgentConfig(m.ProfilePath)
	if err != nil {
		return err
	}
	if digest != m.PreviousConfigSHA256 {
		return errors.New("protected agent config changed after composition")
	}
	return nil
}

type retainedInstallLocker interface {
	LockFile(context.Context) (*os.File, hardwareowner.Unlock, error)
}

// installLockPath returns the path bound to the concrete locker. Custom
// ownerLocker implementations remain valid for host state-machine fixtures;
// production lock-fd validation falls back to the fixed protected path.
func (m *InstallManager) installLockPath() string {
	if m != nil {
		if locker, ok := m.InstallLocker.(*hardwareowner.Locker); ok && locker.Path != "" {
			return locker.Path
		}
	}
	return InstallLockPath
}

// acquireRecoveryLock retains the exact flock descriptor until supervisor
// exec. A normal callback-only fixture may use the narrow Lock interface, but
// an actual supervisor handoff is rejected unless the descriptor-retaining
// form is available.
func (m *InstallManager) acquireRecoveryLock(ctx context.Context) (int, hardwareowner.Unlock, error) {
	if m == nil || m.InstallLocker == nil {
		return -1, nil, ErrRunnerConfiguration
	}
	if m.InheritedLockFD > 0 {
		fd := m.InheritedLockFD
		if err := validateInheritedInstallLockFD(fd, m.installLockPath()); err != nil {
			return -1, nil, err
		}
		var once sync.Once
		var closeErr error
		return fd, func() error {
			once.Do(func() { closeErr = unix.Close(fd) })
			return closeErr
		}, nil
	}
	if locker, ok := m.InstallLocker.(retainedInstallLocker); ok {
		file, unlock, err := locker.LockFile(ctx)
		if err != nil {
			if unlock != nil {
				return -1, nil, errors.Join(err, unlock())
			}
			return -1, nil, err
		}
		if file == nil || unlock == nil {
			if unlock != nil {
				_ = unlock()
			}
			if file != nil {
				_ = file.Close()
			}
			return -1, nil, ErrRunnerConfiguration
		}
		return int(file.Fd()), unlock, nil
	}
	unlock, err := m.InstallLocker.Lock(ctx)
	if err != nil {
		if unlock != nil {
			return -1, nil, errors.Join(err, unlock())
		}
		return -1, nil, err
	}
	if unlock == nil {
		return -1, nil, ErrRunnerConfiguration
	}
	return -1, unlock, nil
}

func clearCloseOnExec(fd int) error {
	if fd < 0 {
		return errors.New("supervisor handoff has no retained lock fd")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		return fmt.Errorf("inspect retained install lock flags: %w", err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		return nil
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, flags&^unix.FD_CLOEXEC); err != nil {
		return fmt.Errorf("clear retained install lock close-on-exec: %w", err)
	}
	return nil
}

func (m *InstallManager) handoffSupervisor(ctx context.Context, fd int) error {
	if m == nil {
		return ErrRunnerConfiguration
	}
	if m.ExecSupervisor == nil && m.SupervisorExecutable == "" {
		// A terminal journal means the boot trampoline has completed the
		// install mutation. It must hand the retained install lock to the
		// reviewed supervisor; silently returning would release the lock and
		// leave the machine with no protected owner.
		return ErrRunnerConfiguration
	}
	if fd < 0 {
		return ErrRunnerConfiguration
	}
	if err := clearCloseOnExec(fd); err != nil {
		return err
	}
	if m.ExecSupervisor != nil {
		return m.ExecSupervisor(ctx, fd)
	}
	args := make([]string, 0, len(m.SupervisorArguments)+3)
	args = append(args, m.SupervisorExecutable, "--inherited-fd", strconv.Itoa(fd))
	args = append(args, m.SupervisorArguments...)
	// Exec replaces this recovery process. If it returns, the supervisor could
	// not be started and the deferred lock release in Recover remains active.
	return syscall.Exec(m.SupervisorExecutable, args, os.Environ())
}

func (m *InstallManager) prepareRecoveryPlan(record InstallJournalRecord, ctx context.Context) error {
	if m == nil || m.SourcePlan == nil {
		return nil
	}
	plan := m.SourcePlan
	if len(plan.Sources) == 0 {
		plan.Sources = cloneSourceRecords(record.Sources)
	}
	if len(m.Sources) == 0 {
		m.Sources = cloneSourceRecords(record.Sources)
	}
	if len(m.Sources) != len(record.Sources) {
		return errors.New("recovery source set differs from journal")
	}
	for index := range record.Sources {
		if m.Sources[index] != record.Sources[index] || plan.Sources[index] != record.Sources[index] {
			return errors.New("recovery source set differs from journal")
		}
	}
	if plan.BackupDir == "" {
		plan.BackupDir = m.Journal.BackupDir
		if plan.BackupDir == "" {
			plan.BackupDir = filepath.Join(filepath.Dir(m.Journal.Path), "fpgadev-install-v1-backups")
		}
	}
	if err := m.bindSourcePlan(); err != nil {
		return err
	}
	return plan.Prepare(ctx)
}

func (m *InstallManager) sourcePlanFullyRestored() bool {
	if m == nil || m.SourcePlan == nil {
		return false
	}
	plan := m.SourcePlan
	if plan.Supervisor == "" || plan.Dispatcher == "" {
		return false
	}
	if _, err := os.Lstat(plan.Supervisor); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	for _, source := range plan.Sources {
		data, err := readProtectedBackup(source.BackupPath, plan.ExpectedUID)
		if err != nil || verifyProtectedBytes(source.Path, data, source.Mode, plan.ExpectedUID) != nil {
			return false
		}
	}
	return true
}

// Recover executes the exact next-boot table. It never starts Main or the
// agent for a nonterminal state; only terminal hands off to the sole
// supervisor with the retained install-lock descriptor.
func (m *InstallManager) Recover(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil || m.Journal == nil || m.InstallLocker == nil {
		return ErrRunnerConfiguration
	}
	// The trampoline itself owns the install lock for the whole decision. A
	// terminal record hands that descriptor to the supervisor; every other
	// record acquires the owner lock only after the install lock is held. This
	// keeps recovery's lock ordering identical whether the descriptor was
	// inherited or opened by this process.
	heldFD, installUnlock, err := m.acquireRecoveryLock(ctx)
	if err != nil {
		return err
	}
	if installUnlock == nil {
		return ErrRunnerConfiguration
	}
	defer func() { _ = installUnlock() }()
	record, exists, err := m.Journal.Load()
	if err != nil {
		return err
	}
	if !exists {
		handled, authorityErr := m.recoverPreJournalAuthority(ctx)
		if authorityErr != nil {
			return authorityErr
		}
		if handled {
			return nil
		}
		return m.chainOriginal(ctx)
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if record.State == InstallStateTerminal {
		return m.handoffSupervisor(ctx, heldFD)
	}
	if m.OwnerLocker == nil {
		return ErrRunnerConfiguration
	}
	ownerUnlock, err := m.OwnerLocker.Lock(ctx)
	if err != nil {
		if ownerUnlock != nil {
			return errors.Join(err, ownerUnlock())
		}
		return err
	}
	if ownerUnlock == nil {
		return ErrRunnerConfiguration
	}
	defer func() { _ = ownerUnlock() }()
	if err := m.bindJournalRecord(record); err != nil {
		return err
	}
	return m.recoverNonterminal(ctx, record)
}

func (m *InstallManager) recoverNonterminal(ctx context.Context, record InstallJournalRecord) error {
	switch record.State {
	case InstallStatePrepared:
		return m.recoverPrepared(ctx, record)
	case InstallStateInstalled:
		if err := m.disableSources(ctx); err != nil {
			_ = m.requestReboot(ctx)
			return err
		}
		record.State = InstallStateTerminal
		if err := m.replaceJournalCheckpoint(record, InstallStateTerminal); err != nil {
			_ = m.requestReboot(ctx)
			return err
		}
		if err := m.requestReboot(ctx); err != nil {
			return err
		}
		if !m.packageAware() {
			return nil
		}
		return ErrRebootRequested
	case InstallStateUninstalling:
		return m.recoverUninstalling(ctx, record)
	case InstallStateRestored:
		if err := m.finishRestored(ctx); err != nil {
			_ = m.requestReboot(ctx)
			return err
		}
		return nil
	case InstallStateTerminal:
		// A terminal record must be handed off by the descriptor-retaining path.
		return ErrRunnerConfiguration
	default:
		return ErrInstallNotTerminal
	}
}

func (m *InstallManager) recoverPrepared(ctx context.Context, record InstallJournalRecord) error {
	if m.SourcePlan == nil {
		if err := m.bindJournalRecord(record); err != nil {
			return err
		}
	}
	if !m.packageAware() {
		return m.recoverLegacyPrepared(ctx, record)
	}
	pkg, stage, err := m.locatePersistentStage()
	if stage == "" {
		return m.fencePrepared(ctx, stage, fmt.Errorf("persistent recovery stage unavailable: %w", err))
	}
	// Retain a canonical candidate path even when package validation reports a
	// damaged non-helper member; rollback must remove that exact stage after
	// restoring backups, while helper mismatch remains fenced below.
	m.StagePath = stage
	m.StageManifestSHA256 = filepath.Base(stage)
	gotHelperHash, expectedHelperHash, helperErr := inspectStagedHelperBinding(stage, m.SourcePlan.Dispatcher, m.Journal.ExpectedUID)
	if helperErr != nil || gotHelperHash == "" || expectedHelperHash == "" || gotHelperHash != expectedHelperHash {
		if helperErr == nil {
			helperErr = errors.New("recovery helper is missing or mismatched")
		}
		return m.fencePrepared(ctx, stage, helperErr)
	}
	if err != nil {
		// The helper is the recovery authority. Once its binding is intact, any
		// other damaged member is a safe rollback rather than an attempt to run
		// a partial installation.
		return m.rollbackPrepared(ctx, record, fmt.Errorf("persistent staged payload is incomplete: %w", err))
	}
	helperHash := pkg.MemberSHA256["bin/mister-fpga-dev"]
	if helperHash == "" || helperHash != gotHelperHash {
		return m.fencePrepared(ctx, stage, errors.New("persistent recovery helper is not bound by manifest"))
	}
	trampoline := m.SourcePlan.trampolineBytes()
	if m.SourcePlan.Dispatcher != "" {
		if current, readErr := os.ReadFile(m.SourcePlan.Dispatcher); readErr == nil && bytes.Contains(current, []byte("expected='")) {
			trampoline = current
			m.SourcePlan.TrampolineBytes = append([]byte(nil), current...)
		}
	}
	if !bytes.Contains(trampoline, []byte(helperHash)) || !bytes.Contains(trampoline, []byte(stage)) {
		return m.fencePrepared(ctx, stage, errors.New("recovery trampoline binding does not match staged helper"))
	}
	if m.PackageRoot != "" {
		// The transfer directory is not a recovery authority. Validate the
		// persistent stage itself and do not reopen packageRoot here.
		m.PackageRoot = ""
	}
	if err := m.installFixedMembers(pkg, stage); err != nil {
		return m.rollbackPrepared(ctx, record, err)
	}
	if m.InstallSupervisor != nil {
		if err := invokeMutation(ctx, m.InstallSupervisor); err != nil {
			return m.rollbackPrepared(ctx, record, err)
		}
	}
	record.State = InstallStateInstalled
	if err := m.replaceJournalCheckpoint(record, InstallStateInstalled); err != nil {
		return err
	}
	if err := m.disableSources(ctx); err != nil {
		_ = m.requestReboot(ctx)
		return err
	}
	record.State = InstallStateTerminal
	if err := m.replaceJournalCheckpoint(record, InstallStateTerminal); err != nil {
		_ = m.requestReboot(ctx)
		return err
	}
	if err := m.requestReboot(ctx); err != nil {
		return err
	}
	return ErrRebootRequested
}

func (m *InstallManager) packageAware() bool {
	return m != nil && (m.StageRoot != "" || m.StagePath != "" || m.StageManifestSHA256 != "" || m.PackageRoot != "" || len(m.FixedMembers) != 0)
}

func (m *InstallManager) recoverLegacyPrepared(ctx context.Context, record InstallJournalRecord) error {
	if err := m.installTrampolineIfNeeded(ctx); err != nil {
		return err
	}
	if m.InstallSupervisor != nil {
		if err := invokeMutation(ctx, m.InstallSupervisor); err != nil {
			return err
		}
	}
	record.State = InstallStateInstalled
	if err := m.replaceJournalCheckpoint(record, InstallStateInstalled); err != nil {
		return err
	}
	if err := m.disableSources(ctx); err != nil {
		return err
	}
	record.State = InstallStateTerminal
	if err := m.replaceJournalCheckpoint(record, InstallStateTerminal); err != nil {
		return err
	}
	if err := m.requestReboot(ctx); err != nil {
		return err
	}
	return nil
}

func (m *InstallManager) installTrampolineIfNeeded(ctx context.Context) error {
	if m == nil {
		return ErrRunnerConfiguration
	}
	if m.InstallTrampoline != nil {
		return invokeMutation(ctx, m.InstallTrampoline)
	}
	if m.SourcePlan != nil {
		return m.SourcePlan.InstallTrampoline(ctx)
	}
	return nil
}

func inspectStagedHelperBinding(stage, dispatcher string, expectedUID uint32) (string, string, error) {
	helperPath := filepath.Join(stage, "bin", "mister-fpga-dev")
	if err := ensureNoSymlinkComponents(filepath.Dir(helperPath)); err != nil {
		return "", "", err
	}
	info, err := os.Lstat(helperPath)
	if err != nil {
		return "", "", err
	}
	if err := validatePackageFileInfo(info, 0o755); err != nil {
		return "", "", err
	}
	if uid, ok := journalUID(info); ok && uid != expectedUID {
		return "", "", errors.New("recovery helper owner does not match protected uid")
	}
	helper, err := readRegularFileNoFollow(helperPath, info)
	if err != nil {
		return "", "", err
	}
	helperDigest := sha256.Sum256(helper)
	got := hex.EncodeToString(helperDigest[:])
	if dispatcher == "" {
		return got, "", errors.New("approved dispatcher is unavailable")
	}
	dispatcherInfo, err := os.Lstat(dispatcher)
	if err != nil {
		return got, "", err
	}
	if err := validatePackageFileInfo(dispatcherInfo, 0o755); err != nil {
		return got, "", err
	}
	if uid, ok := journalUID(dispatcherInfo); ok && uid != expectedUID {
		return got, "", errors.New("recovery trampoline owner does not match protected uid")
	}
	dispatcherBytes, err := readRegularFileNoFollow(dispatcher, dispatcherInfo)
	if err != nil {
		return got, "", err
	}
	const marker = "expected='"
	start := bytes.Index(dispatcherBytes, []byte(marker))
	if start < 0 {
		return got, "", errors.New("recovery trampoline has no embedded helper hash")
	}
	start += len(marker)
	endRel := bytes.IndexByte(dispatcherBytes[start:], '\'')
	if endRel <= 0 {
		return got, "", errors.New("recovery trampoline helper hash is malformed")
	}
	expected := string(dispatcherBytes[start : start+endRel])
	if !manifestHashPattern.MatchString(expected) {
		return got, "", errors.New("recovery trampoline helper hash is not canonical")
	}
	return got, expected, nil
}

func (m *InstallManager) fencePrepared(ctx context.Context, stage string, cause error) error {
	var diagnosticErr error
	if diagnostic := m.diagnosticPath(stage); diagnostic != "" {
		diagnosticErr = writeRecoveryDiagnostic(diagnostic, cause.Error())
	}
	// Missing/mismatched helper is deliberately fenced without invoking a
	// reboot or any child starter. The operator replaces the verified package
	// and power-cycles the disposable target.
	return errors.Join(cause, diagnosticErr)
}

func (m *InstallManager) rollbackPrepared(ctx context.Context, record InstallJournalRecord, cause error) error {
	if err := m.disableSupervisor(ctx); err != nil {
		_ = m.requestReboot(ctx)
		return errors.Join(cause, err)
	}
	if err := m.restoreNonDispatcher(ctx); err != nil {
		_ = m.requestReboot(ctx)
		return errors.Join(cause, err)
	}
	record.State = InstallStateRestored
	if err := m.replaceJournalCheckpoint(record, InstallStateRestored); err != nil {
		_ = m.requestReboot(ctx)
		return errors.Join(cause, err)
	}
	if err := m.restoreDispatcher(ctx); err != nil {
		_ = m.requestReboot(ctx)
		return errors.Join(cause, err)
	}
	if err := m.removePersistentStage(); err != nil {
		_ = m.requestReboot(ctx)
		return errors.Join(cause, err)
	}
	_ = m.requestReboot(ctx)
	return errors.Join(cause, ErrRebootRequested)
}

func (m *InstallManager) recoverUninstalling(ctx context.Context, record InstallJournalRecord) error {
	if err := m.disableSupervisor(ctx); err != nil {
		_ = m.requestReboot(ctx)
		return err
	}
	if err := m.restoreNonDispatcher(ctx); err != nil {
		_ = m.requestReboot(ctx)
		return err
	}
	record.State = InstallStateRestored
	if err := m.replaceJournalCheckpoint(record, InstallStateRestored); err != nil {
		_ = m.requestReboot(ctx)
		return err
	}
	if err := m.finishRestored(ctx); err != nil {
		_ = m.requestReboot(ctx)
		return err
	}
	return nil
}

func (m *InstallManager) finishRestored(ctx context.Context) error {
	if err := m.failureHook("before-dispatcher-restoration"); err != nil {
		return err
	}
	if err := m.restoreDispatcher(ctx); err != nil {
		return err
	}
	if err := m.failureHook("after-dispatcher-restoration"); err != nil {
		return m.retainRestoredRetryTrampoline(ctx, err)
	}
	if err := m.failureHook("before-stage-removal"); err != nil {
		return m.retainRestoredRetryTrampoline(ctx, err)
	}
	if err := m.removePersistentStage(); err != nil {
		return m.retainRestoredRetryTrampoline(ctx, err)
	}
	if err := m.failureHook("after-stage-removal"); err != nil {
		return err
	}
	if m.preJournalAuthorityPath != "" && m.Journal != nil {
		if err := removePreJournalRecoveryAuthority(m.preJournalAuthorityPath, m.Journal.ExpectedUID); err != nil {
			return err
		}
		m.preJournalAuthority = nil
	}
	// This is deliberately the final operation. Production ChainOriginal is a
	// verified syscall.Exec and must never be followed by another mutation.
	return m.chainOriginal(ctx)
}

// retainRestoredRetryTrampoline re-arms the verified recovery trampoline when
// a failure occurs after the original dispatcher was restored but while the
// persistent stage still exists. The durable journal is already restored, so
// the next boot can retry finishRestored; if stage removal physically
// completed before only its directory fsync failed, leaving the original
// dispatcher in place is safe and no retry authority is needed.
func (m *InstallManager) retainRestoredRetryTrampoline(ctx context.Context, cause error) error {
	if m == nil || m.SourcePlan == nil {
		return cause
	}
	stage := m.StagePath
	if m.packageAware() {
		// A successor manager has no in-memory trampoline payload. Reconstruct
		// the exact published bytes from the durable stage and journal binding
		// before asking the source plan to re-arm the dispatcher. A static
		// fallback would lose the helper/stage authority and could not safely
		// resume finishRestored after this failure seam.
		pkg, discovered, locateErr := m.locatePersistentStage()
		if discovered != "" {
			stage = discovered
		}
		if locateErr != nil {
			if errors.Is(locateErr, os.ErrNotExist) {
				return cause
			}
			return errors.Join(cause, locateErr)
		}
		if stage == "" {
			return cause
		}
		helperHash := pkg.MemberSHA256["bin/mister-fpga-dev"]
		if !manifestHashPattern.MatchString(helperHash) {
			return errors.Join(cause, errors.New("persistent recovery helper hash is unavailable for restored retry"))
		}
		trampoline := BuildRecoveryTrampoline(stage, helperHash, m.diagnosticPath(stage))
		expectedHash := ""
		for _, source := range m.SourcePlan.Sources {
			if source.Path == m.SourcePlan.Dispatcher && source.DisabledState == "approved_trampoline" {
				expectedHash = source.DisabledSHA256
				break
			}
		}
		if !manifestHashPattern.MatchString(expectedHash) || hashBytes(trampoline) != expectedHash {
			return errors.Join(cause, errors.New("restored retry trampoline is not bound by the durable journal"))
		}
		m.SourcePlan.TrampolineBytes = trampoline
	}
	if stage == "" {
		return cause
	}
	if _, err := os.Lstat(stage); errors.Is(err, os.ErrNotExist) {
		return cause
	} else if err != nil {
		return errors.Join(cause, err)
	}
	if err := m.SourcePlan.InstallTrampoline(ctx); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func writeRecoveryDiagnostic(path, detail string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("recovery diagnostic path is invalid")
	}
	if err := ensureNoSymlinkComponents(filepath.Dir(path)); err != nil {
		return err
	}
	return atomicReplaceProtected(path, []byte(detail+"\n"), 0o600)
}

// Uninstall keeps the recovery trampoline earliest until the restored journal
// has been durably committed. Every operation is idempotent so a successor
// boot can resume after any seam.
func (m *InstallManager) Uninstall(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil || m.Journal == nil || m.InstallLocker == nil || m.OwnerLocker == nil {
		return ErrRunnerConfiguration
	}
	if err := m.bindSourcePlan(); err != nil {
		return err
	}
	installUnlock, ownerUnlock, err := lockInstallThenOwner(ctx, m.InstallLocker, m.OwnerLocker)
	if err != nil {
		return err
	}
	defer func() { _ = releaseInstallOwner(installUnlock, ownerUnlock) }()
	record, exists, err := m.Journal.Load()
	if err != nil {
		return err
	}
	if !exists {
		return ErrInstallNotTerminal
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if err := m.bindJournalRecord(record); err != nil {
		return err
	}
	if m.OwnerStore != nil {
		owner, ownerExists, ownerErr := m.OwnerStore.Load()
		if ownerErr != nil {
			return ownerErr
		}
		if !ownerExists || owner.Validate() != nil || owner.State != hardwareowner.StateNormalMain {
			return ErrInstallOwnerInvalid
		}
		bootID, bootErr := m.BootID()
		if bootErr != nil || owner.BootID != bootID {
			return ErrInstallOwnerInvalid
		}
	}
	switch record.State {
	case InstallStateTerminal:
		record.State = InstallStateUninstalling
		if err := m.replaceJournalCheckpoint(record, InstallStateUninstalling); err != nil {
			return err
		}
	case InstallStateUninstalling:
		// Continue the durable intent after a crash.
	case InstallStateRestored:
		return m.finishRestored(ctx)
	default:
		return ErrInstallNotTerminal
	}
	if err := m.failureHook("before-supervisor-disable"); err != nil {
		return err
	}
	if err := m.disableSupervisor(ctx); err != nil {
		return err
	}
	if err := m.failureHook("after-supervisor-disable"); err != nil {
		return err
	}
	if err := m.restoreNonDispatcher(ctx); err != nil {
		return err
	}
	record.State = InstallStateRestored
	if err := m.replaceJournalCheckpoint(record, InstallStateRestored); err != nil {
		return err
	}
	return m.finishRestored(ctx)
}

func invokeMutation(ctx context.Context, value any) error {
	if value == nil {
		return nil
	}
	if fn, ok := value.(func(context.Context) error); ok {
		return fn(ctx)
	}
	if fn, ok := value.(func() error); ok {
		return fn()
	}
	return invokeReflect(ctx, value)
}

// Keep the public helper useful in tiny command tests while avoiding an
// accidental compile-time dependency on target-only process types.
func CurrentProcessIdentity() (uint64, uint64) {
	return uint64(os.Getpid()), uint64(time.Now().UnixNano())
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
