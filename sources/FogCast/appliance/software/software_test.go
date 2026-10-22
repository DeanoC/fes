package software

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

func privateRoot(t *testing.T) string {
	t.Helper()
	// Go 1.26 creates t.TempDir as 0777 before umask, usually 0755. The store
	// refuses anything other than an owner-only root.
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	return root
}
func fixture(t *testing.T) (Manifest, []byte) {
	t.Helper()
	return fixtureWith(t, map[string]string{"runtime": "2"})
}
func fixtureWith(t *testing.T, interfaces map[string]string) (Manifest, []byte) {
	t.Helper()
	files := fstest.MapFS{}
	m := Manifest{Format: 1, Board: "de10-nano", Architecture: "armv7", BaseContract: "fes-base-v1", Version: "1.0", Interfaces: interfaces, Provenance: map[string]string{"fes": "different-from-host"}}
	for _, p := range append(append([]string{}, RequiredExecutables...), "share/fogcast/assets/test.txt") {
		data := []byte(p)
		h := sha256.Sum256(data)
		mode := uint32(0444)
		if p[:4] == "bin/" {
			mode = 0555
		}
		m.Files = append(m.Files, File{p, hex.EncodeToString(h[:]), int64(len(data)), mode})
		files[p] = &fstest.MapFile{Data: data, Mode: 0755}
	}
	var b bytes.Buffer
	if e := WriteBundle(&b, m, files); e != nil {
		t.Fatal(e)
	}
	return m, b.Bytes()
}
func compatibility(m Manifest) Compatibility {
	return Compatibility{Board: m.Board, Architecture: m.Architecture, BaseContract: m.BaseContract, Interfaces: map[string]string{"runtime": "2"}}
}
func TestDeterministicIdentity(t *testing.T) {
	m, b := fixture(t)
	r, e := InspectBundle(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	id, _ := m.ID()
	if r.ID != id {
		t.Fatal("wrong identity")
	}
	m.Files[0], m.Files[1] = m.Files[1], m.Files[0]
	id2, _ := m.ID()
	if id != id2 {
		t.Fatal("order affected identity")
	}
	m.Provenance["fes"] = "different"
	id2, _ = m.ID()
	if id == id2 {
		t.Fatal("provenance omitted from identity")
	}
	m.BaseContract = "other"
	if m.Compatible(compatibility(r.Manifest)) == nil {
		t.Fatal("accepted incompatible base")
	}
}
func rewrite(t *testing.T, b []byte, change func(*tar.Header, []byte) (*tar.Header, []byte)) []byte {
	t.Helper()
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	r := tar.NewReader(bytes.NewReader(b))
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(r)
		if e != nil {
			t.Fatal(e)
		}
		h, data = change(h, data)
		if h == nil {
			continue
		}
		h.Size = int64(len(data))
		if e = w.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func TestArchiveRejectsInvalidMembers(t *testing.T) {
	_, b := fixture(t)
	tests := map[string]func(*tar.Header, []byte) (*tar.Header, []byte){"traversal": func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == RequiredExecutables[0] {
			h.Name = "../escape"
		}
		return h, d
	}, "symlink": func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == RequiredExecutables[0] {
			h.Typeflag = tar.TypeSymlink
			h.Linkname = "/etc/passwd"
			d = nil
		}
		return h, d
	}, "corrupt": func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == RequiredExecutables[0] {
			d[0] ^= 1
		}
		return h, d
	}, "missing": func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == RequiredExecutables[0] {
			return nil, nil
		}
		return h, d
	}, "mode": func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == RequiredExecutables[0] {
			h.Mode = 0777
		}
		return h, d
	}, "unknown": func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == RequiredExecutables[0] {
			h.Name = "share/unknown"
		}
		return h, d
	}}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			if _, e := InspectBundle(bytes.NewReader(rewrite(t, b, change))); e == nil {
				t.Fatal("accepted malformed archive")
			}
		})
	}
	if _, e := InspectBundle(bytes.NewReader(append(b, []byte("garbage")...))); e == nil {
		t.Fatal("accepted trailing data")
	}
	if _, e := InspectBundle(bytes.NewReader(b[:len(b)/2])); e == nil {
		t.Fatal("accepted truncated archive")
	}
}
func TestManifestDuplicateAndCollision(t *testing.T) {
	m, _ := fixture(t)
	b, _ := m.Canonical()
	b = bytes.Replace(b, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1)
	if _, e := DecodeManifest(bytes.NewReader(b)); e == nil {
		t.Fatal("duplicate accepted")
	}
	m.Files = append(m.Files, File{Path: "bin/mister-agent/child", SHA256: m.Files[0].SHA256, Size: 1, Mode: 0444})
	if m.Validate() == nil {
		t.Fatal("file directory collision accepted")
	}
}
func TestStoreConcurrentIdempotenceAndCorruption(t *testing.T) {
	m, b := fixture(t)
	root := privateRoot(t)
	ctx := context.Background()
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := New(root, compatibility(m))
			if e != nil {
				t.Error(e)
				return
			}
			_, new, e := s.Install(ctx, bytes.NewReader(b))
			if e != nil {
				t.Error(e)
			}
			if new {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("publications: %d", created.Load())
	}
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	list, e := s.List(ctx)
	if e != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, e)
	}
	id := list[0].ID
	p, e := s.Path(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(filepath.Join(p, RequiredExecutables[0]), 0755); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(p, RequiredExecutables[0]), []byte("corrupt"), 0555); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Read(ctx, id); e == nil {
		t.Fatal("read corrupt release")
	}
	if _, _, e = s.Install(ctx, bytes.NewReader(b)); e == nil {
		t.Fatal("silently replaced corrupt release")
	}
}
func TestStoreRejectsDirectoryReplacement(t *testing.T) {
	m, b := fixture(t)
	root := privateRoot(t)
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(filepath.Join(root, "releases"), filepath.Join(root, "old")); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(root, "releases"), 0755); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Install(context.Background(), bytes.NewReader(b)); e == nil {
		t.Fatal("accepted replaced store")
	}
}
func TestStoreUnpublishedFailure(t *testing.T) {
	m, b := fixture(t)
	root := privateRoot(t)
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	bad := rewrite(t, b, func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == RequiredExecutables[0] {
			d[0] ^= 1
		}
		return h, d
	})
	if _, _, e = s.Install(context.Background(), bytes.NewReader(bad)); e == nil {
		t.Fatal("accepted corruption")
	}
	entries, _ := os.ReadDir(filepath.Join(root, "releases"))
	incoming, _ := os.ReadDir(filepath.Join(root, "incoming"))
	if len(entries) != 0 || len(incoming) != 0 {
		t.Fatal("partial release published")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = s.Install(ctx, bytes.NewReader(b)); e == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestStoreRejectsSymlink(t *testing.T) {
	m, b := fixture(t)
	root := privateRoot(t)
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	r, _, e := s.Install(context.Background(), bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, "releases", r.ID, RequiredExecutables[0])
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink("/etc/passwd", p); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Read(context.Background(), r.ID); e == nil {
		t.Fatal("symlink admitted")
	}
}
func TestPersistentMountParsing(t *testing.T) {
	good := "10 1 179:3 / /run/fogcast/fesdata3 rw,relatime - ext4 /dev/mmcblk0p3 rw\n"
	if !persistentMount(good, "/run/fogcast/fesdata3", "179:3") {
		t.Fatal("valid mount rejected")
	}
	for _, bad := range []string{"10 1 179:3 /subdir /run/fogcast/fesdata3 rw - ext4 /dev/mmcblk0p3 rw", "10 1 179:3 / /run/fogcast/fesdata3 ro - ext4 /dev/mmcblk0p3 rw", "10 1 179:3 / /run/fogcast/fesdata3 rw - tmpfs none rw"} {
		if persistentMount(bad, "/run/fogcast/fesdata3", "179:3") {
			t.Fatal("invalid mount admitted")
		}
	}
}

func TestRetainedIncompatibleReleaseRemainsInspectable(t *testing.T) {
	m, b := fixture(t)
	root := privateRoot(t)
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	r, _, e := s.Install(context.Background(), bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	nextBase := compatibility(m)
	nextBase.BaseContract = "fes-base-v2"
	reopened, e := New(root, nextBase)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = reopened.Read(context.Background(), r.ID); e != nil {
		t.Fatal("historical release cannot be inspected", e)
	}
	if list, e := reopened.List(context.Background()); e != nil || len(list) != 1 {
		t.Fatal("historical inventory unavailable", list, e)
	}
	if _, e = reopened.Path(context.Background(), r.ID); e == nil {
		t.Fatal("incompatible executable path admitted")
	}
	if _, _, e = reopened.Install(context.Background(), bytes.NewReader(b)); e == nil {
		t.Fatal("incompatible install admitted")
	}
	if _, e = reopened.Path(context.Background(), r.ID); !errors.Is(e, ErrIncompatible) {
		t.Fatal("base contract mismatch is not an incompatibility", e)
	}
}

func TestReleaseAcceptsRequiredVersions(t *testing.T) {
	for _, tc := range []struct {
		provided, required string
		ok                 bool
	}{
		{"1.0", "1.0", true},
		{"1.2", "1.1", true},
		{"1.10", "1.9", true},
		{"1.0", "1.1", false},
		{"2.0", "1.0", false},
		{"10.0", "1.0", false},
		{"", "1.0", false},
		{"01.0", "1.0", false},
		{"1.0", "1", false},
		{"1.0", "1.0.0", false},
	} {
		m := Manifest{Interfaces: map[string]string{"runtime": "2"}}
		if tc.provided != "" {
			m.Interfaces["menu.fes.video.menu-display"] = tc.provided
		}
		e := m.Accepts(map[string]string{"menu.fes.video.menu-display": tc.required})
		if (e == nil) != tc.ok || (e != nil && !errors.Is(e, ErrIncompatible)) {
			t.Errorf("provided %q required %q: %v", tc.provided, tc.required, e)
		}
	}
	m := Manifest{Interfaces: map[string]string{"menu.a": "1.0", "menu.b": "1.0"}}
	if e := m.Accepts(map[string]string{"menu.a": "1.0", "menu.c": "1.0", "menu.d": "1.0"}); e == nil || !strings.Contains(e.Error(), "menu.c 1.0") {
		t.Fatal("first missing interface not reported in key order", e)
	}
}

func TestRequiredInterfacesGateInstallAndPath(t *testing.T) {
	menu := map[string]string{"runtime": "2", "menu.fes.video.fixed-720p60": "1.0", "menu.fes.video.menu-display": "1.0"}
	m, b := fixtureWith(t, menu)
	base := compatibility(m)
	// A base that requires nothing, such as one whose supervisor predates
	// Requires, matches only its own interfaces and ignores the menu keys.
	if e := m.Compatible(base); e != nil {
		t.Fatal("menu interfaces broke the base interface match", e)
	}
	base.Requires = map[string]string{"menu.fes.video.fixed-720p60": "1.0", "menu.fes.video.menu-display": "1.0"}
	if e := m.Compatible(base); e != nil {
		t.Fatal("accepted requirements refused", e)
	}
	root := privateRoot(t)
	s, e := New(root, base)
	if e != nil {
		t.Fatal(e)
	}
	r, _, e := s.Install(context.Background(), bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	base.Requires["menu.fes.video.menu-blitter"] = "1.0"
	newer, e := New(root, base)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = newer.Path(context.Background(), r.ID); !errors.Is(e, ErrIncompatible) || !strings.Contains(e.Error(), "menu.fes.video.menu-blitter 1.0") {
		t.Fatal("release lacking a required interface admitted", e)
	}
	if _, e = newer.Read(context.Background(), r.ID); e != nil {
		t.Fatal("held-back release cannot be inspected", e)
	}
	_, older := fixture(t)
	if _, _, e = newer.Install(context.Background(), bytes.NewReader(older)); !errors.Is(e, ErrIncompatible) {
		t.Fatal("release without menu interfaces installed on a base that requires them", e)
	}
}

func longSharePath(i int) string {
	name := fmt.Sprintf("f%04x", i)
	name += strings.Repeat("n", 84-len(name))
	// 155-byte prefix and an 84-byte final component stay inside USTAR and validPath.
	return "share/" + strings.Repeat("d", 149) + "/" + name
}
func payloadBytes(path string) []byte {
	if strings.HasPrefix(path, "bin/") {
		return []byte("exe:" + path)
	}
	return []byte("x")
}
func releaseWithFiles(n int) Manifest {
	m := Manifest{Format: 1, Board: "de10-nano", Architecture: "armv7", BaseContract: "fes-base-v1", Version: "1.0", Interfaces: map[string]string{"runtime": "2"}, Provenance: map[string]string{"fes": "limit"}}
	for _, p := range RequiredExecutables {
		data := payloadBytes(p)
		sum := sha256.Sum256(data)
		m.Files = append(m.Files, File{p, hex.EncodeToString(sum[:]), int64(len(data)), 0555})
	}
	data := payloadBytes("share/file")
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	for i := 0; i < n; i++ {
		m.Files = append(m.Files, File{longSharePath(i), digest, int64(len(data)), 0444})
	}
	return m
}
func payloadFS(m Manifest) fstest.MapFS {
	files := fstest.MapFS{}
	for _, f := range m.Files {
		mode := fs.FileMode(0444)
		if strings.HasPrefix(f.Path, "bin/") {
			mode = 0555
		}
		files[f.Path] = &fstest.MapFile{Data: payloadBytes(f.Path), Mode: mode}
	}
	return files
}
func TestPackInspectNearManifestLimit(t *testing.T) {
	sample := longSharePath(0)
	if len(sample) != 240 {
		t.Fatalf("path length %d", len(sample))
	}
	lo, hi := 0, MaxFiles
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if _, e := releaseWithFiles(mid).Canonical(); e == nil {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo < 1000 || lo >= MaxFiles {
		t.Fatalf("file count %d is not near the manifest cap", lo)
	}
	m := releaseWithFiles(lo)
	c, e := m.Canonical()
	if e != nil {
		t.Fatal(e)
	}
	if gap := MaxManifestSize - len(c); gap < 0 || gap > 1024 {
		t.Fatalf("canonical length %d leaves %d bytes, want an adjacent manifest", len(c), gap)
	}
	files := payloadFS(m)
	meta := m
	meta.Files = nil
	populated, e := PopulateFiles(meta, files)
	if e != nil {
		t.Fatal(e)
	}
	id, e := m.ID()
	if e != nil {
		t.Fatal(e)
	}
	populatedID, e := populated.ID()
	if e != nil || populatedID != id {
		t.Fatalf("populate identity %s: %v", populatedID, e)
	}
	var packed bytes.Buffer
	if e = WriteBundle(&packed, m, files); e != nil {
		t.Fatal(e)
	}
	got, e := InspectBundle(bytes.NewReader(packed.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	if got.ID != id || len(got.Manifest.Files) != len(m.Files) {
		t.Fatalf("inspect id %s files %d", got.ID, len(got.Manifest.Files))
	}
	over := releaseWithFiles(lo + 1)
	if _, e = over.Canonical(); e == nil {
		t.Fatal("canonical accepted an oversize manifest")
	}
	overMeta := over
	overMeta.Files = nil
	if _, e = PopulateFiles(overMeta, payloadFS(over)); e == nil {
		t.Fatal("populate accepted an oversize manifest")
	}
	var rejected bytes.Buffer
	if e = WriteBundle(&rejected, over, fstest.MapFS{}); e == nil || rejected.Len() != 0 {
		t.Fatal("pack emitted an oversize manifest")
	}
	if _, e = releaseWithFiles(MaxFiles).Canonical(); e == nil {
		t.Fatal("maximum file count at maximum path length encoded under the manifest cap")
	}
}
func TestRejectsNonCanonicalManifest(t *testing.T) {
	m, bundle := fixture(t)
	c, e := m.Canonical()
	if e != nil {
		t.Fatal(e)
	}
	spaced := bytes.Replace(c, []byte(`"format":1`), []byte(`"format": 1`), 1)
	if bytes.Equal(spaced, c) {
		t.Fatal("spacing did not change the manifest")
	}
	if _, e = DecodeManifest(bytes.NewReader(spaced)); e == nil {
		t.Fatal("accepted spaced manifest")
	}
	reversed := m
	reversed.Files = append([]File(nil), m.Files...)
	sort.Slice(reversed.Files, func(i, j int) bool { return reversed.Files[i].Path > reversed.Files[j].Path })
	raw, e := json.Marshal(reversed)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(raw, c) {
		t.Fatal("reordered files matched canonical bytes")
	}
	if _, e = DecodeManifest(bytes.NewReader(raw)); e == nil {
		t.Fatal("accepted reordered manifest")
	}
	spacedBundle := rewrite(t, bundle, func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == "manifest.json" {
			d = spaced
		}
		return h, d
	})
	if _, e = InspectBundle(bytes.NewReader(spacedBundle)); e == nil {
		t.Fatal("inspect accepted spaced manifest")
	}
	reorderedBundle := rewrite(t, bundle, func(h *tar.Header, d []byte) (*tar.Header, []byte) {
		if h.Name == "manifest.json" {
			d = raw
		}
		return h, d
	})
	if _, e = InspectBundle(bytes.NewReader(reorderedBundle)); e == nil {
		t.Fatal("inspect accepted reordered manifest")
	}

	root := privateRoot(t)
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	rel, _, e := s.Install(context.Background(), bytes.NewReader(bundle))
	if e != nil {
		t.Fatal(e)
	}
	manifestPath := filepath.Join(root, "releases", rel.ID, "manifest.json")
	if e = os.Chmod(manifestPath, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(manifestPath, spaced, 0444); e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(manifestPath, 0444); e != nil {
		t.Fatal(e)
	}
	if _, e = VerifyDirectory(context.Background(), filepath.Join(root, "releases", rel.ID)); e == nil {
		t.Fatal("directory accepted spaced manifest")
	}
	if _, e = s.Read(context.Background(), rel.ID); e == nil {
		t.Fatal("read accepted spaced manifest")
	}
}
func TestStoreReclaimsInterruptedStaging(t *testing.T) {
	m, bundle := fixture(t)
	root := privateRoot(t)
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	rel, _, e := s.Install(context.Background(), bytes.NewReader(bundle))
	if e != nil {
		t.Fatal(e)
	}
	incoming := filepath.Join(root, "incoming")
	staged, e := os.MkdirTemp(incoming, "bundle-")
	if e != nil {
		t.Fatal(e)
	}
	nested := filepath.Join(staged, "share")
	if e = os.Mkdir(nested, 0755); e != nil {
		t.Fatal(e)
	}
	partial := filepath.Join(nested, "partial")
	if e = os.WriteFile(partial, bytes.Repeat([]byte("x"), 4096), 0444); e != nil {
		t.Fatal(e)
	}
	manual := filepath.Join(incoming, "bundle-42")
	if e = os.Mkdir(manual, 0700); e != nil {
		t.Fatal(e)
	}
	keep := filepath.Join(incoming, "keep")
	if e = os.Mkdir(keep, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(keep, "note"), []byte("stay"), 0600); e != nil {
		t.Fatal(e)
	}
	reopened, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(staged); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("mktemp staging remains", e)
	}
	if _, e = os.Lstat(manual); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("decimal staging remains", e)
	}
	if data, e := os.ReadFile(filepath.Join(keep, "note")); e != nil || string(data) != "stay" {
		t.Fatal("unrelated incoming entry removed", e)
	}
	if _, e = reopened.Read(context.Background(), rel.ID); e != nil {
		t.Fatal("published release removed", e)
	}
	link := filepath.Join(incoming, "bundle-7")
	target := filepath.Join(root, "releases", rel.ID)
	if e = os.Symlink(target, link); e != nil {
		t.Fatal(e)
	}
	if _, e = New(root, compatibility(m)); e == nil {
		t.Fatal("accepted staging symlink")
	}
	if _, e = os.Lstat(link); e != nil {
		t.Fatal("staging symlink removed", e)
	}
	if _, e = os.Lstat(target); e != nil {
		t.Fatal("symlink target removed", e)
	}
	if e = os.Remove(link); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(incoming, "bundle-8")
	if e = os.WriteFile(file, []byte("nope"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = New(root, compatibility(m)); e == nil {
		t.Fatal("accepted staging file")
	}
	if _, e = os.Lstat(file); e != nil {
		t.Fatal("staging file removed", e)
	}
}
func TestStoreReclaimWaitsForSharedLock(t *testing.T) {
	m, _ := fixture(t)
	root := privateRoot(t)
	if _, e := New(root, compatibility(m)); e != nil {
		t.Fatal(e)
	}
	orphan := filepath.Join(root, "incoming", "bundle-99")
	if e := os.Mkdir(orphan, 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(root, ".lock"), os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, err := New(root, compatibility(m))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("reclaimed while the store lock was held", err)
	case <-time.After(200 * time.Millisecond):
	}
	if _, e = os.Lstat(orphan); e != nil {
		t.Fatal("removed while the store lock was held", e)
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); e != nil {
		t.Fatal(e)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not finish after the lock was released")
	}
	if _, e = os.Lstat(orphan); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("orphan remains", e)
	}
}
func TestStoreRejectsSharedPermissions(t *testing.T) {
	m, bundle := fixture(t)
	root := privateRoot(t)
	for _, mode := range []os.FileMode{0755, 0770, 0777} {
		if e := os.Chmod(root, mode); e != nil {
			t.Fatal(e)
		}
		if _, e := New(root, compatibility(m)); e == nil {
			t.Fatalf("accepted store mode %o", mode)
		}
	}
	if _, e := os.Lstat(filepath.Join(root, "releases")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("created children in a shared store", e)
	}
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := New(root, compatibility(m)); e != nil {
		t.Fatal(e)
	}
	for _, child := range []string{"releases", "incoming"} {
		path := filepath.Join(root, child)
		if e := os.Chmod(path, 0755); e != nil {
			t.Fatal(e)
		}
		if _, e := New(root, compatibility(m)); e == nil {
			t.Fatalf("accepted %s mode 0755", child)
		}
		if e := os.Chmod(path, 0700); e != nil {
			t.Fatal(e)
		}
	}
	lock := filepath.Join(root, ".lock")
	if e := os.Chmod(lock, 0664); e != nil {
		t.Fatal(e)
	}
	if _, e := New(root, compatibility(m)); e == nil {
		t.Fatal("accepted group-writable lock")
	}
	if e := os.Chmod(lock, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := New(root, compatibility(m))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0707); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Install(context.Background(), bytes.NewReader(bundle)); e == nil {
		t.Fatal("used a store after it became world-accessible")
	}
}

func TestInventoryAuthenticatesManifestWithoutReadingPayload(t *testing.T) {
	m, archive := fixture(t)
	store, err := New(privateRoot(t), compatibility(m))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	release, _, err := store.Install(ctx, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(store.root, "releases", release.ID)
	payload := filepath.Join(dir, RequiredExecutables[0])
	if err := os.Chmod(payload, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, []byte("corrupt"), 0555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(payload, 0555); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != release.ID {
		t.Fatalf("inventory must read manifest independently of payload: %v %v", listed, err)
	}
	if _, err := store.Read(ctx, release.ID); err == nil {
		t.Fatal("activation read accepted corrupt payload")
	}
	if _, err := store.Path(ctx, release.ID); err == nil {
		t.Fatal("execution path accepted corrupt payload")
	}
	manifest := filepath.Join(dir, "manifest.json")
	if err := os.Chmod(manifest, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("{}"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifest, 0444); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx); err == nil {
		t.Fatal("inventory accepted invalid manifest")
	}
}

type cancelAfterContextChecks struct {
	context.Context
	cancel      context.CancelFunc
	checks      int
	cancelAfter int
}

func (c *cancelAfterContextChecks) Err() error {
	c.checks++
	if c.checks > c.cancelAfter {
		c.cancel()
	}
	return c.Context.Err()
}

func TestListCanceledDuringLastManifestRead(t *testing.T) {
	m, archive := fixture(t)
	store, err := New(privateRoot(t), compatibility(m))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Install(context.Background(), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// List checks the context twice before reading manifests; the manifest
	// reader checks once before opening the last manifest. Expire it on the
	// next check, after that manifest has been read and authenticated.
	inventoryCtx := &cancelAfterContextChecks{Context: ctx, cancel: cancel, cancelAfter: 3}
	if _, err := store.List(inventoryCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("List error = %v, want context.Canceled", err)
	}
}
