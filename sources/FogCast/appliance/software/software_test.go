package software

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
)

func fixture(t *testing.T) (Manifest, []byte) {
	t.Helper()
	files := fstest.MapFS{}
	m := Manifest{Format: 1, Board: "de10-nano", Architecture: "armv7", BaseContract: "fes-base-v1", Version: "1.0", Interfaces: map[string]string{"runtime": "2"}, Provenance: map[string]string{"fes": "different-from-host"}}
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
	return Compatibility{m.Board, m.Architecture, m.BaseContract, map[string]string{"runtime": "2"}}
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
	root := t.TempDir()
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
	root := t.TempDir()
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
	root := t.TempDir()
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
	root := t.TempDir()
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
