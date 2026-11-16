# The integration fit's provenance

The harness is the sibling Agilex 5 measurement's `fit_i386_device.py`, at the
version current on `deano/happy-khorana-9c57db` when these fits ran. **That
version recorded the fitted netlist's digest but not what produced it**, and
`fes_revision` reads `unknown` because the source tree was copied to the fit
host and its `.git` pointer did not resolve there. A later version of that
script adds `--netlist-source`, which is the proper fix and which these fits
predate. **The chain is therefore stated here instead**, with every link
independently checkable.

| Link | Value |
| --- | --- |
| Branch | `deano/de25-x87-envelope` (PR #1907) |
| Source commit | `79f34caa8214058bd7dc0db7776812eb442f91a1` |
| Plus | two uncommitted scratch edits to `pc386_system.sv` — [`scratch-edits.patch`](scratch-edits.patch) |
| `pc386_system.sv` as elaborated, SHA-256 | `c4819c06494c0cf8…` |
| Netlist **as elaborated** (powerboat, the locked Yosys), SHA-256 | `95b72a8d546f8c0a1475…`, 133,035 lines |
| Netlist **as fitted**, SHA-256 | `a4a8a146f6d85cb4…` |
| Fit host | `chip-30x0-ai-dev`, Quartus Pro 26.3.0 Build 114 |

The two netlist digests differ because the runner's `prepare_netlist`
retargets `ramstyle = "M10K"` to `"M20K"` for Agilex 5 — 57 occurrences — and
then drops duplicate attributes. All four seeds record the same fitted digest,
which is what makes the seed comparison a comparison of placements rather than
of designs.

The scratch edits are the two lines that define what was fitted: the identity
on (`x87_present = 1'b1`, as the sibling's M+ does it) and the DE25 arithmetic
backend selected (`.X87_BACKEND(1)` on the `fes_i386_cpu` instantiation). They
are a scratch commit for measurement, not merged RTL.

## What is in this directory

- `fit-seed{1..4}.json` — the fit and timing results per seed, from the runner.
- `inputs-seed{1..4}.json` — the runner's own inputs record per seed. Note the
  `fes_revision: unknown` and absent netlist provenance described above.
- `scratch-edits.patch` — the two-line diff that defines the design fitted.
- `timing.tsv` — the per-corner timing table.

**Removed deliberately:** an earlier `inputs.json`, `netlist.json` and
`timing.json` from a *superseded* project-only run on powerboat. They were
copied here by mistake and described a different elaboration from the one
fitted, which a reader would reasonably have taken as describing these results.
