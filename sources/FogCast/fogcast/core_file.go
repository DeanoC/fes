package fogcast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const maxHostCoreBytes = 256 << 20

var coreDigestCache sync.Map // map[string]string

func openRegularCore(path string) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > maxHostCoreBytes {
		return nil, nil, errors.New("core path is not a bounded regular file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() != before.Size() || info.Size() > maxHostCoreBytes {
		_ = f.Close()
		return nil, nil, errors.New("core path changed or is not a bounded regular file")
	}
	return f, info, nil
}

func coreFileCacheKey(path string, info os.FileInfo) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%v", filepath.Clean(path), info.Size(), info.ModTime().UnixNano(), info.Sys())
}

func coreFileDigest(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, info, err := openRegularCore(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := coreFileCacheKey(path, info)
	if digest, ok := coreDigestCache.Load(key); ok {
		return digest.(string), nil
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx: ctx, r: f}, info.Size()+1))
	if err != nil {
		return "", err
	}
	if n != info.Size() {
		return "", errors.New("core file size changed while hashing")
	}
	digest := hex.EncodeToString(h.Sum(nil))
	coreDigestCache.Store(key, digest)
	return digest, nil
}
