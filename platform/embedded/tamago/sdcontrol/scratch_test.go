// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func scratchCard(t *testing.T) (*rpc, *card, ScratchPlan) {
	t.Helper()
	firmware, c := newRPC(t, "")
	c.image = bytes.Repeat([]byte{0x91}, 8192*512)
	clear(c.image[:512])
	mbr := c.image[:512]
	mbr[510], mbr[511], mbr[450] = 0x55, 0xaa, 0x0c
	binary.LittleEndian.PutUint32(mbr[454:], 2048)
	binary.LittleEndian.PutUint32(mbr[458:], 1024)
	p := ScratchPlan{Format: 1, Scope: "nano-sd-scratch-write-v1", CID: "12345699aabbcc5566778811223344", Sectors: 8192,
		MBRSHA256: fmt.Sprintf("%x", sha256.Sum256(mbr)), OriginalSHA256: fmt.Sprintf("%x", sha256.Sum256(c.image[len(c.image)-4096:]))}
	return firmware, c, p
}

func scratchFixture(t *testing.T) (*rpc, *card, *Session, ScratchPlan) {
	t.Helper()
	firmware, c, p := scratchCard(t)
	s, err := Open(firmware, c, Config{InputClockHz: 200000000, WriteCard: p.Identity()})
	if err != nil {
		t.Fatal(err)
	}
	return firmware, c, s, p
}

func TestScratchInspectionOnReadOnlySessionNeverWritesOrStops(t *testing.T) {
	firmware, c, want := scratchCard(t)
	s, err := Open(firmware, c, Config{InputClockHz: 200000000})
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(c.image)
	p, err := InspectScratch(s)
	if err != nil || p != want || len(c.written) != 0 || firmware.stats[3] != 0 || !bytes.Equal(before, c.image) {
		t.Fatal(p, err, firmware.stats)
	}
	if err = s.Quiesce(); err != nil {
		t.Fatal(err)
	}
}

func TestScratchActualFirmwarePIOWriteReadbackAndContainResume(t *testing.T) {
	firmware, c, s, p := scratchFixture(t)
	before := bytes.Clone(c.image)
	var log bytes.Buffer
	if err := RunScratch(s, p, &log); err != nil {
		t.Fatal(err, log.String())
	}
	if !bytes.Equal(before[:len(before)-4096], c.image[:len(c.image)-4096]) {
		t.Fatal("bytes outside scratch changed")
	}
	for i, value := range c.image[len(c.image)-4096:] {
		if value != byte(i*37+(i/512)*17+0x5a) {
			t.Fatalf("pattern byte %d", i)
		}
	}
	if len(c.written) != 8 {
		t.Fatal("write count", c.written)
	}
	for i, lba := range c.written {
		if lba != 8184+uint32(i) {
			t.Fatal("write escaped scratch", lba)
		}
	}
	r, valid := s.LastReceipt()
	if !valid || r.Active || c.active || firmware.stats[3] != 2 {
		t.Fatal("final ownership", r, firmware.stats)
	}
	for _, stage := range []string{"VERIFIED", "WRITTEN", "CONTAINED", "RESUMED", "PASS"} {
		if strings.Count(log.String(), "FES_NANO_SDWRITE "+stage+" ") != 1 {
			t.Fatal(log.String())
		}
	}
	accesses, calls := c.accesses, len(firmware.requests)
	if err := RunScratch(s, p, io.Discard); err == nil || c.accesses != accesses || len(firmware.requests) != calls {
		t.Fatal("contained diagnostic replay", err)
	}
}

func TestScratchRejectsCardContentAndPartitionChangesBeforeWrites(t *testing.T) {
	for _, name := range []string{"cid", "mbr-hash", "scratch-hash", "partition-tail", "partition-overlap", "protective-gpt", "no-fat", "partial-empty", "already-written"} {
		t.Run(name, func(t *testing.T) {
			firmware, c, s, p := scratchFixture(t)
			mbr := c.image[:512]
			switch name {
			case "cid":
				p.CID = "ab" + p.CID[2:]
			case "mbr-hash":
				p.MBRSHA256 = strings.Repeat("0", 64)
			case "scratch-hash":
				p.OriginalSHA256 = strings.Repeat("0", 64)
			case "partition-tail":
				binary.LittleEndian.PutUint32(mbr[458:], 6144)
			case "partition-overlap":
				copy(mbr[462:478], mbr[446:462])
				mbr[466] = 0xa2
			case "protective-gpt":
				mbr[450] = 0xee
			case "no-fat":
				mbr[450] = 0xa2
			case "partial-empty":
				mbr[480] = 1
			case "already-written":
				copy(c.image[len(c.image)-4096:], scratchPattern())
			}
			if name != "mbr-hash" {
				p.MBRSHA256 = fmt.Sprintf("%x", sha256.Sum256(mbr))
			}
			before := bytes.Clone(c.image)
			if err := RunScratch(s, p, io.Discard); err == nil {
				t.Fatal("unsafe scratch accepted")
			}
			if len(c.written) != 0 || firmware.stats[3] != 0 || !bytes.Equal(before, c.image) {
				t.Fatal("rejection wrote or auto-stopped", firmware.stats)
			}
		})
	}
}

func TestScratchUncertainWriteAndStopFailuresRetainOwnership(t *testing.T) {
	for _, name := range []string{"write", "stop"} {
		t.Run(name, func(t *testing.T) {
			firmware, c, s, p := scratchFixture(t)
			if name == "write" {
				c.failWrite = true
			} else {
				firmware.fault = 6
			}
			if err := RunScratch(s, p, io.Discard); !errors.Is(err, ErrRetired) {
				t.Fatal(err)
			}
			if _, valid := s.LastReceipt(); valid {
				t.Fatal("uncertain session remains valid")
			}
			if name == "write" && (len(c.written) != 1 || firmware.stats[3] != 0) {
				t.Fatal("write replay or auto-stop", c.written, firmware.stats)
			}
			if name == "stop" && (len(c.written) != 8 || firmware.stats[3] != 1) {
				t.Fatal("stop replay", firmware.stats)
			}
			accesses, calls := c.accesses, len(firmware.requests)
			if err := RunScratch(s, p, io.Discard); err == nil || c.accesses != accesses || len(firmware.requests) != calls {
				t.Fatal("uncertain diagnostic replay", err)
			}
		})
	}
}

func TestScratchPlanStrictFieldsGeometryAndReplay(t *testing.T) {
	_, _, _, p := scratchFixture(t)
	data, _ := json.Marshal(p)
	if _, err := ParseScratchPlan(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{string(data) + " {}", strings.Replace(string(data), `"format":1`, `"format":2`, 1), strings.Replace(string(data), `"sectors":8192`, `"sectors":8193`, 1), strings.TrimSuffix(string(data), "}") + `,"extra":true}`} {
		if _, err := ParseScratchPlan([]byte(bad)); err == nil {
			t.Fatal("malformed plan accepted", bad)
		}
	}
	p.OriginalSHA256 = fmt.Sprintf("%x", sha256.Sum256(scratchPattern()))
	if p.Validate() == nil {
		t.Fatal("already written pattern admitted")
	}
}
