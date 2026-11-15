// SPDX-License-Identifier: GPL-3.0-or-later

// Package storagepolicy binds a writable FAT mount to trusted boot-media
// identity and protected files. It supplies no storage lease or activation grant.
package storagepolicy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

type Device interface {
	block.Device
	Identity() sdhci.Identity
}

type Plan struct {
	Format         int    `json:"format"`
	Scope          string `json:"scope"`
	CID            string `json:"cid"`
	Sectors        uint64 `json:"sectors"`
	MBRSHA256      string `json:"mbr_sha256"`
	RecoveryBytes  int    `json:"recovery_bytes"`
	RecoverySHA256 string `json:"recovery_sha256"`
	FitBytes       int    `json:"fit_bytes"`
	FitSHA256      string `json:"fit_sha256"`
}

func (p Plan) Validate() error {
	hexadecimal := func(s string, n int) bool {
		v, err := hex.DecodeString(s)
		return err == nil && len(v) == n && strings.ToLower(s) == s
	}
	if p.Format != 1 || p.Scope != "nano-owned-fat-storage-v1" || !hexadecimal(p.CID, 15) ||
		p.CID == strings.Repeat("0", 30) || p.CID == strings.Repeat("f", 30) ||
		p.Sectors < 131072 || p.Sectors > 1<<32 || p.Sectors%1024 != 0 ||
		!hexadecimal(p.MBRSHA256, 32) ||
		p.RecoveryBytes <= 0 || p.RecoveryBytes > 16384 || !hexadecimal(p.RecoverySHA256, 32) ||
		p.FitBytes <= 0 || p.FitBytes > 8<<20 || !hexadecimal(p.FitSHA256, 32) {
		return fmt.Errorf("storage policy: invalid trusted card/file plan")
	}
	return nil
}

func ParsePlan(data []byte) (Plan, error) {
	var p Plan
	if len(data) == 0 || len(data) > 4096 {
		return p, fmt.Errorf("storage policy: invalid plan length")
	}
	// Reject ambiguous duplicate fields before decoding the typed contract.
	fields := json.NewDecoder(bytes.NewReader(data))
	start, err := fields.Token()
	if err != nil || start != json.Delim('{') {
		return p, fmt.Errorf("storage policy: plan object required")
	}
	seen := map[string]bool{}
	for fields.More() {
		key, err := fields.Token()
		if err != nil {
			return p, err
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return p, fmt.Errorf("storage policy: duplicate plan field")
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
		return p, fmt.Errorf("storage policy: trailing plan data")
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
		return nil, fmt.Errorf("storage policy: %s size differs", name)
	}
	return io.ReadAll(io.LimitReader(contextReader{ctx: ctx, source: f}, int64(size)+1))
}

type confined struct {
	Device
	first, end, backup uint64
}

func (d *confined) WriteSectors(lba uint64, data []byte) error {
	count := uint64(len(data) / 512)
	if len(data)%512 != 0 || lba < d.first || lba > d.end || count > d.end-lba ||
		(count > 0 && (lba == d.first || lba <= d.backup && d.backup-lba < count)) {
		return fmt.Errorf("storage policy: write outside allowed partition metadata/data")
	}
	return d.Device.WriteSectors(lba, data)
}

func partition(media Device, p Plan) (*confined, []byte, []byte, error) {
	mbr := make([]byte, 512)
	if err := media.ReadSectors(0, mbr); err != nil {
		return nil, nil, nil, err
	}
	if mbr[510] != 0x55 || mbr[511] != 0xaa || fmt.Sprintf("%x", sha256.Sum256(mbr)) != p.MBRSHA256 {
		return nil, nil, nil, fmt.Errorf("storage policy: MBR differs")
	}
	var first, count uint64
	found := false
	for i := 0; i < 4; i++ {
		row := mbr[446+i*16 : 462+i*16]
		if bytes.Equal(row, make([]byte, 16)) {
			continue
		}
		if found || row[4] != 0x0c || row[0] != 0 && row[0] != 0x80 {
			return nil, nil, nil, fmt.Errorf("storage policy: exactly one FAT32 MBR partition required")
		}
		found = true
		first = uint64(binary.LittleEndian.Uint32(row[8:]))
		count = uint64(binary.LittleEndian.Uint32(row[12:]))
	}
	if first == 0 || count < 65536 || first >= p.Sectors || count > p.Sectors-first {
		return nil, nil, nil, fmt.Errorf("storage policy: partition outside card")
	}
	boot := make([]byte, 512)
	if err := media.ReadSectors(first, boot); err != nil {
		return nil, nil, nil, err
	}
	reserved := uint64(binary.LittleEndian.Uint16(boot[14:]))
	backup := uint64(binary.LittleEndian.Uint16(boot[50:]))
	if backup == 0 || backup >= reserved {
		return nil, nil, nil, fmt.Errorf("storage policy: missing bounded boot backup")
	}
	copyBoot := make([]byte, 512)
	if err := media.ReadSectors(first+backup, copyBoot); err != nil {
		return nil, nil, nil, err
	}
	if !bytes.Equal(boot, copyBoot) {
		return nil, nil, nil, fmt.Errorf("storage policy: boot backup differs")
	}
	return &confined{Device: media, first: first, end: first + count, backup: first + backup}, mbr, boot, nil
}

// Volume exposes the normal FAT API through partition-confined writes.
// Its owner must serialize SD access; Check is called only under an active lease.
type Volume struct {
	*fat.Volume
	device    Device
	plan      Plan
	mbr, boot []byte
}

func Mount(ctx context.Context, media Device, p Plan) (*Volume, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil || media == nil {
		return nil, fmt.Errorf("storage policy: owned inputs required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if media.Identity() != p.Identity() || media.SectorCount() != p.Sectors {
		return nil, fmt.Errorf("storage policy: card differs")
	}
	disk, mbr, boot, err := partition(media, p)
	if err != nil {
		return nil, err
	}
	fs, err := fat.Mount(disk)
	if err != nil {
		return nil, err
	}
	v := &Volume{Volume: fs, device: media, plan: p, mbr: mbr, boot: boot}
	if err = v.Check(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

// Check rechecks boot metadata and protected recovery files without mutation.
func (v *Volume) Check(ctx context.Context) error {
	if v == nil || ctx == nil || v.device == nil || v.Volume == nil {
		return fmt.Errorf("storage policy: missing checked volume")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.device.Identity() != v.plan.Identity() {
		return fmt.Errorf("storage policy: card changed")
	}
	_, mbr, boot, err := partition(v.device, v.plan)
	if err != nil {
		return err
	}
	if !bytes.Equal(mbr, v.mbr) || !bytes.Equal(boot, v.boot) {
		return fmt.Errorf("storage policy: boot metadata changed")
	}
	for _, f := range []struct {
		name string
		size int
		hash string
	}{
		{"FES/RECOVERY.JSON", v.plan.RecoveryBytes, v.plan.RecoverySHA256},
		{"u-boot.itb", v.plan.FitBytes, v.plan.FitSHA256},
	} {
		data, err := readFile(ctx, v.Volume, f.name, f.size)
		if err != nil {
			return err
		}
		if len(data) != f.size || fmt.Sprintf("%x", sha256.Sum256(data)) != f.hash {
			return fmt.Errorf("storage policy: protected %s differs", f.name)
		}
	}
	return ctx.Err()
}

// Remount is for a completed same-card containment/resume boundary.
func (v *Volume) Remount(ctx context.Context) (*Volume, error) {
	if v == nil {
		return nil, fmt.Errorf("storage policy: missing original mount")
	}
	next, err := Mount(ctx, v.device, v.plan)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(next.mbr, v.mbr) || !bytes.Equal(next.boot, v.boot) {
		return nil, fmt.Errorf("storage policy: remounted boot metadata differs")
	}
	return next, nil
}
