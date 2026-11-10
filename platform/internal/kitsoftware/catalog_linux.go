package kitsoftware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/DeanoC/FogCast/appliance/software"
)

const catalogRecordLimit = 64 << 10

// CatalogTransitions uses the existing software worker lock. The caller must
// positively verify hostRoot as persistent FESDATA3 storage before opening it,
// and call Apply/Recover only after the controller has stopped the whole group.
// The SQLite helper belongs to an authenticated, retained repaired-image factory
// release; legacy candidate and baseline bundles need not contain that helper.
type CatalogTransitions struct {
	repository                       *FileRepository
	root, hostRoot, database, helper string
	resolve                          func(context.Context, string) (string, error)
	identities                       catalogDirectories
	journalIdentity                  catalogDirectory
	// command is instance-local injection for filesystem/journal tests.
	command func(context.Context, string, []string) (catalogImage, error)
}

type catalogDirectory struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type catalogDirectories struct {
	Root  catalogDirectory `json:"root"`
	State catalogDirectory `json:"state"`
}

// catalogImage is the helper's stopped, checkpointed SQLite image identity.
type catalogImage struct {
	Present bool   `json:"present"`
	Schema  int    `json:"schema"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type catalogRecord struct {
	Version  int     `json:"version"`
	Sequence uint64  `json:"sequence"`
	Request  Request `json:"request"`
	Previous string  `json:"previous"`
	// Source is the verified bundle that actually ran before Stop. A held-back
	// selection may have run the image's factory instead of Previous.
	Source      string             `json:"source"`
	HostRoot    string             `json:"host_root"`
	Directories catalogDirectories `json:"directories"`
	Helper      string             `json:"helper"`
	// preparing cannot have changed logical catalog contents: only an inspect
	// or WAL checkpoint was permitted. prepared is durable before any restore
	// or candidate startup, so recovery must restore its before snapshot.
	Phase          string       `json:"phase"`
	Before         catalogImage `json:"before"`
	Target         string       `json:"target,omitempty"`
	RecoverySource catalogImage `json:"recovery_source"`
}

// OpenCatalogTransitions opens a journal adjacent to the durable software
// repository. resolve authenticates the complete release for each requested
// helper ID and returns its root. It must also resolve retained older helpers.
func OpenCatalogTransitions(repository *FileRepository, hostRoot, helper string, resolve func(context.Context, string) (string, error)) (*CatalogTransitions, error) {
	if repository == nil || resolve == nil || !software.ValidID(helper) || !filepath.IsAbs(hostRoot) || filepath.Clean(hostRoot) != hostRoot {
		return nil, errors.New("invalid kit catalog transition configuration")
	}
	if err := repository.check(); err != nil {
		return nil, err
	}
	repoInfo, err := repository.dir.Stat()
	if err != nil {
		return nil, err
	}
	repoStat, ok := repoInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.New("software repository filesystem identity unavailable")
	}
	if err := catalogRealAncestors(hostRoot); err != nil {
		return nil, err
	}
	parentInfo, err := os.Stat(filepath.Dir(hostRoot))
	if err != nil {
		return nil, err
	}
	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || parentStat.Dev != repoStat.Dev {
		return nil, errors.New("kit catalog parent is outside the software repository filesystem")
	}
	// Do not create unverified ancestors. The caller has already mounted and
	// verified FESDATA3, and the software repository is on that same volume.
	for _, path := range []string{hostRoot, filepath.Join(hostRoot, "state")} {
		if err := catalogPrivateDirectory(path); err != nil {
			return nil, err
		}
	}
	identities, err := catalogRootIdentities(hostRoot)
	if err != nil {
		return nil, err
	}
	if uint64(repoStat.Dev) != identities.Root.Device {
		return nil, errors.New("kit catalog is outside the software repository filesystem")
	}
	root := filepath.Join(repository.root, "catalog-transitions")
	if err := catalogPrivateDirectory(root); err != nil {
		return nil, err
	}
	journalIdentity, err := catalogDirectoryIdentity(root)
	if err != nil {
		return nil, err
	}
	m := &CatalogTransitions{repository: repository, root: root, hostRoot: hostRoot,
		database: filepath.Join(hostRoot, "state", "library.sqlite3"), helper: helper,
		resolve: resolve, identities: identities, journalIdentity: journalIdentity}
	return m, nil
}

func catalogPrivateDirectory(path string) error {
	if err := catalogRealAncestors(path); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("catalog journal requires a private real directory: %s", path)
	}
	return catalogSyncDirectory(filepath.Dir(path))
}

func catalogRealAncestors(path string) error {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("kit catalog ancestor is not a real directory")
		}
		if parent == string(filepath.Separator) {
			break
		}
	}
	return nil
}

func catalogDirectoryIdentity(path string) (catalogDirectory, error) {
	if err := catalogRealAncestors(path); err != nil {
		return catalogDirectory{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return catalogDirectory{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return catalogDirectory{}, errors.New("kit catalog root is not a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return catalogDirectory{}, errors.New("kit catalog directory identity unavailable")
	}
	return catalogDirectory{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func catalogRootIdentities(root string) (catalogDirectories, error) {
	a, err := catalogDirectoryIdentity(root)
	if err != nil {
		return catalogDirectories{}, err
	}
	b, err := catalogDirectoryIdentity(filepath.Join(root, "state"))
	if err != nil {
		return catalogDirectories{}, err
	}
	if a.Device != b.Device {
		return catalogDirectories{}, errors.New("kit catalog state is on a different filesystem")
	}
	return catalogDirectories{Root: a, State: b}, nil
}

func (m *CatalogTransitions) check(op Operation, phase string) error {
	if err := m.repository.check(); err != nil {
		return err
	}
	journal, err := catalogDirectoryIdentity(m.root)
	if err != nil {
		return err
	}
	if journal != m.journalIdentity {
		return errors.New("catalog journal storage identity changed")
	}
	now, err := catalogRootIdentities(m.hostRoot)
	if err != nil {
		return err
	}
	if now != m.identities {
		return errors.New("kit catalog storage identity changed")
	}
	s, err := m.repository.Read()
	if err != nil {
		return err
	}
	p := s.Pending
	if p == nil || p.Request != op.Request || p.Previous != op.Previous || p.Phase != phase || s.Generation != op.Generation {
		return ErrConflict
	}
	return nil
}

func (m *CatalogTransitions) opRoot(id string) string   { return filepath.Join(m.root, id) }
func (m *CatalogTransitions) snapshot(id string) string { return filepath.Join(m.opRoot(id), "before") }

func (m *CatalogTransitions) read(id string) (catalogRecord, error) {
	var record catalogRecord
	if !software.ValidID(id) {
		return record, errors.New("invalid catalog operation identity")
	}
	if _, err := catalogDirectoryIdentity(m.opRoot(id)); err != nil {
		return record, err
	}
	path := filepath.Join(m.opRoot(id), "operation.json")
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return record, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return record, err
	}
	if !info.Mode().IsRegular() || info.Size() > catalogRecordLimit || info.Mode().Perm() != 0600 {
		return record, errors.New("invalid catalog operation journal")
	}
	decoder := json.NewDecoder(io.LimitReader(f, catalogRecordLimit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return record, errors.New("trailing catalog operation journal")
	}
	if record.Version != 1 || record.Sequence == 0 || record.Request.ID != id || !software.ValidID(record.Request.Candidate) || !software.ValidID(record.Previous) || !software.ValidID(record.Source) || !software.ValidID(record.Helper) || record.HostRoot != m.hostRoot || record.Directories != m.identities {
		return record, errors.New("catalog operation journal identity mismatch")
	}
	switch record.Phase {
	case "preparing":
	case "prepared", "applied", "recovering", "recovered":
		if err := record.Before.validate(); err != nil {
			return record, err
		}
	default:
		return record, errors.New("invalid catalog operation phase")
	}
	if record.Target != "" && (!software.ValidID(record.Target) || record.Target == id) {
		return record, errors.New("invalid catalog restore target")
	}
	if record.Phase == "recovering" {
		if err := record.RecoverySource.validate(); err != nil {
			return record, err
		}
	}
	return record, nil
}

func (image catalogImage) validate() error {
	if !image.Present {
		if image.Schema != 0 || image.Size != 0 || image.SHA256 != "" {
			return errors.New("invalid absent catalog image")
		}
		return nil
	}
	if image.Schema < 0 || image.Size <= 0 || !software.ValidID(image.SHA256) {
		return errors.New("invalid catalog image identity")
	}
	return nil
}

func (m *CatalogTransitions) write(record catalogRecord, op Operation, phase string) error {
	if err := m.check(op, phase); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(data) > catalogRecordLimit {
		return errors.New("catalog operation journal too large")
	}
	root := m.opRoot(record.Request.ID)
	if err := catalogPrivateDirectory(root); err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".operation-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(root, "operation.json")); err != nil {
		return err
	}
	return catalogSyncDirectory(root)
}

func catalogSyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func (m *CatalogTransitions) records() ([]catalogRecord, error) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil, err
	}
	var records []catalogRecord
	sequences := map[uint64]bool{}
	for _, entry := range entries {
		if !software.ValidID(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("foreign catalog transition entry")
		}
		record, err := m.read(entry.Name())
		if err != nil {
			return nil, err
		}
		if sequences[record.Sequence] {
			return nil, errors.New("duplicate catalog transition sequence")
		}
		sequences[record.Sequence] = true
		records = append(records, record)
	}
	return records, nil
}

// Apply first preserves this operation's live catalog. A known target bundle
// receives its prior snapshot only when the live schema is newer. Unknown
// legacy targets are attempted with the protected live catalog; startup must
// reject one that cannot reopen it, after which Recover restores this operation.
func (m *CatalogTransitions) Apply(ctx context.Context, op Operation, source string) error {
	if err := m.check(op, "applying"); err != nil {
		return err
	}
	if !software.ValidID(source) {
		return errors.New("catalog checkpoint requires the actual source bundle")
	}
	record, err := m.read(op.ID)
	if errors.Is(err, os.ErrNotExist) {
		records, err := m.records()
		if err != nil {
			return err
		}
		var sequence uint64
		for _, item := range records {
			if item.Sequence > sequence {
				sequence = item.Sequence
			}
		}
		if sequence == math.MaxUint64 {
			return errors.New("catalog transition sequence exhausted")
		}
		record = catalogRecord{Version: 1, Sequence: sequence + 1, Request: op.Request, Previous: op.Previous, Source: source,
			HostRoot: m.hostRoot, Directories: m.identities, Helper: m.helper, Phase: "preparing"}
		if err := m.write(record, op, "applying"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if record.Request != op.Request || record.Previous != op.Previous || record.Source != source {
		return ErrConflict
	}
	if record.Phase == "applied" {
		return m.verifySnapshot(ctx, record)
	}
	if record.Phase != "preparing" && record.Phase != "prepared" {
		return ErrConflict
	}
	if record.Phase == "preparing" {
		// An interrupted checkpoint never authorizes candidate startup. The
		// helper publishes exclusively; a preparation interrupted before its
		// journal update is recovered without authorizing candidate startup.
		before, err := m.run(ctx, record.Helper, "checkpoint", "--database", m.database, "--snapshot", m.snapshot(op.ID))
		if err != nil {
			return err
		}
		record.Before, record.Phase = before, "prepared"
		records, err := m.records()
		if err != nil {
			return err
		}
		var target *catalogRecord
		knownHelper := op.Candidate == m.helper
		for _, item := range records {
			if item.Helper == op.Candidate {
				knownHelper = true
			}
			if item.Request.ID == op.ID || item.Source != op.Candidate || item.Phase == "preparing" || item.Request.Generation > op.Generation {
				continue
			}
			if target == nil || item.Sequence > target.Sequence {
				copy := item
				target = &copy
			}
		}
		if target != nil && record.Before.Present && target.Before.Present && record.Before.Schema > target.Before.Schema {
			// During image-trial reconciliation the factory host may still use
			// tmpfs while this durable snapshot is older. Its own authenticated
			// schema command proves it can read a later live catalog without
			// restoring that stale snapshot. Never probe an unknown old bundle.
			supported := -1
			if knownHelper {
				supported, err = m.supportedSchema(ctx, op.Candidate)
				if err != nil {
					return err
				}
			}
			if supported < record.Before.Schema {
				if err := m.verifySnapshot(ctx, *target); err != nil {
					return err
				}
				record.Target = target.Request.ID
			}
		}
		if err := m.write(record, op, "applying"); err != nil {
			return err
		}
	}
	if err := m.verifySnapshot(ctx, record); err != nil {
		return err
	}
	if record.Target != "" {
		target, err := m.read(record.Target)
		if err != nil {
			return err
		}
		if target.Source != op.Candidate || target.Before.Schema >= record.Before.Schema {
			return errors.New("catalog rollback snapshot does not match target bundle")
		}
		if err := m.verifySnapshot(ctx, target); err != nil {
			return err
		}
		if err := m.check(op, "applying"); err != nil {
			return err
		}
		if _, err := m.run(ctx, record.Helper, "restore", "--database", m.database, "--snapshot", m.snapshot(record.Target), "--retain", filepath.Join(m.opRoot(op.ID), "rollback-retain"), "--expected-current-schema", fmt.Sprint(record.Before.Schema)); err != nil {
			return err
		}
	}
	record.Phase = "applied"
	return m.write(record, op, "applying")
}

// Recover restores this operation's own pre-activation catalog, including when
// the failed candidate was itself a return to an older baseline. The helper
// retains the displaced catalog and rejects a replay after unrelated writes.
func (m *CatalogTransitions) Recover(ctx context.Context, op Operation, target string) error {
	if err := m.check(op, "recovering"); err != nil {
		return err
	}
	record, err := m.read(op.ID)
	if err != nil {
		return fmt.Errorf("catalog recovery requires its operation journal: %w", err)
	}
	if record.Request != op.Request || record.Previous != op.Previous {
		return ErrConflict
	}
	if record.Phase == "preparing" {
		// prepared is durable before a restore or candidate Start, so this
		// matching record proves no logical catalog change was authorized.
		current, err := m.run(ctx, record.Helper, "inspect", "--database", m.database)
		if err != nil {
			return err
		}
		return m.recoveryReadable(ctx, record, current, target)
	}
	if err := m.verifySnapshot(ctx, record); err != nil {
		return err
	}
	if err := m.recoveryReadable(ctx, record, record.Before, target); err != nil {
		return err
	}
	if record.Phase == "recovered" {
		current, err := m.run(ctx, record.Helper, "inspect", "--database", m.database)
		if err != nil {
			return err
		}
		if current != record.Before {
			return errors.New("recovered catalog changed before software recovery completed")
		}
		return nil
	}
	if record.Phase != "recovering" {
		current, err := m.run(ctx, record.Helper, "inspect", "--database", m.database)
		if err != nil {
			return err
		}
		record.RecoverySource, record.Phase = current, "recovering"
		if err := m.write(record, op, "recovering"); err != nil {
			return err
		}
	}
	if err := m.check(op, "recovering"); err != nil {
		return err
	}
	if _, err := m.run(ctx, record.Helper, "restore", "--database", m.database, "--snapshot", m.snapshot(op.ID), "--retain", filepath.Join(m.opRoot(op.ID), "recovery-retain"), "--expected-current-schema", fmt.Sprint(record.RecoverySource.Schema)); err != nil {
		return err
	}
	record.Phase = "recovered"
	return m.write(record, op, "recovering")
}

// A held-back selection may recover with a different factory from the bundle
// that owned the checkpoint. Prove that actual target can reopen the snapshot
// before replacing live data. An unknown legacy target is never executed as a
// schema probe; its own prior snapshots provide only observed readability.
func (m *CatalogTransitions) recoveryReadable(ctx context.Context, record catalogRecord, image catalogImage, target string) error {
	if !software.ValidID(target) {
		return errors.New("catalog recovery target identity unavailable")
	}
	if target == record.Source || !image.Present {
		return nil
	}
	records, err := m.records()
	if err != nil {
		return err
	}
	knownHelper := target == m.helper
	var observed *catalogRecord
	for _, item := range records {
		if item.Helper == target {
			knownHelper = true
		}
		if item.Source == target && item.Phase != "preparing" && item.Before.Present && item.Before.Schema >= image.Schema && (observed == nil || item.Sequence > observed.Sequence) {
			copy := item
			observed = &copy
		}
	}
	if knownHelper {
		supported, err := m.supportedSchema(ctx, target)
		if err != nil {
			return err
		}
		if supported >= image.Schema {
			return nil
		}
		return errors.New("recovery target does not support the checkpoint catalog schema")
	}
	if observed != nil {
		return m.verifySnapshot(ctx, *observed)
	}
	return errors.New("recovery target catalog readability is unproven; preserving current catalog and checkpoint")
}

func (m *CatalogTransitions) verifySnapshot(ctx context.Context, record catalogRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := catalogDirectoryIdentity(m.snapshot(record.Request.ID)); err != nil {
		return err
	}
	metadataPath := filepath.Join(m.snapshot(record.Request.ID), "metadata.json")
	metadata, err := os.OpenFile(metadataPath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	info, statErr := metadata.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > catalogRecordLimit {
		metadata.Close()
		return errors.New("invalid catalog snapshot metadata file")
	}
	decoder := json.NewDecoder(io.LimitReader(metadata, catalogRecordLimit+1))
	decoder.DisallowUnknownFields()
	var saved catalogImage
	decodeErr := decoder.Decode(&saved)
	var trailing any
	if decodeErr == nil && decoder.Decode(&trailing) != io.EOF {
		decodeErr = errors.New("trailing catalog snapshot metadata")
	}
	closeErr := metadata.Close()
	if err := errors.Join(decodeErr, closeErr); err != nil {
		return err
	}
	if saved != record.Before {
		return errors.New("catalog snapshot metadata changed")
	}
	path := filepath.Join(m.snapshot(record.Request.ID), "catalog.sqlite3")
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if !record.Before.Present {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err == nil {
			f.Close()
		}
		return errors.New("absent catalog snapshot contains database")
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != record.Before.Size {
		return errors.New("catalog snapshot size or type changed")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, catalogContextReader{ctx, f}); err != nil {
		return err
	}
	if hex.EncodeToString(digest.Sum(nil)) != record.Before.SHA256 {
		return errors.New("catalog snapshot digest changed")
	}
	return nil
}

type catalogContextReader struct {
	context context.Context
	reader  io.Reader
}

func (r catalogContextReader) Read(p []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type catalogOutput struct{ bytes.Buffer }

func (b *catalogOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > catalogRecordLimit {
		return 0, errors.New("catalog helper output too large")
	}
	return b.Buffer.Write(p)
}

func (m *CatalogTransitions) run(ctx context.Context, helper string, args ...string) (catalogImage, error) {
	var image catalogImage
	if m.command != nil {
		result, err := m.command(ctx, helper, args)
		if err == nil {
			err = result.validate()
		}
		return result, err
	}
	data, err := m.execute(ctx, helper, args...)
	if err != nil {
		return image, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&image); err != nil {
		return image, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return image, errors.New("trailing catalog helper result")
	}
	return image, image.validate()
}

func (m *CatalogTransitions) supportedSchema(ctx context.Context, helper string) (int, error) {
	if m.command != nil {
		result, err := m.command(ctx, helper, []string{"schema"})
		if err != nil {
			return 0, err
		}
		if result.Schema <= 0 {
			return 0, errors.New("invalid catalog helper supported schema")
		}
		return result.Schema, nil
	}
	data, err := m.execute(ctx, helper, "schema")
	if err != nil {
		return 0, err
	}
	var result struct {
		Schema int `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return 0, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return 0, errors.New("trailing catalog helper schema")
	}
	if result.Schema <= 0 {
		return 0, errors.New("invalid catalog helper supported schema")
	}
	return result.Schema, nil
}

func (m *CatalogTransitions) execute(ctx context.Context, helper string, args ...string) ([]byte, error) {
	root, err := m.resolve(ctx, helper)
	if err != nil {
		return nil, fmt.Errorf("authenticate catalog helper: %w", err)
	}
	var stdout, stderr catalogOutput
	command := exec.CommandContext(ctx, filepath.Join(root, "bin", "fogcast-kit"), append([]string{"catalog"}, args...)...)
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("catalog helper %s: %w: %s", args[0], err, stderr.String())
	}
	return stdout.Bytes(), nil
}
