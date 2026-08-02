package targetcache_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/internal/targetcache"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestKeyBuildsOnlyRegisteredCacheDestinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		system    protocol.System
		extension string
	}{
		{name: "SNES native", system: protocol.SystemSNES, extension: "sfc"},
		{name: "Mega Drive native", system: protocol.SystemMegaDrive, extension: "md"},
		{name: "SNES bin", system: protocol.SystemSNES, extension: "bin"},
		{name: "Mega Drive bin", system: protocol.SystemMegaDrive, extension: "bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			content := []byte("synthetic-" + tt.name)
			identity := contentIdentity(content, tt.extension)
			path := writeCacheFile(t, root, tt.system, identity, content)

			manager := openTestManager(t, root)
			resolved, apiErr := manager.Resolve(context.Background(), tt.system, identity)
			if apiErr != nil {
				t.Fatalf("Resolve: %v", apiErr)
			}
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Root != resolvedRoot {
				t.Fatalf("resolved root = %q, want %q", resolved.Root, resolvedRoot)
			}
			if resolved.Path != path {
				t.Fatalf("resolved path = %q, want %q", resolved.Path, path)
			}
			assertBeneath(t, resolved.Root, resolved.Path)
			wantBase := identity.SHA256 + "." + tt.extension
			if filepath.Base(resolved.Path) != wantBase {
				t.Fatalf("resolved base = %q, want %q", filepath.Base(resolved.Path), wantBase)
			}
			if filepath.Base(filepath.Dir(resolved.Path)) != string(tt.system) {
				t.Fatalf("resolved system directory = %q, want %q", filepath.Base(filepath.Dir(resolved.Path)), tt.system)
			}
		})
	}
}

func TestKeyRejectsCrossSystemAndMalformedValuesWithoutPathLeak(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := openTestManager(t, root)
	digest := strings.Repeat("a", 64)
	tests := []struct {
		name   string
		system protocol.System
		key    protocol.ContentKey
		code   protocol.ErrorCode
	}{
		{name: "SNES rejects md", system: protocol.SystemSNES, key: protocol.ContentKey{SHA256: digest, Extension: "md"}, code: protocol.CodeUnsupportedSystem},
		{name: "Mega Drive rejects sfc", system: protocol.SystemMegaDrive, key: protocol.ContentKey{SHA256: digest, Extension: "sfc"}, code: protocol.CodeUnsupportedSystem},
		{name: "unknown system", system: "nes", key: protocol.ContentKey{SHA256: digest, Extension: "bin"}, code: protocol.CodeUnsupportedSystem},
		{name: "uppercase digest", system: protocol.SystemSNES, key: protocol.ContentKey{SHA256: strings.Repeat("A", 64), Extension: "sfc"}, code: protocol.CodeBadRequest},
		{name: "short digest", system: protocol.SystemSNES, key: protocol.ContentKey{SHA256: strings.Repeat("a", 63), Extension: "sfc"}, code: protocol.CodeBadRequest},
		{name: "dotted extension", system: protocol.SystemSNES, key: protocol.ContentKey{SHA256: digest, Extension: ".sfc"}, code: protocol.CodeBadRequest},
		{name: "slash extension", system: protocol.SystemSNES, key: protocol.ContentKey{SHA256: digest, Extension: "../sfc"}, code: protocol.CodeBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, apiErr := manager.Probe(context.Background(), tt.system, tt.key)
			assertSafeAPIError(t, apiErr, tt.code, root, filepath.Join(root, string(tt.system), tt.key.SHA256+"."+tt.key.Extension))
		})
	}
}

func TestResolveMissDoesNotExposePrivatePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := openTestManager(t, root)
	identity := protocol.ContentIdentity{SHA256: strings.Repeat("b", 64), Size: 123, Extension: "sfc"}
	_, apiErr := manager.Resolve(context.Background(), protocol.SystemSNES, identity)
	assertSafeAPIError(t, apiErr, protocol.CodeContentNotCached, root, identity.SHA256+".sfc")
}

func TestOpenResolvesConfiguredRootAndCreatesPrivateSystemDirectories(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	realRoot := filepath.Join(parent, "persistent-cache")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(parent, "configured-cache")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Fatal(err)
	}
	manager := openTestManager(t, linkedRoot)

	identity := contentIdentity([]byte("after-open"), "bin")
	path := writeCacheFile(t, realRoot, protocol.SystemSNES, identity, []byte("after-open"))
	// A new manager models an agent restart and inventories the persisted file.
	manager = openTestManager(t, linkedRoot)
	resolved, apiErr := manager.Resolve(context.Background(), protocol.SystemSNES, identity)
	if apiErr != nil {
		t.Fatalf("Resolve after reopen: %v", apiErr)
	}
	resolvedRealRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Root != resolvedRealRoot || resolved.Path != path {
		t.Fatalf("resolved = %#v, want root=%q path=%q", resolved, resolvedRealRoot, path)
	}
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
		info, err := os.Lstat(filepath.Join(realRoot, string(system)))
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s directory mode = %v, want private directory", system, info.Mode())
		}
	}
}

func assertBeneath(t *testing.T, root, path string) {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("path %q is not beneath root %q (relative=%q, error=%v)", path, root, relative, err)
	}
}

func assertSafeAPIError(t *testing.T, apiErr *protocol.APIError, code protocol.ErrorCode, forbidden ...string) {
	t.Helper()
	if apiErr == nil || apiErr.Code != code {
		t.Fatalf("API error = %#v, want code %s", apiErr, code)
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(apiErr.Error(), value) {
			t.Fatalf("API error %q exposes private value %q", apiErr, value)
		}
	}
}

func contentIdentity(content []byte, extension string) protocol.ContentIdentity {
	digest := sha256.Sum256(content)
	return protocol.ContentIdentity{SHA256: fmt.Sprintf("%x", digest), Size: int64(len(content)), Extension: extension}
}

func writeCacheFile(t *testing.T, root string, system protocol.System, identity protocol.ContentIdentity, content []byte) string {
	t.Helper()
	directory := filepath.Join(root, string(system))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, identity.SHA256+"."+identity.Extension)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func openTestManager(t *testing.T, root string, options ...targetcache.Option) *targetcache.Manager {
	t.Helper()
	options = append([]targetcache.Option{
		targetcache.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}, options...)
	manager, err := targetcache.Open(testManagerConfig(root), core.DefaultRegistry(), options...)
	if err != nil {
		t.Fatalf("Open target cache: %v", err)
	}
	return manager
}

func testManagerConfig(root string) targetcache.Config {
	return targetcache.Config{
		Root:         root,
		ActiveRecord: filepath.Join(filepath.Dir(root), "run", "fogcast-active.json"),
		MaxBytes:     64 << 20,
	}
}
