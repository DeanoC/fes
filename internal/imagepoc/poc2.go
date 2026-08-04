package imagepoc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// POC2Base binds a POC 2 root-image build to the accepted POC 1 provenance.
type POC2Base struct {
	POC1ALockSHA256       string `toml:"poc1a_lock_sha256"`
	POC1BLockSHA256       string `toml:"poc1b_lock_sha256"`
	AcceptedDevRootSHA256 string `toml:"accepted_dev_root_sha256"`
	AcceptedKernelSHA256  string `toml:"accepted_kernel_sha256"`
}

// POC2Outputs records the two twice-reproduced POC 2 root images.
type POC2Outputs struct {
	ProdRootFSSHA256 string `toml:"prod_rootfs_sha256"`
	DevRootFSSHA256  string `toml:"dev_rootfs_sha256"`
}

// POC2Lock is the complete, versioned POC 2 output provenance lock.
type POC2Lock struct {
	Format  int         `toml:"format"`
	Base    POC2Base    `toml:"base"`
	Outputs POC2Outputs `toml:"outputs"`
}

// LoadPOC2 strictly loads and validates a POC 2 output lock.
func LoadPOC2(path string) (POC2Lock, error) {
	var lock POC2Lock
	if err := decodeFile(path, &lock); err != nil {
		return POC2Lock{}, fmt.Errorf("load POC 2 lock: %w", err)
	}
	if err := validatePOC2(lock); err != nil {
		return POC2Lock{}, fmt.Errorf("validate POC 2 lock: %w", err)
	}
	return lock, nil
}

// WritePOC2 atomically writes a stable POC 2 output lock.
func WritePOC2(path string, lock POC2Lock) error {
	if err := validatePOC2(lock); err != nil {
		return fmt.Errorf("validate POC 2 lock: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary POC 2 lock: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("chmod temporary POC 2 lock: %w", err)
	}
	if err := toml.NewEncoder(temporary).Encode(lock); err != nil {
		temporary.Close()
		return fmt.Errorf("encode POC 2 lock: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync POC 2 lock: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close POC 2 lock: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace POC 2 lock: %w", err)
	}
	return nil
}

// RecordPOC2 verifies the accepted base and current kernel, then records the
// POC 2 production and development root-image digests.
func RecordPOC2(poc1aPath, poc1bPath, prodPath, devPath, outputPath string) error {
	if _, err := LoadPOC1A(poc1aPath); err != nil {
		return err
	}
	poc1b, err := LoadPOC1B(poc1bPath)
	if err != nil {
		return err
	}
	if poc1b.Outputs == nil {
		return fmt.Errorf("POC 1B lock has no accepted outputs")
	}
	poc1aDigest, _, err := regularFileDigest(poc1aPath)
	if err != nil {
		return fmt.Errorf("digest POC 1A lock: %w", err)
	}
	poc1bDigest, _, err := regularFileDigest(poc1bPath)
	if err != nil {
		return fmt.Errorf("digest POC 1B lock: %w", err)
	}
	if err := verifyDigestOnly(canonicalPOC1BKernel(poc1bPath), poc1b.Outputs.ReproducedKernelSHA256); err != nil {
		return fmt.Errorf("verify accepted POC 1B kernel: %w", err)
	}
	prodDigest, _, err := regularFileDigest(prodPath)
	if err != nil {
		return fmt.Errorf("digest production root: %w", err)
	}
	devDigest, _, err := regularFileDigest(devPath)
	if err != nil {
		return fmt.Errorf("digest development root: %w", err)
	}
	return WritePOC2(outputPath, POC2Lock{
		Format: 1,
		Base: POC2Base{
			POC1ALockSHA256:       poc1aDigest,
			POC1BLockSHA256:       poc1bDigest,
			AcceptedDevRootSHA256: poc1b.Outputs.DevRootFSSHA256,
			AcceptedKernelSHA256:  poc1b.Outputs.ReproducedKernelSHA256,
		},
		Outputs: POC2Outputs{
			ProdRootFSSHA256: prodDigest,
			DevRootFSSHA256:  devDigest,
		},
	})
}

// VerifyPOC2 read-only verifies the complete POC 2 provenance chain.
func VerifyPOC2(lockPath, poc1aPath, poc1bPath, prodPath, devPath string) error {
	lock, err := LoadPOC2(lockPath)
	if err != nil {
		return err
	}
	if _, err := LoadPOC1A(poc1aPath); err != nil {
		return err
	}
	poc1b, err := LoadPOC1B(poc1bPath)
	if err != nil {
		return err
	}
	if poc1b.Outputs == nil {
		return fmt.Errorf("POC 1B lock has no accepted outputs")
	}
	poc1aDigest, _, err := regularFileDigest(poc1aPath)
	if err != nil {
		return fmt.Errorf("digest POC 1A lock: %w", err)
	}
	if poc1aDigest != lock.Base.POC1ALockSHA256 {
		return fmt.Errorf("POC 1A lock digest mismatch")
	}
	poc1bDigest, _, err := regularFileDigest(poc1bPath)
	if err != nil {
		return fmt.Errorf("digest POC 1B lock: %w", err)
	}
	if poc1bDigest != lock.Base.POC1BLockSHA256 {
		return fmt.Errorf("POC 1B lock digest mismatch")
	}
	if poc1b.Outputs.DevRootFSSHA256 != lock.Base.AcceptedDevRootSHA256 {
		return fmt.Errorf("accepted POC 1B development root mismatch")
	}
	if poc1b.Outputs.ReproducedKernelSHA256 != lock.Base.AcceptedKernelSHA256 {
		return fmt.Errorf("accepted POC 1B kernel mismatch")
	}
	if err := verifyDigestOnly(canonicalPOC1BKernel(poc1bPath), lock.Base.AcceptedKernelSHA256); err != nil {
		return fmt.Errorf("verify accepted POC 1B kernel: %w", err)
	}
	if err := verifyDigestOnly(prodPath, lock.Outputs.ProdRootFSSHA256); err != nil {
		return fmt.Errorf("verify POC 2 production root: %w", err)
	}
	if err := verifyDigestOnly(devPath, lock.Outputs.DevRootFSSHA256); err != nil {
		return fmt.Errorf("verify POC 2 development root: %w", err)
	}
	return nil
}

func validatePOC2(lock POC2Lock) error {
	if lock.Format != 1 {
		return fmt.Errorf("format must be 1")
	}
	digests := []string{
		lock.Base.POC1ALockSHA256,
		lock.Base.POC1BLockSHA256,
		lock.Base.AcceptedDevRootSHA256,
		lock.Base.AcceptedKernelSHA256,
		lock.Outputs.ProdRootFSSHA256,
		lock.Outputs.DevRootFSSHA256,
	}
	for _, digest := range digests {
		if !validSHA256(digest) {
			return fmt.Errorf("all POC 2 digests must be lowercase SHA-256")
		}
	}
	return nil
}

func canonicalPOC1BKernel(poc1bPath string) string {
	return filepath.Join(filepath.Dir(poc1bPath), "output", "poc1b", "kernel", "zImage_dtb")
}

func verifyDigestOnly(path, expected string) error {
	_, size, err := regularFileDigest(path)
	if err != nil {
		return err
	}
	return VerifyFile(path, expected, size)
}

func regularFileDigest(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", 0, fmt.Errorf("not a nonempty regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if !os.SameFile(info, opened) {
		return "", 0, fmt.Errorf("file identity changed")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), info.Size(), nil
}
