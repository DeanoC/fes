// Package appliancesplash publishes an image's sealed idle core to FAT.
//
// U-Boot programs /media/fat/idle.rbf before Linux starts. The running image
// carries the same bytes at /usr/share/mister-runtime/idle.rbf, and
// build-inputs records that digest as idle_sha256 (the image build rejects a
// splash/idle mismatch). Staging keeps a copy next to the release image.
// Confirmation replaces the FAT file only after the trial is known-good, and
// the watchdog stays armed until that publish is recorded. An unconfirmed
// trial does not touch the FAT file, so fallback still boots the previous
// splash. A non-trial boot, including factory fallback, republishes the
// running image's splash when FAT does not match, even if the staged copy
// was removed. A later confirmation of a restored image writes that image's
// splash back.
package appliancesplash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/atomicwrite"
	"golang.org/x/sys/unix"
)

const (
	DefaultIdle   = "/usr/share/mister-runtime/idle.rbf"
	DefaultRecord = "/usr/share/mister-runtime/build-inputs"
	DefaultFAT    = "/media/fat/idle.rbf"
	// maxSplash matches release admission. A larger file is a mistaken upload.
	maxSplash = store.MaxStagedSplash
	maxRecord = 64 << 10
)

var sha256Value = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Paths are the running image's idle core, its build-inputs record, the FAT
// boot splash, and the absolute release-store root that holds staged copies.
type Paths struct {
	Idle        string
	Record      string
	FAT         string
	ReleaseRoot string
}

// Installer copies a verified splash into the release store and onto FAT.
type Installer struct {
	Paths
}

func New(p Paths) *Installer {
	if p.Idle == "" {
		p.Idle = DefaultIdle
	}
	if p.Record == "" {
		p.Record = DefaultRecord
	}
	if p.FAT == "" {
		p.FAT = DefaultFAT
	}
	return &Installer{Paths: p}
}

// Install stages the running image's idle core beside the release image and
// then publishes it to FAT. Both steps check the digest against build-inputs.
func (p *Installer) Install(ctx context.Context, imageSHA string) error {
	if err := p.Stage(ctx, imageSHA); err != nil {
		return err
	}
	return p.Publish(ctx, imageSHA)
}

// Stage writes splashes/<image>.rbf and its digest sidecar. It does not
// modify the FAT boot file.
func (p *Installer) Stage(ctx context.Context, imageSHA string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, sum, err := p.verifiedIdle()
	if err != nil {
		return err
	}
	rbf, digest, err := store.SplashPaths(p.ReleaseRoot, imageSHA)
	if err != nil {
		return err
	}
	if existing, existingSum, readErr := readRegular(rbf); readErr == nil && existingSum == sum && len(existing) == len(data) {
		if side, sideErr := readSidecar(digest); sideErr == nil && side == sum {
			return nil
		}
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err = os.MkdirAll(filepath.Dir(rbf), 0o700); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// 0600 skips the chmod atomicwrite would otherwise attempt. exFAT on the
	// kit rejects or ignores mode changes; the mount still exposes the file.
	if err = atomicwrite.WriteFile(rbf, data, 0o600); err != nil {
		return err
	}
	return atomicwrite.WriteFile(digest, []byte(sum+"\n"), 0o600)
}

// Publish copies the staged splash for imageSHA onto the FAT boot path.
// A file that already matches is left in place.
func (p *Installer) Publish(ctx context.Context, imageSHA string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !release.ValidHash(imageSHA) {
		return errors.New("invalid splash image")
	}
	rbf, digest, err := store.SplashPaths(p.ReleaseRoot, imageSHA)
	if err != nil {
		return err
	}
	data, sum, err := readRegular(rbf)
	if err != nil {
		return err
	}
	side, err := readSidecar(digest)
	if err != nil {
		return err
	}
	if side != sum {
		return errors.New("staged splash sha256 mismatch")
	}
	switch action, err := fatAction(p.FAT, data, sum); {
	case err != nil:
		return err
	case action == fatUnchanged:
		return nil
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return atomicwrite.WriteFile(p.FAT, data, 0o600)
}

// FATSHA256 is the current FAT splash digest, or empty when it cannot be read.
func (p *Installer) FATSHA256() string {
	if p == nil || p.FAT == "" {
		return ""
	}
	_, sum, err := readRegular(p.FAT)
	if err != nil {
		return ""
	}
	return sum
}

// IdleSHA256 is the image record's idle digest, or empty when it is absent.
func (p *Installer) IdleSHA256() string {
	if p == nil || p.Record == "" {
		return ""
	}
	sum, _, _, err := readIdleRecord(p.Record)
	if err != nil {
		return ""
	}
	return sum
}

func (p *Installer) verifiedIdle() ([]byte, string, error) {
	if p == nil || p.Idle == "" || p.Record == "" || p.ReleaseRoot == "" {
		return nil, "", errors.New("splash paths are incomplete")
	}
	expected, size, hasSize, err := readIdleRecord(p.Record)
	if err != nil {
		return nil, "", err
	}
	data, sum, err := readRegular(p.Idle)
	if err != nil {
		return nil, "", err
	}
	if sum != expected || (hasSize && int64(len(data)) != size) {
		return nil, "", errors.New("idle.rbf sha256 differs from the image record")
	}
	return data, sum, nil
}

const (
	fatUnchanged = iota
	fatReplace
)

func fatAction(path string, data []byte, sum string) (int, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fatReplace, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fatReplace, nil
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("splash file is not a usable regular file")
	}
	existing, existingSum, err := readRegular(path)
	if err != nil {
		return 0, err
	}
	if existingSum == sum && len(existing) == len(data) {
		return fatUnchanged, nil
	}
	return fatReplace, nil
}

func readRegular(path string) ([]byte, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, "", errors.New("splash file is not a usable regular file")
	}
	if info.Size() <= 0 || info.Size() > maxSplash {
		return nil, "", errors.New("splash file is not a usable regular file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSplash+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) != info.Size() {
		return nil, "", errors.New("splash file changed while reading")
	}
	got := sha256.Sum256(data)
	return data, hex.EncodeToString(got[:]), nil
}

func readSidecar(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > store.MaxSplashSidecar {
		return "", errors.New("splash digest is not a usable file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, store.MaxSplashSidecar+1))
	if err != nil {
		return "", err
	}
	sum := strings.TrimSpace(string(data))
	if !sha256Value.MatchString(sum) {
		return "", errors.New("splash digest is invalid")
	}
	return sum, nil
}

func readIdleRecord(path string) (sum string, size int64, hasSize bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxRecord {
		return "", 0, false, errors.New("image record is not a usable file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", 0, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRecord+1))
	if err != nil {
		return "", 0, false, err
	}
	if int64(len(data)) != info.Size() {
		return "", 0, false, errors.New("image record changed while reading")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return "", 0, false, errors.New("image record is invalid")
		}
		if _, exists := fields[key]; exists {
			return "", 0, false, errors.New("image record is invalid")
		}
		fields[key] = value
	}
	if format, ok := fields["format"]; ok && format != "1" {
		return "", 0, false, errors.New("image record is invalid")
	}
	sum = fields["idle_sha256"]
	if !sha256Value.MatchString(sum) {
		return "", 0, false, errors.New("image record has no idle sha256")
	}
	raw, ok := fields["idle_size"]
	if !ok {
		return sum, 0, false, nil
	}
	n, conv := strconv.ParseInt(raw, 10, 64)
	if conv != nil || n <= 0 || raw != strconv.FormatInt(n, 10) {
		return "", 0, false, errors.New("image record idle size is invalid")
	}
	return sum, n, true, nil
}
