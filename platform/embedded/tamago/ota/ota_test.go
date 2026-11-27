// SPDX-License-Identifier: GPL-3.0-or-later

package ota_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/ota"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
	"github.com/DeanoC/fes/platform/embedded/tamago/storagepolicy"
)

type recordingCard struct {
	mu          sync.Mutex
	id          sdhci.Identity
	sectors     uint64
	data        map[uint64][block.SectorSize]byte
	recordSnaps bool
	snapshots   []map[uint64][block.SectorSize]byte
}

func newApplianceFATCard(cidFill byte) *recordingCard {
	const (
		totalSectors = uint64(131072) // 64 MiB
		fatStart     = uint64(4096)
		fatCount     = uint64(66600)
		reserved     = uint64(32)
		fats         = uint64(2)
		fatSectors   = uint64(516)
		backupSec    = uint64(6)
	)
	var id sdhci.Identity
	for i := range id.CID {
		id.CID[i] = cidFill
	}
	id.Sectors = totalSectors

	c := &recordingCard{
		id:      id,
		sectors: totalSectors,
		data:    make(map[uint64][block.SectorSize]byte),
	}

	var mbr [block.SectorSize]byte
	// Include a non-FAT32 0xa2 HPS preloader partition to test multi-partition layouts.
	row0 := mbr[446:462]
	row0[4] = 0xa2
	binary.LittleEndian.PutUint32(row0[8:], 2048)
	binary.LittleEndian.PutUint32(row0[12:], 2048)

	row1 := mbr[462:478]
	row1[0] = 0x80
	row1[4] = 0x0c
	binary.LittleEndian.PutUint32(row1[8:], uint32(fatStart))
	binary.LittleEndian.PutUint32(row1[12:], uint32(fatCount))
	mbr[510], mbr[511] = 0x55, 0xaa
	c.data[0] = mbr

	var boot [block.SectorSize]byte
	binary.LittleEndian.PutUint16(boot[11:], block.SectorSize)
	boot[13] = 1 // 512 B clusters (realistic small media exercise)
	binary.LittleEndian.PutUint16(boot[14:], uint16(reserved))
	boot[16] = byte(fats)
	binary.LittleEndian.PutUint32(boot[32:], uint32(fatCount))
	binary.LittleEndian.PutUint32(boot[36:], uint32(fatSectors))
	binary.LittleEndian.PutUint32(boot[44:], 2)
	binary.LittleEndian.PutUint16(boot[48:], 1)
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

func (c *recordingCard) Identity() sdhci.Identity {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

func (c *recordingCard) SectorCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sectors
}

func (c *recordingCard) ReadSectors(lba uint64, dst []byte) error {
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

func (c *recordingCard) WriteSectors(lba uint64, src []byte) error {
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
		if c.recordSnaps {
			c.snapshots = append(c.snapshots, maps.Clone(c.data))
		}
	}
	return nil
}

func (c *recordingCard) Flush() error { return nil }

func mountAppliance(t *testing.T, card storagepolicy.Device) *storagepolicy.Volume {
	t.Helper()
	vol, err := storagepolicy.MountAppliance(context.Background(), card, storagepolicy.AppliancePolicy{
		RequireCID:                true,
		AllowAdditionalPartitions: true,
	})
	if err != nil {
		t.Fatalf("MountAppliance: %v", err)
	}
	return vol
}

func openTestManager(t *testing.T, card storagepolicy.Device, mutateCfg ...func(*ota.Config)) (*storagepolicy.Volume, *ota.Manager) {
	t.Helper()
	vol := mountAppliance(t, card)
	tick := int64(1730000000)
	cfg := ota.Config{
		Board:         "de25-nano",
		MaxImageBytes: 2 << 20,
		MaxTries:      3,
		Now: func() time.Time {
			tick += 10
			return time.Unix(tick, 0)
		},
	}
	for _, fn := range mutateCfg {
		fn(&cfg)
	}
	mgr, err := ota.Open(context.Background(), vol, cfg)
	if err != nil {
		t.Fatalf("ota.Open: %v", err)
	}
	return vol, mgr
}

func shaHex(b []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func TestABUpdateLifecycleAcrossSlotsAndReboots(t *testing.T) {
	ctx := context.Background()
	card := newApplianceFATCard(0x21)
	v1Payload := bytes.Repeat([]byte("V1-IMAGE-"), 512)
	v2Payload := bytes.Repeat([]byte("V2-IMAGE-"), 768)
	v3Payload := bytes.Repeat([]byte("V3-IMAGE-"), 640)

	_, mgr := openTestManager(t, card)
	if st := mgr.Status(); st.ActiveSlot != ota.SlotA || st.Phase != ota.PhaseIdle {
		t.Fatalf("unexpected initial state: %+v", st)
	}
	seedRec, err := mgr.SeedActiveSlot(ctx, ota.StageRequest{
		Version:       "1.0.0",
		Target:        "de25-nano",
		Payload:       v1Payload,
		RollbackEpoch: 1,
	})
	if err != nil || !seedRec.Healthy {
		t.Fatalf("SeedActiveSlot: rec=%+v err=%v", seedRec, err)
	}

	// Reboot -> SelectBoot should stay cleanly on SlotA in Idle.
	_, mgr = openTestManager(t, card)
	dec, err := mgr.SelectBoot(ctx)
	if err != nil || dec.Slot != ota.SlotA || dec.IsTrial || dec.RolledBack {
		t.Fatalf("SelectBoot idle: dec=%+v err=%v", dec, err)
	}

	// Stage v2 into SlotB and verify ActiveSlot remains SlotA until ConfirmBoot.
	stagedRec, err := mgr.Stage(ctx, ota.StageRequest{
		Version:        "2.0.0",
		Target:         "de25-nano",
		Payload:        v2Payload,
		ExpectedSHA256: shaHex(v2Payload),
		RollbackEpoch:  2,
	})
	if err != nil || stagedRec.Healthy {
		t.Fatalf("Stage v2: rec=%+v err=%v", stagedRec, err)
	}
	if _, err := mgr.Activate(ctx); err != nil {
		t.Fatalf("Activate v2: %v", err)
	}

	// Reboot onto trial SlotB, confirm it, then verify SlotB is active across fresh reopen.
	_, mgr = openTestManager(t, card)
	dec, err = mgr.SelectBoot(ctx)
	if err != nil || dec.Slot != ota.SlotB || !dec.IsTrial || dec.TriesRemaining != 2 || dec.Manifest == nil || dec.Manifest.Version != "2.0.0" {
		t.Fatalf("SelectBoot trial SlotB: dec=%+v err=%v", dec, err)
	}
	confirmed, err := mgr.ConfirmBoot(ctx, ota.SlotB)
	if err != nil || confirmed.ActiveSlot != ota.SlotB || confirmed.Phase != ota.PhaseIdle || !confirmed.SlotB.Healthy {
		t.Fatalf("ConfirmBoot SlotB: st=%+v err=%v", confirmed, err)
	}
	// Idempotent ConfirmBoot in Idle returns cleanly.
	if again, err := mgr.ConfirmBoot(ctx, ota.SlotB); err != nil || again.Generation != confirmed.Generation {
		t.Fatalf("idempotent ConfirmBoot: %+v err=%v", again, err)
	}

	// Stage v3 back into SlotA and confirm A->B->A ping-pong.
	if _, err := mgr.Stage(ctx, ota.StageRequest{Version: "3.0.0", Target: "de25-nano", Payload: v3Payload, RollbackEpoch: 3}); err != nil {
		t.Fatalf("Stage v3 into SlotA: %v", err)
	}
	if _, err := mgr.Activate(ctx); err != nil {
		t.Fatalf("Activate v3: %v", err)
	}
	_, mgr = openTestManager(t, card)
	dec, err = mgr.SelectBoot(ctx)
	if err != nil || dec.Slot != ota.SlotA || !dec.IsTrial {
		t.Fatalf("SelectBoot trial SlotA: dec=%+v err=%v", dec, err)
	}
	finalSt, err := mgr.ConfirmBoot(ctx, ota.SlotA)
	if err != nil || finalSt.ActiveSlot != ota.SlotA || finalSt.Phase != ota.PhaseIdle || !finalSt.SlotA.Healthy || !finalSt.SlotB.Healthy {
		t.Fatalf("ConfirmBoot SlotA: st=%+v err=%v", finalSt, err)
	}
}

func TestTrialBootExhaustionAndCorruptCandidateTriggerAutomaticRollback(t *testing.T) {
	ctx := context.Background()
	card := newApplianceFATCard(0x22)
	v1 := bytes.Repeat([]byte("GOOD-A-"), 400)
	v2 := bytes.Repeat([]byte("BAD-B-"), 400)

	_, mgr := openTestManager(t, card, func(c *ota.Config) { c.MaxTries = 2 })
	if _, err := mgr.SeedActiveSlot(ctx, ota.StageRequest{Version: "1.0.0", Target: "de25-nano", Payload: v1}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Stage(ctx, ota.StageRequest{Version: "2.0.0", Target: "de25-nano", Payload: v2}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Activate(ctx); err != nil {
		t.Fatal(err)
	}

	// Boot 1 & Boot 2 fail to ConfirmBoot -> tries drop 2 -> 1 -> 0.
	for _, wantTries := range []int{1, 0} {
		_, mgr = openTestManager(t, card, func(c *ota.Config) { c.MaxTries = 2 })
		dec, err := mgr.SelectBoot(ctx)
		if err != nil || !dec.IsTrial || dec.Slot != ota.SlotB || dec.TriesRemaining != wantTries || dec.RolledBack {
			t.Fatalf("trial boot (wantTries=%d): dec=%+v err=%v", wantTries, dec, err)
		}
	}

	// Boot 3 sees TriesRemaining == 0 and automatically rolls back to SlotA.
	_, mgr = openTestManager(t, card, func(c *ota.Config) { c.MaxTries = 2 })
	dec, err := mgr.SelectBoot(ctx)
	if err != nil || dec.Slot != ota.SlotA || dec.IsTrial || !dec.RolledBack {
		t.Fatalf("expected auto-rollback to SlotA: dec=%+v err=%v", dec, err)
	}
	st := mgr.Status()
	if st.Phase != ota.PhaseIdle || st.ActiveSlot != ota.SlotA || st.PendingSlot != "" || st.SlotB == nil || st.SlotB.Healthy {
		t.Fatalf("unexpected state after auto-rollback: %+v", st)
	}

	// Corrupt candidate image on disk before trial SelectBoot -> immediate auto-rollback.
	if _, err := mgr.Stage(ctx, ota.StageRequest{Version: "2.1.0", Target: "de25-nano", Payload: v2}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	vol := mountAppliance(t, card)
	corrupt := append([]byte(nil), v2...)
	corrupt[17] ^= 0x55
	if err := vol.ReplaceFile(ctx, ota.SlotB.ImagePath(ota.DefaultRootDir), corrupt); err != nil {
		t.Fatal(err)
	}
	_, mgr = openTestManager(t, card)
	dec, err = mgr.SelectBoot(ctx)
	if err != nil || dec.Slot != ota.SlotA || !dec.RolledBack {
		t.Fatalf("expected immediate rollback on corrupt candidate: dec=%+v err=%v", dec, err)
	}
	if _, err := mgr.VerifySlot(ctx, ota.SlotA); err != nil {
		t.Fatalf("SlotA should remain valid: %v", err)
	}
}

func TestValidationGuardsRollbackAndRecovery(t *testing.T) {
	ctx := context.Background()
	card := newApplianceFATCard(0x33)
	v1 := bytes.Repeat([]byte("ONE"), 512)
	v2 := bytes.Repeat([]byte("TWO"), 512)

	var sigFail bool
	vol, mgr := openTestManager(t, card, func(c *ota.Config) {
		c.EnforceRollbackProtection = true
		c.VerifySignature = func(manifest ota.SlotManifest, digest [32]byte, signature []byte) error {
			if sigFail || string(signature) != "valid-sig" {
				return errors.New("bad signature")
			}
			return nil
		}
	})
	if _, err := mgr.SeedActiveSlot(ctx, ota.StageRequest{
		Version:       "1.0.0",
		Target:        "de25-nano",
		Payload:       v1,
		RollbackEpoch: 5,
		Signature:     []byte("valid-sig"),
	}); err != nil {
		t.Fatal(err)
	}

	// Target mismatch, digest mismatch, rollback epoch, signature, and space checks.
	if _, err := mgr.Stage(ctx, ota.StageRequest{Version: "2", Target: "qemu", Payload: v2, RollbackEpoch: 5, Signature: []byte("valid-sig")}); !errors.Is(err, ota.ErrTargetMismatch) {
		t.Fatalf("got %v, want ErrTargetMismatch", err)
	}
	if _, err := mgr.Stage(ctx, ota.StageRequest{Version: "2", Target: "de25-nano", Payload: v2, ExpectedSHA256: shaHex(v1), RollbackEpoch: 5, Signature: []byte("valid-sig")}); !errors.Is(err, ota.ErrDigestMismatch) {
		t.Fatalf("got %v, want ErrDigestMismatch", err)
	}
	if _, err := mgr.Stage(ctx, ota.StageRequest{Version: "2", Target: "de25-nano", Payload: v2, RollbackEpoch: 4, Signature: []byte("valid-sig")}); !errors.Is(err, ota.ErrRollbackRejected) {
		t.Fatalf("got %v, want ErrRollbackRejected", err)
	}
	sigFail = true
	if _, err := mgr.Stage(ctx, ota.StageRequest{Version: "2", Target: "de25-nano", Payload: v2, RollbackEpoch: 6, Signature: []byte("valid-sig")}); !errors.Is(err, ota.ErrSignatureInvalid) {
		t.Fatalf("got %v, want ErrSignatureInvalid", err)
	}
	sigFail = false

	// Rollback from Idle fails when SlotB is still empty/unhealthy.
	if _, err := mgr.Rollback(ctx, "no slot b"); !errors.Is(err, ota.ErrSlotUnavailable) {
		t.Fatalf("got %v, want ErrSlotUnavailable", err)
	}

	// Game-appliance mode with EnforceRollbackProtection=false allows intentional downgrade.
	_, mgrDowngrade := openTestManager(t, card, func(c *ota.Config) { c.EnforceRollbackProtection = false })
	if _, err := mgrDowngrade.Stage(ctx, ota.StageRequest{Version: "0.9.0", Target: "de25-nano", Payload: v2, RollbackEpoch: 1}); err != nil {
		t.Fatalf("expected downgrade stage allowed when EnforceRollbackProtection=false: %v", err)
	}
	// CancelPending returns cleanly from Staged -> Idle.
	canceled, err := mgrDowngrade.CancelPending(ctx)
	if err != nil || canceled.Phase != ota.PhaseIdle || canceled.PendingSlot != "" {
		t.Fatalf("CancelPending: st=%+v err=%v", canceled, err)
	}

	// Stage, Activate, trial boot, ConfirmBoot SlotB, then manual Idle Rollback back to SlotA.
	if _, err := mgrDowngrade.Stage(ctx, ota.StageRequest{Version: "2.0.0", Target: "de25-nano", Payload: v2, RollbackEpoch: 6}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgrDowngrade.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	// Staging while PhasePending must fail with ErrInvalidPhase.
	if _, err := mgrDowngrade.Stage(ctx, ota.StageRequest{Version: "2.1.0", Target: "de25-nano", Payload: v2}); !errors.Is(err, ota.ErrInvalidPhase) {
		t.Fatalf("got %v, want ErrInvalidPhase during pending", err)
	}
	if _, err := mgrDowngrade.SelectBoot(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := mgrDowngrade.ConfirmBoot(ctx, ota.SlotB); err != nil {
		t.Fatal(err)
	}
	rolled, err := mgrDowngrade.Rollback(ctx, "user downgraded")
	if err != nil || rolled.ActiveSlot != ota.SlotA || rolled.Phase != ota.PhaseIdle {
		t.Fatalf("Idle Rollback to SlotA: st=%+v err=%v", rolled, err)
	}

	// Strict JSON state corruption detection and operator ResetToActive recovery.
	rawState, err := vol.ReadFile("FES/OTA/STATE.JSN")
	if err != nil {
		t.Fatal(err)
	}
	dupState := bytes.Replace(rawState, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1)
	if err := vol.ReplaceFile(ctx, "FES/OTA/STATE.JSN", dupState); err != nil {
		t.Fatal(err)
	}
	if _, err := ota.Open(ctx, vol, ota.Config{}); !errors.Is(err, ota.ErrCorruptState) {
		t.Fatalf("expected ErrCorruptState on duplicate JSON field, got %v", err)
	}
	recovered, err := ota.ResetToActive(ctx, vol, ota.Config{}, ota.SlotA)
	if err != nil || recovered.Status().ActiveSlot != ota.SlotA || recovered.Status().Phase != ota.PhaseIdle {
		t.Fatalf("ResetToActive failed: err=%v", err)
	}
	var parsed ota.State
	newRaw, err := vol.ReadFile("FES/OTA/STATE.JSN")
	if err != nil || json.Unmarshal(newRaw, &parsed) != nil || parsed.ActiveSlot != ota.SlotA {
		t.Fatalf("persisted reset state unexpected: %s (%v)", newRaw, err)
	}

	// Insufficient free space check using huge MaxImageBytes against 32 MiB partition.
	_, bigMgr := openTestManager(t, card, func(c *ota.Config) { c.MaxImageBytes = 64 << 20 })
	if _, err := bigMgr.Stage(ctx, ota.StageRequest{
		Version: "9.0.0",
		Target:  "de25-nano",
		Payload: make([]byte, 40<<20),
	}); !errors.Is(err, ota.ErrInsufficientSpace) {
		t.Fatalf("expected ErrInsufficientSpace, got %v", err)
	}
}

func TestPowerLossCrashConsistencyAcrossEverySectorWrite(t *testing.T) {
	ctx := context.Background()
	card := newApplianceFATCard(0x44)
	v1 := bytes.Repeat([]byte("ACTIVE-SLOT-A-"), 400)    // 5600 bytes (11 clusters)
	v2 := bytes.Repeat([]byte("CANDIDATE-SLOT-B-"), 400) // 6800 bytes (14 clusters)

	_, mgr := openTestManager(t, card)
	if _, err := mgr.SeedActiveSlot(ctx, ota.StageRequest{
		Version:       "1.0.0",
		Target:        "de25-nano",
		Payload:       v1,
		RollbackEpoch: 1,
	}); err != nil {
		t.Fatal(err)
	}

	// Record every intermediate sector snapshot across Stage -> Activate -> SelectBoot -> ConfirmBoot.
	card.mu.Lock()
	card.recordSnaps = true
	card.snapshots = nil
	card.mu.Unlock()

	if _, err := mgr.Stage(ctx, ota.StageRequest{
		Version:        "2.0.0",
		Target:         "de25-nano",
		Payload:        v2,
		ExpectedSHA256: shaHex(v2),
		RollbackEpoch:  2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	if dec, err := mgr.SelectBoot(ctx); err != nil || dec.Slot != ota.SlotB {
		t.Fatalf("SelectBoot: dec=%+v err=%v", dec, err)
	}
	if _, err := mgr.ConfirmBoot(ctx, ota.SlotB); err != nil {
		t.Fatal(err)
	}

	card.mu.Lock()
	snaps := append([]map[uint64][block.SectorSize]byte(nil), card.snapshots...)
	card.recordSnaps = false
	card.mu.Unlock()

	if len(snaps) < 20 {
		t.Fatalf("expected substantial sector write trace, got %d snapshots", len(snaps))
	}

	var sawStagingRecover, sawStagedStayA, sawTrialB, sawConfirmedB int
	for i, snap := range snaps {
		crashCard := &recordingCard{
			id:      card.id,
			sectors: card.sectors,
			data:    maps.Clone(snap),
		}
		vol, err := storagepolicy.MountAppliance(ctx, crashCard, storagepolicy.AppliancePolicy{
			RequireCID:                true,
			AllowAdditionalPartitions: true,
		})
		if err != nil {
			t.Fatalf("snapshot %d/%d failed MountAppliance: %v", i, len(snaps), err)
		}
		crashMgr, err := ota.Open(ctx, vol, ota.Config{Board: "de25-nano", MaxTries: 3})
		if err != nil {
			t.Fatalf("snapshot %d/%d failed ota.Open: %v", i, len(snaps), err)
		}
		st := crashMgr.Status()
		if err := st.Validate(); err != nil {
			t.Fatalf("snapshot %d/%d invalid state %+v: %v", i, len(snaps), st, err)
		}

		// Active slot A must ALWAYS remain bit-for-bit readable, and once confirmed on B
		// ActiveSlot B must match v2 bit-for-bit.
		gotA, err := vol.ReadFile(ota.SlotA.ImagePath(ota.DefaultRootDir))
		if err != nil || !bytes.Equal(gotA, v1) {
			t.Fatalf("snapshot %d/%d corrupted SlotA image: len=%d err=%v", i, len(snaps), len(gotA), err)
		}
		if _, err := crashMgr.VerifySlot(ctx, st.ActiveSlot); err != nil {
			t.Fatalf("snapshot %d/%d active slot %s failed VerifySlot: %v", i, len(snaps), st.ActiveSlot, err)
		}
		if st.Phase == ota.PhaseStaged || st.Phase == ota.PhasePending || st.Phase == ota.PhaseTrial {
			if _, err := crashMgr.VerifySlot(ctx, st.PendingSlot); err != nil {
				t.Fatalf("snapshot %d/%d in phase %s has unverified candidate %s: %v", i, len(snaps), st.Phase, st.PendingSlot, err)
			}
		}

		dec, err := crashMgr.SelectBoot(ctx)
		if err != nil {
			// A snapshot captured strictly between writing FAT0 and FAT1 refuses further
			// FAT mutations until fsck repairs mirrors; reads and VerifySlot above still succeeded.
			if strings.Contains(err.Error(), "allocation FAT mirror changed") {
				continue
			}
			t.Fatalf("snapshot %d/%d SelectBoot failed: %v", i, len(snaps), err)
		}
		if _, err := crashMgr.VerifySlot(ctx, dec.Slot); err != nil {
			t.Fatalf("snapshot %d/%d selected unverified slot %s: %v", i, len(snaps), dec.Slot, err)
		}
		switch {
		case dec.RecoveredInterruptedStage && dec.Slot == ota.SlotA:
			sawStagingRecover++
		case st.Phase == ota.PhaseStaged && dec.Slot == ota.SlotA:
			sawStagedStayA++
		case dec.Slot == ota.SlotB && dec.IsTrial:
			sawTrialB++
		case dec.Slot == ota.SlotB && !dec.IsTrial:
			sawConfirmedB++
		}
	}
	if sawStagingRecover == 0 || sawStagedStayA == 0 || sawTrialB == 0 || sawConfirmedB == 0 {
		t.Fatalf("crash snapshots missed lifecycle phases: stagingRecover=%d stagedStayA=%d trialB=%d confirmedB=%d",
			sawStagingRecover, sawStagedStayA, sawTrialB, sawConfirmedB)
	}
}

func TestManagerConcurrentStatusAndVerifyUnderRace(t *testing.T) {
	ctx := context.Background()
	card := newApplianceFATCard(0x55)
	v1 := bytes.Repeat([]byte("RACE-TEST-"), 256)
	_, mgr := openTestManager(t, card)
	if _, err := mgr.SeedActiveSlot(ctx, ota.StageRequest{Version: "1.0.0", Target: "de25-nano", Payload: v1}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				st := mgr.Status()
				if !st.ActiveSlot.Valid() {
					t.Errorf("invalid active slot: %+v", st)
				}
				if _, err := mgr.VerifySlot(ctx, ota.SlotA); err != nil {
					t.Errorf("VerifySlot: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}
