# FX68K

The shared Motorola 68000 implementation is FX68K by Jorge Cwik, used by the
FES Atari 520ST simulation slice.

- Upstream: <https://github.com/ijor/fx68k>
- Commit: `0602ee4627b10f301298f2673d826cdd6baa9327` (2021-02-16).
- License: GPL-3.0-or-later; see [LICENSE](LICENSE) and the copyright and
  license notice in [fx68k.txt](fx68k.txt).
- Local changes: none. [source.json](source.json) records the SHA-256 of
  every copied upstream file. The RTL and ROM files retain their original
  bytes and line endings.

Compile `fx68k.sv`, `fx68kAlu.sv` and `uaddrPla.sv` together as SystemVerilog.
The microcode initialization uses the literal relative paths `microrom.mem`
and `nanorom.mem`; the simulation runner provides these exact files in its
working directory. These are CPU microcode, separate from the pluggable
Atari firmware input.

The core uses one synchronous clock with alternating `enPhi1` and `enPhi2`
enables. Each enable is one host clock long; the host clock must be at least
twice the emulated CPU clock. Reset is synchronous and active high, and
`pwrUp` must accompany `extReset` on a cold start. FES `st_cpu.sv` asserts
both for each reset and generates the enables without a derived clock.

The bus follows 68000 signal polarity: `ASn`, `UDSn`, `LDSn`, `DTACKn`,
`BERRn` and interrupt inputs are active low; `eRWn` is high for a read.
Address output is `eab[23:1]`; the byte strobes supply the byte lane. The
memory adapter keeps data and acknowledgment valid until `ASn` releases.
The upstream input synchronizers may insert wait states when the response
arrives late; an acknowledgment must be held rather than pulsed.

FES currently verifies this source with Verilator. Its use of unpacked
SystemVerilog structures is not qualified through the pinned Yosys
`read_verilog -sv` frontend. An OSS producer requires a separately verified
frontend or conversion step. No synthesis, timing, RBF or kit result is
claimed by this import.
