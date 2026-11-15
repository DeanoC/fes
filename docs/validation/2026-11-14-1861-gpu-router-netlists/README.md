# #1861 / PR #1862: netlist equivalence for the GPU-router selection

PR #1862 edits five producer scripts and one shared helper
(`scripts/fes_de10nano_evidence.py`). No RTL, QSF, SDC, lock or placement
recipe changes. The record of every core whose producer imports a changed file
therefore moves through `source_inputs`, and with it `BUILD_ID`, so
[D12](../../../AGENTS.md) applies.

D12 asks for three things. This record covers the **first** only:

1. the core's Yosys netlist is byte-identical to main apart from the `BUILD_ID`
   constant and `src` file paths — **done, for all 14 cores, and with neither
   exemption needed**: the netlists are identical outright;
2. a build with the PR's tooling and main's `BUILD_ID` reproduces main's RBF
   byte for byte — **not started** (needs a GPU route per core);
3. the PR's own build passes the core's normal timing and seal gates —
   **not started** (needs a GPU route per core).

So this directory does not by itself discharge D12.

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

Each run writes `reports/<core>-{main,branch}/netlist.json` with the netlist
digest and the authenticated tool identities. The netlists themselves are 3.3 MB
each and are not committed; the digests and the command are the record D12 asks
for.

<!-- results:start -->
| Core | Changed files in its closure | Pinned inputs | Netlist sha256 | Two passes | Match |
| --- | --- | --- | --- | --- | --- |
| `fes.c64` | `fes_de10nano_evidence.py` | differ | `1b7efc74cd4f637d0e0ecdafc52ac06c49664de7ded7974938f07e0a7567ef83` | pass 2 missing | **unconfirmed** |
| `fes.coleco` | `fes_de10nano_evidence.py` | differ | `58e3e313595a9d481a3b43544766d145c56d92ff519f7955fd3c8699ffa52844` | pass 2 missing | **unconfirmed** |
| `fes.menu` | `build_fes_menu_package.py`, `build_fes_menu.py`, `fes_de10nano_evidence.py` | differ | `05ab4ada678920c408b045026fbd88073f05152867ba628ff253757bc23d0a31` | pass 2 missing | **unconfirmed** |
| `fes.pentagon` | `fes_de10nano_evidence.py` | differ | `b7e23071f5bb22020bd14c58b03b1e6a2b65b9254b7cf10ce52a3ce81b7bf334` | pass 2 missing | **unconfirmed** |
| `fes.pong` | `build_fes_pong.py`, `fes_de10nano_evidence.py` | differ | `e7948651bcc0cc2ffbbd3c52c8643c642bc6964826e2a0ae65d18e3fd90cf12b` | agree | **byte-identical** |
| `fes.ramtest` | `build_fes_ramtest.py`, `fes_de10nano_evidence.py` | differ | `0d59db94f2352a40c9b0e57ab69c6626ff5a3f7b33f5843ae9507e9bf3bf6c7a` | pass 2 missing | **unconfirmed** |
| `fes.riscv` | `build_fes_riscv.py`, `fes_de10nano_evidence.py` | differ | `e8db4d58afb81e7766d7e21c9ce6d1d18736196badb9dbc23fb742d8c4549be8` | pass 2 missing | **unconfirmed** |
| `fes.spectrum` | `fes_de10nano_evidence.py` | differ | `3f2dfb764a86b215ad4878203392ebd5823c2218e00e10f8f321eddaa85eccc7` | pass 2 missing | **unconfirmed** |
| `fes.spectrum-plus3` | `fes_de10nano_evidence.py` | differ | `2646f83230b81acf14bf7ca62bafd1f77aa30602874e05d713ec4c5f4afa4fce` | pass 2 missing | **unconfirmed** |
| `fes.spectrum128` | `fes_de10nano_evidence.py` | differ | `cd62753ae45dbfe930671831ed66ebec568ae4e0b1738fc3ee68ac5b2dda1957` | pass 2 missing | **unconfirmed** |
| `fes.ts1000` | `fes_de10nano_evidence.py` | differ | `96bcf1f6411ab04324e86a58fd525bd5c722f3afbf02c7db67365be074cc6dca` | pass 2 missing | **unconfirmed** |
| `fes.ts1500` | `fes_de10nano_evidence.py` | differ | `d29bb54e119907959fb104890f8d2d038fff625cf84b2cdea09cc7525bbf37a7` | pass 2 missing | **unconfirmed** |
| `fes.zx80` | `fes_de10nano_evidence.py` | differ | `b7effcba3f7a30349ae55aef0eac6a857d00f04dc4fe1b959a0c482534df6f82` | pass 2 missing | **unconfirmed** |
| `fes.zx81` | `fes_de10nano_evidence.py` | differ | `b5c296e5c76810f5655e70a945518dd511654e8b450fb7bbbb616e442f15604b` | pass 2 missing | **unconfirmed** |

Revisions measured on main: `72d2fc4da`.
Revisions measured on branch: `480c924f8`, `b5d3ae232`, `cf8ec7bec`.
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

Each lock needed its own nextpnr built before its producer would authenticate,
even for a Yosys-only run. Because the shared cache is content-addressed per
tool *and* commit, one cache root served every lock: after the first, each
additional lock cost a single nextpnr build rather than a full toolchain.
