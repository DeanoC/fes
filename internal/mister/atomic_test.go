package mister_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/internal/mister"
)

func TestWriteAtomicMGL(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "fogcast")
	path, err := mister.WriteAtomicMGL(dir, []byte("new mgl\n"))
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "launch.mgl") {
		t.Fatalf("path = %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new mgl\n" {
		t.Fatalf("content = %q", got)
	}
	if _, err := os.Stat(path + ".new"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains: %v", err)
	}
}
