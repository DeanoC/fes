# DE25 pc386: reviewed bases for the window control block's held bundles

The i486DX slice-1 fit (DX + #2177 + #2178, seed 1) stopped at #2036's
reviewed-signoff gate:

    CDC-50001 row matches 0 bases: ...window_control|base_lo[13] ; ...window_control|base_machine[13] ;
    pll_inst|iopll_0_outclk0 ; fes_machine_pll|machine_pll_outclk0 ; Asynchronous transfer

`pc386-clocks.sdc` bounds that crossing (the `window_base` held bundle, with
`set_net_delay`, `set_data_delay`, `set_max_skew` and `set_max_delay` at
10 ns), but `signoff_bases.py` had no basis for it. This change adds
CDC-50001 bases for `window_base` and its twin `window_fault_record`. It
changes no RTL, SDC or engine code.

## Why #2036's seal never reported the row

The design assistant reports these rows for some designs and not others. The
same design reports the same rows on every seed. These are the design
assistant tables (`golden_top.tq.drc.signoff.rpt`) of every DE25 pc386 fit
on Powerboat and chip from 22–24 Nov that has the presenter bases'
crossings:

| Fit | Seeds | CDC-50001 rows | `window_base` rows | Bits |
|---|---|---|---|---|
| #2036 D12 producer fits wt3–wt6 (the last is the seal, BUILD_ID f93f215e) | 1 each | 67 | 0 | — |
| #2036 + audio cut (audio-area), + VGA arm (vga-area) | 3 + 5 | 67 | 0 | — |
| #2036 dfacd28e1 (ps2fifo-area base23b) | 4 | 67 | 0 | — |
| base23b + DMA2 registers (dma223b) | 4 | 67 | 0 | — |
| 486SX slice 1 (d1616104c) | 4 | 67 | 0 | — |
| base23b + window interconnect latency 0 (slat23b) | 4 | 76 | 9 | 13–16, 22, 28–31 |
| base23b + PS/2 FIFO MLAB (fifo23c) | 4 | 76 | 9 | 13–16, 22, 28–31 |
| xarm (ps2fifo-area) | 1 | 76 | 9 | 13–16, 22, 28–31 |
| i486DX slice 1 seed 1 (wt-5112461a9) | 1 | 76 | 9 | 13–16, 22, 28–31 |

The cause is not placement: none of these designs varies between seeds. The
DX change is not required either. Two changes that do not touch the window
both bring the same nine rows: the qsys interconnect-latency cut, a board.py
change only, and the PS/2 FIFO derivation, a machine change only. A
read-only `quartus_sta` query
(`get_fanins -no_logic` on each `base_machine[k]|d`) found the same
structure in the seal and in DX seed 1: every one of the 20 kept bits is fed
by `base_lo[k]` alone. Nothing in the netlist structure separates the nine
reported bits from the other eleven. Which bits the design assistant flags
follows the compiled design, by a rule this record has not identified. It
is not determined.

The row count is 0 for one design and 9 for another, and #2036 and the DX
slice share this table. So the two bases do not pin an exact count, as the
other crossing bases do. They record their count, as the advisory bases do,
and they bind every row by:
- the register names (`window_control|base_lo[k]`, or a fitter
  `~DUPLICATE` of one, into `window_control|base_machine[k]`;
  `fault_launch[k]` into `machine_fault[k]`);
- both clocks, in the bundle's direction.

`window_fault_record` has never been reported. It has the same SDC shape, so
it has a basis too.

## Checks

- `machine_findings.review` on real reports, with each fit's compiled
  `machine-clocks.sdc` and the full basis table:
  - seal f93f215e: passes, with `window-base` at 0 rows;
  - base23b and dma223b seeds 2–4: pass, at 0;
  - fifo23c seeds 2–4, slat23b seeds 3–4: pass, at 9;
  - DX seed 1 and slat23b seed 2: CDC rules pass, at 9.
- `tests/test_de25_machine_findings.py`, against the full table:
  - 0 rows, and DX's nine rows verbatim plus a `~DUPLICATE` and the five
    fault bits, are admitted;
  - each of these matches 0 bases:
    - a `base_lo` outside `window_control` (`system|`, `arbiter|`);
    - `base_lo` into another sink;
    - `base_hi`;
    - either bundle with reversed clocks;
    - the base from the pixel clock;
    - the fault record into `base_machine`;
  - removing either bundle's SDC label line refuses.

  Widening the source pattern to `.*base_(lo|hi)` fails four of those
  cases.
- `fes_de25_hps_window_control.v` is pinned in `SOURCES`, at
  `8c313031…bcbe`. It and `pc386-clocks.sdc` are byte-identical in the DX
  fit's tree.

## Fitter copies and the decoder state (second commit)

Deano answered two questions on 24 Nov 2026, through the coordinating
session:
- "Accept unpinned": the window bases above.
- "Widen, with tests": admit fitter ~DUPLICATE/~_Duplicate copies of
  names the bases already accept, plus machine_at_decoder_state.

`signoff_bases.py` now has one copy suffix, `COPY`: `~DUPLICATE`, then
`~_Duplicate` with or without a number. The TMC-20602 tension advisory
and both LNT-30010 bases use it. The tension advisory also names
`machine_at_decoder_state`, pc386_io.sv's two-bit I/O cycle state.
`machine_findings.lint` admits only `~_Duplicate_N`, and it is in every
DE25 machine's closure, so the pc386 table builds its own LNT-30010 bases.
The engine's `lint` now has no caller; deleting it would move every DE25
machine's BUILD_ID, so that is left for a change that rebuilds them.

The full table against every report above, all rules:

| Fit | Result |
|---|---|
| seal f93f215e | passes |
| base23b, dma223b, fifo23c, slat23b seeds 2–4 (slat23b seed 2's `cpu_biu_reset~_Duplicate` included) | pass |
| audio-area, vga-area, 486SX seeds 2–4 | pass |
| i486DX seed 1 | stopped then: 23 TMC-20602 rows still matched no basis |

The 23 rows were:
- `audio_dsp_write`;
- `machine_at_decoder_phase[0–5]` (`[2]` as a `~DUPLICATE`);
- `machine_at_decoder_cycle_port[0–15]`.

## The decoder phase and port, and the DSP write (third commit)

Deano answered "Add them, with tests" on 24 Nov 2026, through the
coordinating session. The tension advisory now also names these registers,
with their fitter copies:
- `machine_at_decoder_phase` and `machine_at_decoder_cycle_port`,
  pc386_io.sv's phase counter and latched port;
- `audio_dsp_write`, the register on pc386_audio.sv's DSP (`pc386_sb`)
  write input.

Tests show that neighbouring names still match 0 bases:
- `machine_at_decoder_cycle_write`;
- `machine_at_decoder_next_port[3]~DUPLICATE`;
- `machine_at_decoder_phase_x[1]`;
- `audio_dsp_read~DUPLICATE`;
- `audio_dsp_write_data[3]`;
- `machine_audio_dsp_write`.

The full table now passes on all 40 recorded pc386 design assistant
reports, every rule, with no unmatched row:
- seal f93f215e;
- i486DX seed 1, with 42 TMC-20602 rows;
- slat23b seed 2;
- every seed of base23b, dma223b, fifo23c, slat23b, audio-area, vga-area
  and 486SX.

No reseal: this file is in pc386's closure only, and Deano's in-progress
exemption covers it. The DX slice merges this branch and refits.

## Every recorded copy spelling (fourth commit)

Codex (P2 at 88fbca0cb) found that `COPY` missed `~_Duplicate1` and
`~_Duplicate1DUPLICATE`. Those spellings appear in the recorded i486DX
plan fits' `timing.tsv`. A grep of every tracked file, and every
`timing.tsv`, `*.sta.rpt` and `*.signoff.rpt` under `/data/projects` and
`~/fes-scratch`, finds these Quartus spellings:
- `~DUPLICATE`;
- `~_Duplicate`;
- `~_DuplicateN` (N may be 0);
- `~_Duplicate_N`;
- `~_DuplicateNDUPLICATE`;
- `~_Duplicate_NDUPLICATE`;
- `~_DuplicateDUPLICATE`.

The other rare spellings come only from test files. `COPY` is now
`(?:~DUPLICATE)?(?:~_Duplicate(?:_?\d+)?(?:DUPLICATE)?)?`. Tests:
- each recorded form is admitted for a name the bases accept;
- malformed suffixes still match 0 bases: `~DUPLICATE1`,
  `~_Duplicate1_`, `~_Duplicate1DUPLICATE2`, `~_Duplicate~DUPLICATE`,
  lower-case forms, `~_Duplicate_1_2`, `~_DuplicateX`;
- under the previous `COPY` the two copy tests fail.

The full table still passes all 40 recorded pc386 reports.

## The advisory's RTL pinned (fifth commit)

Codex (P2 on #2177 against 37ba9e67a) found that the third commit's
advisory names cite RTL that `SOURCES` did not pin. `SOURCES` now pins:
- `cores/fes-pc386/rtl/pc386_io.sv` (`4ae84cce…`): the AT decoder's state,
  phase and port;
- `cores/fes-pc386/rtl/pc386_audio.sv` (`3e3d1f4f…`): `audio_dsp_write`.

A change to either file now stops the review until the bases are
reviewed again. Neither file differs between main, this branch, the
S-lat branch (`deano/de25-shell-lat`) and the i486DX branches. The i486DX
seed 1 and 486SX records, and the recorded x87, i486 and audio-area
reseals, captured these same digests.

BUILD_IDs: `signoff_bases.py` is one of pc386's `LOCAL_FILES` and no
other machine's. Both RTL files were already pc386 source inputs, and the
engine is unchanged. So only pc386's BUILD_ID moves, through the table's
own digest, as with the earlier commits.

The full table still passes all 40 recorded pc386 reports.
