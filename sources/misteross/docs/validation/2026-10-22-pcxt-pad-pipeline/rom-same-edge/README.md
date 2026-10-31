# Rejected system-edge microcode ROM experiment

Moving the microcode ROM read to the system edge preserves its proved output
cadence but worsens both tested routed timing recipes. This derived source is
not adopted. Both full routes return 1, retain 105 unsupported boundaries and
have no negative related hold rows. These are host diagnostics; no hardware,
seal, production lock or shared contract changes.

| Complete native result | Original source | Same-edge, window cost | Same-edge, timing cost |
| --- | ---: | ---: | ---: |
| Setup failures | 111 | 407 | 515 |
| System setup WNS | -967 ps | -1990 ps | -2556 ps |
| ROM address failures / WNS | 3 / -199 ps | 191 / -1763 ps | 145 / -1897 ps |
| ROM return failures / WNS | 0 / +915 ps | 0 / +188 ps | 1 / -207 ps |
| Pixel setup WNS | +2354 ps | +12 ps | +643 ps |
| Complete native endpoint rows | 17199 | 17151 | 17151 |

The source base is FES `2a1cedf48efe68ccc92c0f7c7bfed52f0dc35dbd`. The derived
snapshot changes only `mcl86_ucode.sv` and its local provenance record. It removes
48 local address registers and reads the same four ROM-byte arrays from the
system edge, retaining all 90 registered word/selector outputs, complete ROM
contents and PLL parameters. The output transport proof, CPU diagnostics and
mapped 192-address/90-output checks pass. The mapped correspondence has explicit
state assumptions and is not whole-board sequential or physical equivalence.
All original proof boundaries and negative cases are retained in the archives.

The original comparator is the fresh `2a1cedf4` build from the bare ADD pair,
without the rejected ADD mapping or optional overflow balancing. It is not the
14-failure balanced recipe. The new five-LUT overflow cone is rejected by the
existing four-LUT guard before routing; the failed preflight is included and no
guard is relaxed. Both same-edge routes omit that option. System remains
100 MHz and pixel 74.25 MHz.

The window-cost pair uses the same frozen `a361eac0` compiler and seed-1
weight-2000/exponent-5 recipe. The second route uses `2fc1b49a` and adds only
its default-off hard-block timing cost. Its flag-OFF preparation matches the
old compiler byte for byte apart from `creator`; this is a CPU preparation
comparison, not another full-route control. Full native endpoint unions,
changed phase windows, all margins and physical metadata are preserved. Each
route's complete graph proof independently accounts for its generated identity
helpers and rejects 13 deliberate corruptions. These checks do not decode RBF
wiring, establish electrical timing or qualify unsupported boundaries.

The first route charges 303.2311640176922 seconds and the second
470.62573734717444 seconds, including lease waits. Both are already counted in
`../continued-runs.jsonl`; archiving and host proofs add no route charge.

## Capture contents

Extract the three archives into one empty directory. Their paths retain the
original task-relative `out/` layout. Every archive contains a manifest with
all member hashes and sizes. `capture-readback.json` binds the compressed
archives and records complete member readback; no samples are used.

- `host.tar.gz`: 93 evidence files and its manifest, including both complete
  source archives, source/CPU/formal/synthesis receipts, raw synthesis and
  explicitly bound original-source dependencies.
- `window-route.tar.gz`: 52 evidence files and its manifest, including original
  and candidate final artifacts, complete analysis, failed overflow preflight
  and exact same-binary native preparation.
- `timing-cost-route.tar.gz`: 32 evidence files and its manifest, including the
  second full route, flag-OFF/ON preparations and complete paired proofs.

The immutable original manifests are `validation-receipt.json`,
`first-route-evidence-manifest.json` and `cost-route-evidence-manifest.json` under
`out/rom-same-edge-20261022/`. Their hashes and every bound file are checked
before archive construction and again after writing each destination archive.
The original scripts retain historical absolute paths and strict compiler
hash checks; replaying them requires those exact inputs and tools. Compiler
and Yosys executables are not bundled. Source/build identity for the compilers
is retained in the [native-window and cost captures](https://github.com/DeanoC/nextpnr/tree/c2e9cb77/docs/validation).

The next timing work keeps the original source and separately evaluates guarded
local repairs. Neither this source experiment nor a positive local margin
inherits complete timing, multi-seed, producer or exact-artifact hardware
acceptance.
