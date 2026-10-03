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

func (m *localROMMatcher) match(ctx context.Context, hostID string, fetch func(context.Context, string) (string, error), resolve func(context.Context, string) (string, error), games []hostclient.Game) (string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	want, err := fetch(checkCtx, hostID)
	if err != nil || protocol.ValidateDigest(want) != nil {
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

func launchMatchedLocalGame(ctx context.Context, client LocalCoreClient, fetch func(context.Context, string) (string, error), resolve func(context.Context, string) (string, error), games []hostclient.Game, matcher *localROMMatcher, id string) error {
	if containsLocalID(games, id) {
		return launchLocalGame(ctx, client, resolve, games, id)
	}
	if fetch == nil || resolve == nil || matcher == nil {
		return errCartridgeCheck
	}
	localID, err := matcher.match(ctx, id, fetch, resolve, games)
	if err != nil {
		return err
	}
	return launchLocalGame(ctx, client, resolve, games, localID)
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
