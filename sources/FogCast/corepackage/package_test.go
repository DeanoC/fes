package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fixtureCase struct {
	Name      string `json:"name"`
	Manifest  string `json:"manifest"`
	Payload   string `json:"payload"`
	Valid     bool   `json:"valid"`
	PackageID string `json:"package_id"`
}

type uriCases struct {
	Cases []struct {
		Repository string `json:"repository"`
		Valid      bool   `json:"valid"`
	} `json:"cases"`
}

type cancelAtEOFReader struct {
	data   []byte
	cancel context.CancelFunc
}

type markAtEOFReader struct {
	data []byte
	done *bool
}

func (r *markAtEOFReader) Read(output []byte) (int, error) {
	if len(r.data) == 0 {
		*r.done = true
		return 0, io.EOF
	}
	count := copy(output, r.data)
	r.data = r.data[count:]
	if len(r.data) == 0 {
		*r.done = true
		return count, io.EOF
	}
	return count, nil
}

type publicationCancelContext struct {
	context.Context
	bodyDone *bool
	checks   int
}

func (c *publicationCancelContext) Err() error {
	if *c.bodyDone {
		c.checks++
		if c.checks >= 3 {
			return context.Canceled
		}
	}
	return nil
}

func (r *cancelAtEOFReader) Read(output []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	count := copy(output, r.data)
	r.data = r.data[count:]
	if len(r.data) == 0 {
		r.cancel()
		return count, io.EOF
	}
	return count, nil
}

func loadCases(t *testing.T) []fixtureCase {
	t.Helper()
	data, err := os.ReadFile("testdata/core-bundle-v2/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func fixtureBytes(t *testing.T, c fixtureCase) ([]byte, []byte) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join("testdata/core-bundle-v2", c.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join("testdata/core-bundle-v2", c.Payload))
	if err != nil {
		t.Fatal(err)
	}
	return manifest, payload
}

func writeDirectory(t *testing.T, manifest, payload []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.rbf"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInspectMatchesSharedConformanceCorpus(t *testing.T) {
	for _, c := range loadCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			manifest, payload := fixtureBytes(t, c)
			descriptor, err := Inspect(writeDirectory(t, manifest, payload))
			if c.Valid && err != nil {
				t.Fatalf("valid fixture rejected: %v", err)
			}
			if !c.Valid && err == nil {
				t.Fatalf("invalid fixture accepted: %#v", descriptor)
			}
			if c.Valid && descriptor.Format != 2 {
				t.Fatalf("format=%d", descriptor.Format)
			}
		})
	}
}

func TestInspectPackageReturnsIdentityAndDescriptorFromOneRead(t *testing.T) {
	c := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, c)
	result, err := InspectPackage(writeDirectory(t, manifest, payload))
	if err != nil {
		t.Fatal(err)
	}
	if result.PackageID != c.PackageID {
		t.Fatalf("package ID=%q", result.PackageID)
	}
	if result.Descriptor.Core.ID != "fes.pong" {
		t.Fatalf("descriptor=%#v", result.Descriptor)
	}
}

func TestInspectPackageReturnsEveryClosedDescriptorField(t *testing.T) {
	c := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, c)
	expected := Descriptor{
		Format: 2,
		Core: Core{ID: "fes.pong", Name: "FES Pong",
			Description: "Synthetic test-only core bundle fixture; never deploy.",
			Version:     "0.1.0"},
		Target: Target{Platform: "de10_nano", Device: "5CSEBA6U23I7",
			ProgrammingProfile: "fes-gp-v1"},
		Payload: Payload{File: "core.rbf", Size: 12,
			SHA256: "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1"},
		ABI: Contract{ID: "fes.simple-game", Major: 1, Minor: 0},
		Interfaces: []Interface{
			{ID: "fes.gamepad", Major: 1, Minor: 0, Required: true},
			{ID: "fes.video.fixed-720p60", Major: 1, Minor: 0, Required: true},
		},
		Build: Build{ID: "0123456789abcdef0123456789abcdef",
			Repository:   "https://example.invalid/fes-pong",
			Revision:     "1111111111111111111111111111111111111111",
			RecipeSHA256: "2222222222222222222222222222222222222222222222222222222222222222",
			Toolchain:    "synthetic fixture generator 1.0 (test-only)"},
	}
	for _, tc := range []struct {
		name     string
		manifest []byte
		expected Descriptor
	}{
		{name: "absent-system", manifest: manifest, expected: expected},
		{name: "present-system", manifest: bytes.Replace(manifest,
			[]byte("version = \"0.1.0\""),
			[]byte("version = \"0.1.0\"\nsystem = \"megadrive\""), 1),
			expected: func() Descriptor {
				withSystem := expected
				withSystem.Core.System = "megadrive"
				return withSystem
			}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := InspectPackage(writeDirectory(t, tc.manifest, payload))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Descriptor, tc.expected) {
				t.Fatalf("descriptor mismatch:\n got %#v\nwant %#v",
					result.Descriptor, tc.expected)
			}
		})
	}
}

func TestRepositoryURIParity(t *testing.T) {
	data, err := os.ReadFile("testdata/repository-uri-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases uriCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	base := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, base)
	const original = "https://example.invalid/fes-pong"
	if !bytes.Contains(manifest, []byte(original)) {
		t.Fatal("fixture repository changed")
	}
	for _, c := range cases.Cases {
		t.Run(c.Repository, func(t *testing.T) {
			changed := bytes.Replace(manifest, []byte(original), []byte(c.Repository), 1)
			_, err := Inspect(writeDirectory(t, changed, payload))
			if (err == nil) != c.Valid {
				t.Fatalf("valid=%v err=%v", c.Valid, err)
			}
		})
	}
}

func octal(value int64, width int) []byte {
	return []byte(fmt.Sprintf("%0*o\x00", width-1, value))
}

func testCanonicalHeader(name string, size int64) []byte {
	header := make([]byte, 512)
	copy(header[0:100], name)
	copy(header[100:108], []byte("0000644\x00"))
	copy(header[108:116], []byte("0000000\x00"))
	copy(header[116:124], []byte("0000000\x00"))
	copy(header[124:136], octal(size, 12))
	copy(header[136:148], []byte("00000000000\x00"))
	for i := 148; i < 156; i++ {
		header[i] = ' '
	}
	header[156] = '0'
	copy(header[257:263], []byte("ustar\x00"))
	copy(header[263:265], []byte("00"))
	var sum int
	for _, value := range header {
		sum += int(value)
	}
	copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
	return header
}

func archiveEntries(entries ...struct {
	name string
	data []byte
}) []byte {
	var output bytes.Buffer
	for _, entry := range entries {
		output.Write(testCanonicalHeader(entry.name, int64(len(entry.data))))
		output.Write(entry.data)
		padding := (512 - len(entry.data)%512) % 512
		output.Write(make([]byte, padding))
	}
	output.Write(make([]byte, 1024))
	return output.Bytes()
}

func canonicalArchive(manifest, payload []byte) []byte {
	return archiveEntries(struct {
		name string
		data []byte
	}{"manifest.toml", manifest},
		struct {
			name string
			data []byte
		}{"core.rbf", payload})
}

func TestInspectRequiresRestrictedUstarAndSafeDirectory(t *testing.T) {
	c := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, c)
	archive := canonicalArchive(manifest, payload)
	path := filepath.Join(t.TempDir(), "core.fcore")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(path); err != nil {
		t.Fatalf("canonical archive: %v", err)
	}

	third := archiveEntries(struct {
		name string
		data []byte
	}{"manifest.toml", manifest},
		struct {
			name string
			data []byte
		}{"core.rbf", payload}, struct {
			name string
			data []byte
		}{"extra", []byte("x")})
	reversed := archiveEntries(struct {
		name string
		data []byte
	}{"core.rbf", payload},
		struct {
			name string
			data []byte
		}{"manifest.toml", manifest})
	mutations := map[string][]byte{
		"truncated":        archive[:len(archive)-1],
		"extra-zero-block": append(append([]byte(nil), archive...), make([]byte, 512)...),
		"third-member":     third,
		"reversed":         reversed,
		"header-extension": func() []byte { b := append([]byte(nil), archive...); b[156] = 'x'; return b }(),
		"base256-size":     func() []byte { b := append([]byte(nil), archive...); b[124] = 0x80; return b }(),
		"bad-checksum":     func() []byte { b := append([]byte(nil), archive...); b[0] ^= 1; return b }(),
		"nonzero-padding":  func() []byte { b := append([]byte(nil), archive...); b[512+len(manifest)] = 1; return b }(),
	}
	for name, content := range mutations {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "bad.fcore")
			if err := os.WriteFile(p, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Inspect(p); err == nil {
				t.Fatal("malformed archive accepted")
			}
		})
	}

	dir := writeDirectory(t, manifest, payload)
	if err := os.WriteFile(filepath.Join(dir, "extra"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(dir); err == nil {
		t.Fatal("extra directory member accepted")
	}
	linkRoot := t.TempDir()
	if err := os.Symlink(path, filepath.Join(linkRoot, "package")); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(filepath.Join(linkRoot, "package")); err == nil {
		t.Fatal("package symlink accepted")
	}
	memberLink := t.TempDir()
	if err := os.WriteFile(filepath.Join(memberLink, "manifest.toml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", filepath.Base(path)), filepath.Join(memberLink, "core.rbf")); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(memberLink); err == nil {
		t.Fatal("member symlink accepted")
	}
}

func TestReadDirectoryRejectsRootReplacedAfterAdmission(t *testing.T) {
	c := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, c)
	parent := t.TempDir()
	path := filepath.Join(parent, "package")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "manifest.toml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "core.rbf"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	admitted, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".admitted"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "manifest.toml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "core.rbf"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readDirectory(path, admitted); err == nil {
		t.Fatal("replacement directory accepted after initial admission")
	}
}

func TestStagePublishesPrivateExactPackageWithoutAliasing(t *testing.T) {
	c := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, c)
	archive := canonicalArchive(manifest, payload)
	root := t.TempDir()
	first, err := Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = first.Cleanup()
	})
	if first.PackageID != c.PackageID || first.Descriptor.Core.ID != "fes.pong" {
		t.Fatalf("staged=%#v", first)
	}
	if filepath.Dir(first.Directory) != root {
		t.Fatalf("outside root: %s", first.Directory)
	}
	directoryInfo, err := os.Stat(first.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0o500 {
		t.Fatalf("directory mode=%o", directoryInfo.Mode().Perm())
	}
	for name, expected := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload} {
		actual, err := os.ReadFile(filepath.Join(first.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("%s changed", name)
		}
		info, err := os.Stat(filepath.Join(first.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o400 {
			t.Fatalf("%s mode=%o", name, info.Mode().Perm())
		}
	}
	second, err := Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = second.Cleanup()
	})
	if first.Directory == second.Directory {
		t.Fatal("separate staging requests aliased")
	}
	if _, err := os.Stat(first.Directory); err != nil {
		t.Fatalf("first publication removed: %v", err)
	}
	retainedManifest, err := os.ReadFile(filepath.Join(first.Directory, "manifest.toml"))
	if err != nil || !bytes.Equal(retainedManifest, manifest) {
		t.Fatalf("first publication mutated: %v", err)
	}
	if err := first.Cleanup(); err != nil {
		t.Fatalf("public cleanup: %v", err)
	}
	if _, err := os.Lstat(first.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleaned publication remains: %v", err)
	}
	if err := first.Cleanup(); err != nil {
		t.Fatalf("idempotent public cleanup: %v", err)
	}
}

func TestStageRejectsBoundsCancellationAndCleansIncompleteData(t *testing.T) {
	c := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, c)
	archive := canonicalArchive(manifest, payload)
	if _, err := Stage(context.Background(), ".", int64(len(archive)), bytes.NewReader(archive)); err == nil {
		t.Fatal("relative staging root accepted")
	}
	corrupt := append([]byte(nil), archive...)
	corrupt[0] ^= 1
	cases := []struct {
		name      string
		length    int64
		body      []byte
		cancelled bool
	}{
		{"zero-length", 0, nil, false},
		{"over-bound", MaxArchiveSize + 1, nil, false},
		{"declared-truncated", int64(len(archive) + 1), archive, false},
		{"declared-extra", int64(len(archive) - 1), archive, false},
		{"corrupt", int64(len(archive)), corrupt, false},
		{"cancelled", int64(len(archive)), archive, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ctx := context.Background()
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := Stage(ctx, root, tc.length, bytes.NewReader(tc.body)); err == nil {
				t.Fatal("invalid stage accepted")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("incomplete data retained: %v", entries)
			}
		})
	}

	cancelRoot := t.TempDir()
	cancelCtx, cancel := context.WithCancel(context.Background())
	late := &cancelAtEOFReader{data: append([]byte(nil), archive...), cancel: cancel}
	if _, err := Stage(cancelCtx, cancelRoot, int64(len(archive)), late); !errors.Is(err, context.Canceled) {
		t.Fatalf("late cancellation error=%v", err)
	}
	entries, err := os.ReadDir(cancelRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("late cancellation retained data: %v %v", entries, err)
	}

	publicationRoot := t.TempDir()
	bodyDone := false
	publicationContext := &publicationCancelContext{
		Context: context.Background(), bodyDone: &bodyDone}
	publicationReader := &markAtEOFReader{
		data: append([]byte(nil), archive...), done: &bodyDone}
	publication, err := Stage(publicationContext, publicationRoot,
		int64(len(archive)), publicationReader)
	if err == nil {
		t.Cleanup(func() { _ = publication.Cleanup() })
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("publication cancellation error=%v", err)
	}
	entries, err = os.ReadDir(publicationRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("publication cancellation retained data: %v %v", entries, err)
	}

	target := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "root")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(context.Background(), link, int64(len(archive)), bytes.NewReader(archive)); err == nil {
		t.Fatal("symlink root accepted")
	}
}

func TestPackageIdentityUsesExactLengthPrefixedBytes(t *testing.T) {
	manifest, payload := []byte("m"), []byte("p")
	digest := sha256.New()
	digest.Write([]byte("FES-CORE-PACKAGE-2\n"))
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(manifest)))
	digest.Write(length[:])
	digest.Write(manifest)
	binary.LittleEndian.PutUint64(length[:], uint64(len(payload)))
	digest.Write(length[:])
	digest.Write(payload)
	if got := packageIdentity(manifest, payload); got != hex.EncodeToString(digest.Sum(nil)) {
		t.Fatalf("identity=%s", got)
	}
	if strings.ToUpper(packageIdentity(manifest, payload)) == packageIdentity(manifest, payload) {
		t.Fatal("identity is not lowercase")
	}
}

func TestRetainProgrammedBitstreamCleansWithThePackage(t *testing.T) {
	root := t.TempDir()
	publication := "pkg"
	if err := os.Mkdir(filepath.Join(root, publication), 0o700); err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	publicationInfo, err := os.Stat(filepath.Join(root, publication))
	if err != nil {
		t.Fatal(err)
	}
	staged := Staged{root: root, rootInfo: rootInfo, publication: publication, publicationInfo: publicationInfo}
	path, err := staged.RetainProgrammedBitstream([]byte("programmed"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err = staged.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("programmed bitstream after cleanup: %v", err)
	}
}
