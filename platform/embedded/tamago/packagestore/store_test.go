// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

type testLease struct {
	mu                       sync.Mutex
	r                        sdcontrol.Receipt
	valid                    bool
	stops, resumes           int
	stopErr, resumeErr       error
	stopChange, resumeChange func(*sdcontrol.Receipt)
	onStop                   func()
}

func lease() *testLease {
	return &testLease{r: sdcontrol.Receipt{Owner: 17, Generation: 3, Active: true, Prepared: true}, valid: true}
}
func (l *testLease) LastReceipt() (sdcontrol.Receipt, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r, l.valid
}
func (l *testLease) Quiesce() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stops++
	if l.onStop != nil {
		l.onStop()
	}
	if l.stopErr != nil {
		return l.stopErr
	}
	l.r.Active = false
	l.r.Prepared = false
	l.r.Generation++
	if l.stopChange != nil {
		l.stopChange(&l.r)
	}
	return nil
}
func (l *testLease) Resume() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resumes++
	if l.resumeErr != nil {
		return l.resumeErr
	}
	l.r.Active = true
	l.r.Prepared = true
	l.r.Generation += 2
	if l.resumeChange != nil {
		l.resumeChange(&l.r)
	}
	return nil
}
func hash(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func read(t *testing.T, path string) []byte {
	t.Helper()
	v, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func canonical(t *testing.T, manifest, payload, mapping []byte) []byte {
	t.Helper()
	dir := t.TempDir()
	for name, data := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload, "rom-map.json": mapping} {
		if data != nil {
			if e := os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	data, e := corepackage.CanonicalArchive(dir)
	if e != nil {
		t.Fatal(e)
	}
	return data
}
func basic(t *testing.T) ([]byte, Request, []byte) {
	t.Helper()
	base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2"
	m := read(t, base+"/manifests/valid-basic.toml")
	p := read(t, base+"/payloads/fes-fixture.rbf")
	return canonical(t, m, p, nil), Request{Path: "FES/CORE.FCP", PackageID: "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0"}, p
}

type trackedFS struct {
	base                  fs.FS
	lease                 *testLease
	opens, reads, handles atomic.Int64
	closeErr              error
	readHook              func([]byte) (int, error)
}

func (f *trackedFS) Open(name string) (fs.File, error) {
	f.opens.Add(1)
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active {
		return nil, errors.New("unexpected filesystem access while contained")
	}
	v, e := f.base.Open(name)
	if e != nil {
		return nil, e
	}
	f.handles.Add(1)
	return &trackedFile{File: v, fs: f}, nil
}

type trackedFile struct {
	fs.File
	fs *trackedFS
}

func (f *trackedFile) Read(out []byte) (int, error) {
	f.fs.reads.Add(1)
	if f.fs.readHook != nil {
		return f.fs.readHook(out)
	}
	r, ok := f.fs.lease.LastReceipt()
	if !ok || !r.Active {
		return 0, errors.New("unexpected read while contained")
	}
	return f.File.Read(out)
}
func (f *trackedFile) Close() error {
	f.fs.handles.Add(-1)
	return errors.Join(f.File.Close(), f.fs.closeErr)
}
func open(t *testing.T, files fs.FS, l *testLease) (*Store, *trackedFS) {
	t.Helper()
	f := &trackedFS{base: files, lease: l}
	s, e := New(f, l)
	if e != nil {
		t.Fatal(e)
	}
	return s, f
}

func TestNewRequiresPreparedActiveOwnership(t *testing.T) {
	files := fstest.MapFS{}
	for _, change := range []func(*testLease){
		func(l *testLease) { l.valid = false },
		func(l *testLease) { l.r.Owner = 0 },
		func(l *testLease) { l.r.Generation = 0 },
		func(l *testLease) { l.r.Active = false },
		func(l *testLease) { l.r.Prepared = false },
	} {
		l := lease()
		change(l)
		if s, e := New(files, l); !errors.Is(e, ErrState) || s != nil {
			t.Fatal("invalid ownership created store", e)
		}
		if l.stops != 0 || l.resumes != 0 {
			t.Fatal("constructor mutated lease")
		}
	}
	if _, e := New(nil, lease()); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	if _, e := New(files, nil); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
}

func TestSelectedInputRejectsChangingUnboundedOrStalledFiles(t *testing.T) {
	for _, fault := range []string{"short", "extra", "stalled", "invalid-count", "read-error", "close-error", "canceled", "oversize", "directory"} {
		t.Run(fault, func(t *testing.T) {
			l := lease()
			file := &fstest.MapFile{Data: []byte("rom")}
			_, files := open(t, fstest.MapFS{"ROM.BIN": file}, l)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "short":
				r := bytes.NewReader([]byte("ro"))
				files.readHook = r.Read
			case "extra":
				r := bytes.NewReader([]byte("rom!"))
				files.readHook = r.Read
			case "stalled":
				files.readHook = func([]byte) (int, error) { return 0, nil }
			case "invalid-count":
				files.readHook = func(out []byte) (int, error) { return len(out) + 1, nil }
			case "read-error":
				files.readHook = func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
			case "close-error":
				files.closeErr = errors.New("failed close")
			case "canceled":
				files.readHook = func(out []byte) (int, error) { cancel(); return copy(out, "rom"), io.EOF }
			case "oversize":
				file.Data = []byte("large")
			case "directory":
				file.Mode = fs.ModeDir
			}
			if data, e := readInput(ctx, files, "ROM.BIN", 3, 3, hash([]byte("rom"))); e == nil || data != nil {
				t.Fatal("invalid selected input retained", e)
			}
			if files.handles.Load() != 0 || files.reads.Load() > 100 || l.stops != 0 || l.resumes != 0 {
				t.Fatal("invalid input leaked handle, retried indefinitely or changed lease")
			}
		})
	}
}

func TestRetainContainResumeAndStaleSelections(t *testing.T) {
	body, req, want := basic(t)
	files := fstest.MapFS{req.Path: &fstest.MapFile{Data: body}}
	l := lease()
	s, f := open(t, files, l)
	p, e := s.Retain(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if f.handles.Load() != 0 {
		t.Fatal("retained an open source handle")
	}
	// Corruption after retention cannot change this selection; a fresh read must
	// reject it without stopping the still-healthy current storage owner.
	original := bytes.Clone(body)
	body[0] ^= 1
	if v, e := s.Retain(context.Background(), req); e == nil || v != nil || l.stops != 0 {
		t.Fatal("corrupt source accepted or owner changed", e)
	}
	copy(body, original)
	meta := p.Inspection()
	meta.Descriptor.Core.ID = "mutated"
	if p.Inspection().Descriptor.Core.ID == "mutated" {
		t.Fatal("metadata alias")
	}
	h, e := s.Contain(p)
	if e != nil {
		t.Fatal(e)
	}
	if h.Receipt() != (sdcontrol.Receipt{Owner: 17, Generation: 4}) {
		t.Fatal(h.Receipt())
	}
	before := f.reads.Load()
	current, e := h.Selection()
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(current.Archive().Payload())
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("retained bytes", e)
	}
	if f.reads.Load() != before {
		t.Fatal("contained read touched storage")
	}
	if _, e = s.Retain(context.Background(), req); !errors.Is(e, ErrState) {
		t.Fatal("contained source access", e)
	}
	if e = s.Resume(); e != nil {
		t.Fatal(e)
	}
	if _, e = h.Selection(); !errors.Is(e, ErrState) {
		t.Fatal("stale handoff survived resume", e)
	}
	if _, e = s.Contain(p); !errors.Is(e, ErrState) || l.stops != 1 {
		t.Fatal("stale selection stopped new lease", e)
	}
	p, e = s.Retain(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	h, e = s.Contain(p)
	if e != nil || h.Receipt().Generation != 7 || l.stops != 2 || l.resumes != 1 {
		t.Fatal("second containment", e)
	}
	other, _ := open(t, files, lease())
	if _, e = other.Contain(p); !errors.Is(e, ErrState) {
		t.Fatal("foreign selection", e)
	}
	if _, e = (*Handoff)(nil).Selection(); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
}

func TestInvalidRequestsAndCancellationKeepActiveOwner(t *testing.T) {
	body, req, _ := basic(t)
	l := lease()
	s, f := open(t, fstest.MapFS{req.Path: &fstest.MapFile{Data: body}}, l)
	for _, change := range []func(*Request){
		func(r *Request) { r.Path = "../CORE.FCP" }, func(r *Request) { r.Path = "/FES/CORE.FCP" },
		func(r *Request) { r.PackageID = "bad" }, func(r *Request) { r.Parts = []Part{{ID: "bad", Path: "FES/PART", SHA256: strings.Repeat("a", 64)}} },
	} {
		bad := req
		change(&bad)
		if p, e := s.Retain(context.Background(), bad); e == nil || p != nil {
			t.Fatal("bad request", bad)
		}
	}
	if f.opens.Load() != 0 {
		t.Fatal("invalid request reached filesystem")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, e := s.Retain(ctx, req); !errors.Is(e, context.Canceled) || p != nil {
		t.Fatal("cancel", e)
	}
	if f.opens.Load() != 0 {
		t.Fatal("canceled request opened storage")
	}
	missing := req
	missing.Path = "FES/MISSING.FCP"
	if _, e := s.Retain(context.Background(), missing); !errors.Is(e, fs.ErrNotExist) {
		t.Fatal("missing", e)
	}
	wrong := req
	wrong.PackageID = strings.Repeat("0", 64)
	if p, e := s.Retain(context.Background(), wrong); e == nil || p != nil {
		t.Fatal("wrong identity")
	}
	f.closeErr = errors.New("close failed")
	if p, e := s.Retain(context.Background(), req); e == nil || p != nil {
		t.Fatal("close error published selection")
	}
	if l.stops != 0 || l.resumes != 0 || f.handles.Load() != 0 {
		t.Fatal("rejection changed lease/handle ownership")
	}
}

func TestFailedOrChangedLeaseNeverPublishesHandoffOrRetries(t *testing.T) {
	body, req, _ := basic(t)
	for _, fault := range []string{"stop-error", "stop-owner", "stop-generation", "stop-prepared", "resume-error", "resume-owner", "resume-generation", "resume-state", "read-retired"} {
		t.Run(fault, func(t *testing.T) {
			l := lease()
			s, f := open(t, fstest.MapFS{req.Path: &fstest.MapFile{Data: body}}, l)
			if fault == "read-retired" {
				f.readHook = func([]byte) (int, error) { l.mu.Lock(); l.valid = false; l.mu.Unlock(); return 0, io.ErrUnexpectedEOF }
				if p, e := s.Retain(context.Background(), req); !errors.Is(e, ErrRetired) || p != nil {
					t.Fatal(e)
				}
			} else {
				p, e := s.Retain(context.Background(), req)
				if e != nil {
					t.Fatal(e)
				}
				switch fault {
				case "stop-error":
					l.stopErr = errors.New("uncertain reset")
				case "stop-owner":
					l.stopChange = func(r *sdcontrol.Receipt) { r.Owner++ }
				case "stop-generation":
					l.stopChange = func(r *sdcontrol.Receipt) { r.Generation++ }
				case "stop-prepared":
					l.stopChange = func(r *sdcontrol.Receipt) { r.Prepared = true }
				}
				h, e := s.Contain(p)
				if strings.HasPrefix(fault, "stop-") {
					if !errors.Is(e, ErrRetired) || h != nil {
						t.Fatal("bad stop published handoff", e)
					}
				} else {
					if e != nil {
						t.Fatal(e)
					}
					switch fault {
					case "resume-error":
						l.resumeErr = errors.New("uncertain resume")
					case "resume-owner":
						l.resumeChange = func(r *sdcontrol.Receipt) { r.Owner++ }
					case "resume-generation":
						l.resumeChange = func(r *sdcontrol.Receipt) { r.Generation-- }
					case "resume-state":
						l.resumeChange = func(r *sdcontrol.Receipt) { r.Prepared = false }
					}
					if e = s.Resume(); !errors.Is(e, ErrRetired) {
						t.Fatal(e)
					}
					if _, e = h.Selection(); !errors.Is(e, ErrRetired) {
						t.Fatal("failed owner handoff", e)
					}
				}
			}
			before, stops, resumes := f.opens.Load(), l.stops, l.resumes
			if p, e := s.Retain(context.Background(), req); !errors.Is(e, ErrRetired) || p != nil {
				t.Fatal("retired retain", e)
			}
			if _, e := s.Contain(nil); !errors.Is(e, ErrRetired) {
				t.Fatal("retired contain", e)
			}
			if e := s.Resume(); !errors.Is(e, ErrRetired) {
				t.Fatal("retired resume", e)
			}
			if f.opens.Load() != before || l.stops != stops || l.resumes != resumes || f.handles.Load() != 0 {
				t.Fatal("failure retried I/O or leaked handle")
			}
		})
	}
}

func TestContainWaitsForRetainsAndClosedFileHandles(t *testing.T) {
	body, req, _ := basic(t)
	l := lease()
	s, f := open(t, fstest.MapFS{req.Path: &fstest.MapFile{Data: body}}, l)
	p, e := s.Retain(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	started, release, stop := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	reader := bytes.NewReader(body)
	f.readHook = func(out []byte) (int, error) { once.Do(func() { close(started); <-release }); return reader.Read(out) }
	l.onStop = func() {
		if f.handles.Load() != 0 {
			t.Error("stopped with retained file handle still active")
		}
		close(stop)
	}
	retained := make(chan error, 1)
	go func() { _, e := s.Retain(context.Background(), req); retained <- e }()
	<-started
	contained := make(chan error, 1)
	go func() { _, e := s.Contain(p); contained <- e }()
	select {
	case <-stop:
		t.Fatal("contained during active retain")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if e := <-retained; e != nil {
		t.Fatal(e)
	}
	if e := <-contained; e != nil {
		t.Fatal(e)
	}
	<-stop
}

type guardedBlock struct {
	device        *block.File
	lease         *testLease
	reads, writes atomic.Int64
}

func (d *guardedBlock) SectorCount() uint64 { return d.device.SectorCount() }
func (d *guardedBlock) ReadSectors(lba uint64, out []byte) error {
	r, ok := d.lease.LastReceipt()
	if !ok || !r.Active {
		return errors.New("contained block access")
	}
	d.reads.Add(1)
	return d.device.ReadSectors(lba, out)
}
func (d *guardedBlock) WriteSectors(uint64, []byte) error { d.writes.Add(1); return block.ErrReadOnly }
func (d *guardedBlock) Flush() error                      { return d.device.Flush() }

func TestRealReadOnlyFATPackageRemainsUsableWhileSDContained(t *testing.T) {
	for _, name := range []string{"mformat", "mmd", "mcopy"} {
		if _, e := exec.LookPath(name); e != nil {
			t.Fatal("required FAT test tool", name, e)
		}
	}
	body, req, want := basic(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sd.img")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(64 << 20); e != nil {
		t.Fatal(e)
	}
	f.Close()
	command := func(args ...string) {
		t.Helper()
		if data, e := exec.Command(args[0], args[1:]...).CombinedOutput(); e != nil {
			t.Fatalf("%v: %v %s", args, e, data)
		}
	}
	command("mformat", "-i", path, "-F", "::")
	command("mmd", "-i", path, "::FES")
	input := filepath.Join(dir, "CORE.FCP")
	if e = os.WriteFile(input, body, 0600); e != nil {
		t.Fatal(e)
	}
	command("mcopy", "-i", path, input, "::FES/CORE.FCP")
	fileHash := func() string {
		file, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer file.Close()
		digest := sha256.New()
		if _, e = io.Copy(digest, file); e != nil {
			t.Fatal(e)
		}
		return hex.EncodeToString(digest.Sum(nil))
	}
	before := fileHash()
	device, e := block.OpenFile(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer device.Close()
	l := lease()
	disk := &guardedBlock{device: device, lease: l}
	volume, e := fat.Mount(disk)
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(volume, l)
	if e != nil {
		t.Fatal(e)
	}
	for cycle := 0; cycle < 2; cycle++ {
		p, e := s.Retain(context.Background(), req)
		if e != nil {
			t.Fatal(e)
		}
		h, e := s.Contain(p)
		if e != nil {
			t.Fatal(e)
		}
		count := disk.reads.Load()
		p, e = h.Selection()
		if e != nil {
			t.Fatal(e)
		}
		got, e := io.ReadAll(p.Archive().Payload())
		if e != nil || !bytes.Equal(got, want) {
			t.Fatal("memory payload", e)
		}
		if disk.reads.Load() != count || disk.writes.Load() != 0 {
			t.Fatal("contained payload touched card")
		}
		if cycle == 0 {
			if e = s.Resume(); e != nil {
				t.Fatal(e)
			}
		}
	}
	if before != fileHash() {
		t.Fatal("read-only package retention changed FAT image")
	}
}

func TestROMInputsUseExistingLinkerAndSurviveStorageContainment(t *testing.T) {
	base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2"
	manifest := string(read(t, base+"/manifests/valid-basic.toml"))
	original := read(t, base+"/payloads/fes-fixture.rbf")
	vectors := "../../../../sources/misteross/expansion/testdata/rom/"
	unzip := func(name string) []byte {
		t.Helper()
		f, e := os.Open(vectors + name + ".gz")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		z, e := gzip.NewReader(f)
		if e != nil {
			t.Fatal(e)
		}
		defer z.Close()
		v, e := io.ReadAll(z)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	rbf, mapping, rom := unzip("blank.rbf"), unzip("map.json"), unzip("ramp.rom")
	var oracle struct {
		Cases []struct {
			Name string `json:"name"`
			SHA  string `json:"rbf_sha256"`
		}
	}
	if e := json.Unmarshal(read(t, vectors+"oracle.json"), &oracle); e != nil {
		t.Fatal(e)
	}
	golden := ""
	for _, c := range oracle.Cases {
		if c.Name == "ramp" {
			golden = c.SHA
		}
	}
	if golden == "" {
		t.Fatal("missing independent Mistral oracle")
	}
	for _, format := range []int{3, 4} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			m := strings.Replace(manifest, "format = 2", fmt.Sprintf("format = %d", format), 1)
			m = strings.Replace(m, "size = 12", fmt.Sprintf("size = %d", len(rbf)), 1)
			m = strings.ReplaceAll(m, hash(original), hash(rbf))
			mapFields := fmt.Sprintf("file = \"rom-map.json\"\nsize = %d\nsha256 = \"%s\"\n", len(mapping), hash(mapping))
			sources := []corepackage.ROMSource{{ID: "machine-rom", Role: "firmware", Data: rom}}
			if format == 3 {
				m += fmt.Sprintf("\n[rom]\nid = \"machine-rom\"\nrole = \"firmware\"\nsource_size = %d\n", len(rom)) + mapFields
			} else {
				m += fmt.Sprintf("\n[[roms]]\nid = \"bios\"\nrole = \"firmware\"\nsource_size = 1024\nsource_offset = 0\n\n[[roms]]\nid = \"cart\"\nrole = \"cartridge\"\nsource_size = %d\nsource_offset = 1024\n\n[rom_map]\n", len(rom)-1024) + mapFields
				sources = []corepackage.ROMSource{{ID: "bios", Role: "firmware", Data: rom[:1024]}, {ID: "cart", Role: "cartridge", Data: rom[1024:]}}
			}
			body := canonical(t, []byte(m), rbf, mapping)
			archive, e := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
			if e != nil {
				t.Fatal(e)
			}
			linked, e := archive.LinkROMs(context.Background(), sources)
			if e != nil {
				t.Fatal(e)
			}
			req := Request{Path: "FES/ROM.FCP", PackageID: archive.Inspection().PackageID, ReceiptSHA256: linked.ReceiptSHA256()}
			files := fstest.MapFS{req.Path: &fstest.MapFile{Data: body}}
			for i, v := range sources {
				path := fmt.Sprintf("FES/ROM%d.BIN", i)
				files[path] = &fstest.MapFile{Data: bytes.Clone(v.Data)}
				req.Sources = append(req.Sources, Source{ID: v.ID, Role: v.Role, Path: path, SHA256: hash(v.Data)})
			}
			l := lease()
			s, f := open(t, files, l)
			for _, mutation := range []string{"receipt", "digest", "order", "source-size"} {
				bad := req
				bad.Sources = append([]Source(nil), req.Sources...)
				switch mutation {
				case "receipt":
					bad.ReceiptSHA256 = strings.Repeat("0", 64)
				case "digest":
					bad.Sources[0].SHA256 = strings.Repeat("0", 64)
				case "order":
					bad.Sources[0].Role = "wrong"
				case "source-size":
					files[bad.Sources[0].Path].Data = rom[:3]
				}
				if p, e := s.Retain(context.Background(), bad); e == nil || p != nil {
					t.Fatal("bad ROM inputs", mutation)
				}
				files[req.Sources[0].Path].Data = bytes.Clone(sources[0].Data)
			}
			p, e := s.Retain(context.Background(), req)
			if e != nil {
				t.Fatal(e)
			}
			files[req.Sources[0].Path].Data[0] ^= 1
			h, e := s.Contain(p)
			if e != nil {
				t.Fatal(e)
			}
			count := f.reads.Load()
			p, e = h.Selection()
			if e != nil {
				t.Fatal(e)
			}
			data, e := io.ReadAll(p.Linked().Programmed())
			if e != nil || hash(data) != golden {
				t.Fatal("linked output differs from oracle", e)
			}
			retained, e := p.Linked().Source(sources[0].ID, sources[0].Role)
			if e != nil {
				t.Fatal(e)
			}
			data, e = io.ReadAll(retained)
			if e != nil || !bytes.Equal(data, sources[0].Data) {
				t.Fatal("retained ROM changed", e)
			}
			if f.reads.Load() != count || f.handles.Load() != 0 {
				t.Fatal("contained ROM linked through storage")
			}
		})
	}
}

func TestActiveReceiptReportsOnlyThisStoresUnchangedUsableLease(t *testing.T) {
	l := lease()
	s, files := open(t, fstest.MapFS{}, l)
	r, err := s.ActiveReceipt()
	if err != nil || r != l.r || files.opens.Load() != 0 || l.stops != 0 || l.resumes != 0 {
		t.Fatal(r, err)
	}
	l.mu.Lock()
	l.r.Generation++
	l.mu.Unlock()
	if _, err = s.ActiveReceipt(); !errors.Is(err, ErrRetired) {
		t.Fatal("changed lease accepted", err)
	}
	l.mu.Lock()
	l.r.Generation--
	l.mu.Unlock()
	if _, err = s.ActiveReceipt(); !errors.Is(err, ErrRetired) {
		t.Fatal("retired store revived", err)
	}
	if l.stops != 0 || l.resumes != 0 || files.opens.Load() != 0 {
		t.Fatal("query performed transition or file I/O")
	}
}
