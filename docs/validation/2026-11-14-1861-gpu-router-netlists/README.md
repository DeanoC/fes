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

## Runs

Pending. Results land in `reports/<core>-{main,branch}/netlist.json`, each
carrying the netlist digest and the authenticated tool identities, and the
table below records whether the two digests match.

| Core | Changed files in its closure | main netlist | branch netlist | Match |
| --- | --- | --- | --- | --- |
| `fes.pong` | `build_fes_pong.py`, `fes_de10nano_evidence.py` | pending | pending | pending |
