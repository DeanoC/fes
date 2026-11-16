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

## Both clock domains were analysed, checked rather than assumed

A gap the sibling measurement found in the shared harness after these fits
ran: `fit_timing.tcl` required the SDC clock *count* and **design-wide**
setup/hold paths, and neither notices a design that keeps both clock ports
while one domain's registers are swept — the design-wide queries are answered
by the surviving domain, so a two-clock fit could publish as "analysed at
every corner" having analysed one.

`pc386_system` has two clocks, so this record is exposed to it. It is
checkable from the retained evidence without re-fitting, because the fitter
derives a clock's Fmax **only** from paths inside that clock's own domain: a
finite Fmax for both clocks at a corner is affirmative evidence both domains
were analysed there. Run over every fit in this directory:

| Fit | Corners | Both clocks at every corner | Corner 1 Fmax |
| --- | ---: | --- | --- |
| `fit-seed1` | 4 | yes | `clk` 131.15, `pixel_clk` 203.38 |
| `fit-seed2` | 4 | yes | `clk` 129.74, `pixel_clk` 206.61 |
| `fit-seed3` | 4 | yes | `clk` 132.10, `pixel_clk` 215.89 |
| `fit-seed4` | 4 | yes | `clk` 136.65, `pixel_clk` 211.15 |
| `ab-backend0-seed1` | 4 | yes | `clk` 130.63, `pixel_clk` 204.71 |

**All five fits analysed both domains at all four corners.** The harness now
scopes each expected clock's own register-to-register paths with
`-from_clock`/`-to_clock` and writes per-clock rows, so later fits carry the
coverage explicitly instead of it being recoverable from Fmax. These fits
predate that and the check above is what stands in for it.

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
