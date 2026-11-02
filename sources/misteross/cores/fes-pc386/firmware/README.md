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

The boot order and disk-only boot need no patch: the machine writes CMOS 2Dh,
38h, 3Dh and 3Fh. The floppy POST and INT 13h floppy paths stay; CMOS 10h says
no drives and the unclaimed controller ports read FFh. The Bochs debug ports
400h-403h and VGABios's 500h are the machine's debug sink.

## Licence obligations

The images are separate programs loaded into ROM, not linked with the RTL.
Their corresponding source is these trees and patches at the package's source
revision, with the build script, in the public FES repository; `COPYING` in
each tree is the LGPL-2.1 text and the upstream notices are kept.
