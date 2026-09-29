// Package atomicwrite replaces a file through a same-directory temporary whose
// name can never alias the target on the kit's /media/fat exFAT volume.
//
// The MiSTer kernel (5.15.1-MiSTer, built-in "exFAT: Version 1.2.11" driver)
// resolves a lookup of name N to an existing entry E when E is a prefix of N and
// E is not a plain 8.3 name (issue #317). So "launcher.json.tmp",
// "launcher.json.bak" and "launcher.jsonx" all open launcher.json itself:
// os.WriteFile(path+".tmp") truncates the live file in place, and an error-path
// os.Remove(path+".tmp") deletes it. Temporaries therefore start with "." and
// are created with O_EXCL (os.CreateTemp), so they never begin with the target
// name, and a collision with any other prefix entry fails closed with EEXIST
// instead of opening that entry.
package atomicwrite

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TempPattern is the os.CreateTemp pattern for a sibling temporary of target:
// ".<base>.<random>.tmp".
func TempPattern(target string) string {
	return "." + filepath.Base(target) + ".*.tmp"
}

// SafeTempName reports whether temp (a base name) is safe to use as the
// temporary for target (a path or base name) on the kit's exFAT volume: it is
// dot-prefixed and does not begin with the target's base name. The comparison
// is case-insensitive to stay safe on case-folding FAT drivers too.
func SafeTempName(target, temp string) bool {
	base := strings.ToLower(filepath.Base(target))
	temp = strings.ToLower(filepath.Base(temp))
	if base == "" || base == "." || base == string(filepath.Separator) || temp == base {
		return false
	}
	return strings.HasPrefix(temp, ".") && !strings.HasPrefix(temp, base)
}

// beforeRename is a test hook run with the temporary path just before rename.
var beforeRename func(temp string)

// WriteFile atomically replaces path with data. The data is written to a new
// ".<base>.<random>.tmp" file in the same directory (same filesystem, so the
// rename is atomic), synced, renamed over path, and the directory is synced
// (best effort). The temporary is removed on any failure before the rename.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, TempPattern(path))
	if err != nil {
		return err
	}
	temp := f.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(temp)
		}
	}()
	if !SafeTempName(path, filepath.Base(temp)) {
		_ = f.Close()
		return fmt.Errorf("atomicwrite: unsafe temporary name for %s", filepath.Base(path))
	}
	if perm.Perm() != 0o600 {
		if err := f.Chmod(perm.Perm()); err != nil {
			_ = f.Close()
			return err
		}
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if beforeRename != nil {
		beforeRename(temp)
	}
	if err := os.Rename(temp, path); err != nil {
		return err
	}
	renamed = true
	SyncDir(dir)
	return nil
}

// SyncDir fsyncs a directory so a completed rename is durable. It is best
// effort: some filesystems reject directory fsync, and the rename has already
// happened.
func SyncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
