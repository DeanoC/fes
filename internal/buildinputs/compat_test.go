package buildinputs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExpectedRuntimeCommitMatchesNativeLock(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "build", "native-runtime.inputs.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "commit = '" + ExpectedRuntimeCommit() + "'"
	if !strings.Contains(string(data), want) {
		t.Fatalf("lock does not contain %s", want)
	}
}
