# 2026-10-04: fes.riscv 0.1.0 host seal

Host-only record of the first sealed `fes.riscv` package. It records a routed,
timing-closed bitstream and its host simulations. It is not a kit diagnostic
and not hardware acceptance; no bitstream was loaded on a board.

## Artifact

| Item | Value |
| --- | --- |
| Source | FES `42544707d` (`sources/misteross`), producer `scripts/build_fes_riscv.py` |
| Lock | `toolchain.lock` (Yosys `886afa63`, Mistral `7ed06e21`, nextpnr `3d4a5b35`, HIP `gfx1100;gfx1201`) |
| Build ID | `3547ac28dcaacae0f5bbcf3969e72230` |
| Package | `46c140ee0a9efdd1bb3a1d2a3e350897082029ef6650c590b28858e1aabb4c5e` |
| RBF | sha256 `b1265b970e3bf1f816e99334be39377c1fd405f2d41e6dd5d452904493af357b`, 2,232,518 bytes |
| Placement | first-pass search, seed 1 / HeAP weight 10 closed on the first candidate |
| Pixel clock | 77.35 MHz achieved against the 74.25 MHz constraint (single sequential domain) |
| Resources | 2,842 MISTRAL_COMB, 761 MISTRAL_FF, 54 MISTRAL_M10K, 31 MISTRAL_IO, 1 altera_pll, HPS GP and I2C blocks; no MLAB, DSP or SDRAM bridge |

An earlier run of the same RTL from an uncommitted tree closed seed 2 at
75.86 MHz after seed 1 reached 73.50 MHz, so the margin at 74.25 MHz is a few
percent and depends on the placement seed. The producer's seed order is part
of the recipe; a new `BUILD_ID` changes placement.

## Host checks

- `make sim-fes-riscv`: ALU (2,001,936 operations), CPU directed program with
  zero and random wait states (15 exceptions, 6 interrupts), six random
  instruction streams in lockstep with the independent model (about 113,000
  instructions, 3,047 exceptions, 634 interrupts), firmware images current,
  `fes_riscv_system` with the real firmware through its HDMI pixel stream
  (border, gradient, banner, gamepad movement, timer-interrupt colour change,
  border clamp, restart on hold), and the board shell through the mailbox.
- `python3 -m unittest tests.test_build_fes_riscv` and the FES
  `tests.test_affected`, `tests.test_recipes`, `tests.test_core_catalog`.
- `make check` at `42544707d`.

## Not established

Visible HDMI output, gamepad delivery on a kit, interrupt timing on hardware,
and any appliance image. `fes.riscv` is registered for `make core-dev` only
and is not in the factory package set.
