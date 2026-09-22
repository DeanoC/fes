# 910 MiSTer SDRAM addon

`make sim EXP=910_sdram_addon` writes `0xA65A` and `0x1234` through the
application mailbox and reads them back from a 16-bit SDRAM model.
`make oss EXP=910_sdram_addon` places that controller on the GPIO header
used by the optional MiSTer SDRAM addon. Quartus comparison is not
implemented.

The 32 MB, 64 MB and 128 MB modules share this 16-bit pinout. Both
designated boards have the 128 MB addon wired on the board. The probe
uses halfword addresses `0x0010` and `0x0011`, inside the first 128 KB,
so it does not depend on the extra address bit of the larger modules.
This is not HPS DDR.

GPO/GPI use `fes.application` 1.0 framing (signature `0xF5`, request
toggle bit 31, ACK bit 23). Identity opcode 1 returns magic `0x4546` /
`0x3153` and ABI tag 3. Opcode 18 is local to this probe: index 0 sets
the halfword address, index 1 writes the argument, index 2 reads it.

Claim the designated kit with `scripts/kit.py session`, load the exact
OSS RBF, and run `hardware/probe.sh`. Never take over another owner.
The open-source lane drives these pins as ordinary I/O at 50 MHz. Each
DQ bit is a width-one `altiobuf_bidir` so the packer can attach output
enable to the pad. That does not qualify the addon at the higher clocks
MiSTer cores use.
