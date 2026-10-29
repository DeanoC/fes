# DE25 ZX81 RAM-pack PR proof, 2026-10-29

Base: `f7b7f806adb211ce4f0665b6f79d32fcf6733d97` (PR #1258).
Scope: off-board static ZX81 with empty and 16 KiB RAM-pack personas, using
Quartus Pro 26.3 and native/AArch64 artifact composition. The
[profile guide](../../../boards/de25nano/link-proof/zx81-ram/README.md)
describes the current diagnostic and its boundaries.

The retained BASIC simulation at `5d4ba535f7` runs the real machine/CPU,
stock firmware and actual RAM-pack RTL. CPU writes establish exclusive
RAMTOP bounds 0x4400/0x8000, followed by the real BASIC keyboard idle loop.
Actual CSR LAST_DATA reads agree with the machine peek. Full 16 KiB two-pass
address patterns, the last address 0x7fff, and an out-of-range write at 0x8000
are checked. Selection resets/re-sizes the machine back to 1 KiB.
The separately committed `0720730457` drain probe includes the full request
pipeline and checks no writes or response changes after Frozen at six phases.
Both records identify their exact source/test hashes and behavioral-memory
scope. They do not model physical PR or M20K analogue behavior.

Failed probes are retained under `failed/`: a Verilog wildcard-port parse
failure; mismatched ROM initialization depth; the actual stale CSR byte with
the earlier four-cycle diagnostic; and a compiled base whose twenty constant
response outputs failed BBD-60000/BBD-60002. That base's passing internal timing
is diagnostic evidence for a rejected candidate, not PR acceptance. A private
same-part constant-register synthesis probe confirms that explicit
preserve/dont_merge/noprune attributes retain all twenty FFs. The profile
requires the same result in its real synthesis before fitting.

The fresh protected-register base/A/B campaign, final boundary audit and
native/AArch64 actual-artifact composition are still running. No hardware,
SDM, image/boot, electrical IO, HDMI or production acceptance is claimed.
