package kitsoftware

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"
)

// A preparation is scoped to one verified startup sequence, including its
// bounded process-exit retries. It never authorizes a later operation without
// fresh Verify. Metadata is checked without following symlinks; ctime detects
// same-size writes even when the original mtime is restored.
type preparedRelease struct {
	id, root string
	entries  map[string]releaseIdentity
}
type releaseIdentity struct {
	dev, ino     uint64
	mode         uint32
	size         int64
	mtime, ctime syscall.Timespec
}

func releaseIdentities(ctx context.Context, root string) (map[string]releaseIdentity, error) {
	entries := map[string]releaseIdentity{}
	record := func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("release member is not a regular file or directory: %s", path)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("release identity metadata unavailable")
		}
		entries[path] = releaseIdentity{uint64(stat.Dev), uint64(stat.Ino), stat.Mode, stat.Size, stat.Mtim, stat.Ctim}
		return nil
	}
	// Pin every ancestor too: a replaced ancestor or symlink must not redirect a
	// path while retaining the same leaf bytes. Ancestor timestamps are excluded
	// because unrelated store installs legitimately change directory contents.
	for parent := filepath.Dir(root); ; parent = filepath.Dir(parent) {
		if err := record(parent); err != nil {
			return nil, err
		}
		identity := entries[parent]
		identity.size, identity.mtime, identity.ctime = 0, syscall.Timespec{}, syscall.Timespec{}
		entries[parent] = identity
		if parent == string(filepath.Separator) {
			break
		}
	}
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return record(path)
	})
	return entries, err
}

func (c *ProcessController) prepare(ctx context.Context, id string) (*preparedRelease, error) {
	root, err := c.Resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	before, err := releaseIdentities(ctx, root)
	if err != nil {
		return nil, err
	}
	// Linux ctime updates may share a coarse clock tick. Wait one monotonic
	// second after capture before authenticating bytes. Never compare ctime to
	// CLOCK_REALTIME: the kit has no RTC and retained files may appear future-
	// dated after reboot. This barrier is preparation, outside startup budgets.
	if err := preparationTick(ctx, time.Second); err != nil {
		return nil, err
	}
	// Resolve authenticates bytes and compatibility. Repeat once bracketed by
	// metadata snapshots so a write during verification cannot become a trusted
	// fingerprint. Both full reads happen before services are stopped, never on
	// Start or on a retry within the readiness deadline.
	verified, err := c.Resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	verified, err = filepath.Abs(verified)
	if err != nil {
		return nil, err
	}
	after, err := releaseIdentities(ctx, root)
	if err != nil {
		return nil, err
	}
	if verified != root || !reflect.DeepEqual(before, after) {
		return nil, errors.New("release changed during verification")
	}
	return &preparedRelease{id: id, root: root, entries: after}, nil
}

// preparedPath is called with c.mu held. No digest work or resolver call occurs
// here. A changed release fails closed rather than silently re-preparing inside
// the bounded startup window.
func (c *ProcessController) preparedPath(ctx context.Context, id string) (string, error) {
	p := c.prepared
	if p == nil || p.id != id {
		return "", errors.New("software release is not prepared")
	}
	current, err := releaseIdentities(ctx, p.root)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(p.entries, current) {
		return "", errors.New("prepared software release changed")
	}
	return p.root, nil
}

// preparationTick deliberately accepts no filesystem timestamps or wall clock.
// A missing RTC or a clock correction cannot skip or extend this fixed barrier.
func preparationTick(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
