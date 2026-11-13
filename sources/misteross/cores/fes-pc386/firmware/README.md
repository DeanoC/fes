# pc386 firmware

The system BIOS and VGA BIOS of the [pc386 machine](../../../docs/pc386-machine-design.md#firmware),
built from the vendored sources here by
[`scripts/build_pc386_firmware.py`](../../../scripts/build_pc386_firmware.py).

| Image | Source | Build | Size | Licence |
| --- | --- | --- | --- | --- |
| `pc386-bios.bin`, at F0000h | [`rombios/`](rombios/): the Bochs ROM BIOS `rombios.c` at Bochs `REL_3_1_FINAL` (`30cfd566`) | `BIOS-bochs-legacy`: `-DLEGACY`, no 32-bit part | 64 KiB | LGPL-2.1-or-later |
| `pc386-vgabios.bin`, at C0000h | [`vgabios/`](vgabios/): the LGPL VGABios 0.9d (`12d978bd`) | without `-DVBE -DPCIBIOS` | 32 KiB | LGPL-2.1-or-later |

Each tree's `source.json` records the upstream repository and commit, every
file's upstream path and hash, and the dated local changes; a changed file
keeps its upstream hash beside its own. The build fetches dev86 (`bcc`,
`as86`; GPL-2.0) at tag `v1.0.1` into the ignored `build/` directory only with
`--download`, preprocesses with the host gcc as the upstream makefiles do,
and checks both images against [`firmware.lock`](firmware.lock), which also
fixes the build-date strings so the images are reproducible. `--update-lock`
records new digests after a deliberate change.

```sh
python3 scripts/build_pc386_firmware.py --download
```

## Local changes

Marked in the source with `FES <date>` comments, as LGPL-2.1 section 2 asks of
a modified library:

- `rombios.h`: `BX_PCIBIOS`, `BX_APM` and `BX_PNPBIOS` are 0. pc386 has no PCI
  bus, APM or PnP; the legacy build's PCI BIOS would only find FFh at CF8h.
- `rombios.c`: the POST call to `pci_set_pam_regs_ro` is built only with the
  PCI BIOS; upstream it is unconditional and does not assemble without it.
- `rombios.c`: INT 16h AH=03h AL=00h/05h sets the typematic rate and delay
  with keyboard command F3h, and INT 16h AH=09h reports it.
- `rombios.c`: A20 goes through both of pc386's controls, the 8042's output
  port bit 1 and port 92h bit 1, since A20 is on while either is set.
  - **POST** gates A20 off for the OS through both. It reads the output port
    with D0h and writes it back with D1h, with bit 1 clear and bit 0 set (clear
    resets the CPU). This happens in `keyboard_init` while the keyboard is
    disabled and the aux interface is off, so the D0h response cannot meet a
    device byte. A shutdown-byte resume skips it and keeps its A20 state.
  - **`set_enable_a20`** (INT 15h AX=2400h/2401h, 87h and 89h) enables
    through port 92h only when A20 is off. To disable, it clears port 92h and
    then, if A20 is still on, writes the output port with the AT BIOS's DDh.
    So 87h puts back exactly the state it found.
  - **AX=2402h** reports the state memory sees (`get_enable_a20`, a wrap test
    at 500h).

  Upstream drove port 92h alone, so on pc386 A20 stayed on through POST and
  through every BIOS disable.
- `rombios.c`: INT 15h C2xx, the PS/2 pointing-device services, run with the
  keyboard interface disabled ([#1330](https://github.com/DeanoC/fes/issues/1330)).
  They exchange bytes with the aux device through the 8042 with interrupts
  off. Upstream, a key byte in the output buffer was read as the command
  byte, or held the device's reply behind it while `get_mouse_data` spun
  forever.
  - `int15_handler_mouse` calls `mouse_exchange_begin` before each call and
    `mouse_exchange_end` (AEh) after it, so every exit path enables the
    keyboard again.
  - `mouse_exchange_begin` sends ADh. A byte already in the output buffer
    goes to its interrupt handler, INT 09h or INT 74h, with interrupts on, so
    a key is not lost. A byte that no handler takes within 4,096 status
    reads (its IRQ masked, or a higher IRQ in service) is discarded.
    INT 09h sends AEh as it exits, so ADh is sent again until the buffer is
    empty behind it.
  - `get_kbd_command_byte` reads the command byte (20h) for
    `inhibit_mouse_int_and_events` and `enable_mouse_int_and_events`,
    discarding an aux byte ahead of the reply.

- `rombios.c`, `rombios.h`: POST programs PIT counter 1, the refresh timer,
  as an AT BIOS does: binary, LSB only, mode 2, count 18 (15.09 µs). Port 61h
  bit 4 toggles with its output, and INT 15h AH=86h counts those toggles as
  15 µs each. Bochs toggles the bit by itself, so upstream never set the
  counter, and on pc386 every AH=86h wait that needed a toggle never returned
  ([#1150](https://github.com/DeanoC/fes/issues/1150)). `rombios.h` names port
  41h `PORT_PIT_COUNTER1`.
- `rombios.c`, `rombios.h`: POST programs PIT counter 2, the speaker timer, in
  the mode an AT BIOS's POST beep leaves it: binary, LSB then MSB, mode 3, with
  the IBM BIOS BEEP routine's count 0533h (896 Hz). Port 61h's gate and speaker bits stay off, so it does not beep. Games such as
  Ultima VI write only a count to port 42h and toggle port 61h bits 0 and 1,
  so they need that mode. Upstream never programs the counter, which then stays
  in KF8253's reset state (mode 0, MSB-only counts), and those games' speaker
  effects played no tones ([#1457](https://github.com/DeanoC/fes/issues/1457)).
  `rombios.h` names port 42h `PORT_PIT_COUNTER2`.
- `rombios.c`: after the copyright line the ROM carries a BIOS identification
  line, `Compatible: IBM PC/AT 1984, FES pc386 BIOS.`, in the shape of a period
  AT BIOS's copyright line. That shape is a 198x year, then a comma, dash or
  space, then a capitalised name ending in a full stop. 1984 is the PC/AT's
  year. Programs find the BIOS vendor by such a year, and every AT BIOS of the
  time has one; the upstream ROM's only year is 2001-2026. SpeedSys 4.78's scan
  leaves DS at F000h when it finds no year, so its speed test then sorted the
  VGA ROM for ever ([#1557](https://github.com/DeanoC/fes/issues/1557)).
  `tests/test_pc386_machine.py` replays that scan on the line and on a built
  image.
- `rombios.c`: INT 13h AH=08h reports the highest cylinder below the AT's
  diagnostic cylinder: the parameter table's count less two, as the IBM PC/AT
  BIOS reports it. Upstream took off only one, while its own AH=15h already
  left the diagnostic cylinder out of the disk's size. Windows' 32-bit disk
  driver, WDCTRL, compares AH=08h with the INT 41h table, allows for the AT's
  reserved cylinder, and refused the disk at "phase 03"
  ([#1719](https://github.com/DeanoC/fes/issues/1719)). Partitions still reach
  the last cylinder: reads and writes check the drive's own geometry. A
  one-cylinder disk reserves none, in AH=08h and AH=15h alike; upstream's
  AH=15h reported it as empty.
- `rombios.c`: the INT 75h handler (IRQ 13, the coprocessor's error) keeps
  AX and writes 00h to port F0h, as the IBM PC/AT BIOS's `INT_287` does
  (PUSH AX, OUT F0h, EOI to both PICs, POP AX, INT 02h). Upstream left the EOI
  value in AL. FreeDOS's interrupt-stack switcher keeps its stack size in AX
  across the handler it calls, so each IRQ 13 moved its stack pool by the
  difference, onto the MCB chain
  ([#1768](https://github.com/DeanoC/fes/issues/1768)).
- `vgabios.c`: INT 10h AH=00h, the mode set, keeps every register but AL, as
  IBM's VGA BIOS does: the dispatcher saves ES, DS and the general registers
  around `biosfn_set_video_mode`. Upstream saved only DS, BX, CX and AX, so the
  mode set returned with DX changed. MicroProse's `MGRAPHIC.EXE`, which
  Darklands and its Art Gallery load, keeps the mode it asked for in DL and
  compares it with INT 10h AH=0Fh's AL, so it judged mode 13h unsupported and
  printed its message from the wrong offset
  ([#1553](https://github.com/DeanoC/fes/issues/1553)). SCROLLT.COM's
  `modeset` check (`--suite scroll`) sets modes 13h, 93h and 03h and counts
  the registers kept.

The boot order and disk-only boot need no patch: the machine writes CMOS 2Dh,
38h, 3Dh and 3Fh. The floppy POST and INT 13h floppy paths stay; CMOS 10h says
no drives and the unclaimed controller ports read FFh. The Bochs debug ports
400h-403h and VGABios's 500h are the machine's debug sink.

## Licence obligations

The images are separate programs loaded into ROM, not linked with the RTL.
Their corresponding source is these trees and patches at the package's source
revision, with the build script, in the public FES repository; `COPYING` in
each tree is the LGPL-2.1 text and the upstream notices are kept.
