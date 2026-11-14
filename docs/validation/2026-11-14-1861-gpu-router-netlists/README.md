# #1861 / PR #1862: netlist equivalence for the GPU-router selection

PR #1862 edits five producer scripts and one shared helper
(`scripts/fes_de10nano_evidence.py`). No RTL, QSF, SDC, lock or placement
recipe changes. The record of every core whose producer imports a changed file
therefore moves through `source_inputs`, and with it `BUILD_ID`, so
[D12](../../../AGENTS.md) applies.

D12 asks for three things. This record covers the **first** only:

1. the core's Yosys netlist is byte-identical to main apart from the `BUILD_ID`
   constant and `src` file paths — **in progress here**;
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

```
python3 netlist_digest.py --tree <tree>/sources/misteross --core fes.pong \
    --cache-root /data/fes1861-netlist/cache --out reports/<core>-<side>
```

## Trees

| Side | Tree | Commit |
| --- | --- | --- |
| main | `/data/fes1861-netlist/mainref` | `72d2fc4da` (the PR's merge base) |
| branch | `/home/deano/fes/.claude/worktrees/gpu-router-default` | see `reports/*/netlist.json` |

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

Exact binary digests are in each run's `netlist.json`. Only nextpnr differs
between these locks, and nextpnr routes nothing here; it is authenticated
because the producers authenticate their whole slot before synthesising.

## Runs

Each run writes `reports/<core>-{main,branch}/netlist.json` with the netlist
digest and the authenticated tool identities. The netlists themselves are 3.3 MB
each and are not committed; the digests and the command are the record D12 asks
for.

<!-- results:start -->
| Core | Changed files in its closure | Netlist sha256 | Match |
| --- | --- | --- | --- |
| `fes.menu` | `build_fes_menu_package.py`, `build_fes_menu.py`, `fes_de10nano_evidence.py` | `05ab4ada678920c408b045026fbd88073f05152867ba628ff253757bc23d0a31` | **byte-identical** |
| `fes.pong` | `build_fes_pong.py`, `fes_de10nano_evidence.py` | `e7948651bcc0cc2ffbbd3c52c8643c642bc6964826e2a0ae65d18e3fd90cf12b` | **byte-identical** |
| `fes.ramtest` | `build_fes_ramtest.py`, `fes_de10nano_evidence.py` | `0d59db94f2352a40c9b0e57ab69c6626ff5a3f7b33f5843ae9507e9bf3bf6c7a` | **byte-identical** |
| `fes.riscv` | `build_fes_riscv.py`, `fes_de10nano_evidence.py` | `e8db4d58afb81e7766d7e21c9ce6d1d18736196badb9dbc23fb742d8c4549be8` | **byte-identical** |
| `fes.ts1000` | `fes_de10nano_evidence.py` | `96bcf1f6411ab04324e86a58fd525bd5c722f3afbf02c7db67365be074cc6dca` | **byte-identical** |
| `fes.ts1500` | `fes_de10nano_evidence.py` | `d29bb54e119907959fb104890f8d2d038fff625cf84b2cdea09cc7525bbf37a7` | **byte-identical** |
| `fes.zx80` | `fes_de10nano_evidence.py` | `b7effcba3f7a30349ae55aef0eac6a857d00f04dc4fe1b959a0c482534df6f82` | **byte-identical** |
| `fes.zx81` | `fes_de10nano_evidence.py` | `b5c296e5c76810f5655e70a945518dd511654e8b450fb7bbbb616e442f15604b` | **byte-identical** |
<!-- results:end -->

The table is rendered into those markers by `python3 summarise.py --write`, so
no digest in it is transcribed by hand.

**All four cores whose own producer script this PR edits** — `fes.pong`,
`fes.menu`, `fes.ramtest` and `fes.riscv` — produce byte-identical netlists, as
do the ZX81-family cores measured so far, which change only through the shared
helper.

`fes.pong` is the strongest case in the set: both its own producer script and
the shared helper changed. `cmp` reports the two 3,294,796-byte netlists
identical, over a top module of 1,422 cells and 1,804 nets, from one Yosys
binary. Nothing was normalised or excluded. `fes.menu` is the same kind of case
through the package producer, and the ZX81 models stand for the ten cores whose
closure picks up only the shared helper.

Remaining cores are in progress; each needs its lock's nextpnr built before its
producer will authenticate, even for a Yosys-only run.
