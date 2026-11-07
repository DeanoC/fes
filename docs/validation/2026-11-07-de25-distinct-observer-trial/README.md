# Fresh distinct-package trial with the repaired observer

This is a new single-use procedure for TRWU8CBL. The preceding
[attempt](../2026-11-07-de25-distinct-launch-trial/README.md) remains failed and
consumed: it observed ZX81 launch/return and ZX80 launch, with no second
return, third launch or Quit. At that preceding stop, the last observed board state was running ZX80
at application generation 3; no park was inferred.

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

## Physical result

The fresh procedure passed on TRWU8CBL in 317.002 seconds with operator source
`4dfdcfda8b7ddca008f29ac91c5206accaf4b319`. All nine Ethernet uploads passed
write/readback verification and separate fresh SD reads. The native runtime
then completed ZX81 → return → ZX80 → return → ZX81 → return, with launch/return
generations 1/2, 3/4 and 5/6, before owned Quit. Launches took 54.861, 54.331
and 54.888 seconds. Package, FPGA build, payload and retained ROM identities
matched each selected core; instruction fetch activity was observed.

Three master-stop completion records and 41 shared USB reports were captured.
Independent cleanup passed: JTAG clock restored to 15 MHz, USB identity and
permissions unchanged, owned private server gone and UART unowned. The last
observed board state is native Tenfoot exit ready at generation 6 with USB
stopped. The continuing top-kit claim remains held; factory restoration was
waived. The original factory card and bottom kit were untouched.

The [result](result.json) retains exact evidence hashes and preserves the
preceding failed attempt. This is physical serial launch/return evidence for
the two specified packages. No physical input action or HDMI observation was
part of this procedure. General admission, native runtime networking, physical
DDR/DMA drain and FPGA timing signoff remain unqualified.
