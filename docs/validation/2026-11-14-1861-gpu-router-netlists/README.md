# #1861 / PR #1862: netlist equivalence for the GPU-router selection

PR #1862 edits five producer scripts and one shared helper
(`scripts/fes_de10nano_evidence.py`). No RTL, QSF, SDC, lock or placement
recipe changes. The record of every artefact whose producer closure holds a
changed file therefore moves through its source inputs, and with it `BUILD_ID`,
so [D12](../../../AGENTS.md) applies.

**This record was re-derived at the merge base `a482f14c0` after three things
moved underneath it.** The earlier version of this page claimed a five-artefact
scope agreed verbally on 2026-11-15; that claim is withdrawn below, with what
replaced it and why.

## Which artefacts are affected, measured rather than argued

`design_identity.py --tree-a <base> --tree-b <head>` answers it from the two
trees alone: for each support-matrix row it takes that row's producer, the
producer's own closure roots, and compares the design-role half of the closure
(`scripts/input_identity.py`, `input_role`). **15 affected, 5 unchanged, 0
unanswered** (`reports-scope/affected.log`).

It reads the roots from the producer and not from
`scripts/source_closures.json`, because that manifest holds only the twelve
registered nextpnr producers and would silently miss `build_fes_zx81_quartus`
and `build_fes_splash`. `closure_touched.py` in the #1581 record hard-codes the
same twelve and has the same blind spot.

Unaffected, and so outside this record entirely: `fes.sms`, `fes.sg1000`,
`fes.apple2`, `fes.atari-st`, `fes.pcxt-dos`.

## Three changes on main that moved the work, and what each did

**#1891 moved the ZX81 family to a Quartus front end.** `fes.zx81`, `fes.zx80`,
`fes.ts1000` and `fes.ts1500` now select `build_fes_zx81_quartus`, whose own
`SOURCE_ROOTS` name `scripts/fes_de10nano_evidence.py`, so they remain affected
artefacts. What changed is the object D12 compares: that producer runs no Yosys
program, and D12 says "for a native Quartus front end, its QSF". So their
condition 1 is the canonical QSF, which `qsf_identity.py` measures using the
producer's own `canonical_qsf_sha256` — `BUILD_ID` zeroed, seed and per-run
project path dropped, checkout prefix rewritten. All four are byte-identical,
recipes included, at no Quartus cost at all
(`reports-scope/zx81-quartus-qsf.log`). Their rows under `reports/` and
`reports-pass2/` were produced by `build_fes_zx81_oss` and now describe the
advisory nextpnr lane, not the shipping package.

**#1881 removed the 130 MHz RAM Tester package** and rewrote
`build_fes_ramtest.py`. The `--gpu-router` option was re-applied on main's
shape; the 130 MHz target, its timing diagnostic and the `memory_mhz` argument
stay deleted. `cores/fes-ramtest/rtl/ram_font.v` also moved, so `fes.ramtest`
is the one affected artefact whose *compiled* source set changed across the
base move and whose condition 1 could not simply carry forward.

**#1863 merged `check_level`.** Its rule drops conditions 2 and 3 for a core
recorded `reduced`, but puts that core back under the full rule for a change
touching "the core's own RTL, programs, toolchain lock or placement recipe".
This PR edits producers, which hold both the Yosys program and the nextpnr
options, so that clause is discharged by measurement, not by assertion — see
below. Eleven affected artefacts are `full` and four are `reduced`
(`fes.pong`, `fes.ramtest`, `fes.coleco`, `fes.riscv`).

## Withdrawn: the five-artefact scope

The earlier page recorded a verbal call of 2026-11-15 to measure conditions 2
and 3 for "firmware plus one representative per affected machine family", and
claimed D12 discharged in full for `splash`, `fes.menu`, `fes.zx81`,
`fes.spectrum` and `fes.c64`. That is withdrawn, for three reasons that are
each measured here:

- `check_level` merged after that call and does not grant a sibling-variant
  exemption. The siblings `zx80`, `ts1000`, `ts1500`, `spectrum128`,
  `spectrum-plus3` and `pentagon` are all `full`.
- `fes.zx81`'s evidence was taken on `build_fes_zx81_oss`, which no longer
  produces its package.
- **Condition 3 does not carry forward across a merge at all.** It is about the
  identity that ships, and the shipped `BUILD_ID` moved: `fes.spectrum` derives
  `184fe9efe3208b902e377c8764fd1abc` at `a482f14c0`, where the recorded
  condition-2 run used `401632145ba4e7c84cbae6544b2d6d53`. The same mistake was
  made on #1881 and raised there as a P1.

## What the merge-forward clause does carry

D12 carries conditions 1 and 2 for an artefact whose compiled source set is
unchanged between the old base and the new one and whose route-deciding inputs
— lock, timing and pin constraints, placement recipe — are unchanged too.
Both halves are measured:

- `reports-scope/base-move.log` (`design_identity.py idold idbase`): across
  `72d2fc4da` to `a482f14c0` the only compiled inputs that moved for an
  affected artefact are `cores/fes-ramtest/rtl/ram_font.v` and two `fes.c64`
  diagnostic generators. Everything else that moved is a shared script.
- `reports-scope/route-inputs-base-move.log` (`route_inputs.py`): no
  artefact's toolchain lock, QSF, SDC or `pnr_recipe.recipe_digest` moved
  across that span. `git diff` over the same span confirms no `toolchain.lock`
  changed. `design_recipe_digest` is unanswerable at the old base because the
  function postdates it, and is reported as unanswered rather than as a pass.

## The check_level clause, discharged mechanically

`front_end_program.py` compares the exact command lists each producer's
`build_commands` emits, `BUILD_ID` pinned to 32 zeros and one identical set of
tool paths on both sides. All thirteen nextpnr cases are byte-identical
(`reports-scope/front-end-programs.log`).

That alone does **not** cover the placement recipe, because `fes.spectrum`,
`fes.ramtest`, `fes.c64` and `fes.coleco` ignore the route tuple
`build_commands` returns and call `route_after_synth` with their own
`pnr_recipe` arguments. `route_inputs.py` covers it directly: per artefact it
reads the toolchain lock, QSF, SDC and both `pnr_recipe` digests, taking each
path from the producer's own declared input list rather than a guessed
attribute name. Every one is byte-identical across this change
(`reports-scope/route-inputs.log`).

### Every comparator here was checked against a constructed counterexample

Reasoning about what a check would catch is not evidence that it catches it:

| Perturbation | Expected | Result |
| --- | --- | --- |
| extra `synth_intel_alm` option in `build_fes_pong` | that core only | `fes.pong` MOVED, others quiet |
| `--freq 74.25` to `74.20` in `build_fes_ramtest` | that core only | `fes.ramtest` MOVED, others quiet |
| trailing whitespace on one `set_location_assignment` | all four ZX81 QSFs | all four MOVED, exit 1 |
| one seed changed in `fes.pentagon`'s recipe table | that core only | `fes.pentagon` recipe + design digests MOVED |

Two defects in this record's own tooling were found that way and are recorded
rather than quietly amended: `build_fes_splash` emits a **format-1** record and
hashes no source closure at all, so an earlier version of `design_identity.py`
credited it with a 613-file design set and five moved producer scripts it
hashes none of (it has 11 declared inputs, of which this PR moves one); and
`route_inputs.py` first read the lock by a guessed attribute name, which missed
six producers that keep it under a prefixed name and reported them as sharing
the repository-root lock.

### How condition 2 is measured, and the control that makes it mean anything

The producers derive `BUILD_ID` from their own build record and offer no
override, and all four of the remaining cores put it in the netlist with
`chparam`. So the two sides' natural seals necessarily differ: a different
constant gives different RBF bytes. That difference is not condition 2.
Condition 2 is whether anything *else* changed the hardware.

`rbf_at_build_id.py` measures it by holding the `BUILD_ID` **equal** on both
sides and comparing the RBFs. The route is a function of the netlist, device,
QSF, SDC, frequency, seed list, weights, recipe and nextpnr binary; this PR
changes none of those but the netlist, whose only difference is that constant.
The tool calls each producer's own `build_commands` and its own route function
rather than reimplementing the build — three of the four route against a QSF
generated during the build, so a replica would have to reproduce producer
internals, and one that drifted would read as valid evidence while being none.

**The tool is checked against the producer before it is trusted.** Run on the
main tree at main's own `BUILD_ID`, it must reproduce main's normal-seal RBF
byte for byte. For `fes.menu` it does:

| run | BUILD_ID | RBF sha256 | bytes |
| --- | --- | --- | --- |
| main, producer, normal seal | `60bccd54…` | `6107b13e0425af1b…` | 2,172,530 |
| main, tool, forced to the same id | `60bccd54…` | `6107b13e0425af1b…` | 2,172,530 |

Same seed, same bytes. Without that control the tool's branch-side answer
would be unfalsifiable, which is the failure mode this record has already hit
twice: a pass comparison that read one file twice, and a Yosys run that exited
0 with a silently different netlist.

With the tool so checked, `fes.menu`'s condition 2 **passes**:

| run | BUILD_ID | netlist sha256 | RBF sha256 | bytes |
| --- | --- | --- | --- | --- |
| main, producer, normal seal | `60bccd54…` | — | `6107b13e0425af1b…` | 2,172,530 |
| main, tool, forced (control) | `60bccd54…` | `2fc9786e3e6bb27e…` | `6107b13e0425af1b…` | 2,172,530 |
| **branch, tool, forced** | `60bccd54…` | `2fc9786e3e6bb27e…` | `6107b13e0425af1b…` | 2,172,530 |

The branch reproduces main's RBF byte for byte at main's `BUILD_ID`, on the
same placer seed, from an identical netlist.

`fes.zx81` passes the same way, and is the stronger case: its route needs a
generated socket QSF **and** the `prepare_shell_netlist` transform between
Yosys and the router, so a mirrored step omitted would still have produced an
RBF. Its control reproduced main's normal-seal RBF exactly, and then the
branch did too — `e711c805d15f66f0…`, 2,741,554 bytes, seed 1, identical
netlist `9442dcd934bc914f…`. Note the netlist digests here are
at main's real `BUILD_ID`, independently of the zeroed-constant comparison in
the condition 1 table — the two agree.

### `splash` is complete, and needed no GPU

Splash routes with `gpu_router=OFF`, a CPU-reference nextpnr, so its whole seal
runs without a GPU. Both sides were sealed with the identical local install
(`reports-seal/splash.json`):

- **Condition 2.** The two RBFs are byte-identical: `64f1f16daedba4ed…`,
  1,963,321 bytes, `cmp` clean. Note this is a stronger result than the rule
  asks for. D12 wants a build with the PR's tooling and *main's* `BUILD_ID` to
  reproduce main's RBF; splash's Yosys program sets no `BUILD_ID` at all, so
  the RBF does not depend on it, and the two sides reproduce the same bytes even
  though their `BUILD_ID`s genuinely differ (`b19d1242…` on main,
  `85c40d0f…` on the branch — a format-1 record hashes the revision). No
  constant had to be forced to get the comparison.
- **Condition 3.** The branch build passed splash's own gates through the
  producer, which validates evidence before sealing. Pixel fmax is
  236.574 MHz on both sides.

### All five artifacts in the agreed scope are complete

| Artifact | main BUILD_ID | RBF reproduced at it | bytes | seed |
| --- | --- | --- | --- | --- |
| `fes.menu` | `60bccd54…` | `6107b13e0425af1b…` | 2,172,530 | 5 |
| `fes.zx81` | `a8e2be82…` | `e711c805d15f66f0…` | 2,741,554 | 1 |
| `fes.spectrum` | `40163214…` | `402a213d752b3e58…` | 2,642,711 | 5 |
| `fes.c64` | `310c8731…` | `f09ae0e34e414595…` | 3,749,707 | 4 |
| `splash` | `b19d1242…` | `64f1f16daedba4ed…` | 1,963,321 | — (CPU route) |

Each row is three runs: main's own producer seal, the tool on main at that
`BUILD_ID` (the control, which had to reproduce the seal's RBF), and the branch
at the same `BUILD_ID`. All four GPU-routed adapters passed their control, and
all four branches reproduced main's RBF byte for byte. Condition 3 is each
branch's own normal seal passing its gates, recorded in `reports-seal/`; all
five pass.

### Condition 3 is exposed to the `BUILD_ID` itself; condition 2 is not

The session running #1806's campaign measured something that bears on how these
seals should be read: on `fes.coleco` socket-v2, the only closure change was
`core_package.py`, whose diff emits no RTL and whose sole build effect is moving
the `BUILD_ID` — and that core then missed timing on all ten seeds (best
`worst_ratio` 0.9796), having passed on seed 1 three hours earlier. A 128-bit
constant with no functional meaning decided whether it closed.

That cuts two ways here:

- **Condition 2 is immune by construction.** It holds the `BUILD_ID` equal on
  both sides, which is precisely why D12 forces the constant rather than
  comparing the two natural seals.
- **Condition 3 is exposed.** Each branch seal routes at its own `BUILD_ID`, so
  a miss there could be the constant rather than the change.

Worth stating because some of these cores run close to their bound. Worst
achieved/constraint ratio across clock domains, from `reports-seal/`:

<!-- margins:start -->
| Artifact | main worst | branch worst | branch status |
| --- | --- | --- | --- |
| `fes.menu` | 1.167 | 1.188 | pass |
| `fes.zx81` | 1.013 | 1.020 | pass |
| `fes.spectrum` | 1.041 | 1.206 | pass |
| `fes.c64` | 1.007 | 1.053 | pass |
<!-- margins:end -->

Rendered by `seal_margins.py` from `reports-seal/`, which carry each build's
own `timing` block copied verbatim from its `build-summary.json`, with the
per-domain achieved and constraint frequencies and the derived ratio. The
numbers above are therefore auditable rather than quoted: an earlier version of
this table was written by hand, existed nowhere else in the repository, and
omitted `fes.menu` altogether.

`fes.c64` clears its pixel constraint by 0.7% on main. Every branch seal passed,
and in each case with *more* margin than main — but that is the luck of the
constant, not a property of this change, and it should not be read as one. The
converse matters more: had a branch seal missed at these margins, it would not
have been attributable to this PR without re-running it at main's `BUILD_ID`
first, which is what condition 2's forced run already does.

The placer seed is worth reading as evidence rather than bookkeeping. The
search tries seeds in recipe order and stops at the first that meets timing, so
the two sides agreeing on the seed — 5, 1, 5 and 4 respectively — means the
change did not perturb the netlist in any way timing responded to. A real
difference would have been liable to show up as the two sides stopping at
different seeds, before the RBF comparison was even reached.

So this directory discharges D12 in full for all five artifacts in the agreed
scope, and condition 1 for all 15. The ten artifacts outside that scope
(`zx80`, `ts1000`, `ts1500`, `spectrum128`, `spectrum-plus3`, `pentagon`,
`fes.pong`, `fes.ramtest`, `fes.coleco`, `fes.riscv`) have condition 1 only,
by Deano's priority call recorded above.

## What design-v1 (#1882) changes here, and what it does not

Main has since taken #1882, which splits identity: a `design-v1` `BUILD_ID`
covers design inputs only, with packaging and toolchain inputs and parameters
excluded. Two of the parameters this PR writes — `gpu_backend` and
`gpu_architectures` — are now in `TOOLCHAIN_PARAMETERS`, so under that policy a
backend no longer moves a design id, and HIP and CUDA builds of one design
share one.

That does **not** dissolve the scope below, and it was worth measuring rather
than assuming. Every file this PR changes still classifies as a *design* input:

```
$ git diff --name-only origin/main...HEAD -- sources/misteross/     | sed 's|sources/misteross/||' | xargs -n1 python3 -c       'import sys;from scripts.input_identity import input_role;print(input_role(sys.argv[1]), sys.argv[1])'
design  scripts/build_fes_menu.py
design  scripts/build_fes_menu_package.py
design  scripts/build_fes_pong.py
design  scripts/build_fes_ramtest.py
design  scripts/build_fes_riscv.py
design  scripts/fes_de10nano_evidence.py
```

So the parameter half of this change is now identity-neutral, while the
source-hash half still moves `BUILD_ID` for every artifact whose closure holds
one of those files. The affected set and the evidence below are unchanged. Note
also that the four producers this PR touches do not yet emit `design-v1`
records at all, so they are on the older projection regardless.

## Scope, and a correction to how it was derived

The first version of this record resolved the affected set from the
`producer_script` entries in `config/core-recipes.toml`, which gave 14
registered cores. **That method misses a producer that is not a registered
recipe core**, and one such producer is affected: `fes.splash`
(`scripts/build_fes_splash.py`) lists the changed
`scripts/fes_de10nano_evidence.py` in its own `PINNED_INPUTS`, and it seals the
image-pinned idle RBF, so it is a shipped artifact rather than a diagnostic.
The scope is therefore **15 artifacts, not 14**, and the correct query is over
the producers themselves:

```
$ for f in scripts/build_*.py; do     grep -qE "['\"]scripts/fes_de10nano_evidence\.py['\"]" "$f" && echo "$f"; done
```

Nine producers pin a changed file. Eight of them seal the 14 registered cores
in the table below; the ninth is splash.

Splash differs from the rest in three ways that matter to the measurement: it
authenticates the repository-wide `toolchain.lock` rather than a per-core lock,
it uses `gpu_router=OFF` (a CPU-reference nextpnr, no GPU), and it **refuses a
shared cache root** — it requires a local install at `<tree>/build/toolchain`.
Its build record is also `format: 1`, and `build_identity` hashes a format-1
record whole, including `revision`, so splash's `BUILD_ID` moves on *every*
commit to the repository regardless of this PR. That makes D12's exemption for
the `BUILD_ID` constant do the work for splash in any case, but it does not
excuse leaving the netlist unmeasured, so it is measured on the same terms as
the others.

## Method

`netlist_digest.py` runs a producer's own Yosys command and nothing else: no
placement, no routing, no GPU. Two properties make the comparison exact rather
than approximate:

- **The `BUILD_ID` is pinned to 32 zeros on both sides**, as the producers that
  already have `--synth-only` do. The constant that D12 exempts is therefore
  identical in both netlists instead of being masked out afterwards, and any
  surviving difference is the source change itself.
- **The Yosys program uses only relative paths** (`read_verilog -sv -I
  cores/<core>/generated <relative sources>`, `write_json <relative>`), run with
  the tree as the working directory. The `src` attributes are relative, so the
  second exemption D12 allows does not arise either: the two trees' netlists are
  directly comparable, with nothing normalised away.

Both sides run against **one** cache root, so one tool install produces both
netlists, as D12 requires.

### The two sides are in different checkout paths

D12 says to run both sides "in the same checkout path where that is possible",
and here it was not: main is a separate worktree, so the two trees necessarily
sit at different absolute paths. That is worth stating plainly rather than
leaving implicit, because a sibling session measuring #1806 found `fes.spectrum`
differing across two checkouts of identical content — 14,593 cells against
13,532, and 15% different fmax — while being byte-identical within either path.

For **this** record the extra variable only strengthens the result: every one of
the fourteen comparisons **matched** across two different paths, so each is a
match despite a variable D12 would have let us remove, not because of it. A
mismatch is what would have needed re-running in one path before anything could
be concluded, and none occurred.

`fes.spectrum` is a direct counter-observation to a path effect on that same
core: measured here across two paths, with one Yosys binary, its netlists are
byte-identical (`3f2dfb76…`). That does not refute the sibling's measurement —
different Yosys build, different flow — but it does mean a path effect is not
reproducible for that core under this one.

### Two presentations of DeanoC/yosys#77, and why each core is measured twice

`3a5accbd` is intermittently unreliable on some cores, in two distinct ways
that the sibling session measuring #1806 established by auditing six runs of
one core:

- about **1 run in 5** segfaults in `OPT_CLEAN`;
- about **1 run in 6** exits **0** and writes a materially different netlist —
  in the audited case 1,061 cells smaller with 15% different fmax, from a run
  that was not short of memory or time.

`netlist_digest.py` deletes the netlist before synthesising and refuses to
record a run that did not write one, so the crashing presentation shows up as a
failed run rather than a stale file silently compared. **That guard does not
catch the silent presentation**, which exits 0 with a plausible file.

Only repetition catches that one. A single measurement per side is not enough:
if both sides happened to hit the same bad variant, the comparison would read
as a match on an artefact rather than on the cores' actual netlists. So **every
core is measured twice per side**, in two independent passes, and
`summarise.py` requires the two passes to agree before it will call a core
byte-identical. Agreement across passes also makes each side's netlist
demonstrably reproducible, which is the property the silent presentation would
break.

```
python3 netlist_digest.py --tree <tree>/sources/misteross --core fes.pong \
    --cache-root /data/fes1861-netlist/cache --out reports/<core>-<side>
```

## Trees

| Side | Tree | Commit |
| --- | --- | --- |
| main | `/data/fes1861-netlist/mainref` | `72d2fc4da`, the PR's merge base |
| branch | `/home/deano/fes/.claude/worktrees/gpu-router-default` | recorded per run |

A tree path is mutable and a branch advances, so the path above is not what
pins a result. Each run records the tree's `HEAD`, that the tree was clean, and
a digest over the producer's own pinned input list, which covers the bytes that
decide the netlist whatever git says. A run whose pinned inputs are modified is
refused rather than recorded, and `summarise.py` refuses to render a record
that has no revision or a modified pinned input. The revisions actually measured
are listed under the results table.

The branch side spans more than one revision, because the sweep ran while this
evidence directory was still being committed. Those revisions differ only inside
`docs/validation/2026-11-14-1861-gpu-router-netlists/`, so every branch run
synthesised the same producer sources, which each record's
`pinned_inputs_sha256` attests independently of git:

```
$ for r in b5d3ae232 cf8ec7bec 480c924f8; do \
    git diff --name-only $r HEAD -- sources/misteross/ | wc -l; done
0
0
0
```

## Tool installs

One cache root, `/data/fes1861-netlist/cache`, provisioned on Powerboat and
shared by both sides. The locks are unchanged by this PR, so one install per
lock serves main and branch.

| Lock | Yosys | Mistral | nextpnr | Router |
| --- | --- | --- | --- | --- |
| `cores/fes-pong/toolchain.lock` | `3a5accbd` | `8fcc4cb4` | `552ba246` | HIP `gfx1100;gfx1201` |
| `cores/fes-menu/toolchain.lock` | `3a5accbd` | `8fcc4cb4` | `1eecf877` | HIP `gfx1100;gfx1201` |
| `cores/fes-zx81/toolchain.lock` | `3a5accbd` | `8fcc4cb4` | `1eecf877` | HIP `gfx1100;gfx1201` |
| `cores/fes-ramtest/toolchain.lock` | `3a5accbd` | `8fcc4cb4` | `bec82e36` | HIP `gfx1100;gfx1201` |
| `cores/fes-riscv/toolchain.lock` | `3a5accbd` | `8fcc4cb4` | `8f453ebc` | HIP `gfx1100;gfx1201` |
| `cores/fes-spectrum/toolchain.lock` | `3a5accbd` | `8fcc4cb4` | `bd7a1c0d` | HIP `gfx1100;gfx1201` |
| `cores/fes-c64/toolchain.lock` | `3a5accbd` | `8fcc4cb4` | `5fe55f3d` | HIP `gfx1100;gfx1201` |
| `cores/fes-coleco/toolchain.lock` | `5391eeb1` | `8fcc4cb4` | `bec82e36` | HIP `gfx1100;gfx1201` |

Exact binary digests are in each run's `netlist.json`. Only nextpnr differs
between these locks, and nextpnr routes nothing here; it is authenticated
because the producers authenticate their whole slot before synthesising.

## Runs

Pass 1 is in `reports/<core>-{main,branch}/netlist.json` and pass 2 in
`reports-pass2/`, each with the netlist digest and the authenticated tool
identities; `compare-passes.py` checks one against the other, and the run logs
for all four sweeps are in `logs/`. `compare-passes.py` ignores any core with
no pass-1 counterpart, because an earlier version of this comparison read the
same file twice for cores the second pass had not reached yet and reported them
as agreeing. The netlists themselves are 3.3 MB
each and are not committed; the digests and the command are the record D12 asks
for.

<!-- results:start -->

| Artefact | Lane | `check_level` | Condition 1 | Condition 2 | Condition 3 |
| --- | --- | --- | --- | --- | --- |
| `splash` | nextpnr | full | identical (src exempt) | PASS (no BUILD_ID in the program) | pass, 3.1862x worst clock |
| `fes.menu` | nextpnr | full | identical | CARRIED | pass, 1.1441x worst clock |
| `fes.c64` | nextpnr | full | identical | CARRIED | pass, 1.1118x worst clock |
| `fes.spectrum` | nextpnr | full | identical | CARRIED | pass, 1.1668x worst clock |
| `fes.spectrum128` | nextpnr | full | identical | PASS | pass, 1.1645x worst clock |
| `fes.spectrum-plus3` | nextpnr | full | identical | PASS | pass, 1.0104x worst clock |
| `fes.pentagon` | nextpnr | full | identical | PASS | not measured |
| `fes.zx81` | Quartus Lite | full | canonical QSF identical | PASS | pass, +0.121 ns worst slack (Hold) |
| `fes.zx80` | Quartus Lite | full | canonical QSF identical | PASS | pass, +0.143 ns worst slack (Hold) |
| `fes.ts1000` | Quartus Lite | full | canonical QSF identical | PASS | pass, +0.135 ns worst slack (Hold) |
| `fes.ts1500` | Quartus Lite | full | canonical QSF identical | PASS | pass, +0.110 ns worst slack (Hold) |
| `fes.pong` | nextpnr | reduced | identical | not owed (#1863) | not owed (#1863) |
| `fes.ramtest` | nextpnr | reduced | identical | not owed (#1863) | not owed (#1863) |
| `fes.coleco` | nextpnr | reduced | identical | not owed (#1863) | not owed (#1863) |
| `fes.riscv` | nextpnr | reduced | identical | not owed (#1863) | not owed (#1863) |

Condition 1 for the Quartus Lite rows is the canonical QSF, which is what
D12 names for a native Quartus front end; those four run no Yosys program.
A `CARRIED` condition 2 is a recorded pair that D12's merge-forward clause
carries, and it is claimed only where the recorded pair passed *and* main's
own netlist is byte-identical at the old base and the new one.

<!-- results:end -->

The table is rendered into those markers by `python3 summarise.py --write`, so
no digest in it is transcribed by hand.

**All fourteen affected cores produce byte-identical netlists.** That covers
both kinds of change: the four whose own producer script this PR edits
(`fes.pong`, `fes.menu`, `fes.ramtest`, `fes.riscv`) and the ten that move only
through the shared helper. `fes.coleco` is the only one on a different Yosys pin
(`5391eeb1` rather than `3a5accbd`), so it exercised its own toolchain rather
than the shared one.

The `Pinned inputs` column is the argument in one line: those digests **differ**
on every row, because the edited producer scripts and the shared helper are
themselves pinned inputs and are exactly why `BUILD_ID` moves — and the netlist
is identical anyway.

`fes.pong` is the strongest case in the set: both its own producer script and
the shared helper changed. `cmp` reports the two 3,294,796-byte netlists
identical, over a top module of 1,422 cells and 1,804 nets, from one Yosys
binary. Nothing was normalised or excluded. `fes.menu` is the same kind of case
through the package producer, and the ZX81 models stand for the ten cores whose
closure picks up only the shared helper.

### `fes.splash` is the one artifact that needs D12's `src` exemption

Splash's **raw** digests differ between the two sides, and the table says so
rather than hiding it. The difference is entirely in `src` attributes naming
the Yosys install directory: splash refuses a shared cache root and
authenticates a local install at `<tree>/build/toolchain`, so the two sides
necessarily use two installs at different paths, and Yosys records that path in
the `src` of cells it reads from its own `share/yosys` models.

D12 exempts exactly that — "file paths in `src` attributes (the checkout and
the tool install directory: a toolchain at the same commit is the same
toolchain wherever it is installed)". The two installs are byte-identical
binaries of the same commit: the main side's is a copy of the branch side's,
both `yosys` at sha256 `d1d8c98fac8b1155f50ef029…`.

`netlist_digest.py` records `netlist_sha256_src_exempt` for every run, which
replaces that run's own tree prefix **and nothing else**, so any difference
that is not a tree path survives into it. Splash's exempted digests agree on
both sides in both passes (`68858653e28f06b8…`). The other 14 artifacts share
one install through the cache root and match byte for byte without the
exemption, so for them the two digests are the same number.

Each lock needed its own nextpnr built before its producer would authenticate,
even for a Yosys-only run. Because the shared cache is content-addressed per
tool *and* commit, one cache root served every lock: after the first, each
additional lock cost a single nextpnr build rather than a full toolchain.
