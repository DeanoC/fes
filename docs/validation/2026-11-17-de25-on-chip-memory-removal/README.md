# DE25 managed shell without the vendor on-chip memory

Date: 2026-11-17. Built revision: `106752761f37dbea7ec86767609517b8efa9df44`.
Baseline for comparison: `main` at `0ff1ebef7`.

## What changed

The Terasic GHRD carries a 16384 x 128-bit `intel_onchip_memory` instance
(`soc_inst|ocm`) as the only responder on the full HPS-to-FPGA bridge. The
managed shell (`managed-scanout-initiators-v4`) now removes that instance and
sets the HPS parameter `H2F_Width` to `0` (Unused), the same way the unused
FPGA-to-HPS bridge is already disabled with `f2s_data_width = 0`.

Nothing in FES addresses the memory or the full bridge. The kernel driver, the
runtime and the embedded firmware use the lightweight bridge at `0x20000000`
only; the embedded firmware opens bridge masks 2 and 8 and never bit 0; the
tree verifier requires that no `fpga-bridges` node exists.

The bridge is disabled rather than left enabled without a responder because an
enabled AXI initiator with nothing behind it stalls any access, and the
managed-fabric verifier already refuses that graph for the other bridge.

## Evidence class

Quartus Prime Pro 26.3 generation, fit, timing signoff and design-assistant
review on `chip-30x0-ai-dev`, signoff compile plus repeat compile for each
producer. No hardware run: DE25 HIL is waived until the boards are ready.
Both packages record `hardware_acceptance = false`.

## Results

| | MENU `main` | MENU now | ZX81 `main` | ZX81 now |
|---|---|---|---|---|
| RAM blocks (of 358) | 146 | 18 | 301 | 173 |
| Block memory bits | 2,344,448 | 247,296 | 3,146,752 | 1,049,600 |
| ALMs | 5,611 | 4,692 | 9,135 | 8,259 |
| Worst setup slack (ns) | 4.232 | 4.944 | 1.433 | 1.515 |
| Worst hold slack (ns) | 0.000 | 0.000 | 0.000 | 0.000 |
| `BUILD_ID` | `0d1c7e0f…` | `b4a0e37e76158cac258b1dab4f7039d1` | `df441b71…` | `cd202566affdc6b347a771223e42fd95` |

Each design gives back exactly 128 M20K blocks and 2,097,152 memory bits, which
is the `soc_inst|ocm` row of the baseline fit reports (128 blocks, 556.7 ALMs).
`soc_inst|ocm` does not appear in either new fit report.

Both producers report `all_reported_timing_pass = true`,
`package_timing_admission = true` and `unreviewed_failed_rules = 0` of 85
signoff rules. The three design-assistant rules with findings (`RDC-50003`,
`TMC-20011`, `TMC-20012`) are the same rules with the same violation counts as
on `main`, each covered by its existing review. No check was changed.

The repeat compile reproduced the signoff bitstream byte for byte:

- MENU `core.rbf` `3dfa85573415f2b742e50851a7f44f1b1e8a6e77713e06056210fdc80935bb0f`,
  package `03f0c1998c4326b006ceb0ef32691755b3b76f6a98a198775b52457473f5833f`
- ZX81 `core.rbf` `d82866b0ec7ab49f90066383afd0b61b36ab1a3a9117e4a29d57dc8194cc30f0`,
  package `0e605a4361d1ba2f66c672c27ce7ac075432fb597cd15a466d8d519cd438c811`

## Boot handoff

`hps_bootloader_handoff.bin` is byte-identical before and after
(`a17de6f2dbf5f4e93643b50b4c8291a33f930064f9a43bad3ece24e9c3dda04a`, also the
checked-in `tests/fixtures/nano-usb-board` copy). `H2F_Width` only decides
whether the bridge ports exist in the fabric; it does not reach the SPL handoff.
The Linux device tree is not generated from this graph and has no bridge nodes.

## Not validated without a board

- The vendor `boot.scr` runs a bare `bridge enable`, and vendor TF-A calls
  `socfpga_bridges_enable(~0)`. Both name every bridge, including the one now
  marked Unused. The FPGA-to-HPS bridge is already disabled the same way
  in the shell on `main`, but the full HPS-to-FPGA case has not run on hardware.
- Exact-artifact boot, menu display and ZX81 session on a DE25 kit.

## Commands

```
python3 boards/de25nano/producers/build_menu_package.py --root <src>/sources/misteross \
  --vendor-archive <DE25-Nano_revB_v.1.0.0_ResourcePackage.zip> --quartus-root /data/altera_pro/26.3 \
  --output build/claude-noocm-menu --package-output build/claude-noocm-menu-package
python3 boards/de25nano/producers/build_zx81_package.py (same arguments, zx81 outputs)
```

Both ran from a clean detached worktree of the built revision under the shared
Quartus lock.
