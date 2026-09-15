package attractvideo

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFitAttractSizeCapsToStage(t *testing.T) {
	t.Parallel()
	w, h := fitAttractSize(1920, 1080)
	if w != 1280 || h != 720 {
		t.Fatalf("1080p fit = %dx%d", w, h)
	}
	w, h = fitAttractSize(64, 36)
	if w != 64 || h != 36 {
		t.Fatalf("small fit = %dx%d", w, h)
	}
}

func TestFFmpegAvailableFalseWhenMissing(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if ffmpegAvailable() {
		t.Fatal("expected unavailable")
	}
	clip := filepath.Join(t.TempDir(), "clip.bin")
	if err := os.WriteFile(clip, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	player, err := openFFmpeg(clip)
	if player != nil {
		player.Close()
		t.Fatal("expected nil player")
	}
	if err != ErrUnavailable {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestOpenFFmpegMissingFileFails(t *testing.T) {
	t.Parallel()
	player, err := openFFmpeg("/no/such/fogcast-attract.mp4")
	if player != nil {
		player.Close()
		t.Fatal("expected nil player")
	}
	if err == nil {
		t.Fatal("expected open error")
	}
}

func TestOpenFFmpegFakeDecoder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fakes")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.bin")
	if err := os.WriteFile(clip, []byte("not-empty"), 0o644); err != nil {
		t.Fatal(err)
	}
	framePath := filepath.Join(dir, "frame.rgba")
	frame := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 0, 255}
	if err := os.WriteFile(framePath, frame, 0o644); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(dir, "ffmpeg")
	ffprobe := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\ncat "+framePath+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ffprobe, []byte("#!/bin/sh\necho 2x2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	origLook := lookPath
	t.Cleanup(func() { lookPath = origLook })
	lookPath = func(name string) (string, error) {
		switch name {
		case "ffmpeg":
			return ffmpeg, nil
		case "ffprobe":
			return ffprobe, nil
		default:
			return origLook(name)
		}
	}

	player, err := openFFmpeg(clip)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	img, _, err := player.Frame()
	if err != nil {
		t.Fatal(err)
	}
	if img == nil || img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
		t.Fatalf("frame = %#v", img)
	}
	if img.Pix[0] != 255 || img.Pix[1] != 0 || img.Pix[2] != 0 {
		t.Fatalf("pix = %v", img.Pix[:4])
	}
}

func TestOpenFFmpegDecodesGeneratedClip(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=red:s=64x36:d=0.3", "-an", "-pix_fmt", "yuv420p", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate clip: %v\n%s", err, out)
	}
	player, err := openFFmpeg(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		frame, ended, frameErr := player.Frame()
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		if frame != nil {
			if frame.Bounds().Dx() != 64 || frame.Bounds().Dy() != 36 {
				t.Fatalf("size = %s", frame.Bounds())
			}
			return
		}
		if ended {
			t.Fatal("ended before a frame")
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no frame")
}

func TestProbeVideoSizeParsesFFProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fakes")
	}
	dir := t.TempDir()
	ffprobe := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(ffprobe, []byte("#!/bin/sh\necho 320x180\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	origLook := lookPath
	t.Cleanup(func() { lookPath = origLook })
	lookPath = func(name string) (string, error) {
		if name == "ffprobe" {
			return ffprobe, nil
		}
		return origLook(name)
	}
	w, h, err := probeVideoSize("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if w != 320 || h != 180 {
		t.Fatalf("size = %dx%d", w, h)
	}
}
