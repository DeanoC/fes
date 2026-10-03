package kitlauncher

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

const maxMatchROMBytes = 4 << 20
const maxMatchArchiveBytes = 8 << 20

type cachedROMHash struct {
	size  int64
	mtime time.Time
	hash  string
}

type localROMMatcher struct {
	mu    sync.Mutex
	cache map[string]cachedROMHash
}

func normalizeROMHash(hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if protocol.ValidateDigest(hash) != nil {
		return ""
	}
	return hash
}

func (m *localROMMatcher) matchDigest(ctx context.Context, want string, resolve func(context.Context, string) (string, error), games []hostclient.Game) (string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if normalizeROMHash(want) == "" {
		return "", errCartridgeCheck
	}
	incomplete := false
	for _, game := range games {
		if !game.LocalCatalogPlayable() {
			continue
		}
		if err := checkCtx.Err(); err != nil {
			return "", errCartridgeCheck
		}
		path, err := resolve(checkCtx, game.ID)
		if err != nil {
			incomplete = true
			continue
		}
		got, err := m.hash(checkCtx, path)
		if err != nil {
			incomplete = true
			continue
		}
		if strings.EqualFold(got, want) {
			return game.ID, nil
		}
	}
	if incomplete {
		return "", errCartridgeCheck
	}
	return "", errNotOnKit
}

// launchMatchedLocalGame plays a browse-only host SMS row through local
// control. The returned digest is one the host just confirmed, so the caller
// can remember it for a later offline launch. A digest already stored on the
// row is not returned again.
//
// Identity order: the kit-local id, then ROM SHA-256 (cached on the row, or
// fetched while the host is up). A hash miss does not fall through to the
// title. With no digest, one local SMS row with the same title is accepted.
// That title match is weaker than a hash: a saved host row has no cartridge
// size, so size is not compared, and two local rows that share the title are
// refused instead of guessed.
func launchMatchedLocalGame(ctx context.Context, client LocalCoreClient, online, kitRow bool, cachedDigest string, fetch func(context.Context, string) (string, error), resolve func(context.Context, string) (string, error), games []hostclient.Game, matcher *localROMMatcher, host hostclient.Game) (string, error) {
	id := strings.TrimSpace(host.ID)
	if kitRow && containsLocalID(games, id) {
		return "", launchLocalGame(ctx, client, resolve, games, id)
	}
	if resolve == nil || matcher == nil {
		return "", errNeedsHost
	}
	want := ""
	learned := ""
	if online {
		// Always ask the connected host: a game id names a library path, not
		// ROM bytes, so a digest on the row or in the cache can be stale.
		if fetch == nil {
			return "", errCartridgeCheck
		}
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		got, err := fetch(checkCtx, id)
		cancel()
		if err != nil || normalizeROMHash(got) == "" {
			return "", errCartridgeCheck
		}
		want = normalizeROMHash(got)
		learned = want
	} else if !online {
		want = normalizeROMHash(cachedDigest)
	}
	if want != "" {
		localID, err := matcher.matchDigest(ctx, want, resolve, games)
		if err != nil {
			return learned, err
		}
		return learned, launchLocalGame(ctx, client, resolve, games, localID)
	}
	if online {
		return "", errCartridgeCheck
	}
	localID, err := matchSMSByTitle(host, games)
	if err != nil {
		return "", err
	}
	return "", launchLocalGame(ctx, client, resolve, games, localID)
}

// matchSMSByTitle is the offline fallback when the kit has no ROM SHA-256.
// It is weaker than a hash match: it pairs a cached host SMS row with one
// local row by the title slug in the game id (sms-datastorm-…) or by the
// letters and digits of the display title (Data Storm and datastorm). It
// does not compare file size or cartridge bytes. More than one local row is
// errNeedsHost. None is errNotOnKit.
func matchSMSByTitle(host hostclient.Game, games []hostclient.Game) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(host.System), "sms") && !strings.HasPrefix(strings.TrimSpace(host.ID), "sms-") {
		return "", errNeedsHost
	}
	slug, slugOK := smsTitleSlug(host.ID)
	loose := looseTitle(host.Title)
	if !slugOK && loose == "" {
		return "", errNeedsHost
	}
	hit := ""
	for _, game := range games {
		if !game.LocalCatalogPlayable() {
			continue
		}
		same := false
		if slugOK {
			if localSlug, ok := smsTitleSlug(game.ID); ok && localSlug == slug {
				same = true
			}
		}
		if !same && loose != "" && looseTitle(game.Title) == loose {
			same = true
		}
		if !same {
			continue
		}
		if hit != "" && hit != game.ID {
			return "", errNeedsHost
		}
		hit = game.ID
	}
	if hit == "" {
		return "", errNotOnKit
	}
	return hit, nil
}

func smsTitleSlug(id string) (string, bool) {
	const prefix = "sms-"
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, prefix) {
		return "", false
	}
	rest := id[len(prefix):]
	if len(rest) < 14 || rest[len(rest)-13] != '-' {
		return "", false
	}
	suffix := rest[len(rest)-12:]
	for _, c := range suffix {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	slug := rest[:len(rest)-13]
	if slug == "" || strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") || strings.Contains(slug, "--") {
		return "", false
	}
	for _, c := range slug {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return "", false
		}
	}
	return slug, true
}

func looseTitle(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// smsHashFill remembers host ROM SHA-256 values for browse-only SMS rows
// while the host catalog is reachable. A later offline launch reads the
// cache and does not call the host. A new catalog cancels the previous fill.
type smsHashFill struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	// checked bounds background refreshes: each id is re-fetched at most
	// once per smsHashRefreshEvery, not on every 30s catalog reload.
	checked map[string]time.Time
}

const smsHashRefreshEvery = 15 * time.Minute

// due reports whether id should be fetched now and marks it checked.
func (f *smsHashFill) due(id string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if last, ok := f.checked[id]; ok && now.Sub(last) < smsHashRefreshEvery {
		return false
	}
	if f.checked == nil {
		f.checked = map[string]time.Time{}
	}
	f.checked[id] = now
	return true
}

func (f *smsHashFill) start(parent context.Context, fetch func(context.Context, string) (string, error), store *DiskStore, games []hostclient.Game) {
	if f == nil || fetch == nil || store == nil || len(games) == 0 {
		return
	}
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	f.cancel = cancel
	f.mu.Unlock()
	copied := append([]hostclient.Game(nil), games...)
	go fillSMSROMHashes(ctx, fetch, store, copied, f)
}

func (f *smsHashFill) stop() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
}

func fillSMSROMHashes(ctx context.Context, fetch func(context.Context, string) (string, error), store *DiskStore, games []hostclient.Game, throttle ...*smsHashFill) {
	if fetch == nil || store == nil {
		return
	}
	for _, game := range games {
		if ctx.Err() != nil {
			return
		}
		if !game.LocalCatalogPlayable() {
			continue
		}
		if len(throttle) > 0 && throttle[0] != nil && !throttle[0].due(game.ID, time.Now()) {
			continue
		}
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		got, err := fetch(reqCtx, game.ID)
		cancel()
		if err != nil {
			continue
		}
		if hash := normalizeROMHash(got); hash != "" {
			_ = store.RememberROMHash(game.ID, hash)
		}
	}
}

func (m *localROMMatcher) hash(ctx context.Context, path string) (string, error) {
	info, err := os.Lstat(path)
	archivePath := strings.EqualFold(filepath.Ext(path), ".zip")
	maxSource := int64(maxMatchROMBytes)
	if archivePath {
		maxSource = maxMatchArchiveBytes
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxSource {
		return "", fmt.Errorf("local cartridge unavailable")
	}
	m.mu.Lock()
	entry, ok := m.cache[path]
	m.mu.Unlock()
	if ok && entry.size == info.Size() && entry.mtime.Equal(info.ModTime()) {
		return entry.hash, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var reader io.Reader = file
	var member io.ReadCloser
	if archivePath {
		archive, err := zip.NewReader(file, info.Size())
		if err != nil {
			return "", err
		}
		var selected *zip.File
		for _, item := range archive.File {
			if strings.EqualFold(filepath.Ext(item.Name), ".sms") {
				if selected != nil || item.UncompressedSize64 < 1 || item.UncompressedSize64 > maxMatchROMBytes {
					return "", fmt.Errorf("local cartridge archive unavailable")
				}
				selected = item
			}
		}
		if selected == nil {
			return "", fmt.Errorf("local cartridge archive unavailable")
		}
		member, err = selected.Open()
		if err != nil {
			return "", err
		}
		defer member.Close()
		reader = member
	}
	h := sha256.New()
	buf := make([]byte, 32<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > maxMatchROMBytes {
				return "", fmt.Errorf("local cartridge too large")
			}
			_, _ = h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	sum := fmt.Sprintf("%x", h.Sum(nil))
	m.mu.Lock()
	if m.cache == nil {
		m.cache = make(map[string]cachedROMHash)
	}
	m.cache[path] = cachedROMHash{info.Size(), info.ModTime(), sum}
	m.mu.Unlock()
	return sum, nil
}
