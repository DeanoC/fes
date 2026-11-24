# G1 at the final head

Base: the four branch heads this PR stacks on, merged without this PR's
commits: #2171 `2d4edf547`, then #2178 `7147b7d05` (its one conflict,
`boards/de25nano/pc386/sim.py`, DE25 only and outside the DE10's closure,
taken from #2178), #2177 `642e22f50` and #2187 `884b5959c`. Merge commit
`87562da40` (local; tree `291a6c3127fb066bbe54fbb26a8f96ce3a5704f2`).
Head: `eab2fe8b5`. Later commits of this PR change only
`docs/validation/2026-11-24-i486dx-s1/`, outside every producer's closure.

`i486dxs1_g1.sh eab2fe8b5 87562da40`, one checkout path
(`/data/projects/i486dx-s1/wt-g1`), Yosys `d273c1cb`, `YOSYS_MAX_THREADS=1`
([g1-eab2fe8b5.log](g1-eab2fe8b5.log)):

| | Base `87562da40` | Head `eab2fe8b5` |
| --- | --- | --- |
| DE10 machine netlist | `65cbd41232883f713ef6c120757a1fa95aced1d8047af86d891491a960274daf` | the same; `cmp` equal |
| design | `426e518d…` | `426e518d…` |
| evidence | `243554b4…` | `243554b4…` |
| record | `79b7218a…` | `79b7218a…` |
| toolchain | `372fcd8d…` | `372fcd8d…` |

**Pass.** The toolchain and record identities differ from `2d4edf547`'s
because #2187's base carries newer main (its pc386 lock); they are equal on
both sides here.

## Note, 24 Nov 2026: #2171's new head merged

#2171's branch moved to `f6956ce50` (main merged in after #2036's
squash-merge) and is merged here (`a2084d33f`). The merge changes no DE10
netlist input against `ea61ccc10`: the diff is empty for
`cores/fes-common/rtl`, `cores/fes-pc386`, `cores/fes-common/sim/i386`,
`scripts/yosys_netlist.py`, `scripts/pc386_quartus.py`,
`scripts/input_identity.py` and `boards/de25nano`. So the netlist comparison
above carries over unchanged. `de10_design_id.py` at the new base and the new
head:

| | `f6956ce50` (new base) | `a2084d33f` (head) |
| --- | --- | --- |
| design | `426e518d…` | `426e518d…` |
| evidence | `243554b4…` | `243554b4…` |
| record | `d7dbba7d…` | `d7dbba7d…` |
| toolchain | `07d3cc5b…` | `07d3cc5b…` |

All four are equal. The record and toolchain identities differ from
`ea61ccc10`'s (`79b7218a…`, `372fcd8d…`) only because of what main brought
into the base.
