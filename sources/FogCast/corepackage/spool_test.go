package corepackage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSpoolInputRejectsWithoutLeavingFiles(t *testing.T) {
	body := bytes.Repeat([]byte{7}, 4096)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name        string
		ctx         context.Context
		size, limit int64
		body        []byte
	}{
		{"short body", context.Background(), 4097, 8192, body},
		{"long body", context.Background(), 4095, 8192, body},
		{"over limit", context.Background(), 4096, 4095, body},
		{"cancelled", cancelled, 4096, 8192, body},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if spool, err := SpoolInput(test.ctx, root, test.size, test.limit, bytes.NewReader(test.body)); err == nil {
				spool.Close()
				t.Fatal("spool accepted")
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatalf("rejected spool left %v", entries)
			}
		})
	}
}

func TestSpooledROMInputPublishesItsSpoolAsInput(t *testing.T) {
	transport, err := PrepareROMInput(context.Background(), linkedROMFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if body := transport.Bytes(); int64(len(body)) != transport.Size() || !IsROMInput(body) {
		t.Fatal("envelope size or framing differs from its stream")
	}
	streamed, err := io.ReadAll(transport.Reader())
	if err != nil || !bytes.Equal(streamed, transport.Bytes()) {
		t.Fatal("a second reader differs", err)
	}
	root := t.TempDir()
	spool, err := SpoolInput(context.Background(), root, transport.Size(), MaxROMInputSize, transport.Reader())
	if err != nil {
		t.Fatal(err)
	}
	spoolInfo, err := os.Stat(filepath.Join(root, spool.name))
	if err != nil {
		t.Fatal(err)
	}
	staged, err := StageROMInputSpooled(context.Background(), root, spool)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "rom-link-"+staged.publication, "input.tar")
	inputInfo, err := os.Stat(input)
	if err != nil || !os.SameFile(spoolInfo, inputInfo) || inputInfo.Mode().Perm() != 0o400 {
		t.Fatalf("input.tar is not the sealed spool: %v %v", inputInfo, err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, spool.name)); !os.IsNotExist(err) {
		t.Fatalf("spool name remains: %v", err)
	}
	if published, err := os.ReadFile(input); err != nil || !bytes.Equal(published, transport.Bytes()) {
		t.Fatal("input.tar lost the envelope", err)
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || *adopted[0].ROMLink != *staged.ROMLink {
		t.Fatalf("adopt: %v %v", adopted, err)
	}
	if err := staged.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("cleanup left %v", entries)
	}
}

func TestRemoveSpoolsKeepsPublications(t *testing.T) {
	root := t.TempDir()
	stale := ".input-" + strings.Repeat("ab", 16)
	for _, name := range []string{stale, ".input-short", "rom-link-x"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, ".input-"+strings.Repeat("cd", 16)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSpools(root); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 3 || slices.Contains(names, stale) {
		t.Fatalf("left %v", names)
	}
}
