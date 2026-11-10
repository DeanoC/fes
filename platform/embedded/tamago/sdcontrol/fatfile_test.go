// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/fatcheck"
)

type checkedMedia struct{ *Session }

func (m checkedMedia) State() (fatcheck.State, bool) {
	r, ok := m.LastReceipt()
	return fatcheck.State{Identity: m.Identity(), Owner: r.Owner, Generation: r.Generation, Active: r.Active, Prepared: r.Prepared}, ok
}

func fatFileFixture(t *testing.T) (*rpc, *card, *Session, fatcheck.Plan) {
	t.Helper()
	dir := t.TempDir()
	image := filepath.Join(dir, "fat.img")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	recovery, fit := []byte(`{"format":1,"scope":"model-recovery"}`), []byte("qualified model recovery FIT")
	for name, data := range map[string][]byte{"RECOVERY.JSON": recovery, "u-boot.itb": fit} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv := range [][]string{{"mformat", "-i", image, "-F", "::"}, {"mmd", "-i", image, "::FES"},
		{"mcopy", "-i", image, filepath.Join(dir, "RECOVERY.JSON"), "::FES/RECOVERY.JSON"},
		{"mcopy", "-i", image, filepath.Join(dir, "u-boot.itb"), "::u-boot.itb"}} {
		if output, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v %v %s", argv, err, output)
		}
	}
	volume, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	firmware, c := newRPC(t, "")
	c.image = make([]byte, (1<<20)+len(volume))
	copy(c.image[1<<20:], volume)
	mbr := c.image[:512]
	mbr[510], mbr[511], mbr[450] = 0x55, 0xaa, 0x0c
	binary.LittleEndian.PutUint32(mbr[454:], 2048)
	binary.LittleEndian.PutUint32(mbr[458:], uint32(len(volume)/512))
	boot := c.image[1<<20:]
	backup := int(binary.LittleEndian.Uint16(boot[50:]))
	binary.LittleEndian.PutUint32(boot[28:], 2048)
	binary.LittleEndian.PutUint32(boot[backup*512+28:], 2048)
	p := fatcheck.Plan{Format: 1, Scope: "nano-sd-fat-file-v1", CID: fmt.Sprintf("%x", expectedWriteCard(uint64(len(c.image)/512)).CID), Sectors: uint64(len(c.image) / 512),
		MBRSHA256: fmt.Sprintf("%x", sha256.Sum256(mbr)), Path: "FES/SDP-0123456789abcdef.JSN", RecoveryBytes: len(recovery), RecoverySHA256: fmt.Sprintf("%x", sha256.Sum256(recovery)), FitBytes: len(fit), FitSHA256: fmt.Sprintf("%x", sha256.Sum256(fit))}
	s, err := Open(firmware, c, Config{InputClockHz: 200000000, WriteCard: p.Identity(), NoSocketWriteProtect: true})
	if err != nil {
		t.Fatal(err)
	}
	return firmware, c, s, p
}

func TestFATFileActualFirmwarePIOCreateUpdateResumeAndIndependentReadback(t *testing.T) {
	firmware, c, s, p := fatFileFixture(t)
	before := bytes.Clone(c.image[:1<<20])
	var log bytes.Buffer
	if err := fatcheck.Run(context.Background(), checkedMedia{s}, p, &log); err != nil {
		t.Fatal(err, log.String())
	}
	if !bytes.Equal(before, c.image[:1<<20]) {
		t.Fatal("write outside partition")
	}
	r, ok := s.LastReceipt()
	if !ok || r.Active || r.Generation != 7 || firmware.stats[3] != 2 {
		t.Fatal("final containment", r, ok, firmware.stats)
	}
	for _, stage := range []string{"READY", "CREATED", "UPDATED", "CONTAINED", "RESUMED", "PASS"} {
		if strings.Count(log.String(), "FES_NANO_FATFILE "+stage+" ") != 1 {
			t.Fatal(log.String())
		}
	}
	for _, lba := range c.written {
		if lba < 2048 || lba >= uint32(len(c.image)/512) || lba == 2048 || lba == 2054 {
			t.Fatal("forbidden write", lba)
		}
	}
	image := filepath.Join(t.TempDir(), "result.img")
	if err := os.WriteFile(image, c.image, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command("mtype", "-i", image+"@@1048576", "::"+p.Path).Output()
	want, _ := fatcheck.Payload(p.Path, true)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("independent readback", err, string(got))
	}
	data, _ := json.Marshal(p)
	t.Logf("FES_MODEL_FAT_PLAN %s", data)
	t.Log(log.String())
	accesses, calls := c.accesses, len(firmware.requests)
	if err := fatcheck.Run(context.Background(), checkedMedia{s}, p, io.Discard); err == nil || c.accesses != accesses || len(firmware.requests) != calls {
		t.Fatal("contained check replay", err)
	}
}

func TestFATFileRefusalsAndWriteFailureNeverRetryOrAutoStop(t *testing.T) {
	for _, fault := range []string{"wrong-card", "wrong-mbr", "wrong-recovery", "wrong-fit", "existing", "cancelled", "write"} {
		t.Run(fault, func(t *testing.T) {
			firmware, c, s, p := fatFileFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "wrong-card":
				p.CID = "ab" + p.CID[2:]
			case "wrong-mbr":
				p.MBRSHA256 = strings.Repeat("0", 64)
			case "wrong-recovery":
				p.RecoverySHA256 = strings.Repeat("0", 64)
			case "wrong-fit":
				p.FitSHA256 = strings.Repeat("0", 64)
			case "existing":
				v, err := fat.Mount(s)
				if err != nil {
					t.Fatal(err)
				}
				if err = v.CreateFile(ctx, p.Path, []byte("existing")); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			case "write":
				c.failWrite = true
			}
			before := bytes.Clone(c.image)
			written := len(c.written)
			if err := fatcheck.Run(ctx, checkedMedia{s}, p, io.Discard); err == nil {
				t.Fatal("unsafe check passed")
			}
			if firmware.stats[3] != 0 {
				t.Fatal("automatic stop after failure")
			}
			if fault != "write" && (len(c.written) != written || !bytes.Equal(before, c.image)) {
				t.Fatal("refusal modified media")
			}
			if fault == "write" {
				_, ok := s.LastReceipt()
				if ok {
					t.Fatal("uncertain write kept valid ownership")
				}
			}
		})
	}
}

type fatFaultLog struct {
	bytes.Buffer
	phase string
	fault func()
}

func (w *fatFaultLog) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	if bytes.HasPrefix(b, []byte("FES_NANO_FATFILE "+w.phase+" ")) {
		w.fault()
	}
	return n, err
}

func TestFATFileLaterFailuresRetainUnresolvedOwnershipAndNeverReportPass(t *testing.T) {
	for _, fault := range []string{"update-write", "stop", "changed-card", "lost-resume", "final-stop"} {
		t.Run(fault, func(t *testing.T) {
			firmware, c, s, p := fatFileFixture(t)
			log := &fatFaultLog{}
			switch fault {
			case "update-write":
				log.phase, log.fault = "CREATED", func() { c.failWrite = true }
			case "stop":
				log.phase, log.fault = "UPDATED", func() { firmware.fault = 6 }
			case "changed-card":
				log.phase, log.fault = "CONTAINED", func() { c.cid++ }
			case "lost-resume":
				log.phase, log.fault = "CONTAINED", func() { firmware.drop = 1 }
			case "final-stop":
				log.phase, log.fault = "RESUMED", func() { firmware.fault = 6 }
			}
			err := fatcheck.Run(context.Background(), checkedMedia{s}, p, log)
			if err == nil || !errors.Is(err, ErrRetired) || strings.Contains(log.String(), "FES_NANO_FATFILE PASS ") {
				t.Fatal("failure not retained", err, log.String())
			}
			_, valid := s.LastReceipt()
			if valid {
				t.Fatal("failed session still authoritative")
			}
			assertRetired(t, s, firmware, c)
		})
	}
}
