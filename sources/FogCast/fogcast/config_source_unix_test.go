//go:build darwin || linux

package fogcast_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"golang.org/x/sys/unix"
)

func TestLoadConfigRejectsConfigSourceIdentitySwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	fifoBackup := filepath.Join(dir, "config.fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	content := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")) + `
[metadata]
enabled = true
provider = "igdb"
client_id = "id"
client_secret = "secret"
`

	loadResult := make(chan error, 1)
	go func() {
		_, err := fogcast.LoadConfig(path)
		loadResult <- err
	}()

	stopWriter := make(chan struct{})
	releaseWriter := make(chan struct{})
	writerReady := make(chan struct{})
	writerWrote := make(chan struct{})
	writerError := make(chan error, 1)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()

		var writer *os.File
		for {
			select {
			case <-stopWriter:
				return
			default:
			}
			fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
			if err == nil {
				writer = os.NewFile(uintptr(fd), path)
				break
			}
			if !errors.Is(err, unix.ENXIO) {
				writerError <- fmt.Errorf("open FIFO writer: %w", err)
				return
			}
			select {
			case <-stopWriter:
				return
			case <-deadline.C:
				writerError <- errors.New("timed out waiting for FIFO reader")
				return
			case <-time.After(time.Millisecond):
			}
		}

		close(writerReady)
		if _, err := writer.WriteString(content); err != nil {
			_ = writer.Close()
			writerError <- fmt.Errorf("write FIFO: %w", err)
			return
		}
		close(writerWrote)
		<-releaseWriter
		if err := writer.Close(); err != nil {
			writerError <- fmt.Errorf("close FIFO writer: %w", err)
			return
		}
		writerError <- nil
	}()

	select {
	case err := <-loadResult:
		close(stopWriter)
		<-writerDone
		if err == nil {
			t.Fatal("FIFO source accepted before identity replacement")
		}
		return
	case <-writerReady:
	}

	select {
	case err := <-writerError:
		close(stopWriter)
		<-writerDone
		if loadErr := <-loadResult; loadErr == nil {
			t.Fatalf("FIFO writer failed (%v), but config was accepted", err)
		}
		return
	case <-writerWrote:
	}

	if err := os.Rename(path, fifoBackup); err != nil {
		close(releaseWriter)
		<-writerDone
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		close(releaseWriter)
		<-writerDone
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		close(releaseWriter)
		<-writerDone
		t.Fatal(err)
	}
	close(releaseWriter)
	<-writerDone
	if err := <-writerError; err != nil {
		t.Fatal(err)
	}

	if err := <-loadResult; err == nil {
		t.Fatal("config accepted bytes from a source whose path identity was replaced")
	}
}

func TestLoadConfigRejectsConfigSourceSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.toml")
	path := filepath.Join(dir, "config.toml")
	content := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")) + `
[metadata]
enabled = true
provider = "igdb"
client_id = "id"
client_secret = "secret"
`
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	if _, err := fogcast.LoadConfig(path); err == nil {
		t.Fatal("config source symlink accepted")
	}
}
