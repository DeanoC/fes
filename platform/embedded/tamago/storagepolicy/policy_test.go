// SPDX-License-Identifier: GPL-3.0-or-later
package storagepolicy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

type syntheticCard struct {
	mu      sync.Mutex
	id      sdhci.Identity
	sectors uint64
	data    map[uint64][block.SectorSize]byte
}

func newSyntheticFATCard(cidFill byte, totalSectors, fatStart, fatCount uint64, partType byte, preloader bool) *syntheticCard {
	const (
		reserved   = uint64(32)
		fats       = uint64(2)
		fatSectors = uint64(516)
		backupSec  = uint64(6)
	)
	var id sdhci.Identity
	for i := range id.CID {
		id.CID[i] = cidFill
	}
	id.Sectors = totalSectors

	c := &syntheticCard{
		id:      id,
		sectors: totalSectors,
		data:    make(map[uint64][block.SectorSize]byte),
	}

	var mbr [block.SectorSize]byte
	slot := 0
	if preloader {
		row := mbr[446+slot*16 : 462+slot*16]
		row[0] = 0x00
		row[4] = 0xa2 // Agilex / Cyclone V HPS preloader partition
		binary.LittleEndian.PutUint32(row[8:], 2048)
		binary.LittleEndian.PutUint32(row[12:], 2048)
		slot++
	}
	row := mbr[446+slot*16 : 462+slot*16]
	row[0] = 0x80
	row[4] = partType
	binary.LittleEndian.PutUint32(row[8:], uint32(fatStart))
	binary.LittleEndian.PutUint32(row[12:], uint32(fatCount))
	mbr[510], mbr[511] = 0x55, 0xaa
	c.data[0] = mbr

	var boot [block.SectorSize]byte
	binary.LittleEndian.PutUint16(boot[11:], block.SectorSize)
	boot[13] = 1
	binary.LittleEndian.PutUint16(boot[14:], uint16(reserved))
	boot[16] = byte(fats)
	binary.LittleEndian.PutUint32(boot[32:], uint32(fatCount))
	binary.LittleEndian.PutUint32(boot[36:], uint32(fatSectors))
	binary.LittleEndian.PutUint32(boot[44:], 2) // root cluster
	binary.LittleEndian.PutUint16(boot[48:], 1) // FSInfo
	binary.LittleEndian.PutUint16(boot[50:], uint16(backupSec))
	boot[510], boot[511] = 0x55, 0xaa
	c.data[fatStart] = boot
	c.data[fatStart+backupSec] = boot

	var fsInfo [block.SectorSize]byte
	binary.LittleEndian.PutUint32(fsInfo[0:], 0x41615252)
	binary.LittleEndian.PutUint32(fsInfo[484:], 0x61417272)
	binary.LittleEndian.PutUint32(fsInfo[488:], 0xffffffff)
	binary.LittleEndian.PutUint32(fsInfo[492:], 0xffffffff)
	binary.LittleEndian.PutUint32(fsInfo[508:], 0xaa550000)
	c.data[fatStart+1] = fsInfo

	var fat0 [block.SectorSize]byte
	binary.LittleEndian.PutUint32(fat0[0:], 0x0ffffff8)
	binary.LittleEndian.PutUint32(fat0[4:], 0x0fffffff)
	binary.LittleEndian.PutUint32(fat0[8:], 0x0fffffff)
	c.data[fatStart+reserved] = fat0
	c.data[fatStart+reserved+fatSectors] = fat0
	return c
}

func (c *syntheticCard) Identity() sdhci.Identity {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

func (c *syntheticCard) SectorCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sectors
}

func (c *syntheticCard) ReadSectors(lba uint64, dst []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(dst)%block.SectorSize != 0 {
		return fmt.Errorf("unaligned read %d", len(dst))
	}
	count := uint64(len(dst) / block.SectorSize)
	if lba > c.sectors || count > c.sectors-lba {
		return fmt.Errorf("out-of-range read %d+%d", lba, count)
	}
	for i := uint64(0); i < count; i++ {
		sec := c.data[lba+i]
		copy(dst[i*block.SectorSize:(i+1)*block.SectorSize], sec[:])
	}
	return nil
}

func (c *syntheticCard) WriteSectors(lba uint64, src []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(src)%block.SectorSize != 0 {
		return fmt.Errorf("unaligned write %d", len(src))
	}
	count := uint64(len(src) / block.SectorSize)
	if lba > c.sectors || count > c.sectors-lba {
		return fmt.Errorf("out-of-range write %d+%d", lba, count)
	}
	for i := uint64(0); i < count; i++ {
		var sec [block.SectorSize]byte
		copy(sec[:], src[i*block.SectorSize:(i+1)*block.SectorSize])
		c.data[lba+i] = sec
	}
	return nil
}

func (c *syntheticCard) Flush() error { return nil }

func TestTrustedStoragePlanCannotAdoptDiagnosticOrAmbiguousIdentity(t *testing.T) {
	p := Plan{Format: 1, Scope: "nano-owned-fat-storage-v1", CID: strings.Repeat("a", 30), Sectors: 7774208, MBRSHA256: strings.Repeat("b", 64), RecoveryBytes: 1470, RecoverySHA256: strings.Repeat("c", 64), FitBytes: 954248, FitSHA256: strings.Repeat("d", 64)}
	raw, _ := json.Marshal(p)
	if got, err := ParsePlan(raw); err != nil || got != p {
		t.Fatal(got, err)
	}
	for _, invalid := range []string{strings.TrimSuffix(string(raw), "}") + `,"format":1}`, strings.TrimSuffix(string(raw), "}") + `,"path":"FES/OTHER"}`, strings.Replace(string(raw), "nano-owned-fat-storage-v1", "nano-sd-fat-file-v1", 1), strings.Replace(string(raw), p.CID, strings.Repeat("0", 30), 1), string(raw) + " true"} {
		if _, err := ParsePlan([]byte(invalid)); err == nil {
			t.Fatal("unsafe storage plan accepted", invalid)
		}
	}
}

func TestPartitionWriteConfinementRejectsOutsideBootAndBackupBeforeDevice(t *testing.T) {
	d := &confined{first: 32768, end: 294912, backup: 32774}
	for _, request := range []struct {
		lba  uint64
		size int
	}{{0, 512}, {32767, 512}, {32768, 512}, {32774, 512}, {32773, 1024}, {294912, 512}, {294911, 1024}, {32775, 511}} {
		if err := d.WriteSectors(request.lba, make([]byte, request.size)); err == nil {
			t.Fatal("unsafe write accepted", request)
		}
	}
}

func TestMountApplianceSupportsSwappableCardsAndPartitions(t *testing.T) {
	ctx := context.Background()
	cardA := newSyntheticFATCard(0x11, 131072, 4096, 66600, 0x0b, true)
	cardB := newSyntheticFATCard(0x22, 196608, 2048, 66600, 0x0c, false)

	// Disallowing additional partitions rejects the 0xa2 preloader partition.
	if _, err := MountAppliance(ctx, cardA, AppliancePolicy{AllowAdditionalPartitions: false}); err == nil {
		t.Fatal("expected rejection when additional 0xa2 partition is present")
	}

	volA, err := MountAppliance(ctx, cardA, AppliancePolicy{
		RequireCID:                true,
		AllowAdditionalPartitions: true,
	})
	if err != nil {
		t.Fatalf("MountAppliance(cardA): %v", err)
	}
	if volA.Mode() != ModeAppliance {
		t.Fatalf("Mode() = %q, want %q", volA.Mode(), ModeAppliance)
	}
	first, count := volA.PartitionRange()
	if first != 4096 || count != 66600 {
		t.Fatalf("PartitionRange() = (%d, %d), want (4096, 66600)", first, count)
	}
	if err := volA.MkdirAll(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	if err := volA.CreateFile(ctx, "FES/PREFS.JSON", []byte(`{"volume":80}`)); err != nil {
		t.Fatal(err)
	}
	if err := volA.Check(ctx); err != nil {
		t.Fatalf("Check after file write: %v", err)
	}
	if volA.CardChanged(cardA) {
		t.Fatal("CardChanged(cardA) = true on same card")
	}
	if !volA.CardChanged(cardB) {
		t.Fatal("CardChanged(cardB) = false on different card")
	}

	// Silent mid-session card swap (mutating card identity under an open volume) fails closed.
	cardA.mu.Lock()
	savedID := cardA.id
	cardA.id.CID[0] = 0x99
	cardA.mu.Unlock()
	if err := volA.Check(ctx); err == nil {
		t.Fatal("expected Check failure after silent card identity change")
	}
	if _, err := volA.Remount(ctx); err == nil {
		t.Fatal("expected Remount failure after silent card identity change")
	}
	cardA.mu.Lock()
	cardA.id = savedID
	cardA.mu.Unlock()

	remountA, err := volA.Remount(ctx)
	if err != nil {
		t.Fatalf("Remount(cardA): %v", err)
	}
	got, err := remountA.ReadFile("FES/PREFS.JSON")
	if err != nil || string(got) != `{"volume":80}` {
		t.Fatalf("remounted read = %q, err = %v", got, err)
	}

	// Explicitly mounting a different card succeeds without build-time pins.
	volB, err := MountAppliance(ctx, cardB, AppliancePolicy{RequireCID: true})
	if err != nil {
		t.Fatalf("MountAppliance(cardB): %v", err)
	}
	if volB.CardIdentity() != cardB.Identity() {
		t.Fatalf("cardB identity mismatch")
	}
	if err := volB.CreateFile(ctx, "CARD_B.TXT", []byte("swapped")); err != nil {
		t.Fatal(err)
	}
	if err := volB.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestAppliancePolicyValidationGuardsAndProtectedFiles(t *testing.T) {
	ctx := context.Background()

	badPolicies := []AppliancePolicy{
		{MinSectors: 200000, MaxSectors: 100000},
		{MaxSectors: (1 << 32) + 1},
		{ProtectedFiles: []string{"../escape"}},
	}
	for _, p := range badPolicies {
		if err := p.Validate(); err == nil {
			t.Fatalf("expected Validate error for %+v", p)
		}
	}

	// RequireCID rejects zero / 0xff CIDs.
	zeroCID := newSyntheticFATCard(0x00, 131072, 2048, 66600, 0x0c, false)
	if _, err := MountAppliance(ctx, zeroCID, AppliancePolicy{RequireCID: true}); err == nil {
		t.Fatal("expected zero CID rejection")
	}
	if _, err := MountAppliance(ctx, zeroCID, AppliancePolicy{RequireCID: false}); err != nil {
		t.Fatalf("expected zero CID to be allowed when RequireCID=false: %v", err)
	}

	// Corrupt backup boot sector is rejected unless SkipBootBackupCheck=true.
	badBackup := newSyntheticFATCard(0x33, 131072, 2048, 66600, 0x0c, false)
	sec := badBackup.data[2048+6]
	sec[0] ^= 0xff
	badBackup.data[2048+6] = sec
	if _, err := MountAppliance(ctx, badBackup, AppliancePolicy{}); err == nil {
		t.Fatal("expected differing backup boot sector rejection")
	}
	if _, err := MountAppliance(ctx, badBackup, AppliancePolicy{SkipBootBackupCheck: true}); err != nil {
		t.Fatalf("expected SkipBootBackupCheck=true to succeed: %v", err)
	}

	// Overlapping MBR partitions must be rejected.
	overlap := newSyntheticFATCard(0x44, 131072, 4096, 66600, 0x0c, true)
	mbr := overlap.data[0]
	binary.LittleEndian.PutUint32(mbr[446+12:], 4096) // [2048..6144) overlaps [4096..)
	overlap.data[0] = mbr
	if _, err := MountAppliance(ctx, overlap, AppliancePolicy{AllowAdditionalPartitions: true}); err == nil {
		t.Fatal("expected overlapping MBR partition rejection")
	}

	// ProtectedFiles checks readability and non-emptiness without pinning content hashes.
	card := newSyntheticFATCard(0x55, 131072, 2048, 66600, 0x0c, false)
	plainVol, err := MountAppliance(ctx, card, AppliancePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if err := plainVol.CreateFile(ctx, "BOOT.TXT", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	guardedVol, err := MountAppliance(ctx, card, AppliancePolicy{ProtectedFiles: []string{"BOOT.TXT"}})
	if err != nil {
		t.Fatalf("MountAppliance with ProtectedFiles: %v", err)
	}
	if err := guardedVol.ReplaceFile(ctx, "BOOT.TXT", []byte("v2-updated-without-pinned-hash")); err != nil {
		t.Fatal(err)
	}
	if err := guardedVol.Check(ctx); err != nil {
		t.Fatalf("Check after mutable update to non-pinned protected file failed: %v", err)
	}
	if err := guardedVol.RemoveFile(ctx, "BOOT.TXT"); err != nil {
		t.Fatal(err)
	}
	if err := guardedVol.Check(ctx); err == nil {
		t.Fatal("expected Check to fail when protected file is removed")
	}

	// Pinned mode still works end-to-end against matching card/files and rejects tampering.
	pinCard := newSyntheticFATCard(0xaa, 131072, 2048, 66600, 0x0c, false)
	prep, err := MountAppliance(ctx, pinCard, AppliancePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	rec := []byte(`{"format":1}`)
	fit := bytes.Repeat([]byte{0x5a}, 4096)
	if err := prep.MkdirAll(ctx, "FES"); err != nil {
		t.Fatal(err)
	}
	if err := prep.CreateFile(ctx, "FES/RECOVERY.JSON", rec); err != nil {
		t.Fatal(err)
	}
	if err := prep.CreateFile(ctx, "u-boot.itb", fit); err != nil {
		t.Fatal(err)
	}
	pinMBR := pinCard.data[0]
	pinPlan := Plan{
		Format:         1,
		Scope:          "nano-owned-fat-storage-v1",
		CID:            strings.Repeat("aa", 15),
		Sectors:        131072,
		MBRSHA256:      fmt.Sprintf("%x", sha256.Sum256(pinMBR[:])),
		RecoveryBytes:  len(rec),
		RecoverySHA256: fmt.Sprintf("%x", sha256.Sum256(rec)),
		FitBytes:       len(fit),
		FitSHA256:      fmt.Sprintf("%x", sha256.Sum256(fit)),
	}
	pinnedVol, err := Mount(ctx, pinCard, pinPlan)
	if err != nil {
		t.Fatalf("pinned Mount: %v", err)
	}
	if pinnedVol.Mode() != ModePinned {
		t.Fatalf("pinned Mode() = %q", pinnedVol.Mode())
	}
	if err := pinnedVol.ReplaceFile(ctx, "u-boot.itb", bytes.Repeat([]byte{0x5b}, 4096)); err != nil {
		t.Fatal(err)
	}
	if err := pinnedVol.Check(ctx); err == nil {
		t.Fatal("expected pinned Check failure after u-boot.itb mutation")
	}
}
