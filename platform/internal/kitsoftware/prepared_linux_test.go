package kitsoftware

import (
	"context"
	"github.com/DeanoC/FogCast/appliance/software"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreparedReleaseChecksMetadataWithoutRehashing(t *testing.T) {
	for _, change := range []string{"none", "content-restored-mtime", "replace", "symlink", "extra", "missing", "mode", "root-replaced"} {
		t.Run(change, func(t *testing.T) {
			release, root, _ := softwareFixture(t, "base")
			calls := 0
			c := &ProcessController{Resolve: func(ctx context.Context, id string) (string, error) {
				calls++
				_, err := software.VerifyDirectory(ctx, root)
				return root, err
			}}
			if err := c.Verify(context.Background(), release.ID); err != nil {
				t.Fatal(err)
			}
			count := calls
			path := filepath.Join(root, "bin/mister-agent")
			info, _ := os.Stat(path)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch change {
			case "content-restored-mtime":
				time.Sleep(time.Millisecond)
				b, _ := os.ReadFile(path)
				b[0] ^= 1
				must(os.Chmod(path, 0755))
				must(os.WriteFile(path, b, 0555))
				must(os.Chmod(path, 0555))
				must(os.Chtimes(path, info.ModTime(), info.ModTime()))
			case "replace":
				b, _ := os.ReadFile(path)
				must(os.Remove(path))
				must(os.WriteFile(path, b, 0555))
			case "symlink":
				must(os.Remove(path))
				must(os.Symlink("/bin/true", path))
			case "extra":
				must(os.WriteFile(filepath.Join(root, "extra"), nil, 0444))
			case "missing":
				must(os.Remove(path))
			case "mode":
				must(os.Chmod(path, 0755))
			case "root-replaced":
				must(os.Rename(root, root+"-old"))
				t.Cleanup(func() { os.RemoveAll(root + "-old") })
				must(os.Mkdir(root, 0755))
			}
			for i := 0; i < 3; i++ {
				_, err := c.preparedPath(context.Background(), release.ID)
				if (err == nil) != (change == "none") {
					t.Fatalf("%s: %v", change, err)
				}
			}
			if calls != count {
				t.Fatal("startup rehashed")
			}
			if _, err := c.preparedPath(context.Background(), testCandidateID); err == nil {
				t.Fatal("reused another release")
			}
		})
	}
}
func TestPreparationRejectsConcurrentMutationAndClearsOld(t *testing.T) {
	release, root, _ := softwareFixture(t, "base")
	calls := 0
	mutate := false
	c := &ProcessController{Resolve: func(ctx context.Context, id string) (string, error) {
		calls++
		if _, err := software.VerifyDirectory(ctx, root); err != nil {
			return "", err
		}
		if mutate && calls%2 == 0 {
			path := filepath.Join(root, "bin/mister-agent")
			b, _ := os.ReadFile(path)
			b[0] ^= 1
			if err := os.Chmod(path, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0555); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0555); err != nil {
				t.Fatal(err)
			}
		}
		return root, nil
	}}
	if err := c.Verify(context.Background(), release.ID); err != nil {
		t.Fatal(err)
	}
	mutate = true
	if err := c.Verify(context.Background(), release.ID); err == nil {
		t.Fatal("concurrent mutation accepted")
	}
	if c.prepared != nil {
		t.Fatal("failed verification retained authority")
	}
}

func TestPreparationBarrierDoesNotUseFilesystemWallTime(t *testing.T) {
	release, root, _ := softwareFixture(t, "base")
	// Factory and retained files can be dated far in the future relative to a
	// no-RTC boot. The fixed monotonic barrier accepts them without wall aging.
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, file := range release.Manifest.Files {
		if err := os.Chtimes(filepath.Join(root, file.Path), future, future); err != nil {
			t.Fatal(err)
		}
	}
	c := &ProcessController{Resolve: func(ctx context.Context, id string) (string, error) {
		_, err := software.VerifyDirectory(ctx, root)
		return root, err
	}}
	started := time.Now()
	if err := c.Verify(context.Background(), release.ID); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < time.Second {
		t.Fatal("preparation skipped monotonic tick barrier")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := preparationTick(ctx, time.Hour); err == nil {
		t.Fatal("preparation ignored cancellation")
	}
}
