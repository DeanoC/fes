# FES pc386 machine

The AT-class 386 PC of the [pc386 machine design](../../docs/pc386-machine-design.md),
built around [`fes_i386_cpu`](../fes-common/rtl/i386/). This directory holds
phase M1: the AT core, the firmware build and a simulation of the machine with
the Bochs oracle standing in for the CPU. There is no VGA beyond a text
stand-in in the testbench, no audio, no shell, no package and no kit evidence.
Everything here is a host-only check.

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
| [`firmware/`](firmware/) | Bochs ROM BIOS and LGPL VGABios sources, `source.json` manifests, `firmware.lock` |
| [`sim/machine_tb.cpp`](sim/machine_tb.cpp) | The machine with the Bochs oracle as CPU, guest memory and the text VGA stand-in |
| [`sim/devices_tb.cpp`](sim/devices_tb.cpp) | Directed checks of the AT core through its I/O port |
| [`sim/freedos.lock`](sim/freedos.lock) | The FreeDOS 1.4 package commits and file hashes of the simulation disk |

The 8254 is the shared KF8253 with `READ_BACK = 1`; the DMA controllers are the
shared [`fes_dma8237.sv`](../fes-common/rtl/fes_dma8237.sv) with AT parameters.

## Running

```sh
python3 scripts/build_pc386_firmware.py --download     # dev86, then both ROMs, checked against the lock
python3 scripts/build_pc386_freedos_image.py --download  # the FreeDOS fixture in build/
python3 scripts/sim_fes_pc386.py --suite devices        # directed device checks, about 10 s
python3 scripts/sim_fes_pc386.py --suite boot --download  # BIOS POST to the FreeDOS prompt, minutes
```

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
DiskDirty read-out, persistence, wall clock); the shell, VGA, audio and the
producer (phases M2-M4).
