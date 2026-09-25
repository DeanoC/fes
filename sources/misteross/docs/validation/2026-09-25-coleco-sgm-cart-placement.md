# Coleco SGM v2 cart placement feasibility (issue #203)

> Dated record (2026-09-25). It describes that day's evidence, not the
> current build. OSS place-and-route starts at [OSS place-and-route
> testing](../oss-pnr.md). Do not treat this note as the schedule for a
> later tree.

FES issue #203 asked whether the independently synthesized SGM v2 cart
(888 packed cells: 608 ALUTs of which 231 arithmetic, 271 FFs) can legally
occupy the Coleco socket `FES_RESERVED_RECT "24 1 28 11"`, and why neither
HeAP nor SA placement finished in 180 s on nextpnr `332c883c`. The fixture
is the exact cart and shell from the issue
(`build/coleco-sgm-pin-check/{cart.json,scaffold.json,cart.qsf,clocks.sdc}`
in the `coleco-sgm-cpu-latest` worktree); every run below was placement
only (`--no-route`) unless stated, on the shared development machine. None of
it is a sealed package, a route, or hardware evidence.

## Finding 1: the stall was a placer defect, not a proof of infeasibility

`FES_RESERVED_RECT` reserved BELs but never created a nextpnr `Region`. HeAP
solved and spread the cart over the whole device and its legaliser sampled
tiles chip-wide, of which the socket is about one percent. With `--debug-placer`
the FF pass was a rip-up cycle: in 60 s, 2,691 legalise calls over 678 cells,
ten AY/control FFs accounting for more than 1,500 of them. HeAP only errors
after 8 x (cell count) legalise calls, roughly 26 minutes here. SA's initial
placement picked chip-wide tiles and stalled once the socket filled. The
unbound cart packer also never clustered carry chains, so the 231 arithmetic
cells were loose.

nextpnr branch `fix/fes-203-slot-region` (PR DeanoC/nextpnr#78, head
`f7370550`, now pinned by `toolchains/coleco-expansion.lock`) creates the
`$FES_SLOT` region, clusters cart carry chains, pairs each cart FF with the
LUT driving it, legalises slot FFs control-set aware with a 500-ripup limit,
refuses `--placer sa` for carts, and prints a `FES slot capacity` report that
fails fast with the violated bound. The v1 Coleco bus diagnostic cart
(FES fixture `87a399c1`) still routes on that pin with the GPU router:
56.31/107.05/173.46 MHz table and 55.99/94.27/177.37 MHz analogue, the same
figures as before; its cart placement and RBF bytes change because of the
LUT/FF pairing.

## Finding 2: the SGM cart does not fit 41 usable LABs under nextpnr's rules

The rectangle holds 44 LAB/MLAB tiles (X24 and X27 LAB, X25 and X28 MLAB,
X26 M10K). The frozen socket FFs occupy X24 Y1 to Y3 (20, 20 and 19 FFs);
`fes_placement_allowed()` excludes those LABs wholesale, leaving 41.

Capacity report for the cart in that rectangle (after pairing, 111 FFs sit
with their LUT):

| Bound | Need | Available |
| --- | ---: | ---: |
| COMB BELs | 617 | 820 |
| FF BELs (two legal per ALM) | 271 | 820 |
| Carry chains (roots at ALM 0; longest 29 cells = 2 LAB rows) | 17 | 41 roots, 11-row runs |
| FF control-set LABs (one SCLR and one ENA per LAB; Mistral never marks the clock global) | >= 29 | 41 |
| LAB input bandwidth (42 unique inputs per LAB; 1771 LUT inputs after chain sharing, best case 386 shared, 160 unpaired FF data inputs) | >= 37 | 41 |

Every bound is satisfied in the best case, so the report cannot refuse the
cart outright. Measured placement then shows the best case is far from
reachable:

| Rectangle | Usable LABs | Result |
| --- | ---: | --- |
| `24 1 28 11` (socket) | 41 | rip-up limit after 501 re-placements of a LUT6 (control-set model on or off) |
| `24 1 31 11` | 41 | same |
| `24 1 33 11`, `24 1 34 11`, `24 1 28 16`, `24 1 28 24` | 44 to 48 | LUT pass completes; FF pass fails on an 8-FF AY register group (attempt limit or 500 re-placements) |
| `24 1 43 11` (diagnostic only) | 111 | places in under 2 s using 55 to 57 LABs |

In the 111-LAB run the 41 socket LABs hold 6 to 20 LUTs each (their input
budget) and the unpaired FF groups spill into 14 LABs east of the socket.
The realistic minimum is therefore about 45 LABs (bandwidth with typical
sharing) and the current placer needs about 55 to 57. The socket's 41 cannot hold
this cart.

## Suggested targets

Either of these, re-validated through the normal shell and module recipes:

- A reserved rectangle with at least 55 usable LABs at shell build time, for
  example `24 1 28 15` (60 tiles minus the three frozen LABs). Rows 12 and
  above in those columns are occupied by the current shell, so the shell
  must be re-placed and re-routed with the new rectangle and a new CRAM
  region; the socket contract changes.
- Or an RTL reduction of roughly a quarter of the LUT input demand (1771 to
  about 1300 unique LUT inputs) together with fewer unpaired FF data inputs
  (160). The 27-bit AY phase accumulator with its 28-bit subtract, the three
  16-bit PCM adders, and the 69 request-latch FFs fed straight from the
  socket request bits are the largest contributors.

## Reproducer

From `sources/misteross`, with the pinned `nextpnr-mistral`:

```
HIP_VISIBLE_DEVICES=0 timeout 600 nextpnr-mistral \
  --json build/coleco-sgm-pin-check/scaffold.json --device 5CSEBA6U23I7 \
  --qsf build/coleco-sgm-pin-check/cart.qsf --sdc build/coleco-sgm-pin-check/clocks.sdc \
  --freq 52.224 --fes-scaffold --fes-cart build/coleco-sgm-pin-check/cart.json \
  --fes-slot-clock 'system_clock.clocks[0]' --fes-cram-region 1769,32,2806,1034 \
  --no-pack --seed 3 --router gpu --placer-heap-timingweight 300 --no-route
```

The run prints the capacity report and stops within minutes naming the
cycling cell. Diagnostic rectangles are a copy of `cart.qsf` with a different
`FES_RESERVED_RECT`; they are not a valid socket contract.
