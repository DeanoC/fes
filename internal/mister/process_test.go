package mister_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/mister"
)

func TestProcProcessCheckerFindsExactComm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	processDirectory := filepath.Join(root, "123")
	if err := os.Mkdir(processDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDirectory, "comm"), []byte("MiSTer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checker := mister.ProcProcessChecker{Root: root}
	if !checker.Running("MiSTer") {
		t.Fatal("MiSTer process not detected")
	}
	if checker.Running("other") {
		t.Fatal("different process name matched")
	}
}
