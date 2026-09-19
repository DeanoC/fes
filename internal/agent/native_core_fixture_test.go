package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/internal/misterruntime"
)

func nativeCoreFixture(t *testing.T, extraSystems ...string) misterruntime.RuntimeOption {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "usr/share/mister-runtime/cores")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, system := range append([]string{"megadrive"}, extraSystems...) {
		if err := os.WriteFile(filepath.Join(dir, system+".rbf"), []byte("fixture RBF"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return misterruntime.WithNativeCoreFS(os.DirFS(root))
}
