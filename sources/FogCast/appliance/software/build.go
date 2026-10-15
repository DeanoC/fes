package software

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
)

// PopulateFiles records every regular payload file. Links and special files are
// forbidden; executable bits normalize to 0555 and other files to 0444.
func PopulateFiles(m Manifest, root fs.FS) (Manifest, error) {
	m.Files = nil
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !validPath(p) {
			return fmt.Errorf("invalid payload path %q", p)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("payload is not regular: %s", p)
		}
		if info.Size() > MaxBundleSize {
			return fmt.Errorf("oversize payload %s", p)
		}
		f, e := root.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(f, MaxBundleSize+1))
		f.Close()
		if e != nil {
			return e
		}
		if n != info.Size() {
			return fmt.Errorf("payload changed while hashing %s", p)
		}
		mode := uint32(0444)
		if info.Mode().Perm()&0111 != 0 {
			mode = 0555
		}
		m.Files = append(m.Files, File{p, hex.EncodeToString(h.Sum(nil)), n, mode})
		if len(m.Files) > MaxFiles {
			return fmt.Errorf("too many payload files")
		}
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	return m, m.Validate()
}
