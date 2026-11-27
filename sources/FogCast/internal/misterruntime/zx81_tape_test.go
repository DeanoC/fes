package misterruntime_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/DeanoC/FogCast/internal/misterruntime"
)

type zx81CaptureControl struct {
	recordingControl
	path string
}

func (c *zx81CaptureControl) CaptureZX81Tape(_ context.Context, path, _ string, _ uint64) (misterruntime.Protocol2Response, error) {
	c.path = path
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if info.Mode().Perm() != 0o700 {
		return misterruntime.Protocol2Response{}, fmt.Errorf("capture directory mode %04o", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte{1}, 0o600); err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	return misterruntime.Protocol2Response{OK: true}, nil
}

func TestCaptureZX81TapeUsesSearchablePrivateDirectoryWithRestrictiveUmask(t *testing.T) {
	old := syscall.Umask(0o177)
	defer syscall.Umask(old)
	control := &zx81CaptureControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	data, apiErr := runtime.CaptureZX81Tape(context.Background(), strings.Repeat("a", 64), 1)
	if apiErr != nil || len(data) != 1 || data[0] != 1 {
		t.Fatalf("capture bytes=%v error=%v", data, apiErr)
	}
	if _, err := os.Stat(filepath.Dir(control.path)); !os.IsNotExist(err) {
		t.Fatalf("capture directory was not removed: %v", err)
	}
}
