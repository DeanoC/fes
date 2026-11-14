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

Exact binary digests are in each run's `netlist.json`. Only nextpnr differs
between these locks, and nextpnr routes nothing here; it is authenticated
because the producers authenticate their whole slot before synthesising.

## Runs

Each run writes `reports/<core>-{main,branch}/netlist.json` with the netlist
digest and the authenticated tool identities. The netlists themselves are 3.3 MB
each and are not committed; the digests and the command are the record D12 asks
for.



The table is rendered by `summarise.py` from `reports/`, so no digest in it is
transcribed by hand.

`fes.pong` is the strongest case in the set: both its own producer script and
the shared helper changed. `cmp` reports the two 3,294,796-byte netlists
identical, over a top module of 1,422 cells and 1,804 nets, from one Yosys
binary. Nothing was normalised or excluded. `fes.menu` is the same kind of case
through the package producer, and the ZX81 models stand for the ten cores whose
closure picks up only the shared helper.

Remaining cores are in progress; each needs its lock's nextpnr built before its
producer will authenticate, even for a Yosys-only run.
