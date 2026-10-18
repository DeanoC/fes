# Expected behavior

The FPGA exposes four products of the same two 9-bit operands on HPS GP. There
is no LED or other external pin. A host diagnostic writes the operands, the
product select and the half select through GPO and reads sixteen result bits
through GPI; a blank display is not a failure.

After configuration the high 16 GPI bits stay `0xD61A` and the low 16 bits are
the registered selected half, initially zero.

GPO fields are:

- bits `[8:0]`: left operand
- bits `[17:9]`: right operand
- bit `18`: high-half select of a 32-bit view
- bits `[20:19]`: product (`0` signed 9x9, `1` unsigned 9x9, `2` signed 18x18
  of `{left, right}` by `{right, left}`, `3` signed 27x27 of
  `{left, right, left}` by `{right, left, right}`, low 32 bits)

The 9x9 views are the 18-bit product zero-extended to 32 bits. Each product is
registered, then the selected half, so two clocks after a write GPI shows `view[15:0]` with bit 18 clear or `view[31:16]` with bit 18
set. Operands with bit 8 set separate the DSP block's signed and unsigned
configurations:

| left | right | signed 9x9 | unsigned 9x9 | signed 18x18 | signed 27x27 |
| --- | --- | --- | --- | --- | --- |
| `0x00A` | `0x00C` | `0x00000078` | `0x00000078` | `0x01E1E878` | `0xA5A1E878` |
| `0x1FF` | `0x1FF` | `0x00000001` | `0x0003FC01` | `0x00000001` | `0x00000001` |
| `0x100` | `0x1FF` | `0x00000100` | `0x0001FF00` | `0x01FE0100` | `0x0DFA0100` |
| `0x155` | `0x0AA` | `0x00038E72` | `0x0000E272` | `0x38E31C72` | `0x39C71C72` |

Yosys emits two `MISTRAL_MUL9X9` cells (one signed, one unsigned), one
`MISTRAL_MUL18X18` and one `MISTRAL_MUL27X27`, each with `A_SIGNED` and
`B_SIGNED`. nextpnr-mistral mirrors the Quartus configuration: the signed 9x9
is sign-extended into DATAIN groups 1 and 3 on lane 0 of its own block with all
four sign bits set, the unsigned 9x9 sets no sign bit, the 27x27 sets all four
sign bits, and the 18x18 sets `AX_SIGNED`, `BX_SIGNED` and `BY_SIGNED` with the
nineteenth Y bit on group 5. The two 9x9 cells therefore occupy two DSP sites.
Memory and PLL stay unused. The pinned toolchain before the Yosys `dsp_map.v`
signedness fix configures every product as signed and fails the unsigned 9x9
and every wide product; this rung is the hardware check for that fix.

`make sim EXP=755_dsp_signed` checks all four products through the HPS model.
`make oss EXP=755_dsp_signed` produces compressed `build/oss/755_dsp_signed/top.rbf`
once the toolchain lock carries the fix.
