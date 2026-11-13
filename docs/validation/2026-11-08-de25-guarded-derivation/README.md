# Guarded cold-build derivations and matched startup-input chain

The cold builder now reuses identical isolated HPS worker results within one
file-guarded derivation. It still runs every original validator and requires
matching before/after source bytes and tree membership, including complete
sealed projects, declared source roots, selected entry files and native Go
sources. Separate pre/post compilation calls start empty. Returned JSON is
copied, worker failures are not cached, and environment, interpreter, directory,
project, mode and ROM receipt distinguish queries. Live device/owner checks
are unchanged. No reuse survives the derivation.

The initial broad run executed 1,740 tests, with four errors and three skips.
The four errors identified an unintended private-directory restriction on the
non-HPS diagnostic path. Commit 7c8f1b47f preserves that original path and limits
the new guard to HPS derivations. All 14 handoff tests, nine cold tests and eight
scoped-derivation tests then pass. Other broad-suite passes are retained; the
entire broad suite was not repeated. Native-file admission, HPS context and RAM
checks pass, as do generated-consumer checks (52 generated files, 37 fixtures).
The initial failure and stopped intermediate U-Boot build remain in evidence.

The actual sealed-project comparison on corrected source produces identical
complete cold records: 14 worker launches in 49.484 seconds versus 102 launches
in 233.089 seconds without reuse. This is host build-cost evidence. It supplies
no hardware performance or timing qualification.

The complete SDK/entry/BL31/SPL/FIT/SOF chain passes with boot ID
`c85f65c331fc3ab5`. Secure firmware completes in 109.395 seconds under its
unchanged 360-second limit; U-Boot completes in 225.850 seconds and composition
in 118.738 seconds under their original bounds. PFG composition held the shared
Quartus lock. Independent ELF/load checks retain 7,488 bytes of OCRAM headroom.
Final checks cover 2,261 source/artifact digests, matched receipts, embedded SPL
bytes and unchanged core payload. No incomplete producer result was promoted.

The runtime and entry were built at e8fe4c9a6. The subsequent correction only
changes the cold wrapper and its test, which their selected source mappings do
not include. Their exact original paths and receipts are reused without
rewriting them; all selected source hashes remain current. New firmware and
composition use 7c8f1b47f and independently revalidate those artifacts. The
runtime includes the previously tested startup neutral-input fence and actual
100-ms keyboard idle snapshots; firmware includes the tape callback diagnostics.
These changed artifacts still need physical qualification.

Build and check trees are `out/nano-guarded-derivation-final-20261108/` and
`out/nano-derivation-reuse-20261108/`. Exact unchanged SDK/entry and the stopped
intermediate chain remain in `out/nano-guarded-derivation-runtime-20261108/`.
The retention target is
`/home/deano/fes-retained/de25-guarded-derivation-matched-chain-20261108-r1`;
its retention receipt requires full archive readback and Git bundle verification.

No new board, UART, JTAG, SD or flash operation occurred. TRWU8CBL remains under
the continuing top-kit claim, with its spare card installed and factory restore
waived unless needed; the bottom kit is untouched. The retired 4489 attempt
remains consumed, and its final fabric state is unproven. The user reported
possible unintended keyboard pressure, now cleared; neither a physical press
nor driver fault was proven. The next step is a separately frozen one-use
operator and bounded startup/tape profile trial with fresh live admission.
Tape transfer, ordinary core-load acceptance, physical HDMI and the broader
DE10-equivalent appliance goal remain open.
