# FES pc386 machine

The AT-class 386 PC of the [pc386 machine design](../../docs/pc386-machine-design.md),
built around [`fes_i386_cpu`](../fes-common/rtl/i386/). This directory holds
phases M1 to M5: the AT core, the firmware build, the register-level VGA with
its capture writer, the 720p presenter, the display plane on DDR port 2, and a
simulation of the machine with the Bochs oracle standing in for the CPU. The
speaker, OPL2, Sound Blaster 2.0 DSP and mixer are checked beside the AT core.
Phase M5 (CPU Phase 3) is the [system](#m5-system): the machine with
`fes_i386_cpu` on DDR port 0 in a DE10-Nano package, `fes.pc386`, sealed with
Quartus 17.0.2.

## Layout

| Path | Content |
| --- | --- |
| [`rtl/pc386_at_core.sv`](rtl/pc386_at_core.sv) | The AT core: every on-board device behind the CPU's I/O, INTA, A20M, reset, FLUSH and DMA ports |
| [`rtl/pc386_io.sv`](rtl/pc386_io.sv) | I/O decoder: AT bus sizing and the `isa8`, `isa16` and `vga` timing classes |
| [`rtl/pc386_pic.sv`](rtl/pc386_pic.sv) | Two [KF8259](../fes-common/vendor/kf8259/README.md) in cascade, the slave on master IR2 |
| [`rtl/pc386_sysctl.sv`](rtl/pc386_sysctl.sv) | Ports 61h, 92h and 80h |
| [`rtl/pc386_kbc.sv`](rtl/pc386_kbc.sv) | 8042, PS/2 keyboard and PS/2 aux mouse |
| [`rtl/pc386_rtc.sv`](rtl/pc386_rtc.sv) | MC146818A clock with 128 bytes of CMOS and the configuration bytes |
| [`rtl/pc386_ata.sv`](rtl/pc386_ata.sv) | ATA disk with a two-sector buffer on a 64-bit image port and the dirty map |
| [`rtl/pc386_rate.sv`](rtl/pc386_rate.sv) | Rational clock enable for every guest time base |
| [`rtl/pc386_vga.sv`](rtl/pc386_vga.sv) | VGA registers, the port and MMIO FIFO, planar read and write logic, the write coalescer, the port 1 arbiter |
| [`rtl/pc386_vga_scanout.sv`](rtl/pc386_vga_scanout.sv) | CRTC raster at the guest dot clock, line prefetch, attribute controller, DAC, capture writer |
| [`rtl/pc386_presenter.sv`](rtl/pc386_presenter.sv) | Area-weighted scaling of the newest capture to 960 x 720 in the 720p raster |
| [`rtl/pc386_plane_bridge.sv`](rtl/pc386_plane_bridge.sv) | The display plane's reads on port 2: clock crossing, 128-to-64-bit beats, the plane's port guard |
| [`rtl/pc386_port2.sv`](rtl/pc386_port2.sv) | DDR port 2 arbiter: plane, presenter, capture, then the shell's single beats |
| [`rtl/pc386_machine.sv`](rtl/pc386_machine.sv) | The machine without the CPU: AT core, VGA, presenter, port 2, display plane |
| [`rtl/pc386_ram.sv`](rtl/pc386_ram.sv) | Inferred RAMs and FIFOs: one clock, two clocks, show-ahead |
| [`rtl/pc386_audio.sv`](rtl/pc386_audio.sv) | Audio beside the AT core: the OPL2 and DSP on its sound port, IRQ routing, the mixer |
| [`rtl/pc386_opl2.sv`](rtl/pc386_opl2.sv) | OPL2: the [opl2_fpga import](vendor/opl2_fpga/README.md) behind the AdLib port, with status, timers and the 49,715.9 Hz enable |
| [`rtl/pc386_sb.sv`](rtl/pc386_sb.sv) | Sound Blaster 2.0 DSP, version 2.01: commands, DMA on channel 1, IRQ |
| [`rtl/pc386_mixer.sv`](rtl/pc386_mixer.sv) | Box-filter resampler of speaker, OPL2 and DSP to 48 kHz, gains, DC blocker |
| [`rtl/top.sv`](rtl/top.sv) | The board shell: PLLs, the `fes.computer` mailbox, HDMI I2C, `fes_hps_ddr`, 720p HDMI and I2S |
| [`rtl/pc386_system.sv`](rtl/pc386_system.sv) | The machine, its audio and `fes_i386_cpu`: the CPU's resets, I/O and INTA handshake, attribute map, what the status block reports |
| [`rtl/pc386_rom.sv`](rtl/pc386_rom.sv) | The BIOS and VGA BIOS in M10K, answering the CPU's port 0 reads of their ranges |
| [`rtl/pc386_host_link.sv`](rtl/pc386_host_link.sv) | The HPS's link through the DDR window until M0: configuration, status, debug ring, the ATA image on port 2 |
| [`rtl/pc386_system_pll.v`](rtl/pc386_system_pll.v) | The sealed system clock and the 12.288 MHz audio clock |
| [`constraints-oss.qsf`](constraints-oss.qsf), [`toolchain.lock`](toolchain.lock) | Pins (both lanes), and the core's lock (the i386 lock's pins) |
| [`clocks.sdc`](clocks.sdc) | The Quartus lane's clocks |
| [`vendor/opl2_fpga/`](vendor/opl2_fpga/README.md) | The YM3812 import (LGPL-3.0), with the local changes that make it sample-exact |
| [`vendor/nuked-opl3/`](vendor/nuked-opl3/README.md) | Nuked-OPL3 (LGPL-2.1), the OPL2 reference in simulation only |
| [`firmware/`](firmware/) | Bochs ROM BIOS and LGPL VGABios sources, `source.json` manifests, `firmware.lock` |
| [`sim/machine_tb.cpp`](sim/machine_tb.cpp) | `pc386_machine` with the Bochs oracle as CPU, guest memory and DDR models; records VGA traces and frames. `--cpu rtl` runs `fes_i386_cpu` instead |
| [`sim/system_tb.cpp`](sim/system_tb.cpp) | `pc386_system` with all three DDR ports on one window model, and a host that uses only the window, as the kit's does |
| [`host/`](host/main.go) | `pc386-host`, the kit tool that stages the disk and configuration and follows the status block and debug ring, as `system_tb`'s host does |
| [`sim/rtl_stop_test.asm`](sim/rtl_stop_test.asm) | The stop test disk: instructions the RTL has not built, and a protected-mode stretch, for `--cpu rtl`'s stop path |
| [`sim/devices_tb.cpp`](sim/devices_tb.cpp) | Directed checks of the AT core through its I/O port |
| [`sim/audio_tb.cpp`](sim/audio_tb.cpp) | Directed checks of the DSP, the OPL2's AdLib port and the mixer, through the AT core |
| [`sim/opl2_tb.cpp`](sim/opl2_tb.cpp) | The OPL2 against Nuked-OPL3, sample for sample, on random programs and captured port logs |
| [`sim/audio_link.h`](sim/audio_link.h), [`sim/opl2_reference.h`](sim/opl2_reference.h) | The AT core and audio models joined as wires; the Nuked-OPL3 stepper |
| [`sim/sound.asm`](sim/sound.asm) | SOUND.COM, the start-up exercise the sound suite runs at the FreeDOS prompt |
| [`sim/fixtures/`](sim/fixtures/) | The OPL2 port writes the sound suite captured, replayed by the opl2 suite |
| [`sim/vga_replay_tb.cpp`](sim/vga_replay_tb.cpp), [`sim/vga_replay_top.sv`](sim/vga_replay_top.sv) | Replays a VGA trace into the RTL VGA and Bochs's VGA and compares reads, VRAM and frames |
| [`sim/vga_reference.cpp`](sim/vga_reference.cpp), [`sim/vga_reference.h`](sim/vga_reference.h) | The reference: Bochs's `vgacore.cc` and GUI renderer behind a small interface |
| [`sim/vga_divergences.toml`](sim/vga_divergences.toml) | Every Bochs-versus-RTL VGA divergence with its deciding case |
| [`sim/vga_test.asm`](sim/vga_test.asm) | The VGA test disk: VGA BIOS text, mode 13h, mode Y, mode 12h |
| [`sim/presenter_tb.cpp`](sim/presenter_tb.cpp), [`sim/pc386_presenter_model.h`](sim/pc386_presenter_model.h) | The presenter against its bit-exact C++ model |
| [`sim/capture_tb.cpp`](sim/capture_tb.cpp), [`sim/vga_capture_top.sv`](sim/vga_capture_top.sv) | Forces capture FIFO overflow and checks that the damaged frame is discarded and a later frame completes |
| [`sim/plane_tb.cpp`](sim/plane_tb.cpp), [`sim/plane_top.sv`](sim/plane_top.sv) | The plane through the bridge and the port 2 arbiter beside the presenter and capture traffic |
| [`sim/pc386_ddr_model.h`](sim/pc386_ddr_model.h) | The DDR window and 64-bit port model with the measured latency |
| [`sim/freedos.lock`](sim/freedos.lock) | The FreeDOS 1.4 package commits and file hashes of the simulation disk |

The 8254 is the shared KF8253 with `READ_BACK = 1`; the DMA controllers are the
shared [`fes_dma8237.sv`](../fes-common/rtl/fes_dma8237.sv) with AT parameters.

## Running

```sh
python3 scripts/build_pc386_firmware.py --download     # dev86, then both ROMs, checked against the lock
python3 scripts/build_pc386_freedos_image.py --download  # the FreeDOS fixture in build/
python3 scripts/sim_fes_pc386.py --suite devices        # directed device checks, about 10 s
python3 scripts/sim_fes_pc386.py --suite audio          # DSP, OPL2 timers, mixer, about 1 min
python3 scripts/sim_fes_pc386.py --suite opl2           # OPL2 against Nuked-OPL3, about 30 s
python3 scripts/sim_fes_pc386.py --suite benches        # presenter, capture overflow recovery and display plane on port 2
python3 scripts/sim_fes_pc386.py --suite boot --download  # BIOS POST to the FreeDOS prompt and its VGA replay
python3 scripts/sim_fes_pc386.py --suite boot --cpu rtl   # the RTL must reach the prompt without stops, stalls or oracle steps
python3 scripts/sim_fes_pc386.py --suite rtl-stops        # the RTL's stop path on the stop test disk, against the oracle
python3 scripts/sim_fes_pc386.py --suite sound --download  # boot, then SOUND.COM; writes sound.wav
python3 scripts/sim_fes_pc386.py --suite vga              # presenter, capture and plane benches, VGA test disk, replay
python3 scripts/synth_fes_pc386.py --cache-root /abs/out/cache/misteross-toolchains  # machine-only synthesis
python3 scripts/sim_fes_pc386.py --suite system         # M5: FreeDOS to the prompt on pc386_system, about an hour
make toolchain-fes-pc386 CACHE_ROOT=/abs/out/cache/misteross-toolchains  # once per lock
make build-fes-pc386-quartus CACHE_ROOT=/abs/out/cache/misteross-toolchains  # seal fes.pc386 with Quartus 17.0.2 (SEED=)
```

The boot suite runs the locked BIOS and VGA BIOS on `pc386_machine`, boots the
FreeDOS disk, types `dir` at the prompt and requires its listing in VRAM, then
checks that every CPU I/O completed at its class time and replays the boot's
VGA trace against Bochs's VGA. The vga suite runs the presenter and port 2
benches, boots the VGA test disk, compares each marked picture's 720p frame
(presenter and closed plane) with the presenter model, and replays that trace.
Each writes `build/sim/fes-pc386/<suite>/report.json` (POST codes, debug-port
text, I/O histogram, final screen, frames, replay counts) and logs; the vga
suite writes the marked captures and 720p frames as PPM files in `frames/`.
The boot takes about 7 minutes of wall time and its replay one more; the vga
suite about 6 minutes in all. `PC386_TRACE_IO=1` makes `machine_tb` print every I/O but port
80h.

`make sim-fes-pc386` runs the directed device, audio and OPL2 checks plus the
presenter, capture and display-plane benches. The sound suite boots the same
FreeDOS fixture, runs SOUND.COM, checks the OPL2 samples against Nuked-OPL3,
and checks the speaker and DSP tones in the 48 kHz output. It writes
`build/sim/fes-pc386/sound/sound.wav`, section measurements and the captured
OPL2 port log. The game suite starts Wolfenstein 3D or Ultima VI from
`$FES_PC386_GAMES` and reports its audio activity; the supplied game files and
captures stay outside the repository.

## Simulation model

- **CPU.** The Bochs level-3 oracle of the [i386 Phase 0 adapter](../fes-common/sim/i386/README.md).
  Each retired instruction advances the RTL four system clocks
  (`--cycles-per-instruction`; about a 25 MIPS CPU at 100 MHz). Nothing in the
  boot calibrates against it; it sets how long the boot takes. IN, OUT and INTA
  run through the RTL inside the instruction and take its device time on top.
  A halted CPU lets the RTL run until INTR.
- **RTL CPU (`--cpu rtl`).** [`fes_i386_cpu`](../fes-common/rtl/i386/) on the
  system clock with its L1I, L1D and bus interface: guest memory through HPS
  DDR port 0 (`Port0` below), the machine's MMIO, DMA and FLUSH ports wired to
  its own, its I/O and INTA ports on the AT core, INTR from the PICs, and
  A20M. Its attribute map is the machine's: A0000h-BFFFFh MMIO, the VGA and
  system BIOS ranges read-only, upper RAM on, 16 MiB of RAM. The CPU posts VGA
  stores in order as they retire and reads VGA memory only for the oldest uop,
  so never on a wrong path; an I/O access waits until its write buffer is
  empty. An encoding the CPU has not built stops it: the bench holds the CPU
  in reset until its write buffer has drained, runs that instruction on the
  oracle from the RTL's state, keeps the oracle running through protected
  mode and V86 (serving DMA and FLUSH itself meanwhile), and restarts the RTL
  from the oracle's state. The boot suite's `boot-rtl/report.json` lists the
  stops by stem, with the lockstep's scope. For `--suite boot --cpu rtl`, any
  stop, stall or oracle step fails the run; the RTL itself must reach the
  prompt. While the CPU waits for an I/O cycle or sits in HLT with nothing in
  flight, only the machine is clocked.
- **Memory.** 16 MiB of host pages: the ROMs at F0000h (aliased at FFFF0000h)
  and C0000h are read-only, C8000h-EFFFFh is RAM. With `--cpu rtl` these
  pages are what port 0 serves at window offset 4000000h plus the physical
  address, the ROM images in the two ROM ranges. A0000h-BFFFFh is MMIO into
  the RTL VGA: each access becomes dword requests with byte enables in address
  order, writes posted, reads blocking. Above 16 MiB is a hole.
- **DDR.** Ports 1 and 2 are `sim/pc386_ddr_model.h` ports on one 256 MiB
  window: reads answer 17-18 clocks after the command, 32-51 for 1.5% and
  54-79 for 0.1% (Kit B, [#1076](https://github.com/DeanoC/fes/pull/1076)),
  commands are refused at random and while 14 reads wait, all from a seeded
  generator so a replay sees the same timing. The screen text is read from
  VRAM in the window. With `--cpu rtl`, port 0 is the 128-bit `Port0`, with
  Kit A's port 0 timing: 18-19 clocks for most reads, 47-51 for the slowest
  1%, and a tail to 135.
- **Pixel clock.** 74.25 MHz, interleaved with the system clock in time. It
  runs only while a marked picture waits for its 720p frames (or from
  `--pixel-from`), so the presenter and plane cost nothing otherwise.
- **Disk.** The image file behind the ATA device's 64-bit image port, answered
  in the next clock. The geometry is inferred from the first partition entry's
  end CHS, as FogCast will.
- **Clock.** 100 MHz; every guest rate is a rational enable of it.

## Audio

[`pc386_audio`](rtl/pc386_audio.sv) sits beside the AT core, as the design's
block diagram has it: the AT core decodes the Sound Blaster's base range and
388h-389h onto a sound port (isa8 cycles), takes DREQ1 into its 8237 and the
DSP's IRQ into its PICs, and hands out the speaker level. The benches Verilate
the two as separate models and join them in [`audio_link.h`](sim/audio_link.h)
as wires would; the machine top level will instantiate both.

- **OPL2.** Sample-exact against Nuked-OPL3's default build in OPL2 mode; the
  [import README](vendor/opl2_fpga/README.md) lists the local changes. Register
  writes reach the import only between samples (it computes for 72 clocks of
  each 2,011); one that arrives meanwhile waits in a four-entry queue. Timer 1
  counts every 4 samples (80.5 us) and timer 2 every 16 (321.8 us). Status
  reads {IRQ, T1, T2, 00110b}; the data port reads FFh. Register 01h bit 5 and
  CSM mode are not modelled, as in Nuked-OPL3.
- **DSP.** The commands and behaviour of the
  [design's table](../../docs/pc386-machine-design.md#sound-blaster-20-dsp),
  plus 80h, E4h and E8h. Reset completes 20 us after release; F2h's IRQ comes
  10 us after the command; the status ports read AAh and 2Ah; odd ports alias
  the even one below. The DSP's settings after reset: time constant D3h
  (22.2 kHz), block size 2048, speaker off.
- **Mixer.** OPL2 and DSP at half scale, speaker at 16384 when on, integrated
  per 48 kHz period at system-clock resolution and divided by the period's
  length, then a DC blocker near 30 Hz and saturation. The benches make the
  48 kHz request from a rational enable; the shell will take it from the I2S
  output's frame request.
- **Reset.** The import's registers and register memories start at their
  declared initial values; only the machine reset (a launch)
  restarts the audio, as the AdLib's IC pin did. A CPU reset does not.

## Recorded behaviour and deviations

- **CMOS.** Besides the bytes the design lists, the machine writes what the
  Bochs emulator writes and this BIOS reads: 14h = 04h (VGA, PS/2 port, no
  x87 or floppy), the fixed-disk geometry at 1Bh-23h (the BIOS copies it into
  the INT 41h table) and 3Fh bit 0 (no F12 boot menu, which would wait three
  seconds for a key).
- **ATA writes complete into the buffer.** The Bochs BIOS reads status right
  after its `outsw` and requires DRDY with BSY clear, so a sector is reported
  done once it is whole in the buffer; the store to the image follows, and the
  next command waits for it. A torn sector still never reaches the image.
- **READ MULTIPLE prefetch.** The next sector is fetched while the guest reads
  the current one. The image port must deliver a sector (64 beats) faster than
  the guest reads one (92 us at `isa16`); a slower port would make a block read
  return stale data. The simulation's port answers in a clock.
- **Absent slave.** With DEV set, status reads 00h and the other registers are
  shared, as in Bochs's drive model. The BIOS then lists "ata0 slave: Unknown
  device" and logs one "IDE time out" while waiting for BSY after SRST.
- **KF8259 INT.** The import holds INT once raised until it is acknowledged, so
  a request that is masked before INTA is taken as a spurious IR7.
- **Keyboard input** is set 2 scan codes on `key_valid`/`key_code`. The HID
  row model and typematic repeat of the design are not built yet.

## VGA

- **Ports and FIFO.** VGA port writes and MMIO writes share a 16-entry FIFO
  and are processed in order; a port write completes in four clocks (a clock
  later than the M1 stand-in, as the byte is posted through a register), and
  a byte waits while the FIFO is full. The machine bench reports those waits
  apart from the class time (`IOWAIT`). A port read waits until the FIFO has
  drained. An MMIO read also drains the
  write coalescer first.
- **Planar logic.** One VRAM word write per clock: the bytes of a dword that
  share a planar offset (a chain-4 dword, an odd/even pair) go together, each
  plane taking its own byte. Chain-4 is plane A[1:0] at A >> 2; odd/even is
  planes A[0] ? 1,3 : 0,2 at A with bit 0 cleared, as Bochs and the CRTC's
  word mode (C << 1) have it. Reads use the sequencer's chain-4 and odd/even
  bits for both directions, as Bochs does.
- **Coalescer.** Four 32-byte lines in allocation order; a line drains when a
  write needs its place, before any MMIO read, or after 64 idle clocks.
- **Scanout.** Line N + 1 is fetched during line N, one single-beat read per
  character column (two per column in text mode, the font byte read as each
  character arrives), at most 12 outstanding. The start address, preset row
  scan and byte panning latch at the start of vertical retrace; pixel panning
  at each line start. Doubleword and byte mode address VRAM at C, word mode at
  C << 1 with bit 0 from C[13] or C[15]. The system clock must be at least
  twice the dot clock.
- **Capture.** Up to 800 x 600 pixels a frame, every scan line (mode 13h is
  captured as 320 x 400), 256-colour modes at half the dot rate, into three
  2 MiB buffers on port 2. A beat-FIFO or line-descriptor overflow invalidates
  the in-progress frame; the writer drains before a capture buffer is reused.
  The overscan border is not captured.
- **Presenter.** The newest complete capture at each 720p frame, scaled to
  960 x 720 by the area-weighted filter in the RTL's header; DSP use is nine
  9 x 9 multipliers for the blends and two 27 x 27 for the weights.
- **Divergences from Bochs's VGA** are listed in
  [`sim/vga_divergences.toml`](sim/vga_divergences.toml), including current
  M2 limitations against IBM VGA behavior.
- **Not built:** the vertical retrace interrupt (CRTC 11h bits 4-5), graphics
  blink (attribute bit 3 in graphics modes), cursor skew, display enable skew,
  9-dot 256-colour modes.

## M5 system

`fes.pc386` is a format-2 `fes.computer` 1.0 development package: fixed 720p60
video, 48 kHz audio, HPS DDR and the session-display plane, with
[`pc386_system`](rtl/pc386_system.sv) below the board shell.

- **CPU.** `fes_i386_cpu` with its L1I, L1D and bus interface fills and
  writes back through DDR port 0, guest physical 0 at window offset 64 MiB,
  16 MiB of RAM. Its attribute map is the design's: A0000h-BFFFFh MMIO to the
  VGA, the two ROM ranges read-only, upper RAM on. The machine's I/O, INTA,
  DMA, FLUSH, A20M and INTR ports are wired to the CPU's; I/O and INTA cross a
  small handshake (the machine takes one cycle at a time, and the CPU holds a
  request until its acknowledge). Its reset state is Bochs's. An 8042 or port
  92h reset asserts FLUSH, then holds the CPU at least 4 clocks and until its
  memory is idle; RAM and every device keep their state. There is no NMI and
  no shutdown cycle: a CPU stop (an encoding not built, or a fault inside a
  delivery) leaves it stopped, and the status block shows where.
- **Firmware.** [`pc386_rom`](rtl/pc386_rom.sv) holds the locked BIOS and VGA
  BIOS in 96 KiB of M10K, which
  [`build_pc386_firmware.py`](../../scripts/build_pc386_firmware.py) writes as
  `build/firmware/pc386/pc386-rom.hex`. Until the CPU has its L2 and memory
  router, the L1s fill straight from port 0, so the ROM sits in front of the
  port and answers each read of C0000h-C7FFFh or F0000h-FFFFFh itself, in
  command order. The reset alias at FFFF0000h is the CPU's own mapping onto
  F0000h.
- **Ports.** Port 0 is the CPU's alone. Port 1 is VRAM. Port 2 carries the
  plane, presenter and capture, and at the lowest priority the ATA image and
  the host link's blocks, as single beats.
- **Host link.** Until the `fes.computer` 1.1 contracts (M0) land, the HPS
  stages the disk and the machine's settings in the DDR window
  ([`pc386_host_link`](rtl/pc386_host_link.sv) has the layout). Out of reset
  the link announces itself with a new epoch; the HPS writes the ATA image at
  128 MiB and a configuration block: base and extended memory, the Sound
  Blaster's port, IRQ and DMA, the disk geometry, the self-modifying-code
  profile and the wall clock. The machine stays in reset until that block
  arrives, and the CPU until the RTC has its CMOS setup and wall clock. The
  link then writes a status block about every millisecond (the CPU's CS:EIP,
  instructions retired, halt and stop, POST code, CPU resets, frame and
  underflow counters, audio samples) and the debug-port bytes to a 4 KiB ring.
- **Clock.** The system clock is sealed at 61.44 MHz, a multiple of the
  12.288 MHz audio clock and above twice the 28.322 MHz dot clock that the
  scanout needs.
  [`build_fes_pc386_quartus.py`](../../scripts/build_fes_pc386_quartus.py)
  `guest_rates()` turns it into every guest time base's N/D; the system
  simulation takes them from there.

The system suite runs `pc386_system` alone with
[`sim/system_tb.cpp`](sim/system_tb.cpp): the three DDR ports on one window
model, the pixel clock throughout, and a host that does only what the kit's
host can, through the window. It stages the FreeDOS fixture and the
configuration, follows the POST codes and debug text, and reads the screen
from VRAM. The CPU must reach the prompt alone, with no oracle behind it,
and every underflow counter must stay at zero. On a kit, with the package
running, [`pc386-host`](host/main.go) `boot DISK` does the same through
`/dev/mem` and prints the same lines.

**Quartus lane.** [`scripts/build_fes_pc386_quartus.py`](../../scripts/build_fes_pc386_quartus.py)
seals the package with Quartus 17.0.2, labelled as a Quartus build
([Quartus-built cores](../../../../docs/design/quartus-cores.md)). nextpnr
builds of pc386 are parked with [#1235](https://github.com/DeanoC/fes/issues/1235).
- Quartus 17.0.2 rejects some of the machine's and the CPU's SystemVerilog,
  so the core's locked Yosys elaborates `pc386_system` at the guest rates,
  with the ROM's contents, and writes it as Verilog. Memories with more than
  one write port and no `ramstyle` become registers, and flattened names
  become plain identifiers. Quartus reads that netlist and the shell's own
  files, with `QUARTUS` defined: the HDMI I2C pins come from the hard block,
  and `pc386_system` takes no parameters.
- Every analysis must pass on every clock, or the build publishes nothing.
  The build summary records the fit's ALMs, registers, RAM and DSP blocks.

**M4.** The diagnostic package `fes.pc386-diag`, the machine with a bus master
running commands from a DDR ring in the CPU's place, passed every program on
Kit B ([M4 record](../../../../docs/validation/2026-10-28-pc386-m4-diagnostic/summary.md),
[Quartus record](../../../../docs/validation/2026-10-29-pc386-m4-quartus/summary.md)).
M5 replaced it: the CPU now drives the same machine, and the bus master, its
ring, the port fence its machine reset needed, the diagnostic tool and the
nextpnr producer are gone. Its last open exit, the disk round trip with the
runtime, waits on the M0 contracts either way.

## Not built yet

The HID nine-row keyboard model and typematic repeat, so the package has no
keyboard or mouse yet; the `fes.mouse.relative` and `fes.computer` 1.1
mailbox wiring (disk prepare, commit with CRC, freeze, DiskDirty read-out,
persistence, wall clock), which replaces the host link; the CPU's L2.
