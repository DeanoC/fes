# KF8253 import

Pinned from [PCXT_MiSTer](https://github.com/spark2k06/PCXT_MiSTer/tree/af4f8a98ae562af0e3f9b86cc3a59673f999d0f6/rtl/KFPC-XT/HDL/KF8253)
under the retained MIT license. `source.json` records original paths and hashes.
The PC/XT motherboard and the pc386 AT core supply synchronous bus strobes.
Simulation coverage is described in the [PC/XT README](../../../fes-pcxt/README.md);
importing this implementation does not qualify all chip modes or silicon timing.

Local timing change: mode 3's two-step decrement uses one direct
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

Local feature change (2026-10-26, for pc386): `READ_BACK = 1` makes the chip an
8254. A control word with bits 7:6 = 11 is the read-back command: for each
counter its bits 3:1 select, bit 5 clear latches the count as the counter-latch
command does, and bit 4 clear latches the status byte (OUT, null count, RW,
mode, BCD). A latched status is returned by the next read of that counter and
does not advance the count read sequence. Null count is set by a control word
or count write and cleared when the counting element takes the preset. With
the default `READ_BACK = 0` none of that state exists. `read_back.patch` is the
whole change; `tests/test_fes_kf8253_read_back.py` reverses it, checks the
result against the previous hashes and proves the `READ_BACK = 0` chip
sequentially equivalent to it at every port and matched internal signal.

Local behaviour change (2026-11-04, #1471): upstream reset the write and read
byte pointers only when a control word changed the RW field, so a control word
that repeated it left a half-written or half-read LSB/MSB count mid-sequence.
On the 8253 and 8254 any control word for a counter resets its control logic,
so every control word except the counter-latch command (RW = 00) now resets
both pointers. This applies to both `READ_BACK` settings.
`control_word_reset.patch` is the whole change. The read-back test reverses it
first, checks the read-back-era hash, then reverses `read_back.patch`; its
equivalence proof covers the read-back change alone, on the tree without this
fix. This change has no equivalence claim: the pc386 `devices` bench (8254)
and the PC/XT `motherboard` bench (8253) check the repeated-RW write and read
sequences against 8254 behaviour.

Local behaviour change (2026-11-07, #1532): the import's decrement held the
count at zero after terminal count. On the 8253 and 8254 the counting element
keeps going in modes 0, 1, 4 and 5: binary wraps to FFFFh and BCD to 9999. A
program that times a delay by reading a mode 0 counter until it passes zero
(SpeedSys's ISA plug-and-play probe does, on channel 2) never finished. Below
the step the count now wraps, and inactive decrement candidates clear to the
wrapped values. Because the count no longer rests at zero, OUT in modes 0 and
1 now stays high after terminal count until a new count or trigger, and modes
4 and 5 strobe once per count or trigger rather than on every wrap. Modes 2
and 3 reload before zero and are unchanged. This applies to both `READ_BACK`
settings. `terminal_count_wrap.patch` is the whole change. The read-back test
reverses it first and checks the control-word-era hash; there is no
equivalence claim. `tests/test_pcxt_counter_equivalence.py` proves the decrement
against the original nested-borrow equations with the wrap at zero, and the
pc386 `devices` bench (8254) and the PC/XT `motherboard` bench (8253) check the
wrap and the outputs in modes 0, 1, 4 and 5.
