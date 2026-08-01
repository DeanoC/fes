package packagepoc_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/clawzai2-tech/mister-remote/internal/packagepoc"
)

var archiveFiles = map[string]string{
	"mister-agent":        "binary",
	"agent.toml":          "token",
	"start-agent.sh":      "#!/bin/sh\n",
	"MiSTer.ini.fragment": "[MiSTer]\n",
}

func TestCreateIsDeterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "mister-remote")
	writeArchiveFixture(t, root)
	fixed := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	one, two := filepath.Join(dir, "one.tar.gz"), filepath.Join(dir, "two.tar.gz")
	if err := packagepoc.Create(root, one, fixed); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "agent.toml"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := packagepoc.Create(root, two, fixed); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(one)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(two)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("archives differ")
	}
	entries := readArchiveHeaders(t, one)
	want := []string{"mister-remote/MiSTer.ini.fragment", "mister-remote/agent.toml", "mister-remote/mister-agent", "mister-remote/start-agent.sh"}
	if !slices.Equal(entryNames(entries), want) {
		t.Fatalf("entries = %v", entryNames(entries))
	}
	for _, header := range entries {
		wantMode := int64(0o600)
		if filepath.Base(header.Name) == "mister-agent" || filepath.Base(header.Name) == "start-agent.sh" {
			wantMode = 0o755
		}
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" || !header.ModTime.Equal(fixed) || header.Mode != wantMode || header.Format != tar.FormatUSTAR {
			t.Fatalf("header = %#v", header)
		}
	}
}

func TestCreateRejectsAnythingOutsideAllowlist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "missing", mutate: func(t *testing.T, root string) {
			t.Helper()
			if err := os.Remove(filepath.Join(root, "agent.toml")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "extra", mutate: func(t *testing.T, root string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(root, "extra.txt"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory", mutate: func(t *testing.T, root string) {
			t.Helper()
			if err := os.Remove(filepath.Join(root, "agent.toml")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "agent.toml"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", mutate: func(t *testing.T, root string) {
			t.Helper()
			if err := os.Remove(filepath.Join(root, "agent.toml")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "mister-agent"), filepath.Join(root, "agent.toml")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "source")
			writeArchiveFixture(t, root)
			tt.mutate(t, root)
			output := filepath.Join(filepath.Dir(root), "package.tar.gz")
			if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := packagepoc.Create(root, output, time.Unix(0, 0).UTC()); err == nil {
				t.Fatal("invalid source directory was archived")
			}
			content, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "existing" {
				t.Fatalf("failed archive replaced existing output: %q", content)
			}
		})
	}
}

func writeArchiveFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range archiveFiles {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readArchiveHeaders(t *testing.T, path string) []*tar.Header {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var headers []*tar.Header
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return headers
		}
		if err != nil {
			t.Fatal(err)
		}
		copy := *header
		headers = append(headers, &copy)
	}
}

func entryNames(headers []*tar.Header) []string {
	names := make([]string, len(headers))
	for i, header := range headers {
		names[i] = header.Name
	}
	return names
}
