package kitsoftware

import (
	"bytes"
	"context"
	"github.com/DeanoC/FogCast/appliance/software"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFactoryUsesRunningBaseInodeNotRetainedCopy(t *testing.T) {
	factory, root, archive := softwareFixture(t, "base")
	base := software.Compatibility{Board: "de10-nano", Architecture: "armv7", BaseContract: "base", Interfaces: map[string]string{"runtime": "1"}}
	store, err := software.New(filepath.Join(t.TempDir(), "store"), base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Install(context.Background(), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	retained, err := store.Path(context.Background(), factory.ID)
	if err != nil {
		t.Fatal(err)
	}
	chosen, err := ResolveRelease(context.Background(), factory.ID, root, factory, base, store)
	if err != nil {
		t.Fatal(err)
	}
	if chosen != root {
		t.Fatal("factory resolved to retained copy", chosen)
	}
	live, _ := os.Stat(filepath.Join(chosen, "bin/mister-agent"))
	copy, _ := os.Stat(filepath.Join(retained, "bin/mister-agent"))
	if os.SameFile(live, copy) {
		t.Fatal("test did not create distinct retained inode")
	}
	badBase := base
	badBase.BaseContract = "different"
	if _, err := ResolveRelease(context.Background(), factory.ID, root, factory, badBase, store); err == nil {
		t.Fatal("factory compatibility skipped")
	}
}
