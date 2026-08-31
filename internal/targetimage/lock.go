package targetimage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type Container struct {
	Image    string `toml:"image"`
	Platform string `toml:"platform"`
	Digest   string `toml:"digest"`
}

type Buildroot struct {
	Version string `toml:"version"`
	Commit  string `toml:"commit"`
}

type ImageCreator struct {
	Commit        string `toml:"commit"`
	RootFSSHA256  string `toml:"rootfs_sha256"`
	ModulesSHA256 string `toml:"modules_sha256"`
	KernelSHA256  string `toml:"kernel_sha256"`
}

type Kernel struct {
	Commit    string `toml:"commit"`
	Defconfig string `toml:"defconfig"`
	DTB       string `toml:"dtb"`
	Release   string `toml:"release"`
}

type Sources struct {
	Format       int          `toml:"format"`
	Container    Container    `toml:"container"`
	Buildroot    Buildroot    `toml:"buildroot"`
	ImageCreator ImageCreator `toml:"image_creator"`
	Kernel       Kernel       `toml:"kernel"`
}

func LoadSourceLock(path string) (Sources, error) {
	var lock Sources
	if err := decodeFile(path, &lock); err != nil {
		return Sources{}, fmt.Errorf("load source lock: %w", err)
	}
	if err := validateSourceLock(lock); err != nil {
		return Sources{}, fmt.Errorf("validate source lock: %w", err)
	}
	return lock, nil
}

func WriteSourceLock(path string, lock Sources) error {
	if err := validateSourceLock(lock); err != nil {
		return fmt.Errorf("validate source lock: %w", err)
	}
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary lock: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("chmod temporary lock: %w", err)
	}
	encoder := toml.NewEncoder(temporary)
	if err := encoder.Encode(lock); err != nil {
		temporary.Close()
		return fmt.Errorf("encode source lock: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync source lock: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close source lock: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace source lock: %w", err)
	}
	return nil
}

func VerifyFile(path, expectedSHA256 string, expectedSize int64) error {
	if !validSHA256(expectedSHA256) {
		return fmt.Errorf("invalid expected SHA-256")
	}
	if expectedSize <= 0 {
		return fmt.Errorf("expected size must be positive")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() != expectedSize {
		return fmt.Errorf("size mismatch: got %d, want %d", info.Size(), expectedSize)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedSHA256 {
		return fmt.Errorf("SHA-256 mismatch: got %s, want %s", actual, expectedSHA256)
	}
	return nil
}

func decodeFile(path string, destination any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := toml.NewDecoder(f)
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func validateSourceLock(lock Sources) error {
	if lock.Format != 1 {
		return fmt.Errorf("format must be 1")
	}
	if strings.TrimSpace(lock.Container.Image) == "" || lock.Container.Platform != "linux/amd64" {
		return fmt.Errorf("container image and linux/amd64 platform are required")
	}
	if !strings.HasPrefix(lock.Container.Digest, "sha256:") || !validSHA256(strings.TrimPrefix(lock.Container.Digest, "sha256:")) {
		return fmt.Errorf("invalid container digest")
	}
	if strings.TrimSpace(lock.Buildroot.Version) == "" || !commitPattern.MatchString(lock.Buildroot.Commit) {
		return fmt.Errorf("invalid Buildroot source")
	}
	if !commitPattern.MatchString(lock.ImageCreator.Commit) ||
		!validSHA256(lock.ImageCreator.RootFSSHA256) ||
		!validSHA256(lock.ImageCreator.ModulesSHA256) ||
		!validSHA256(lock.ImageCreator.KernelSHA256) {
		return fmt.Errorf("invalid image-creator source")
	}
	if !commitPattern.MatchString(lock.Kernel.Commit) ||
		strings.TrimSpace(lock.Kernel.Defconfig) == "" || filepath.Base(lock.Kernel.Defconfig) != lock.Kernel.Defconfig ||
		strings.TrimSpace(lock.Kernel.DTB) == "" || filepath.Base(lock.Kernel.DTB) != lock.Kernel.DTB ||
		strings.TrimSpace(lock.Kernel.Release) == "" {
		return fmt.Errorf("invalid kernel source")
	}
	return nil
}

func validSHA256(value string) bool {
	return sha256Pattern.MatchString(value)
}
