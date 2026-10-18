# KF8253 import

Pinned from [PCXT_MiSTer](https://github.com/spark2k06/PCXT_MiSTer/tree/af4f8a98ae562af0e3f9b86cc3a59673f999d0f6/rtl/KFPC-XT/HDL/KF8253)
under the retained MIT license. `source.json` records original paths and hashes.
The motherboard wrapper supplies synchronous bus strobes at 100 MHz.
Simulation coverage is described in the [core README](../../README.md);
importing this implementation does not qualify all chip modes or silicon timing.

Local timing change: mode 3's two-step decrement uses one direct saturating
binary/BCD operation. BCD digit borrows use independent lower-digit prefixes,
rather than passing through two complete nested decrementers. Count/output
register boundaries, zero reload, modes and bus timing are unchanged.
`tests/test_pcxt_counter_equivalence.py` proves one/two-step results against
the original equations for all 17-bit inputs, including non-decimal nibble
patterns. `source.json` retains the upstream counter hash and the local hash.

Internal registers precompute binary and BCD candidates in parallel between
sampled falling count-clock edges. Such edges cannot be adjacent fabric
clocks, so the candidates refresh after a count change before the next use.
Reset/inactive counters clear both count and candidates; current BCD selection
still applies immediately after a control write. The counter-equivalence test
inductively proves candidate freshness at every consuming edge. Whole-chip
sequential equivalence also checks the original timer registers and interface;
internal combinational candidates may differ between consuming edges.

Terminal-count flags are precomputed with the candidates. Period/output control
selects scalar conditions in parallel with next-count data, avoiding a wide
next-count mux followed by a wide comparator. Current gate/load/preset choices
remain live, and the same consumption invariant covers the scalar flags.
