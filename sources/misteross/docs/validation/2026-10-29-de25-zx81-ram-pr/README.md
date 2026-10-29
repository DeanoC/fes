# DE25 ZX81 RAM-pack PR proof, 2026-10-29

Base: `f7b7f806adb211ce4f0665b6f79d32fcf6733d97` (PR #1258).
Scope: off-board static ZX81 with empty and 16 KiB RAM-pack personas, using
Quartus Pro 26.3 and native/AArch64 artifact composition. The
[profile guide](../../../boards/de25nano/link-proof/zx81-ram/README.md)
describes the current diagnostic and its boundaries.

The retained BASIC simulation at `918145ebb4` runs the real machine/CPU,
stock firmware and actual RAM-pack RTL. CPU writes establish exclusive
RAMTOP bounds 0x4400/0x8000, followed by the real BASIC keyboard idle loop.
Actual CSR LAST_DATA reads agree with the machine peek. Full 16 KiB two-pass
address patterns, the last address 0x7fff, and an out-of-range write at 0x8000
are checked. Selection resets/re-sizes the machine back to 1 KiB.
The full request pipeline is exercised at six freeze phases, checking that
neither writes nor responses change after Frozen. The record identifies
its exact committed source/test hashes and behavioral-memory scope. They do not model physical PR or M20K analogue behavior.

Failed probes are retained under `failed/`: a Verilog wildcard-port parse
failure; mismatched ROM initialization depth; the actual stale CSR byte with
the earlier four-cycle diagnostic; and a compiled base whose twenty constant
response outputs failed BBD-60000/BBD-60002. That base's passing internal timing
is diagnostic evidence for a rejected candidate, not PR acceptance. A private
same-part constant-register synthesis probe confirms that explicit
preserve/dont_merge/noprune attributes retain all twenty FFs. A later imported
persona failed to route a folded static ~GND request bit, so the current
profile also retains every static request launch FF. Its real synthesis gate
requires all 46 static launches and all 20 persona response registers, and the
base must pass its complete boundary audit before A/B compilation starts.
A per-edge real-CPU freeze probe then exposed a late read response after
Frozen, despite passing static timing. The current adapter captures the whole
CPU memory bus as idle when RUNNING drops, independent of CPU reset enables.
The committed fixed probe changes read data from166 to57 during settling,
then observes no writes, retirements or bus-response changes after Frozen.

The fresh protected-register base/A/B campaign, final boundary audit and
native/AArch64 actual-artifact composition are still running. No hardware,
SDM, image/boot, electrical IO, HDMI or production acceptance is claimed.
