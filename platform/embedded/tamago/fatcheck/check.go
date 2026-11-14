// SPDX-License-Identifier: GPL-3.0-or-later

// Package fatcheck exercises the real FAT allocation/update path through an
// explicitly owned writable device. It grants no device or package authority.
package fatcheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"

	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
	"github.com/DeanoC/fes/platform/embedded/tamago/storagepolicy"
)

type Plan struct {
	Format         int    `json:"format"`
	Scope          string `json:"scope"`
	CID            string `json:"cid"`
	Sectors        uint64 `json:"sectors"`
	MBRSHA256      string `json:"mbr_sha256"`
	Path           string `json:"path"`
	RecoveryBytes  int    `json:"recovery_bytes"`
	RecoverySHA256 string `json:"recovery_sha256"`
	FitBytes       int    `json:"fit_bytes"`
	FitSHA256      string `json:"fit_sha256"`
}

var targetPath = regexp.MustCompile(`^FES/SDP-[0-9a-f]{16}\.JSN$`)

func (p Plan) storagePlan() storagepolicy.Plan {
	return storagepolicy.Plan{Format: 1, Scope: "nano-owned-fat-storage-v1", CID: p.CID, Sectors: p.Sectors, MBRSHA256: p.MBRSHA256,
		RecoveryBytes: p.RecoveryBytes, RecoverySHA256: p.RecoverySHA256, FitBytes: p.FitBytes, FitSHA256: p.FitSHA256}
}

func (p Plan) Validate() error {
	if p.Format != 1 || p.Scope != "nano-sd-fat-file-v1" || !targetPath.MatchString(p.Path) {
		return fmt.Errorf("fat file check: invalid diagnostic plan")
	}
	return p.storagePlan().Validate()
}

func ParsePlan(data []byte) (Plan, error) {
	var p Plan
	if len(data) == 0 || len(data) > 4096 {
		return p, fmt.Errorf("fat file check: invalid plan length")
	}
	// Reject ambiguous duplicate fields before decoding the typed contract.
	fields := json.NewDecoder(bytes.NewReader(data))
	start, err := fields.Token()
	if err != nil || start != json.Delim('{') {
		return p, fmt.Errorf("fat file check: plan object required")
	}
	seen := map[string]bool{}
	for fields.More() {
		key, err := fields.Token()
		if err != nil {
			return p, err
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return p, fmt.Errorf("fat file check: duplicate plan field")
		}
		seen[name] = true
		var value json.RawMessage
		if err := fields.Decode(&value); err != nil {
			return p, err
		}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	if d.Decode(new(any)) != io.EOF {
		return p, fmt.Errorf("fat file check: trailing plan data")
	}
	return p, p.Validate()
}

func (p Plan) Identity() sdhci.Identity { return p.storagePlan().Identity() }

// State is a snapshot of a real platform receipt. The caller must retain the
// underlying device/session on every failure; Run never repairs or retries it.
type State struct {
	Identity          sdhci.Identity
	Owner, Generation uint64
	Active, Prepared  bool
}

type Media interface {
	storagepolicy.Device
	State() (State, bool)
	Quiesce() error
	Resume() error
}

// Payload is deterministic so an independent loader can compare exact bytes.
// Creation allocates 4096 bytes; the update shrinks the visible file to 1024.
func Payload(path string, updated bool) ([]byte, error) {
	if !targetPath.MatchString(path) {
		return nil, fmt.Errorf("fat file check: invalid payload path")
	}
	state, size := "created", 4096
	if updated {
		state, size = "updated", 1024
	}
	text := fmt.Sprintf("{\"format\":1,\"scope\":\"nano-sd-fat-file-v1\",\"path\":%q,\"state\":%q}", path, state)
	return append([]byte(text), bytes.Repeat([]byte{' '}, size-len(text))...), nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(b) > 4096 {
		b = b[:4096]
	}
	return r.source.Read(b)
}

func readFile(ctx context.Context, v *fat.Volume, name string, size int) ([]byte, error) {
	f, err := v.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() != int64(size) {
		return nil, fmt.Errorf("fat file check: %s size differs", name)
	}
	return io.ReadAll(io.LimitReader(contextReader{ctx: ctx, source: f}, int64(size)+1))
}

// Run creates and updates one new file, contains/resumes the same device,
// remounts and reads it, rechecks protected files and ends contained. It is a
// clean-restart diagnostic, not power-loss recovery or package installation.
func Run(ctx context.Context, media Media, p Plan, log io.Writer) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if ctx == nil || media == nil || log == nil {
		return fmt.Errorf("fat file check: missing owned inputs")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	initial, valid := media.State()
	if !valid || !initial.Active || !initial.Prepared || initial.Owner == 0 || initial.Generation == 0 || initial.Generation > ^uint64(0)-4 || initial.Identity != p.Identity() || media.SectorCount() != p.Sectors {
		return fmt.Errorf("fat file check: wrong active card/ownership")
	}
	created, _ := Payload(p.Path, false)
	updated, _ := Payload(p.Path, true)
	emit := func(stage string, delta uint64, active bool, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		s, ok := media.State()
		if !ok || s.Identity != initial.Identity || s.Owner != initial.Owner || s.Generation != initial.Generation+delta || s.Active != active || active && !s.Prepared {
			return fmt.Errorf("fat file check: %s ownership differs", stage)
		}
		_, err := fmt.Fprintf(log, "FES_NANO_FATFILE %s cid=%s sectors=%d path=%s bytes=%d sha256=%x owner=%016x generation=%d\n", stage, p.CID, p.Sectors, p.Path, len(data), sha256.Sum256(data), s.Owner, s.Generation)
		return err
	}
	mounted, err := storagepolicy.Mount(ctx, media, p.storagePlan())
	if err != nil {
		return err
	}
	volume := mounted.Volume
	if existing, openErr := volume.Open(p.Path); !errors.Is(openErr, fs.ErrNotExist) {
		if existing != nil {
			existing.Close()
		}
		return fmt.Errorf("fat file check: target not absent: %v", openErr)
	}
	if err = emit("READY", 0, true, updated); err != nil {
		return err
	}
	if err = volume.CreateFile(ctx, p.Path, created); err != nil {
		return err
	}
	verify := func(v *fat.Volume, want []byte) error {
		got, err := readFile(ctx, v, p.Path, len(want))
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("fat file check: file readback differs")
		}
		return nil
	}
	if err = verify(volume, created); err != nil {
		return err
	}
	if err = emit("CREATED", 0, true, created); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = volume.WriteFile(p.Path, updated); err != nil {
		return err
	}
	if err = verify(volume, updated); err != nil {
		return err
	}
	if err = emit("UPDATED", 0, true, updated); err != nil {
		return err
	}
	if err = media.Quiesce(); err != nil {
		return err
	}
	if err = emit("CONTAINED", 1, false, updated); err != nil {
		return err
	}
	if err = media.Resume(); err != nil {
		return err
	}
	mounted, err = mounted.Remount(ctx)
	if err != nil {
		return err
	}
	volume = mounted.Volume
	if err = verify(volume, updated); err != nil {
		return err
	}
	if err = emit("RESUMED", 3, true, updated); err != nil {
		return err
	}
	if err = media.Quiesce(); err != nil {
		return err
	}
	return emit("PASS", 4, false, updated)
}
