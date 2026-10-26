# FES pc386 machine

The AT-class 386 PC of the [pc386 machine design](../../docs/pc386-machine-design.md),
built around [`fes_i386_cpu`](../fes-common/rtl/i386/). This directory holds
phase M1, the AT core, the firmware build and a simulation of the machine with
the Bochs oracle standing in for the CPU, and phase M3, the audio: PC speaker,
OPL2, Sound Blaster 2.0 DSP and mixer. There is no VGA beyond a text stand-in
in the testbench, no shell, no package and no kit evidence. Everything here is
a host-only check.

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
| [`rtl/pc386_audio.sv`](rtl/pc386_audio.sv) | Audio beside the AT core: the OPL2 and DSP on its sound port, IRQ routing, the mixer |
| [`rtl/pc386_opl2.sv`](rtl/pc386_opl2.sv) | OPL2: the [opl2_fpga import](vendor/opl2_fpga/README.md) behind the AdLib port, with status, timers and the 49,715.9 Hz enable |
| [`rtl/pc386_sb.sv`](rtl/pc386_sb.sv) | Sound Blaster 2.0 DSP, version 2.01: commands, DMA on channel 1, IRQ |
| [`rtl/pc386_mixer.sv`](rtl/pc386_mixer.sv) | Box-filter resampler of speaker, OPL2 and DSP to 48 kHz, gains, DC blocker |
| [`vendor/opl2_fpga/`](vendor/opl2_fpga/README.md) | The YM3812 import (LGPL-3.0), with the local changes that make it sample-exact |
| [`vendor/nuked-opl3/`](vendor/nuked-opl3/README.md) | Nuked-OPL3 (LGPL-2.1), the OPL2 reference in simulation only |
| [`firmware/`](firmware/) | Bochs ROM BIOS and LGPL VGABios sources, `source.json` manifests, `firmware.lock` |
| [`sim/machine_tb.cpp`](sim/machine_tb.cpp) | The machine with the Bochs oracle as CPU, guest memory and the text VGA stand-in |
| [`sim/devices_tb.cpp`](sim/devices_tb.cpp) | Directed checks of the AT core through its I/O port |
| [`sim/audio_tb.cpp`](sim/audio_tb.cpp) | Directed checks of the DSP, the OPL2's AdLib port and the mixer, through the AT core |
| [`sim/opl2_tb.cpp`](sim/opl2_tb.cpp) | The OPL2 against Nuked-OPL3, sample for sample, on random programs and captured port logs |
| [`sim/audio_link.h`](sim/audio_link.h), [`sim/opl2_reference.h`](sim/opl2_reference.h) | The AT core and audio models joined as wires; the Nuked-OPL3 stepper |
| [`sim/sound.asm`](sim/sound.asm) | SOUND.COM, the start-up exercise the sound suite runs at the FreeDOS prompt |
| [`sim/fixtures/`](sim/fixtures/) | The OPL2 port writes the sound suite captured, replayed by the opl2 suite |
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
python3 scripts/sim_fes_pc386.py --suite boot --download  # BIOS POST to the FreeDOS prompt, minutes
python3 scripts/sim_fes_pc386.py --suite sound          # the boot, then SOUND.COM; writes sound.wav
```

`make sim-fes-pc386` runs the first three; `make sim-fes-pc386-boot` and
`make sim-fes-pc386-sound` the machine runs.

The boot suite runs the locked BIOS and VGA BIOS on the RTL AT core, boots the
FreeDOS disk, types `dir` at the prompt and requires its listing on screen,
then checks that every CPU I/O completed at its class time. It writes
`build/sim/fes-pc386/boot/report.json` (POST codes, debug-port text, I/O
histogram, final screen) and `machine.log`. `PC386_TRACE_IO=1` makes
`machine_tb` print every I/O but port 80h.

## Simulation model

- **CPU.** The Bochs level-3 oracle of the [i386 Phase 0 adapter](../fes-common/sim/i386/README.md).
  Each retired instruction advances the RTL four system clocks
  (`--cycles-per-instruction`; about a 25 MIPS CPU at 100 MHz). Nothing in the
  boot calibrates against it; it sets how long the boot takes. IN, OUT and INTA
  run through the RTL inside the instruction and take its device time on top.
  A halted CPU lets the RTL run until INTR.
- **Memory.** 16 MiB of host pages: the ROMs at F0000h (aliased at FFFF0000h)
  and C0000h are read-only, C8000h-EFFFFh is RAM, and A0000h-BFFFFh is plain
  RAM for the VGA stand-in. Above 16 MiB is a hole.
- **VGA stand-in.** A register file for 3B0h-3DFh with a 70 Hz, 449-line
  retrace on 3DAh. Each VGA port byte completes in the clock after it is
  posted, as the design's `vga` class allows; there is no planar logic, so
  fonts land in RAM at A0000h harmlessly and the screen is read from B8000h.
- **Disk.** The image file behind the ATA device's 64-bit image port, answered
  in the next clock. The geometry is inferred from the first partition entry's
  end CHS, as FogCast will.
- **Clock.** 100 MHz; every guest rate is a rational enable of it.

The sound suite boots as the boot suite does, runs `sound` at the prompt and
requires SOUND.COM's findings on screen (AdLib found, DSP 2.01, six DSP IRQs),
every OPL2 sample equal to Nuked-OPL3's, and each section's tone in the 48 kHz
output: the speaker's 880 Hz, the DSP's 1 kHz. It writes
`build/sim/fes-pc386/sound/sound.wav` to listen to, `report.json` with each
section's frequency, RMS and peak, and `sound-opl2.log`; `--update-fixture`
copies that log to `sim/fixtures/` for the opl2 suite.

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

## Not built yet

The HID nine-row keyboard model and typematic repeat; the `fes.mouse.relative`
and `fes.computer` mailbox wiring (disk prepare, commit with CRC, freeze,
DiskDirty read-out, persistence, wall clock); the shell, VGA and the producer
(phases M2 and M4). The audio has had no synthesis: whether the locked Yosys
frontend takes the import's SystemVerilog (packages, structs, `unique case`) is
M4's question.
