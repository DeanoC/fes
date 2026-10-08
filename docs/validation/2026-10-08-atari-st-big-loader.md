# Atari ST BIG loader: empty-memory acknowledgement and ROM comparison

This host-only follow-up to [task #621](https://github.com/DeanoC/fes/issues/621)
starts at main `10472724c17fa31d43849d81eb56e661e968ddc6`. Main already includes
the request-egress fix and separate Direct/Scanlines qualification from the
work associated with closed PR #620. That routing dependency is resolved;
it is separate from BIG's CPU/firmware behavior.

The [evidence record](2026-10-08-atari-st-big-loader/evidence.json) binds the
frozen input/model identities, before/after traces and independent Hatari run.
No kit commands, programming, deployment or service changes were made. The
normal populated menu was left untouched. This is neither a new FPGA/image
qualification nor acceptance of BIG's display, border effects or audio.

## The early FES fault

The original 409,600-byte BIG disk is unchanged, SHA-256
`608c4beff1780552e8ae6ee5a1efff27198fb6a3ca6274597f1cb528c3970edb`.
The firmware is unchanged stock EmuTOS 1.4 US 192 KiB, SHA-256
`8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc`.
Physical RAM remains 524,288 bytes.

New simulation-only observability captures each rising bus-error edge with
its latched address, direction and function code. The first 64 faults are
logged and the first eight also save RAM; the existing per-second captures
remain. The exported CPU PC is prefetch/exception state, not retired-instruction
PC. Upstream FX68K bytes are unchanged.

The first four faults are supervisor reads probing absent hardware at
`$FF8006`, `$FF860E`, `$FFFC20` and `$FF8A3C`. The earlier geometry record's
RAM-discovery description of these faults was incorrect; its count and captures
remain valid. The fifth fault is a supervisor write to `$3FFFFE`, at fabric
cycle 243,145,893 (about 4.656 seconds), while BIG clears the ST RAM address
window. The exported PC is `$1076`. This happens after one sector/256 DMA
words have been loaded, before the ROM-dependent lookup described below.

The old decoder acknowledged only the configured logical banks. In normal
`$04` configuration, it faulted at `$0A0000` and above. The corrected decoder
keeps the populated bank's address aliases, then acknowledges the rest of the
original 4 MiB RAM decode window. Unpopulated writes are discarded. `$400000`
and higher still fault unless separately claimed by cartridge, ROM or MMIO.
The existing empty-memory read model stays `$FFFF`; it does not emulate the
STF's floating data-bus value.

This boundary follows Hatari 2.5.0's
[release-matched memory implementation](https://github.com/hatari/hatari/blob/v2.5.0/src/cpu/memory.c),
`memory_map_Standard_RAM()`: void space below 4 MiB, bus-error space above it,
then populated-bank mappings. `VoidMem_*put()` discards writes. Its reads use
the previous bus data, a separate behavior not added by this fix.

## Regression and unchanged original loader

The actual CPU diagnostic now writes words and each byte lane at `$080000`,
`$0A0000`, `$200000` and `$3FFFFE`, checks the retained empty-read model, and
checks that real RAM markers and the last populated longword did not alias.
It faults at exactly `$400000`. Both firmware variants pass with immediate
and delayed storage. Running that same new diagnostic against main's original
decoder fails at stage 8, confirming that the regression catches the defect.

The complete `sim-fes-atari-st` aggregate passes, including physical SDRAM,
media geometry, memory arbitration, peripherals and video tests. Nine root
media-helper tests and nineteen Atari producer tests pass. The separate fifteen-second stock EmuTOS boot regression passes: 512 KiB,
screen at `$78000`, timer C and VBL running, and no CPU halt. Parent `make check`
also passes with 18 generated consumers and 34 fixture copies.

Frozen Verilator 5.032 captures execute the original disk for twelve seconds
before the fix and ten seconds after it. Sources, generated models, executable,
microcode, disk and ROM remain unchanged during each execution. The before
capture's frozen source remains authoritative even though the worker RTL was
edited while that model ran. The after capture's compiled input hashes match
the implementation in this change.

After the correction, BIG's high-memory writes acknowledge and execution
passes the clear loop. The complete ten-second capture retains only the four
startup hardware-probe bus faults. Its RAM at `$26` subsequently contains
`$4879`, also seen in the independent loader comparison. This removes the
specific early hardware fault; BIG still does not reach its demo screens.
The trace does not count internal 68000 address-error exceptions, and no
complete CPU exception-frame equivalence is asserted.

## Independent same-input ROM comparison

Private Debian Hatari `2.5.0+dfsg-1+b1` runs the same ROM and disk with ST mode,
512 KiB RAM, 68000 at 8 MHz, cycle-exact/prefetch-compatible CPU, 24-bit
addressing, no blitter, RGB/low resolution and read-only floppy A. TOS patches,
fast boot, Timer-D patches and fast FDC are disabled. Sound is disabled for
this boot diagnosis; it provides no audio acceptance. ROM/disk hashes before
and after match. Tool package, binary and private Capstone hashes are retained.

A debugger capture at boot PC `$10D8` records `A0=$FC06A2`, `D0=$4879` and
`MOVEA.W 6(A0,D0.W),A0`. Its word-read address is `$FC4F21`, which is odd.
The full run records address-error exception 3 there, then invalid execution
through vectors the boot code has overwritten with `$2A2A2A2A`. The word
`$4879` was taken from EmuTOS's ROM code at `$FC06A0` and treated as a table
offset. This is evidence of dependence on an internal ROM layout, not a
failure that can be resolved by increasing the FES RAM buffer.

Hatari's [official EmuTOS record](https://hatari.tuxfamily.org/doc/emutos.txt)
also lists BIG as failing before OS calls. The new comparison identifies a
specific failure for these exact inputs; it does not imply that all BIG disks
or EmuTOS versions fail identically.

The user's library supplies unchanged 192 KiB UK TOS images: 1.00 SHA-256
`5771f9fd1391d3ae0b513ab3fd67aec30fb1843753e7e0345f92615e72c36797`
and 1.02 SHA-256
`ee16750d11299b3e5cf747e6f3c8f9f923aa8ee52c57cc1632d98241500b55f5`.
With the same original disk and emulator settings, both display BIG's
introductory instructions by VBL 700. No user input is injected. These runs
still report address errors; the introductory screen itself says the loader
uses errors and debugger-sensitive tricks. Reaching this screen does not
qualify every demo section or its sound. Only identities, observations and
private capture hashes are checked in; no proprietary firmware or RAM is
published.

With TOS 1.00, the FES model also reaches the introductory instructions by
fabric cycle 156,672,000 (three simulated seconds). The 32,000-byte low-resolution
planar framebuffer at `$70000` matches Hatari's VBL-700 capture exactly:
SHA-256 `87ad658b65f3628c812a1c71ad298ad42983f730dbbea1e6de3b870bc6045e42`.
This compares static RAM contents, not shifter output timing or raster effects.
The ROM, disk and upstream CPU sources remain unchanged. By seven simulated
seconds the model renders BIG's main menu and its scrolling text. A longer
Hatari run also reaches that menu at VBL 2000 without user input. Per-second
static images do not capture its palette changes by scanline or opened borders;
there is no raster or audio acceptance. The full fifteen-second capture
completes with zero external bus faults and no CPU halt. Captured sources,
generated model, executable, microcode, ROM and disk remain unchanged, and
captured RTL/helper hashes match this change. The record still sets
`demo_compatibility_asserted=false`; later demo sections were not selected.

The FES harness accepts such firmware only through an explicit
`--rom-sha256`; the default still requires stock EmuTOS. Mismatched ROMs are
rejected before output creation. Continue comparing the unchanged original TOS
loader through the CPU/chipset model before investigating raster borders and
audio. No demo or firmware patches were applied. New shell, parts and
appliance acceptance must use their own selected-source artifacts after this
RTL change; historical hardware evidence does not qualify it. The shared
manifest, capabilities, generated consumers and wire contracts are unchanged.
