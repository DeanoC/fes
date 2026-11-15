# #1861 / PR #1862: netlist equivalence for the GPU-router selection

PR #1862 edits five producer scripts and one shared helper
(`scripts/fes_de10nano_evidence.py`). No RTL, QSF, SDC, lock or placement
recipe changes. The record of every core whose producer imports a changed file
therefore moves through `source_inputs`, and with it `BUILD_ID`, so
[D12](../../../AGENTS.md) applies.

D12 asks for three things. This record covers the **first** only:

1. the core's Yosys netlist is byte-identical to main apart from the `BUILD_ID`
   constant and `src` file paths — **done, for all 14 cores, in two agreeing
   passes, and with neither exemption needed**: the netlists are identical
   outright;
2. a build with the PR's tooling and main's `BUILD_ID` reproduces main's RBF
   byte for byte — **not started** (needs a GPU route per core);
3. the PR's own build passes the core's normal timing and seal gates —
   **not started** (needs a GPU route per core).

## Agreed scope for conditions 2 and 3

Deano scoped the route-based evidence on 2026-11-15: **firmware plus one
representative per affected machine family**, rather than all 15 artifacts.
That is `fes.menu`, `splash`, `fes.zx81`, `fes.spectrum` and `fes.c64`. The
sibling variants (`zx80`, `ts1000`, `ts1500`, `spectrum128`, `spectrum-plus3`,
`pentagon`) share a producer, a lock and a constraint set with their
representative and have identical netlists, and the held-back cores (`fes.pong`,
`fes.ramtest`, `fes.coleco`, `fes.riscv`) are not shipped. Their conditions 2
and 3 are deliberately not measured; that is a priority call, recorded here so
the gap is explicit rather than silent.

| Artifact | Condition 1 | Condition 2 | Condition 3 |
| --- | --- | --- | --- |
| `splash` | done (src exempt) | **done** | **done** |
| `fes.menu` | done | **done** | **done** |
| `fes.zx81` | done | **done** | **done** |
| `fes.spectrum` | done | pending | pending |
| `fes.c64` | done | pending | pending |

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

So this directory discharges D12 in full for `splash`, and condition 1 only for
the other 14.

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
| Core | Changed files in its closure | Pinned inputs | Netlist sha256 | Two passes | Match |
| --- | --- | --- | --- | --- | --- |
| `fes.c64` | `fes_de10nano_evidence.py` | differ | `1b7efc74cd4f637d0e0ecdafc52ac06c49664de7ded7974938f07e0a7567ef83` | agree | **byte-identical** |
| `fes.coleco` | `fes_de10nano_evidence.py` | differ | `58e3e313595a9d481a3b43544766d145c56d92ff519f7955fd3c8699ffa52844` | agree | **byte-identical** |
| `fes.menu` | `build_fes_menu_package.py`, `build_fes_menu.py`, `fes_de10nano_evidence.py` | differ | `05ab4ada678920c408b045026fbd88073f05152867ba628ff253757bc23d0a31` | agree | **byte-identical** |
| `fes.pentagon` | `fes_de10nano_evidence.py` | differ | `b7e23071f5bb22020bd14c58b03b1e6a2b65b9254b7cf10ce52a3ce81b7bf334` | agree | **byte-identical** |
| `fes.pong` | `build_fes_pong.py`, `fes_de10nano_evidence.py` | differ | `e7948651bcc0cc2ffbbd3c52c8643c642bc6964826e2a0ae65d18e3fd90cf12b` | agree | **byte-identical** |
| `fes.ramtest` | `build_fes_ramtest.py`, `fes_de10nano_evidence.py` | differ | `0d59db94f2352a40c9b0e57ab69c6626ff5a3f7b33f5843ae9507e9bf3bf6c7a` | agree | **byte-identical** |
| `fes.riscv` | `build_fes_riscv.py`, `fes_de10nano_evidence.py` | differ | `e8db4d58afb81e7766d7e21c9ce6d1d18736196badb9dbc23fb742d8c4549be8` | agree | **byte-identical** |
| `fes.spectrum` | `fes_de10nano_evidence.py` | differ | `3f2dfb764a86b215ad4878203392ebd5823c2218e00e10f8f321eddaa85eccc7` | agree | **byte-identical** |
| `fes.spectrum-plus3` | `fes_de10nano_evidence.py` | differ | `2646f83230b81acf14bf7ca62bafd1f77aa30602874e05d713ec4c5f4afa4fce` | agree | **byte-identical** |
| `fes.spectrum128` | `fes_de10nano_evidence.py` | differ | `cd62753ae45dbfe930671831ed66ebec568ae4e0b1738fc3ee68ac5b2dda1957` | agree | **byte-identical** |
| `fes.splash` | `fes_de10nano_evidence.py` | differ | 68858653e28f06b85f4bf3249a2d5e169becb650e9f235d2627cc7f4c4f7410e (after D12's `src` path exemption) | agree | **identical, src paths exempt** |
| `fes.ts1000` | `fes_de10nano_evidence.py` | differ | `96bcf1f6411ab04324e86a58fd525bd5c722f3afbf02c7db67365be074cc6dca` | agree | **byte-identical** |
| `fes.ts1500` | `fes_de10nano_evidence.py` | differ | `d29bb54e119907959fb104890f8d2d038fff625cf84b2cdea09cc7525bbf37a7` | agree | **byte-identical** |
| `fes.zx80` | `fes_de10nano_evidence.py` | differ | `b7effcba3f7a30349ae55aef0eac6a857d00f04dc4fe1b959a0c482534df6f82` | agree | **byte-identical** |
| `fes.zx81` | `fes_de10nano_evidence.py` | differ | `b5c296e5c76810f5655e70a945518dd511654e8b450fb7bbbb616e442f15604b` | agree | **byte-identical** |

Revisions measured on main: `72d2fc4da`.
Revisions measured on branch: `480c924f8`, `760d8a8f0`, `b5d3ae232`, `cf8ec7bec`.
Each row's pinned inputs were unmodified at the revision shown; `summarise.py` refuses to render a record with no revision or with a modified pinned input, and the driver refuses to write one.
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
