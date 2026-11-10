// SPDX-License-Identifier: GPL-3.0-or-later

// Package fatcheck exercises the real FAT allocation/update path through an
// explicitly owned writable device. It grants no device or package authority.
package fatcheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
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

func (p Plan) Validate() error {
	hexadecimal := func(s string, n int) bool {
		v, err := hex.DecodeString(s)
		return err == nil && len(v) == n && strings.ToLower(s) == s
	}
	if p.Format != 1 || p.Scope != "nano-sd-fat-file-v1" || !hexadecimal(p.CID, 15) ||
		p.CID == strings.Repeat("0", 30) || p.CID == strings.Repeat("f", 30) ||
		p.Sectors < 131072 || p.Sectors > 1<<32 || p.Sectors%1024 != 0 ||
		!hexadecimal(p.MBRSHA256, 32) || !targetPath.MatchString(p.Path) ||
		p.RecoveryBytes <= 0 || p.RecoveryBytes > 16384 || !hexadecimal(p.RecoverySHA256, 32) ||
		p.FitBytes <= 0 || p.FitBytes > 8<<20 || !hexadecimal(p.FitSHA256, 32) {
		return fmt.Errorf("fat file check: invalid trusted card/file plan")
	}
	return nil
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

func (p Plan) Identity() sdhci.Identity {
	var id sdhci.Identity
	v, _ := hex.DecodeString(p.CID)
	copy(id.CID[:], v)
	id.Sectors = p.Sectors
	return id
}

// State is a snapshot of a real platform receipt. The caller must retain the
// underlying device/session on every failure; Run never repairs or retries it.
type State struct {
	Identity          sdhci.Identity
	Owner, Generation uint64
	Active, Prepared  bool
}

type Media interface {
	block.Device
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

type confined struct {
	Media
	first, end, backup uint64
}

func (d *confined) WriteSectors(lba uint64, data []byte) error {
	count := uint64(len(data) / 512)
	if len(data)%512 != 0 || lba < d.first || lba > d.end || count > d.end-lba ||
		(count > 0 && (lba == d.first || lba <= d.backup && d.backup-lba < count)) {
		return fmt.Errorf("fat file check: write outside allowed partition metadata/data")
	}
	return d.Media.WriteSectors(lba, data)
}

func partition(media Media, p Plan) (*confined, []byte, []byte, error) {
	mbr := make([]byte, 512)
	if err := media.ReadSectors(0, mbr); err != nil {
		return nil, nil, nil, err
	}
	if mbr[510] != 0x55 || mbr[511] != 0xaa || fmt.Sprintf("%x", sha256.Sum256(mbr)) != p.MBRSHA256 {
		return nil, nil, nil, fmt.Errorf("fat file check: MBR differs")
	}
	var first, count uint64
	found := false
	for i := 0; i < 4; i++ {
		row := mbr[446+i*16 : 462+i*16]
		if bytes.Equal(row, make([]byte, 16)) {
			continue
		}
		if found || row[4] != 0x0c || row[0] != 0 && row[0] != 0x80 {
			return nil, nil, nil, fmt.Errorf("fat file check: exactly one FAT32 MBR partition required")
		}
		found = true
		first = uint64(binary.LittleEndian.Uint32(row[8:]))
		count = uint64(binary.LittleEndian.Uint32(row[12:]))
	}
	if first == 0 || count < 65536 || first >= p.Sectors || count > p.Sectors-first {
		return nil, nil, nil, fmt.Errorf("fat file check: partition outside card")
	}
	boot := make([]byte, 512)
	if err := media.ReadSectors(first, boot); err != nil {
		return nil, nil, nil, err
	}
	reserved := uint64(binary.LittleEndian.Uint16(boot[14:]))
	backup := uint64(binary.LittleEndian.Uint16(boot[50:]))
	if backup == 0 || backup >= reserved {
		return nil, nil, nil, fmt.Errorf("fat file check: missing bounded boot backup")
	}
	copyBoot := make([]byte, 512)
	if err := media.ReadSectors(first+backup, copyBoot); err != nil {
		return nil, nil, nil, err
	}
	if !bytes.Equal(boot, copyBoot) {
		return nil, nil, nil, fmt.Errorf("fat file check: boot backup differs")
	}
	return &confined{Media: media, first: first, end: first + count, backup: first + backup}, mbr, boot, nil
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
	disk, mbr, boot, err := partition(media, p)
	if err != nil {
		return err
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		return err
	}
	protected := func(v *fat.Volume) error {
		for _, file := range []struct {
			name string
			size int
			hash string
		}{
			{"FES/RECOVERY.JSON", p.RecoveryBytes, p.RecoverySHA256}, {"u-boot.itb", p.FitBytes, p.FitSHA256}} {
			if err := ctx.Err(); err != nil {
				return err
			}
			data, err := readFile(ctx, v, file.name, file.size)
			if err != nil {
				return err
			}
			if len(data) != file.size || fmt.Sprintf("%x", sha256.Sum256(data)) != file.hash {
				return fmt.Errorf("fat file check: protected %s differs", file.name)
			}
		}
		return nil
	}
	if err = protected(volume); err != nil {
		return err
	}
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
	next, afterMBR, afterBoot, err := partition(media, p)
	if err != nil {
		return err
	}
	if !bytes.Equal(mbr, afterMBR) || !bytes.Equal(boot, afterBoot) {
		return fmt.Errorf("fat file check: boot metadata changed")
	}
	volume, err = fat.Mount(next)
	if err != nil {
		return err
	}
	if err = verify(volume, updated); err != nil {
		return err
	}
	if err = protected(volume); err != nil {
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
