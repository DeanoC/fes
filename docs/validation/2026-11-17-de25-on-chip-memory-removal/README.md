# DE25 managed shell without the vendor on-chip memory

First built 2026-11-17 at `106752761`, against `main` at `0ff1ebef7`. Rebuilt
2026-11-20 after merging `main` (`9951d04f0`), which added the DE25 Spectrum and
Atari ST producers on the same shell. Built revision:
`18233ba6fd1bdf20e6ce601fe414086879de5e63`. The evidence files in this
directory are from that rebuild.

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
review, signoff compile plus repeat compile for each producer: MENU on
`chip-30x0-ai-dev`, the others on Powerboat. No hardware run in this record;
every package records `hardware_acceptance = false`. Exact-head HIL on Kit D25
(J8G) runs from the PR head.

## Results

All five producers at `18233ba6f`:

| | MENU | ZX81 | Spectrum 48K | Spectrum 128K | Atari ST |
|---|---|---|---|---|---|
| RAM blocks (of 358) | 18 | 173 | 83 | 135 | 172 |
| Block memory bits | 247,296 | 1,049,600 | 1,387,520 | 2,239,488 | 3,106,248 |
| ALMs | 4,719 | 8,272 | 9,436 | 9,806 | 14,562 |
| Worst setup slack (ns) | 5.422 | 1.483 | 1.822 | 1.704 | 1.858 |
| Worst hold slack (ns) | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `BUILD_ID` | `8dd23f0fbf5a58a7483ab7a9cc9885b7` | `c5b70cdf12996c5b13ce082dc6aed371` | `7bcaa4a2ea87fda49a03a618b94cfedd` | `632997afabed239baa5b7233c00e5389` | `25e9f825bfebe510afec7e31331d5ad5` |

The Spectrum 128K and Atari ST were also built from `main` (`9951d04f0`) for
comparison:

| | Spectrum 128K `main` | Spectrum 128K now | Atari ST `main` | Atari ST now |
|---|---|---|---|---|
| RAM blocks (of 358) | 263 | 135 | 300 | 172 |
| Block memory bits | 4,336,640 | 2,239,488 | 5,203,400 | 3,106,248 |
| ALMs | 10,653 | 9,806 | 15,439 | 14,562 |
| Worst setup slack (ns) | 2.081 | 1.704 | 2.104 | 1.858 |

Each design gives back exactly 128 M20K blocks and 2,097,152 memory bits, the
`soc_inst|ocm` instance. `soc_inst|ocm` appears in both `main` fit reports and
in none of the five new ones. The MENU and ZX81 block counts match the
2026-11-17 build (18 and 173). The Spectrum 128K and ST setup slack is lower
than on `main` but positive; the fitter places a different design once the
memory is gone.

Every producer reports `all_reported_timing_pass = true`,
`package_timing_admission = true` and `unreviewed_failed_rules = 0` of 85
signoff rules. The three design-assistant rules with findings (`RDC-50003`,
`TMC-20011`, `TMC-20012`) are the same rules with the same violation counts as
on `main`, each covered by its existing review. No check was changed.

Each producer compares its repeat compile with the signoff bitstream and
refuses the package unless they match byte for byte. Packages (file sha256):

- MENU `65175464…6e36.fcore` `2356af3464dfc4f947dedc45344d9642d827d339058006f920e366396d03566a`
- ZX81 `b7ec1462…30e1.fcore` `526ad1aa5d9057ebfe3571afd11774fe1a419934b16438a0b36cb79b8195961b`
- Spectrum 48K `04fa1bb9…a150.fcore` `e4e7bc14442d6eede2239c2878ec502a8e1ebef08cd53fe215568be71b6bdc24`
- Spectrum 128K `a4191957…44b2.fcore` `776d9705f4e5187129cf804e1a586fa6423ae97ff7a8a6d0d93aa0102473f2f7`
- Atari ST `bc159b92…5916.fcore` `70017538cf6a98a70ade72617c988dcb7f55b8f9a7273f1b9db45cf26705816f`

## Boot handoff

`hps_bootloader_handoff.bin` does not change with this shell. MENU, ZX81,
Spectrum 48K and Atari ST produce
`a17de6f2dbf5f4e93643b50b4c8291a33f930064f9a43bad3ece24e9c3dda04a`, the
checked-in `tests/fixtures/nano-usb-board` copy, before and after. The Spectrum
128K produces `ea3e278385b78a3f6178dbc8c26b7de51017100c8fca807ed01a8b35574e0126`
on both `main` and this branch, so its difference predates this change.
`H2F_Width` only decides whether the bridge ports exist in the fabric; it does
not reach the SPL handoff. The Linux device tree is not generated from this
graph and has no bridge nodes.

## Not validated without a board

- The vendor `boot.scr` runs a bare `bridge enable`, and vendor TF-A calls
  `socfpga_bridges_enable(~0)`. Both name every bridge, including the one now
  marked Unused. The FPGA-to-HPS bridge is already disabled the same way
  in the shell on `main`, but the full HPS-to-FPGA case has not run on hardware.
- Exact-artifact boot, menu display and core sessions on a DE25 kit: covered
  by the exact-head D25 HIL run, not by this record.

## Commands

```
python3 boards/de25nano/producers/build_menu_package.py --root <src>/sources/misteross \
  --vendor-archive <DE25-Nano_revB_v.1.0.0_ResourcePackage.zip> --quartus-root /data/altera_pro/26.3 \
  --output build/claude-1956/menu --package-output build/claude-1956/menu-package
python3 boards/de25nano/producers/build_zx81_package.py (same arguments, zx81 outputs)
python3 boards/de25nano/producers/build_spectrum_package.py --model 48 (and --model 128)
python3 boards/de25nano/producers/build_atari_st_package.py --model 520st
```

Each ran from a clean detached checkout of the built revision under that host's
shared Quartus lock.
