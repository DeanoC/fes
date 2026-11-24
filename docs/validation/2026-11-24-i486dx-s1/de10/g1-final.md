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
