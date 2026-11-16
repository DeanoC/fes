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

**Both netlist digests are recorded because neither can be derived from the
other.** The runner's `prepare_netlist` retargets `ramstyle = "M10K"` to
`"M20K"` for Agilex 5 — 57 occurrences here — **and** then drops duplicate
attributes. So "the fitted netlist is the elaborated one retargeted" is
**false** for any run through that function, not just this one: applying the
rename alone to `95b72a8d…` produces a third digest, neither of the two
recorded. That was found by trying it, after this README's first draft was
about to assert the derivation.

The practical consequence, which generalises past this record: a fit that
records only one of the two digests cannot be tied to its source, and a reader
cannot recover the missing one by reasoning about what the retarget does.

All four seeds record the same **fitted** digest, which is what makes the seed
comparison a comparison of placements rather than of designs.

`real_pins: 2` on every seed is the machine's two clocks staying real under
this envelope. The harness now asserts that count **exactly** rather than as a
maximum, so an envelope change that silently demoted `pixel_clk` to a virtual
pin would refuse these fits instead of re-basing them quietly — the survivor
alone would otherwise have been analysed and reported as full coverage.

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
