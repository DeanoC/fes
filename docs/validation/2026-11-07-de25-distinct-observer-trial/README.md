# Fresh distinct-package trial with the repaired observer

This is a new single-use procedure for TRWU8CBL. The preceding
[attempt](../2026-11-07-de25-distinct-launch-trial/README.md) remains failed and
consumed: it observed ZX81 launch/return and ZX80 launch, with no second
return, third launch or Quit. The last observed board state is running ZX80
at application generation 3; no park is inferred.

The matched SDK, firmware, loader, SOF, two packages and nine upload files are
unchanged. All 4,419 compiled-source checks still match. Only the reviewed host
observer/test files are permitted outside the documentation delta, and their
full bytes must match Git source
`cbe3c115096f3e2dff76857c4103a9957d62794c`. The native runner removes the old
index-one `during_play` callback. The outer result checker drops its obsolete
aggregate loop flag and retains direct, package-specific fetch checks alongside
all identity, generation, transfer, boot, shared-input and closure checks.
ROM-loop fixtures still require address zero when explicitly selected.

Sixteen off-board fixture groups pass: the retained actual four-command
capture, 12 prior-failure/park/count cases, 49 source rejections, 116 outer-result
mutations, 96 uploader cases, 400 receipt mutations and the existing transport,
startup-tail and lifecycle checks. The actual repaired helper's `exercise`
call is executed with the software model, including nonzero fetches. A new
negative control cannot reset the failed-candidate count or claim a prior park.
The initial fixture adaptations that still required the removed callback or
the old zero failure count failed and remain retained; they involved no kit.

The observer repair's affected command passes all planned checks, including
the parent regressions and native integration. Its 21 focused tests and saved
capture replay pass. These remain separate from physical acceptance.

The [frozen operator](operator-source.json) carries one prior failure on the
unchanged compiled candidate, within the existing two-failure limit. Prompt,
startup, launch, configuration, control, native, process, outer and host bounds
remain 50/100/100/20/5/450/480/740/850 seconds. Four original safety helpers
remain byte-identical: clock handling, per-CPU checks, private JTAG and recovery.
The stopped trace and independent 15 MHz/USB/UART cleanup are source-bound
prerequisites. Recovery uses the same bounded volatile recovery programming;
a newly verified loader prompt, card and source are required before writes.
This does not assert a prior park or qualify idle DDR/DMA.

Working files are in `out/nano-distinct-observer-board-20261107/`. Execution
still requires clean source, the continuing top-kit claim, exact transport and
free-UART preflight, verified retention and a fresh attempt fuse. The intended
sequence is nine uploads/fresh reads and ZX81 → ZX80 → ZX81 launch/return,
ending with owned Quit. Factory restoration remains waived; the factory card
and bottom kit stay outside the test. HDMI/input/media/native networking,
general package acceptance, physical DDR drain and FPGA timing remain open.
