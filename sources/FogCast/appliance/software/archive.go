package software

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// WriteBundle emits a deterministic uncompressed tar. All files are verified as
// they stream; callers must discard output if this returns an error.
func WriteBundle(w io.Writer, m Manifest, root fs.FS) error {
	b, e := m.Canonical()
	if e != nil {
		return e
	}
	tw := tar.NewWriter(w)
	if e = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0444, Size: int64(len(b)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); e != nil {
		return e
	}
	if _, e = tw.Write(b); e != nil {
		return e
	}
	files := append([]File(nil), m.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		src, e := root.Open(f.Path)
		if e != nil {
			return e
		}
		info, e := src.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
			src.Close()
			return fmt.Errorf("invalid input %s", f.Path)
		}
		e = tw.WriteHeader(&tar.Header{Name: f.Path, Mode: int64(f.Mode), Size: f.Size, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR})
		if e != nil {
			src.Close()
			return e
		}
		h := sha256.New()
		n, e := io.Copy(io.MultiWriter(tw, h), io.LimitReader(src, f.Size+1))
		src.Close()
		if e != nil {
			return e
		}
		if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return fmt.Errorf("digest mismatch %s", f.Path)
		}
	}
	return tw.Close()
}
func InspectBundle(r io.Reader) (Release, error) { return extract(context.Background(), r, "") }

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func extract(ctx context.Context, r io.Reader, destination string) (Release, error) {
	var out Release
	limit := &io.LimitedReader{R: contextReader{ctx, r}, N: MaxBundleSize + 1}
	tr := tar.NewReader(limit)
	hdr, e := tr.Next()
	if e != nil {
		return out, e
	}
	if hdr.Name != "manifest.json" || hdr.Typeflag != tar.TypeReg || hdr.Size > MaxManifestSize || hdr.Size <= 0 {
		return out, errors.New("bundle must start with regular manifest.json")
	}
	m, e := DecodeManifest(tr)
	if e != nil {
		return out, e
	}
	id, e := m.ID()
	if e != nil {
		return out, e
	}
	out = Release{id, m}
	expected := map[string]File{}
	for _, f := range m.Files {
		expected[f.Path] = f
	}
	seen := map[string]bool{}
	for {
		hdr, e = tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return Release{}, e
		}
		f, ok := expected[hdr.Name]
		if !ok || seen[hdr.Name] || hdr.Typeflag != tar.TypeReg || hdr.Size != f.Size || hdr.Mode != int64(f.Mode) || hdr.Linkname != "" {
			return Release{}, fmt.Errorf("invalid bundle member %q", hdr.Name)
		}
		seen[hdr.Name] = true
		var dst *os.File
		var w io.Writer = io.Discard
		if destination != "" {
			p := filepath.Join(destination, filepath.FromSlash(f.Path))
			if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
				return Release{}, e
			}
			dst, e = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return Release{}, e
			}
			w = dst
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(w, h), tr)
		if dst != nil {
			if copyErr == nil {
				copyErr = dst.Chmod(fs.FileMode(f.Mode))
			}
			if copyErr == nil {
				copyErr = dst.Sync()
			}
			closeErr := dst.Close()
			if copyErr == nil {
				copyErr = closeErr
			}
		}
		if copyErr != nil {
			return Release{}, copyErr
		}
		if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return Release{}, fmt.Errorf("digest mismatch %s", f.Path)
		}
	}
	if len(seen) != len(expected) {
		return Release{}, errors.New("missing bundle members")
	}
	// tar.Reader stops at its end marker. Refuse nonzero trailing bytes and bound
	// even padding, so a second hidden archive cannot escape validation.
	tail := make([]byte, 8192)
	for {
		n, e := limit.Read(tail)
		for _, b := range tail[:n] {
			if b != 0 {
				return Release{}, errors.New("trailing bundle data")
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return Release{}, e
		}
	}
	if limit.N <= 0 {
		return Release{}, errors.New("bundle too large")
	}
	return out, nil
}
