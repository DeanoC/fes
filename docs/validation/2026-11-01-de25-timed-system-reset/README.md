# DE25 same-clock reset timing repair

Producer `34c3a6cb4d2858c273e2f95a0a94fcb46f8b416b` builds canonical MENU with
`timed-system-reset-release-v1`. BUILD_ID is `ca03ea0a398d1bfa5104482d07fdc4c4`.
The 1,744,896-byte RBF has SHA256
`58181300c2ce74ba5092d154d3f04779334da7a8b4b4c8938717c95c9ccae5db`
and remains unprogrammed and unsealed.

The profile removes inappropriate false paths from exactly three reset receivers
released by the same 100 MHz system PLL: `soc_inst|rst_controller`,
`soc_inst|subsys_periph|rst_controller` and `soc_inst|rst_controller_002`.
This strengthens timing requirements and adds no exception. The standalone
maintenance profile keeps its original constraints. No RTL or clock changes
are included.

The private project requires six reset-controller SDC files matching the reviewed
Quartus 26.3 bytes. It retains their originals, changes only the two conditional
clear exceptions and records both hashes in [reset-timing.json](reset-timing.json).
The board SDC removes only the system reset bridge from its asynchronous-clear
set, reducing the count from ten to eight. Pixel, FIFO, debug, configuration and
visibility exceptions remain. Generation and both sides of compilation recheck
the exact constraints; regeneration or changed inputs stop Linux phase conversion.
The canonical record binds the policy before IP generation.

| Check at this producer | Result |
| --- | --- |
| Actual Quartus 26.3 Pro compile, conversion and fitted-input review | PASS, 639.3 s; [receipt](compile.json) |
| Generated interconnect WITH MODELED M20K STORAGE | PASS, ten cases in 70.9 s; [receipt](model.json) |
| Fitted reset paths and raw clock-to-pad characterization | PASS, 10.5 s; [receipt](output-query.json) |
| Independent source, artifact, fitted report and provenance checks | PASS, 1.0 s; [checks](checks.json), [receipt](verification.json) |
| Complete affected software plan | PASS, 1951.3 s; [summary](affected-summary.json), [receipt](software.json), [log](software.log.gz) |

The software plan passes all seven selected commands: planner tests (30 and 32),
committed/working whitespace, 1,114 parent tests (three skipped), generated consumer
consistency and 1737 FPGA tests (76 skipped). Parent tests take
993.645 s; the FPGA suite takes 950.996 s. The 27 focused scanout checks and
12 canonical producer checks also pass during development. They cover SDK drift,
linked/missing inputs, changed retained/derived constraints, the actual Tcl
instance guard and post-compilation regeneration before conversion.

A private copy of this exact fit is queried at all four available operating
conditions. All 24 recovery/removal path checks are present and nonnegative;
both ends use the 100 MHz system clock. Worst recovery is 7.613 ns and worst
removal is 0.265 ns. The [actual paths](reset-paths.tsv) and original reports are
retained. The query changes no original constraints, RBF, reports or database.

All 74 reported timing checks pass. The complete 85-rule inventory now reports
RES-50101 with zero violations. Raw failed classes are TMC-20012 (30), TMC-20011
(two) and RDC-50003 (two), with zero waived findings. The explicitly
[approved two-input policy](../2026-11-01-de25-reviewed-inputs/README.md) still
accepts only HDMI SDA/interrupt missing-delay findings after fresh fitted checks.
Two unreviewed classes continue to block export. No manifest, package or
independent identical-RBF signoff rebuild is produced.

The [reset inventory](rdc-inventory.json) names 12 chains in the system domain and
six in the pixel domain. These are two reported groups, not 18 observed faults.
The common-reset transport model does not prove staggered board startup, full
reset/boot allocation epochs or outer JTAG/interconnect quiescence. No aggregate
reset waiver or whole-boundary qualification is supplied.

Independent verification checks all 372 canonical source inputs, 175 generated
model HDL files and 17 QIPs. The earlier four-case/40-cell HDMI controller proof
is reused only after all ten compiled source files match exactly. It retains its
original producer and `async2sync` digital reset model; physical asynchronous
reset, vendor RAM equivalence and IO timing remain outside those proofs.

The user has no additional PCB files. The public
[Macnica DE25 desktop reference](https://malt.zendesk.com/hc/en-us/articles/55726472185881-SoC-FPGA-Linux-Desktop-Environment-for-Terasic-DE25-Nano)
restores successfully (1,627 files), but its HDMI SDC cuts video, forwarded clock
and I2C paths and supplies no HDMI trace-delay budget. The actual QAR, constraints
and [review](reference-review.json) are retained; this reference cannot close the
FES output gate. The [ADI lookup](hdmi-reference-notes.json) confirms the retained
no-delay clock request; it changes no setting or datasheet requirement. No PCB
delay is invented.

The first `3c95972b1` software run fails committed whitespace in 1.0 s because the
byte-exact vendor fixture contains trailing spaces. Storing those same bytes
compressed fixes the check. Its test-source change alters the canonical closure,
so the first compile is deliberately superseded after 58.9 s. The corrected
producer above is a fresh full compile. Both [original failed receipts](original-failures.json)
and logs remain failed in retention; no first-attempt artifact is reused.

The [archive verifier](archive-checks.json) checks both canonical records, the RBF,
both reset receipts, twelve original/twelve adapted generated SDC copies and both
board SDCs. The [full source bundle](source.json) is restored and its tree and
original failed fixture verified. The final [retention manifest](retention.json)
authenticates 108 files under
`out/artifacts/de25-timed-system-reset/34c3a6cb4/`, including the full compiler
archive, source bundle, reports, completed software and original failures.

No board, SD, kernel, bundle or QSPI change occurs. Native factory/MENU/catalog
lifecycle, managed image/storage/startup wiring, physical IO/reset/security and
exact-artifact acceptance remain open under the
[normal-kit scope](../../design/de25-linux-platform.md). Earlier ABI2 board
observations qualify only their named artifacts. The bottom board remains claimed;
no baseline is promoted or released.
