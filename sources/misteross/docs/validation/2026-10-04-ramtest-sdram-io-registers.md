# RAM-test: SDRAM command/address pad registers on the shared lock

Dated record, 2026-10-04, for [DeanoC/nextpnr#135](https://github.com/DeanoC/nextpnr/issues/135).

## Problem

The 100 MHz SDRAM pattern test failed on kit B when fes.ramtest was built on the
shared `toolchain.lock` (Yosys `886afa63`, nextpnr `3d4a5b35`; rbf `9cff64b3`)
and passed on the older ramtest-only lock (nextpnr `655f3833`; rbf `e76deaa5`).
That is why #502 kept ramtest on its own lock.

## Diagnosis (host only, seed 2 / weight 10 / exponent 2, nextpnr `--sdf`)

Both reference routes reproduce their shipped bitstreams bit for bit. The four
netlist x nextpnr combinations give these worst SDRAM address launch delays
(clock + clock-to-out + route, ns):

| Netlist | nextpnr | Worst address launches |
| --- | --- | --- |
| old synthesis | 655f3833 (passes on HW) | A12 7.0, A9 6.3, A10 4.4, BA1 4.7 |
| new synthesis | 3d4a5b35 (fails on HW) | A12 9.0, A9 8.8, A10 6.7, BA1 6.0 |
| new synthesis | 655f3833 | A12 9.3, A9 6.3, A10 6.0 |
| old synthesis | 3d4a5b35 | A9 7.4, A12 7.1, A10 5.9 |

The SDRAM output flops were ordinary fabric flops with no output timing
constraint, so their placement (and so the pad skew) depended on the netlist
and seed rather than on a nextpnr version. DQ input timing was similar in all
four builds.

## Change

* `sdram_addon_port.v` gains `IO_OUTPUT_REGISTERS` (default 0, so Atari ST is
  unchanged). With 1, every command/address pin (CKE, nCS, nRAS, nCAS, nWE,
  DQML/DQMH, BA, A) gets one plain register with no init, enable or reset, so
  merged nextpnr packs it into the pad's `MISTRAL_SDROUT` via
  `FAST_OUTPUT_REGISTER`. DQ out/OE are delayed one cycle in the fabric and the
  read-capture compare is shifted by the same latency.
* fes.ramtest enables it (`RAM_SDRAM_IO_REGISTERS` under `RAM_OSS_HIGH_SPEED`),
  and `constraints.qsf` sets `FAST_OUTPUT_REGISTER ON` on A, BA, CKE, nCS,
  nRAS, nCAS and nWE (DQML/DQMH are constant 0 in ramtest).
* `sim_fes_ramtest.py` runs both variants. A negative control (pipeline without
  the capture shift) fails, so the simulation checks the latency.
* fes.ramtest moves back onto `toolchain.lock`; the ramtest-only lock is removed.

## Results

Host: all 20 pad drivers synthesize to plain `MISTRAL_FF` and pack into
`MISTRAL_SDROUT` (20/20) on both nextpnr versions. In a CPU route (seed 2) the
command/address pad launches are 1.34-1.50 ns (0.16 ns spread), compared with
up to 9.0 ns before.

Package (FES `5502b569`, image build `0.2.0-hil135-5502b569`, HIP router,
first-pass s2-w10-c2): memory 105.33 MHz / 100, capture 232.56 MHz / 100,
pixel 91.58 MHz / 74.25; 20 `MISTRAL_SDROUT` in `routed.json`. Package
`c4cde3cf`, rbf `b456d45c`.

Kit B (MAC 02:46:43:9d:ac:d6), image `11ff3a6f` (fes-update, then rolled back
to `24c93192`), same boot:

| Package | SDRAM 100 MHz | HPS DDR |
| --- | --- | --- |
| new ramtest `c4cde3cf` (shared lock + IO registers) | INVR 6/6, ERR 0, all pattern counters 0, PASS | P0/P1/P2 7/7 PASS |
| control `3593d7e0` (old lock, rbf `e76deaa5`) | INVR 6/6, ERR 0, PASS | 7/7 PASS |

The menu (`6d732b23`) and Pong (`b9a4129a`) from the same image rendered.

## Remaining risk

DQ out/OE still sit in the fabric (worst DQ out route about 7.9 ns). They are
stable for several cycles before each WRITE, and the hardware test passes.
Bidirectional DQ pad packing needs the unmerged nextpnr #122.
