package pack

import (
	"path/filepath"
	"testing"
)

// quartusHpsDdrLayout packs the fpga2sdram cfg_* inputs the way Quartus 17.0
// allocates bidirectional Avalon-MM f2h_sdram ports (ip/altera/hps/util/
// procedures.tcl, f2sdram::add_port and render_registers): port N takes
// command port N and the first free aligned run of 64-bit read/write FIFOs.
func quartusHpsDdrLayout(t *testing.T, widths []int) map[string]uint32 {
	t.Helper()
	var width, cmdToWrite, cmdToRead, direction [6]uint32
	var readToCmd, writeToCmd [4]uint32
	codes := map[int]uint32{32: 0, 64: 1, 128: 2, 256: 3}
	fifoCounts := map[int]int{32: 1, 64: 1, 128: 2, 256: 4}
	fifos := 0
	for index, bits := range widths {
		count, ok := fifoCounts[bits]
		if !ok || index >= len(width) {
			t.Fatalf("port %d: unsupported %d-bit port", index, bits)
		}
		mask := (1 << count) - 1
		start := -1
		for candidate := 0; candidate+count <= len(readToCmd); candidate += count {
			if fifos&(mask<<candidate) == 0 {
				start = candidate
				break
			}
		}
		if start < 0 {
			t.Fatalf("port %d: no free %d-bit FIFO run", index, bits)
		}
		fifos |= mask << start
		width[index] = codes[bits]
		cmdToWrite[index], cmdToRead[index] = uint32(start), uint32(start)
		direction[index] = 3
		for fifo := start; fifo < start+count; fifo++ {
			readToCmd[fifo], writeToCmd[fifo] = uint32(index), uint32(index)
		}
	}
	pack := func(values []uint32, bits uint) uint32 {
		var packed uint32
		for i, value := range values {
			packed |= value << (uint(i) * bits)
		}
		return packed
	}
	return map[string]uint32{
		"PortWidth":     pack(width[:], 2),
		"CportWfifoMap": pack(cmdToWrite[:], 3),
		"CportRfifoMap": pack(cmdToRead[:], 3),
		"RfifoCportMap": pack(readToCmd[:], 4),
		"WfifoCportMap": pack(writeToCmd[:], 4),
		"CportType":     pack(direction[:], 2),
		"AxiMmSelect":   0,
	}
}

func TestHpsDdrLayoutIsTheMiSTerSysmemLayout(t *testing.T) {
	root := repoRoot(t)
	app, err := LoadABI(filepath.Join(root, "packages/abi/fes_application.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// MiSTer sys/sysmem.sv: f2h_sdram0 is 128-bit, f2h_sdram1 and 2 are
	// 64-bit, all bidirectional Avalon-MM.
	for name, want := range quartusHpsDdrLayout(t, []int{128, 64, 64}) {
		got, ok := app.Constant("FesApplicationHpsDdrCfg" + name)
		if !ok || got != want {
			t.Errorf("FesApplicationHpsDdrCfg%s = 0x%x, Quartus packs 0x%x", name, got, want)
		}
	}
	if burst, ok := app.Constant("FesApplicationHpsDdrMaxBurst"); !ok || burst != 128 {
		t.Errorf("max burst = %d, want the 8-bit Avalon burstcount limit 128", burst)
	}

	platform, err := LoadPlatform(filepath.Join(root, "packages/platform/de10_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	base, _ := app.Constant("FesApplicationHpsDdrWindowBase")
	size, _ := app.Constant("FesApplicationHpsDdrWindowBytes")
	found := false
	for _, window := range platform.SoC.Windows {
		if window.Name == "FPGA_CORE_MEMORY" {
			found = uint64(window.Base) == uint64(base) && uint64(window.Size) == uint64(size)
		}
	}
	if !found {
		t.Errorf("HPS DDR window 0x%x+0x%x is not the SoC FPGA_CORE_MEMORY window", base, size)
	}
}
