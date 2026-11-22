# F3 (#1694): D12 for the cores the floppy ABI touches

F3 adds `fes.media.pc-floppy` to the shared `fes_computer_mailbox_core`
(`ENABLE_PC_FLOPPY`, default 0) and fifteen defines to the generated
`cores/fes-common/generated/fes_computer.vh`. Every core whose producer reads
either file has its `BUILD_ID` moved, so [D12](../../../AGENTS.md) applies
(design decision D12 in [pc386-removable-media](../../design/pc386-removable-media.md)).

- **Base:** `61c33cc2fc1c9fcfb053940e8f7ddbb638e108b2` (the merge base).
- **Head:** `fbb3b13d1` and later; the measured `misteross` inputs are those of `fbb3b13d1`.
- **Tools:** one install for both sides, `/data/fes-f3-d12/cache` on Powerboat,
  Yosys `3a5accbd7` (sha256 `dc232ba5…8619`), the same binary for every row.

## Scope

`docs/validation/2026-11-13-1581-s7-identity-fields/closure_touched.py 61c33cc2f HEAD`
names the nextpnr producers hit (Apple II, C64, Spectrum, Atari ST, ZX81).
It does not know the Quartus producers, so `git grep fes_computer_mailbox`
over the producers adds `quartus_fes_pcxt.py` and `build_fes_zx81_quartus.py`.
Rows are artefacts, not cores: Spectrum and ZX81 are four machines each.
Apple II is `check_level: reduced` and owes condition 1 only; the rest are full.

## Condition 1: netlists

Each row's netlist with `BUILD_ID` pinned to 32 zeros on both sides, two
passes. nextpnr rows run each producer's own Yosys program through
`netlist_digest.py` (copied from the #1862 record, Apple II and Atari ST
added); `fes.pcxt-dos` runs its Quartus producer's Yosys elaboration
(package and DOS profiles) through `pcxt_netlist.py`, both sides in one work
path. Commands: `run_cond1.sh <base|head> <p1|p2> <core...>` and
`pcxt_netlist.py`; each side's summary is in `cond1/`. `summarise_cond1.py`
writes `cond1.json` and this table:

| Artefact | Base netlist | Head netlist | Inputs moved | Verdict |
|---|---|---|---|---|
| `fes.c64` | `e2d6b41b9ef3a8bc` | `e2d6b41b9ef3a8bc` | yes | byte-identical |
| `fes.apple2` | `0a9847b47616922c` | `0a9847b47616922c` | yes | byte-identical |
| `fes.atari-st` | `5b2c7bd4ea56a46e` | `5b2c7bd4ea56a46e` | yes | byte-identical |
| `fes.spectrum` | `29436bf7023a44f9` | `29436bf7023a44f9` | yes | byte-identical |
| `fes.spectrum128` | `bb9bedf8fd01c20d` | `e6362c043502ac9e` | yes | differs |
| `fes.spectrum-plus3` | `e7e24c520633d9ff` | `e7e24c520633d9ff` | yes | byte-identical |
| `fes.pentagon` | `2d81474ad4151e4d` | `59fdbc7a56c74c5c` | yes | differs |
| `fes.zx81` | `bbaa42dd69f16fc3` | `bbaa42dd69f16fc3` | yes | byte-identical |
| `fes.zx80` | `3e51fd9384cf81d1` | `3e51fd9384cf81d1` | yes | byte-identical |
| `fes.ts1000` | `35d6e5536e98fc05` | `35d6e5536e98fc05` | yes | byte-identical |
| `fes.ts1500` | `22c418180d11bb50` | `22c418180d11bb50` | yes | byte-identical |
| `fes.pcxt-dos` | `06b2e8cf5d1d7a18` | `06b2e8cf5d1d7a18` | yes | byte-identical |

Every row's pinned inputs differ between the sides while ten netlists do not,
and both passes agree on every side.

**ZX81 family.** Its registered producer is Quartus Lite reading the RTL
itself, so D12's object for it is the QSF, whose path did not change while the
mailbox's content did. That comparison says nothing here. The rows above are
the advisory nextpnr producer's netlists; for the registered artefacts only
the RBF comparison (condition 2) carries the argument.

### Line numbers (fixed)

At `5419caf3f` the C64 netlist differed from main in 814 `src` attributes,
each a `fes_computer_mailbox.v` line number 3 or 4 higher (`probes` and the
C64 rows in `cond1/` under `c64-head-5419caf3f` on Powerboat): the edit added
four lines above the module's logic. D12 exempts `src` paths, not line numbers.
`fbb3b13d1` keeps every line in place, and the C64 netlist is byte-identical.

### Spectrum 128 and Pentagon (moved; reseal and HIL)

These two netlists differ from main in content (LUT masks and nets), the same
on both passes. Probes, each a local-only commit in a scratch checkout
(`probes/`):

| Probe | Spectrum 128 | Reading |
|---|---|---|
| main, from the head's checkout path | `bb9bedf8…` = main | not the path (DeanoC/yosys#80 ruled out) |
| both sides in one checkout path | head `e6362c04…` | same as two paths |
| main + the 15 new defines only | `bb9bedf8…` = main | the header is inert |
| head, defines moved to the header's end | `e6362c04…` | not the header's line positions |
| head, `ENABLE_PC_FLOPPY` declared last | `e6362c04…` | not the parameter's position |
| head, `ENABLE_PC_FLOPPY` declared and unused | `e6362c04…` | declaring the parameter moves it |

Declaring one more parameter on the mailbox, at 0 and unused, changes how Yosys
maps these two machines; the logic is the same (every instance sets parameters
by name, and the capability word is unchanged). It is the effect #1821 measured
for comment-only edits. Deano chose on 2026-11-22 to keep the parameter and
give `fes.spectrum128` and `fes.pentagon` the normal reseal and HIL rather
than gate it behind a define.

## Conditions 2 and 3

Not yet run. Both are taken at the head that merges, after #2077 and the pc386
reseal: condition 3 never carries across a base move, and condition 2 is
re-measured if a merge moves a row's route-deciding inputs.

## Merge forward to `904140f8f`

Main was merged into the branch at `f153f04b7` (new base `904140f8f`).
`closure_touched.py 61c33cc2f 904140f8f` hits two of these producers:
`build_fes_c64_oss` (C64 RTL and its producer) and `build_fes_spectrum_oss`
(`cores/fes-spectrum/diagnostic/firmware.py`). No toolchain lock or bootstrap
changed. So, under D12's merge-forward clause:

- **Re-measured** at `904140f8f` against `f153f04b7`, two passes (`cond1-newbase/`):
  `fes.c64` `0d107b7f3a2b24de` (main's own netlist moved), `fes.spectrum`
  `29436bf7023a44f9`, `fes.spectrum-plus3` `e7e24c520633d9ff` and
  `fes.pcxt-dos` `06b2e8cf5d1d7a18`, each byte-identical, base to head.
  PC/XT was not hit, and was re-measured anyway since it costs no route.
- **Carried:** `fes.apple2`, `fes.atari-st` and the four ZX81-family machines,
  whose compiled sources and route-deciding inputs did not change between the
  two bases.
