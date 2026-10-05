# 2026-10-04: fes.riscv 0.1.0 host seal

Host-only record of the sealed `fes.riscv` package, resealed after the first review (see Revision). It records a routed,
timing-closed bitstream and its host simulations. It is not a kit diagnostic
and not hardware acceptance; no bitstream was loaded on a board.

## Artifact

| Item | Value |
| --- | --- |
| Source | FES `88d8dc4e7` (`sources/misteross`), producer `scripts/build_fes_riscv.py` |
| Lock | `toolchain.lock` (Yosys `886afa63`, Mistral `7ed06e21`, nextpnr `3d4a5b35`, HIP `gfx1100;gfx1201`) |
| Build ID | `55de600f44d9b309472ce998d4e298f4` |
| Package | `e5c4bd79c0ebf627563eea1040a846626d9bc3ca72fec9966d680e168f1839f4` |
| RBF | sha256 `a4dff71add6e5a0c7ea61113cd96349ce94e4c658c6a6cec1e9ac1403f46a73a`, 2,222,968 bytes |
| Placement | first-pass search, seed 1 / HeAP weight 10 closed on the first candidate |
| Pixel clock | 75.18 MHz achieved against the 74.25 MHz constraint (single sequential domain) |
| Resources | 2,829 MISTRAL_COMB, 761 MISTRAL_FF, 54 MISTRAL_M10K, 31 MISTRAL_IO, 1 altera_pll, HPS GP and I2C blocks; no MLAB, DSP or SDRAM bridge |

The margin at 74.25 MHz is about one percent and depends on the placement
seed: an earlier run of the pre-review RTL from an uncommitted tree missed on
seed 1 at 73.50 MHz and closed seed 2 at 75.86 MHz, and its committed seal
closed seed 1 at 77.35 MHz. The producer's seed order is part
of the recipe; a new `BUILD_ID` changes placement.

## Host checks

- `make sim-fes-riscv` (including the `faults` case): ALU (2,001,936 operations), CPU directed program with
  zero and random wait states (15 exceptions, 6 interrupts), six random
  instruction streams in lockstep with the independent model (about 113,000
  instructions, 3,047 exceptions, 634 interrupts), firmware images current,
  `fes_riscv_system` with the real firmware through its HDMI pixel stream
  (border, gradient, banner, gamepad movement, timer-interrupt colour change,
  border clamp, restart on hold), and the board shell through the mailbox.
- `python3 -m unittest tests.test_build_fes_riscv` and the FES
  `tests.test_affected`, `tests.test_recipes`, `tests.test_core_catalog`.
- `make check` at `88d8dc4e7`.

## Revision

The first seal (package `46c140ee0a9efdd1bb3a1d2a3e350897082029ef6650c590b28858e1aabb4c5e`, FES
`42544707d`, seed 1, 77.35 MHz) was superseded after review. Two defects were
fixed: the I/O block decoded 64 bytes although ten words exist, so unused
words did not fault; and the firmware assembler rejected CSR addresses above
0x7ff. The decode change alters the RTL, hence the new package. The new
`faults` simulation case fails against the old decode (3 of 6 faults) and
passes against the fix.

## Not established

Visible HDMI output, gamepad delivery on a kit, interrupt timing on hardware,
and any appliance image. `fes.riscv` is registered for `make core-dev` only
and is not in the factory package set.
