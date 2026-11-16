// SPDX-License-Identifier: GPL-3.0-or-later
package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

func TestCreateFilesAndDirectoriesIndependentReadback(t *testing.T) {
	image := fixture(t)
	device, v := openVolume(t, image)
	defer device.Close()
	ctx := context.Background()
	if err := v.Mkdir(ctx, "FES/Native packages"); err != nil {
		t.Fatal(err)
	}
	if err := v.Mkdir(ctx, "FES/Native packages/子供"); err != nil {
		t.Fatal(err)
	}
	wanted := map[string][]byte{
		"FES/Native packages/empty.fcp":                   {},
		"FES/Native packages/ZX81 package.fcp":            bytes.Repeat([]byte{0x53, 0x8a, 0xc1}, 1901),
		"FES/Native packages/子供/ROM 日本語.BIN":              []byte("unicode directory and filename"),
		"FES/Native packages/1234567890123":               []byte("exactly thirteen UTF-16 units"),
		"FES/Native packages/" + strings.Repeat("a", 255): []byte("maximum long filename"),
	}
	// Enough LFN sets to cross sectors and grow the parent directory chain.
	for i := range 36 {
		wanted[fmt.Sprintf("FES/Native packages/Emulator package number %02d.fcp", i)] = bytes.Repeat([]byte{byte(i)}, i*131+1)
	}
	for name, data := range wanted {
		if err := v.CreateFile(ctx, name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	remounted, err := Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range wanted {
		got, err := remounted.ReadFile(name)
		if err != nil || !bytes.Equal(got, data) {
			listing, _ := exec.Command("mdir", "-i", image, "::"+path.Dir(name)).CombinedOutput()
			t.Logf("independent directory listing: %s", listing)
			t.Fatalf("remount %s: %v", name, err)
		}
		got, err = exec.Command("mtype", "-i", image, "::"+name).CombinedOutput()
		if err != nil || !bytes.Equal(got, data) {
			listing, _ := exec.Command("mdir", "-i", image, "::"+path.Dir(name)).CombinedOutput()
			t.Logf("independent directory listing: %s", listing)
			t.Fatalf("independent reader %s: %v %s", name, err, got)
		}
	}
	if got, err := remounted.ReadFile("FES/CATALOG.JSON"); err != nil || string(got) != `[{"title":"Pong"}]` {
		t.Fatal("creation damaged existing catalog", string(got), err)
	}
	// mtools must also be able to append through the native-created dot entries.
	command(t, "mmd", "-i", image, "::FES/Native packages/子供/mtools-dir")
	if _, err = Mount(device); err != nil {
		t.Fatal(err)
	}
}

func TestCreateSupplementaryUnicodeStoresSurrogatePair(t *testing.T) {
	image := fixture(t)
	device, v := openVolume(t, image)
	defer device.Close()
	const name = "FES/ROM 😀.BIN"
	wanted := []byte("supplementary Unicode name")
	if err := v.CreateFile(context.Background(), name, wanted); err != nil {
		t.Fatal(err)
	}
	e, err := v.lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 512)
	if err = device.ReadSectors(e.sector, b); err != nil {
		t.Fatal(err)
	}
	if e.offset < 32 {
		t.Fatal("fixture unexpectedly split the single LFN record")
	}
	lfn := b[e.offset-32 : e.offset]
	// Independent normative U+1F600 encoding, rather than the writer's codec.
	if binary.LittleEndian.Uint16(lfn[9:]) != 0xd83d || binary.LittleEndian.Uint16(lfn[14:]) != 0xde00 {
		t.Fatal("incorrect UTF-16 surrogate pair", lfn)
	}
	if string(b[e.offset:e.offset+11]) != "ROM_~1  BIN" {
		t.Fatal("unexpected independent short alias")
	}
	// mtools 4.0.48 also fails to read its own non-BMP names. Its short-name
	// reader still independently checks the directory, cluster chain and data.
	got, err := exec.Command("mtype", "-i", image, "::FES/ROM_~1.BIN").CombinedOutput()
	if err != nil || !bytes.Equal(got, wanted) {
		t.Fatal(string(got), err)
	}
	fresh, err := Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	got, err = fresh.ReadFile(name)
	if err != nil || !bytes.Equal(got, wanted) {
		t.Fatal(err)
	}
}

func TestCreateMultiSectorClustersAndDirectoryGrowth(t *testing.T) {
	image := filepath.Join(t.TempDir(), "larger-clusters.img")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(512 << 20); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	command(t, "mformat", "-i", image, "-F", "-c", "8", "::")
	device, v := openVolume(t, image)
	defer device.Close()
	if v.sectorsPerCluster != 8 {
		t.Fatal("fixture did not select multi-sector clusters")
	}
	ctx := context.Background()
	if err = v.Mkdir(ctx, "Packages"); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0x45, 0xd3, 0x09}, 3001)
	for i := range 60 {
		name := fmt.Sprintf("Packages/Emulator package %02d.fcp", i)
		if err = v.CreateFile(ctx, name, data); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := v.lookup("Packages")
	if err != nil {
		t.Fatal(err)
	}
	chain, err := v.chain(dir.cluster, maxDirectoryBytes)
	if err != nil || len(chain) < 2 {
		t.Fatal("directory did not grow across larger clusters", err)
	}
	fresh, err := Mount(device)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 30, 59} {
		name := fmt.Sprintf("Packages/Emulator package %02d.fcp", i)
		got, err := fresh.ReadFile(name)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("larger-cluster remount", err)
		}
		got, err = exec.Command("mtype", "-i", image, "::"+name).CombinedOutput()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("larger-cluster independent readback", err)
		}
	}
}

// Each test starts from a real mtools-formatted FAT image. The overlay can lose
// a reply after committing the sector, independently of filesystem state.
type createDevice struct {
	block.Device
	sectors           map[uint64][]byte
	events            []string
	failAt            int
	commitBeforeError bool
	readOnly          bool
	readHook          func()
	writeHook         func()
}

func (d *createDevice) ReadSectors(lba uint64, b []byte) error {
	if d.readHook != nil {
		d.readHook()
	}
	if err := block.CheckRange(d.SectorCount(), lba, len(b)); err != nil {
		return err
	}
	for off := 0; off < len(b); off += 512 {
		if data, ok := d.sectors[lba+uint64(off/512)]; ok {
			copy(b[off:off+512], data)
		} else if err := d.Device.ReadSectors(lba+uint64(off/512), b[off:off+512]); err != nil {
			return err
		}
	}
	return nil
}
func (d *createDevice) WriteSectors(lba uint64, b []byte) error {
	if d.readOnly {
		return block.ErrReadOnly
	}
	if err := block.CheckRange(d.SectorCount(), lba, len(b)); err != nil {
		return err
	}
	d.events = append(d.events, fmt.Sprintf("write:%d", lba))
	fail := d.failAt == len(d.events)
	if !fail || d.commitBeforeError {
		for off := 0; off < len(b); off += 512 {
			d.sectors[lba+uint64(off/512)] = bytes.Clone(b[off : off+512])
		}
	}
	if d.writeHook != nil {
		d.writeHook()
	}
	if fail {
		return errors.New("injected sector-write reply lost")
	}
	return nil
}
func (d *createDevice) Flush() error {
	d.events = append(d.events, "flush")
	if d.failAt == len(d.events) {
		return errors.New("injected flush reply lost")
	}
	return nil
}
func overlayVolume(t *testing.T, base block.Device) (*createDevice, *Volume) {
	t.Helper()
	d := &createDevice{Device: base, sectors: map[uint64][]byte{}}
	v, err := Mount(d)
	if err != nil {
		t.Fatal(err)
	}
	return d, v
}

func TestCreateFailureAtEveryWriteAndFlushRetiresWithoutReplay(t *testing.T) {
	base, err := block.OpenFile(fixture(t), false)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	data := bytes.Repeat([]byte{0x86, 0x12}, 1401)
	name := "FES/" + strings.Repeat("n", 255) // also force directory-chain growth
	happy, v := overlayVolume(t, base)
	if err = v.CreateFile(context.Background(), name, data); err != nil {
		t.Fatal(err)
	}
	for operation := 1; operation <= len(happy.events); operation++ {
		for _, lost := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", operation, lost), func(t *testing.T) {
				d, v := overlayVolume(t, base)
				d.failAt = operation
				d.commitBeforeError = lost
				held, err := v.Open("FES/CATALOG.JSON")
				if err != nil {
					t.Fatal(err)
				}
				defer held.Close()
				if err = v.CreateFile(context.Background(), name, data); !errors.Is(err, ErrMutation) {
					t.Fatal("uncertain mutation did not retire", err)
				}
				count := len(d.events)
				if err = v.CreateFile(context.Background(), "FES/retry.fcp", data); !errors.Is(err, ErrMutation) {
					t.Fatal(err)
				}
				if err = v.Mkdir(context.Background(), "FES/retry"); !errors.Is(err, ErrMutation) {
					t.Fatal(err)
				}
				if err = v.WriteFile("FES/PREFS.JSON", []byte(`{}`)); !errors.Is(err, ErrMutation) {
					t.Fatal(err)
				}
				if _, err = v.Open("FES/CATALOG.JSON"); !errors.Is(err, ErrMutation) {
					t.Fatal(err)
				}
				if _, err = held.Read(make([]byte, 1)); !errors.Is(err, ErrMutation) {
					t.Fatal(err)
				}
				if len(d.events) != count {
					t.Fatal("retired mount performed another mutation")
				}
				// Independent inspection can see an already committed final entry.
				// Its original data must never be overwritten by retry or rollback.
				d.failAt = 0
				fresh, err := Mount(d)
				if err != nil {
					t.Fatal(err)
				}
				got, err := fresh.ReadFile("FES/CATALOG.JSON")
				if err != nil || string(got) != `[{"title":"Pong"}]` {
					t.Fatal("failure damaged old file", err)
				}
				if got, err = fresh.ReadFile(name); err == nil && !bytes.Equal(got, data) {
					t.Fatal("published name before complete data")
				}
			})
		}
	}
}

func TestCreateRejectsWithoutMutation(t *testing.T) {
	base, err := block.OpenFile(fixture(t), false)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	for _, name := range []string{".", "../bad", "FES/../bad", "FES/trailing.", "FES/trailing ", "FES/bad:name", "FES/" + strings.Repeat("x", 256), "FES/\uffff", "FES/\xff", "FES/caTALog.json", "missing/new.fcp", "FES/CATALOG.JSON/child"} {
		t.Run(name, func(t *testing.T) {
			d, v := overlayVolume(t, base)
			if err := v.CreateFile(context.Background(), name, nil); err == nil {
				t.Fatal("invalid or existing name admitted")
			}
			if len(d.events) != 0 {
				t.Fatal("invalid request mutated disk")
			}
		})
	}
	d, v := overlayVolume(t, base)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = v.CreateFile(ctx, "FES/new.fcp", nil); !errors.Is(err, context.Canceled) || len(d.events) != 0 {
		t.Fatal(err)
	}
	d.readOnly = true
	if err = v.CreateFile(context.Background(), "FES/new.fcp", []byte("new")); !errors.Is(err, block.ErrReadOnly) || errors.Is(err, ErrMutation) {
		t.Fatal(err)
	}
	if _, err = v.Open("FES/CATALOG.JSON"); err != nil {
		t.Fatal("safe read-only refusal retired mount", err)
	}
}

func TestCreateCancellationBeforeAndAfterMutation(t *testing.T) {
	base, err := block.OpenFile(fixture(t), false)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	for _, after := range []bool{false, true} {
		d, v := overlayVolume(t, base)
		ctx, cancel := context.WithCancel(context.Background())
		if after {
			d.writeHook = cancel
		} else {
			d.readHook = cancel
		}
		err = v.CreateFile(ctx, "FES/new.fcp", []byte("new"))
		cancel()
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrMutation) != after {
			t.Fatal(after, err)
		}
		if after && len(d.events) != 1 || !after && len(d.events) != 0 {
			t.Fatal("cancellation performed later IO", d.events)
		}
	}
}

func TestCreateRespectsMirroringAndActiveFAT(t *testing.T) {
	base, err := block.OpenFile(fixture(t), false)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	for _, activeOnly := range []bool{false, true} {
		d, _ := overlayVolume(t, base)
		boot := make([]byte, 512)
		if err = d.ReadSectors(0, boot); err != nil {
			t.Fatal(err)
		}
		fatBase := uint64(binary.LittleEndian.Uint16(boot[14:]))
		fatSize := uint64(binary.LittleEndian.Uint32(boot[36:]))
		if activeOnly {
			binary.LittleEndian.PutUint16(boot[40:], 0x81)
			d.sectors[0] = boot
		}
		v, err := Mount(d)
		if err != nil {
			t.Fatal(err)
		}
		// Deliberately fragmented free space, with reserved high nibbles set.
		for copyIndex := uint64(0); copyIndex < 2; copyIndex++ {
			b := make([]byte, 512)
			lba := fatBase + copyIndex*fatSize
			if err = d.ReadSectors(lba, b); err != nil {
				t.Fatal(err)
			}
			for c := 4; c < 100; c++ {
				off := c * 4
				value := binary.LittleEndian.Uint32(b[off:])
				if value&0x0fffffff == 0 {
					value = 0xa0000000
					if c%2 == 0 {
						value |= 0x0fffffff
					}
					binary.LittleEndian.PutUint32(b[off:], value)
				}
			}
			d.sectors[lba] = b
		}
		before := bytes.Clone(d.sectors[fatBase])
		data := bytes.Repeat([]byte("fragmented"), 801)
		if err = v.CreateFile(context.Background(), "FES/fragmented.fcp", data); err != nil {
			t.Fatal(err)
		}
		if activeOnly {
			if !bytes.Equal(before, d.sectors[fatBase]) {
				t.Fatal("inactive FAT modified")
			}
		} else if !bytes.Equal(d.sectors[fatBase], d.sectors[fatBase+fatSize]) {
			t.Fatal("FAT copies diverged")
		}
		e, err := v.lookup("FES/fragmented.fcp")
		if err != nil {
			t.Fatal(err)
		}
		chain, err := v.chain(e.cluster, maxFileBytes)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range chain {
			if c < 100 && c%2 == 0 {
				t.Fatal("allocated occupied cluster", c)
			}
		}
		b := make([]byte, 512)
		if err = d.ReadSectors(v.fatStart, b); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(b[chain[0]*4:])>>28 != 0xa {
			t.Fatal("reserved high bits lost")
		}
		if binary.LittleEndian.Uint32(b[4:]) != binary.LittleEndian.Uint32(before[4:]) {
			t.Fatal("successful mutation changed original clean/error flags")
		}
		if err = d.ReadSectors(v.fsInfo, b); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(b[488:]) != 0xffffffff || binary.LittleEndian.Uint32(b[492:]) != 0xffffffff {
			t.Fatal("stale FSInfo hints published")
		}
		got, err := v.ReadFile("FES/fragmented.fcp")
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal(err)
		}
	}
}

func TestCreateDetectsFATMirrorAndFSInfoDamageBeforeMutation(t *testing.T) {
	base, err := block.OpenFile(fixture(t), false)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	for _, fault := range []string{"mirror", "fsinfo", "flags", "full"} {
		t.Run(fault, func(t *testing.T) {
			d, v := overlayVolume(t, base)
			switch fault {
			case "mirror":
				free, err := v.freeClusters(context.Background(), 1)
				if err != nil {
					t.Fatal(err)
				}
				off := uint64(free[0]) * 4
				b := make([]byte, 512)
				lba := v.fatCopies[1] + off/512
				if err = d.ReadSectors(lba, b); err != nil {
					t.Fatal(err)
				}
				binary.LittleEndian.PutUint32(b[off%512:], 0x0fffffff)
				d.sectors[lba] = b
			case "fsinfo":
				d.sectors[v.fsInfo] = make([]byte, 512)
			case "flags":
				boot := make([]byte, 512)
				if err = d.ReadSectors(0, boot); err != nil {
					t.Fatal(err)
				}
				binary.LittleEndian.PutUint16(boot[40:], 0x40) // reserved BPB flag
				d.sectors[0] = boot
				v, err = Mount(d)
				if err != nil {
					t.Fatal("read-only mount should remain available", err)
				}
			case "full":
				for sector := uint64(0); sector*128 < v.clusterCount+2; sector++ {
					b := make([]byte, 512)
					if err = d.ReadSectors(v.fatStart+sector, b); err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 128; i++ {
						off := i * 4
						if binary.LittleEndian.Uint32(b[off:])&0x0fffffff == 0 {
							binary.LittleEndian.PutUint32(b[off:], 0x0fffffff)
						}
					}
					for _, start := range v.fatCopies {
						d.sectors[start+sector] = bytes.Clone(b)
					}
				}
			}
			err := v.CreateFile(context.Background(), "FES/new.fcp", []byte("new"))
			if fault == "full" {
				if !errors.Is(err, ErrNoSpace) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrCorrupt) {
				t.Fatal(err)
			}
			if len(d.events) != 0 {
				t.Fatal("rejected allocation mutated storage")
			}
		})
	}
}

func TestCreateShortAliasesCannotShadowLongNames(t *testing.T) {
	base, err := block.OpenFile(fixture(t), false)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	d, v := overlayVolume(t, base)
	if err = v.CreateFile(context.Background(), "FES/AB~1.FCP", nil); err != nil {
		t.Fatal(err)
	}
	if err = v.CreateFile(context.Background(), "FES/Ab long filename.fcp", nil); err != nil {
		t.Fatal(err)
	}
	e, err := v.lookup("FES/Ab long filename.fcp")
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 512)
	if err = d.ReadSectors(e.sector, b); err != nil {
		t.Fatal(err)
	}
	alias := shortName(b[e.offset : e.offset+32])
	count := len(d.events)
	if err = v.CreateFile(context.Background(), "FES/"+alias, []byte("shadow")); !errors.Is(err, fs.ErrExist) || len(d.events) != count {
		t.Fatal("short alias shadowed", alias, err)
	}
}
