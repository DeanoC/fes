package appliance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/blockprobe"
)

// ensureSpace frees unreferenced images until size, the staging margin, and
// the splash reserve fit. Protected identities are resolved before the first
// deletion. Callers verify the incoming image before this runs, so a failed
// upload never reaches it.
func (s *Store) ensureSpace(ctx context.Context, size int64) error {
	if size < 0 {
		return errors.New("invalid image size")
	}
	need := releaseStorageNeed(size)
	free, err := s.availableBytes(s.root)
	if err != nil {
		return err
	}
	if free >= need {
		return nil
	}
	keep, err := s.protectedImages()
	if err != nil {
		return err
	}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		removed, err := s.removeOldestUnreferenced(keep)
		if err != nil {
			return err
		}
		if !removed {
			return ErrInsufficientStorage
		}
		free, err = s.availableBytes(s.root)
		if err != nil {
			return err
		}
		if free >= need {
			return nil
		}
	}
}

func (s *Store) protectedImages() (map[string]struct{}, error) {
	st, corrupt, err := s.readState()
	if err != nil {
		return nil, err
	}
	if corrupt {
		return nil, ErrCorruptState
	}
	keep := map[string]struct{}{}
	add := func(hash string) {
		if release.ValidHash(hash) {
			keep[hash] = struct{}{}
		}
	}
	add(st.Good)
	add(st.Previous)
	add(st.Pending)
	add(st.TrialImage)
	add(st.ConfirmedImage)
	ticket, ticketErr := s.readBootTicket()
	if ticketErr != nil && !errors.Is(ticketErr, os.ErrNotExist) {
		return nil, ticketErr
	}
	if ticketErr == nil {
		add(ticket.ImageSHA256)
		add(ticket.FallbackImageSHA256)
	}
	// Keep the factory image as the final recovery path even when Previous is usable.
	add(s.factory.ImageSHA256)
	paths, err := s.loopBackings()
	if err != nil {
		return nil, fmt.Errorf("refusing to prune releases: %w", err)
	}
	imagesDir := filepath.Join(s.root, "images")
	for _, path := range paths {
		if hash, ok := imageHashFromBacking(imagesDir, path); ok {
			add(hash)
		}
	}
	return keep, nil
}

// enforceRetention keeps the selected good image and one usable fallback.
// A newly staged image is also retained until it is activated or superseded.
// In-progress selections, the boot ticket, and loop-mounted files remain
// protected by protectedImages. This keeps the stable FAT release set bounded
// while allowing one candidate to exist during an update.
func (s *Store) enforceRetention(ctx context.Context, staged string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	keep, err := s.protectedImages()
	if err != nil {
		return err
	}
	if release.ValidHash(staged) {
		keep[staged] = struct{}{}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		removed, err := s.removeOldestUnreferenced(keep)
		if err != nil {
			return err
		}
		if !removed {
			return nil
		}
	}
}

func (s *Store) removeOldestUnreferenced(keep map[string]struct{}) (bool, error) {
	dir := filepath.Join(s.root, "images")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	type candidate struct {
		hash string
		mod  time.Time
	}
	var stale []candidate
	for _, entry := range entries {
		hash, ok := publishedImageHash(entry.Name())
		if !ok {
			continue
		}
		if _, protected := keep[hash]; protected {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		stale = append(stale, candidate{hash: hash, mod: info.ModTime()})
	}
	if len(stale) == 0 {
		return false, nil
	}
	sort.Slice(stale, func(i, j int) bool {
		if !stale[i].mod.Equal(stale[j].mod) {
			return stale[i].mod.Before(stale[j].mod)
		}
		return stale[i].hash < stale[j].hash
	})
	hash := stale[0].hash
	// Drop the staged splash first. A failure leaves the image in place so the
	// next prune retries; removing the image first would leak the splash.
	if err = s.removeImageSplash(hash); err != nil {
		return false, err
	}
	if err = os.Remove(filepath.Join(dir, hash+".img")); err != nil {
		return false, err
	}
	if err = s.syncDirectory(dir); err != nil {
		return false, err
	}
	manifestDir := filepath.Join(s.root, "manifests")
	manifest := filepath.Join(manifestDir, hash+".json")
	info, err := os.Lstat(manifest)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("release manifest is not a regular file")
	}
	if err = os.Remove(manifest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err = s.syncDirectory(manifestDir); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) removeImageSplash(hash string) error {
	rbf, digest, err := SplashPaths(s.root, hash)
	if err != nil {
		return err
	}
	removed := false
	for _, path := range []string{rbf, digest} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return errors.New("release splash is not a regular file")
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed = true
	}
	if !removed {
		return nil
	}
	return s.syncDirectory(filepath.Dir(rbf))
}

func publishedImageHash(name string) (string, bool) {
	hash, ok := strings.CutSuffix(name, ".img")
	if !ok || !release.ValidHash(hash) {
		return "", false
	}
	return hash, true
}

func imageHashFromBacking(imagesDir, backing string) (string, bool) {
	backing = strings.TrimSpace(backing)
	backing = strings.TrimSuffix(backing, " (deleted)")
	if backing == "" {
		return "", false
	}
	cleaned := filepath.Clean(backing)
	hash, ok := publishedImageHash(filepath.Base(cleaned))
	if !ok || filepath.Dir(cleaned) != filepath.Clean(imagesDir) {
		return "", false
	}
	return hash, true
}

func loopBackingFiles() ([]string, error) {
	return loopBackingFilesFrom("/sys/block")
}

func loopBackingFilesFrom(blockDir string) ([]string, error) {
	entries, err := os.ReadDir(blockDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		// Optical devices and disks with no usable media are ignored before any
		// further sysfs read. Loop backing files are read only for the rest.
		if decision, ok := blockprobe.Decide(blockDir, entry.Name()); ok && !decision.Allow {
			continue
		}
		if !loopDeviceName(entry.Name()) {
			continue
		}
		backing, err := os.ReadFile(filepath.Join(blockDir, entry.Name(), "loop", "backing_file"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		path := strings.TrimSpace(string(backing))
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func loopDeviceName(name string) bool {
	rest, ok := strings.CutPrefix(name, "loop")
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
