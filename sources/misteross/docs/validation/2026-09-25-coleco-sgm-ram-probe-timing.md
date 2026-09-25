# Coleco expansion socket: routing convergence and RAM-probe timing (issue #184)

> Dated record (2026-09-25). It describes that day's evidence, not the
> current build. OSS place-and-route starts at [OSS place-and-route
> testing](../oss-pnr.md). Do not treat this note as the schedule for a
> later tree.

FES issue #184 asked why a throwaway 32 KiB SGM RAM probe on the Coleco CPU
bus missed its 52.224 MHz system clock target, and why a matched no-RAM
current-source route of the same development-only expansion socket
(`toolchains/coleco-expansion.lock`, reserved rect `24 1 28 11`) stalled
around 16-21 overused wires for 126 router iterations before being manually
interrupted. Both questions were investigated against the exact fixtures
from that issue (`build/fes-coleco-socket-dev` synth.json/qsf/sdc for the
no-RAM shell; the issue's throwaway `coleco_machine.sv` probe RTL for the
RAM case), not a re-synthesis, so the netlists are identical to the ones the
issue measured.

## Finding 1: the socket-dev toolchain's nextpnr pin predates the GPU-router analogue-signoff repair

`coleco-expansion.lock` pinned nextpnr `abce51d34`, which is an ancestor of
`5dea3ecd` (merged PR DeanoC/nextpnr#75, already used by
`registered-memory.lock` for the factory Coleco/SMS/SG-1000 recipes). Every
Coleco-specific behaviour on the old pin (`FES_RESERVED_RECT` placement
fencing, physical CRAM fencing, FF route-through `FES_SLOT` preservation,
bounded GPU architecture bind retries) is unchanged on `5dea3ecd`, because
`abce51d34`'s commits are already merged into it.

Re-running the RAM-probe fixture with a `5dea3ecd`-equivalent nextpnr,
seed 3 / `--placer-heap-timingweight 300` reaches full analogue-signoff
closure:

| Clock | Constraint | Achieved | Result |
| --- | ---: | ---: | --- |
| `system_clock.clocks[0]` | 52.224 MHz | 53.52 MHz | PASS |
| `pixel_clk` | 74.25 MHz | 89.69 MHz | PASS |
| `system_clock.clocks[1]` | 12.288 MHz | 142.29 MHz | PASS |

`coleco_expansion.validate_routed_shell()` still passes against this
route: the reserved rectangle stays vacant. Re-run independently, this
result reproduced bit-for-bit (identical achieved MHz to the last digit on
every clock), unlike the seed-5 no-RAM case below — seed 3 does not appear
to sit near the kind of finely-balanced congestion tie that produces
run-to-run variation. Seeds 4-6 and higher timing
weights on the same fixture do not all close (the table model passes with
positive WNS at several seeds/weights, but analogue signoff can still miss
by a wide margin, e.g. seed 6 / weight 300 reached only 45.99 MHz analogue
against a 54.81 MHz table result) — the search is seed-sensitive, as it
already is for the shipped Coleco/SMS/SG-1000 recipes.

**This is not a router or Mistral-fence bug.** The old pin simply never ran
PR #75's repair pass, so the socket-dev recipe was comparing against a
strictly worse baseline than the one already in production for the shipped
cores.

## Finding 2: the no-RAM "stall" is genuine (if occasionally very slow) convergence, not a capacity wall

`FES_RESERVED_RECT` blocks BEL placement inside the rectangle
(`fes_reserved_bels`) but does not restrict routing through it for this
recipe: both of nextpnr's routing-side fences (`checkPipAvailForNet`'s
`fes_fence_active` slot gate, and `fes_pip_preserves_cram`'s
`fes_has_cram_region` gate) are only armed by the cart-merge path
(`fes_slot.cc`'s CRAM/slot-overlay functions), which the plain socket-dev
build never calls. `fes_fence_active` and `fes_has_cram_region` stay false
for the whole route, so ordinary nets can use every pip in the rectangle.

The per-100-iteration congestion dump (`gpurouter.cc`) named the actual
stuck wires: they are inside the T80 CPU core's own ALU/register logic
(`machine.cpu.i_tv80_core.*`, e.g. `ACC`, `Arith16`, `RegAddrA_r`,
`RegBusA`), fighting over shared local LAB-internal wires — nothing to do
with the reserved rectangle, the socket boundary, or the RAM. Different
placer seeds sidestep the conflict entirely: seeds 1-3 on the identical
no-RAM fixture all converge in under 90 iterations and pass both clocks at
the table level without any repair (54-58 MHz system, 89-107 MHz pixel).
Seed 5 (the one manually interrupted in the issue) does eventually recover
on one recorded run — the true worst plateau-without-a-new-minimum across
that 152-iteration run was 29 iterations, not the ~100+ it visually looks
like from the noisy raw overused-wire count.

**Repeated runs of the identical seed 5 / weight 300 fixture do not always
reproduce the same routing.** Comparing several runs of the same JSON,
QSF, SDC, seed and GPU device (with and without the nextpnr change below,
against both of two Mistral pins), every run was byte-identical through
iteration 16, then some diverged onto a fast trajectory (converged by
iteration ~23) while others stayed on the slow/plateaued one, including
two runs of the exact same binary run back-to-back with no other change.
This is inconsistent with the GPU router's documented "same seed produces
the same routing" goal and was not root-caused here (see DeanoC/nextpnr#76
for the detail); it means seed 5's behaviour on this fixture is not a
fixed, reproducible property of that seed, and the "29-iteration worst
plateau" figure above describes one run, not a guarantee. Seeds 1-3 were
only observed to converge quickly once each, not repeated for
determinism, so treat "avoids the conflict" the same way.

## Fix: accelerate stalled congestion resolution (nextpnr)

DeanoC/nextpnr#76 (branch `perf/coleco-sgm-route-184`, on `mistral-stable`
`5dea3ecd`) tracks the best (lowest) overused-wire count in the GPU
router's negotiation loop; if `congestionStallIters` iterations (default 20)
pass without a new minimum, the present-congestion growth rate
(`currCongWeightMult`) is scaled by `congestionStallBoost` (default 1.5),
compounding every further `congestionStallIters` iterations of no
improvement, until a new minimum is found (capped at `congestionStallBoostMax`,
default 50x, since a genuine hard conflict does not respond to any amount
of cost weight — one run hit a persistent single-wire/single-net conflict
late in the route that the uncapped multiplier boosted past 10000x without
ever resolving it, before the cap was added). This only ever accelerates
convergence relative to *that same run* — a steadily-improving route never
triggers it, and `maxIter` remains the existing backstop. Given the
non-determinism above, this cannot be verified as a reliable fix for seed
5 specifically; it is a general router robustness improvement for whichever
run lands on a plateau, not a guarantee that seed 5 now always converges
quickly.

DeanoC/nextpnr#76 picked up two review findings after the run above (a
modulo-by-zero when `congestionStallIters=0`, and the boost state not
resetting when congestion reached zero under `--tmg-ripup`), both fixed
and re-verified before merge (the RAM-probe seed-3 result stayed
bit-identical). `coleco-expansion.lock` and
`build_fes_coleco_socket_dev.py`'s `TOOL_COMMITS` are pinned to the merge
commit (`c9330767`, on `mistral-stable`).

## Note

A concurrent `build_fes_coleco_socket_v2_dev` recipe and `coleco_expansion`
`version=2` were observed running in the issue's own read-only reference
worktree (`coleco-sgm-cpu`) while this investigation was in progress,
suggesting parallel work on a v2 socket approach. This note does not touch
or depend on that work; reconcile before acting on both.

## What this does not do

No RTL changed. The probe's bus-claim-mask widening and inline 32 KiB RAM
live in the issue's own throwaway dev worktree and are out of scope here —
this note only answers the routing/timing investigation the issue asked
for. No RBF from any of these routes has been loaded on hardware.
