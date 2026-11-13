# Bounded menu CPU publication, 2026-11-07

The [previous physical attempt](../2026-11-07-de25-native-menu-extent-trial/README.md)
passed full 8 MiB memory-route admission, control/data opening and independent
active master-stop/closure. Its first 3,686,400-byte CPU publication exceeded the
presenter's old one-second operation deadline before START. No frame was accepted.
That consumed attempt and its complete trace remain retained separately.

`scanout.MenuLimits` now gives the entire spare-slot copy and full-arena
publication/integrity check one explicit budget, capped at four seconds. The
subsequent endpoint commands and waits retain their one-second deadline and
shared 64-poll bound. Opening and stopping retain their one-second bounds.
A transition checks the old deadline, cancellation, ownership and clock
monotonicity before starting the new phase; no chunk or polling loop renews a
deadline. Publication failures are reported separately from copying. Both
slots stay pinned on failure, and native session Close still attempts the
independent firmware master-stop and bridge closure. No integrity check,
firmware gate, memory boundary, FPGA timing rule or outer board bound changed.

The actual menu CSR/video/256-bit DDR model passes 39 presenter cases without
skips. New regressions reproduce 2.5 seconds of CPU work followed by 0.9 seconds
of hardware work for two frames; cumulative copy/publication overruns, a full
one-second hardware overrun, clock regression and ownership loss all reject
without replay. Session tests cover slow success, pre-START overrun, invalid
budgets before device I/O and independent failure closure with memory retained.
All 489 model input hashes equal the committed implementation's exact Git blobs.

The affected parent suite passes 1,633 tests with three existing skips. The
original native integration command stopped during the pinned-reference download
with a DNS lookup failure, before tests. That failed receipt is retained. The
same integration passed using the exact previously verified local source blob,
with the source identity checks unchanged. It exercises native lifecycle, real
prepared C firmware/FPGA RTL and Tenfoot launch/return/failures with injected
platform services; it is not physical acceptance.

A fresh native SDK, entry, BL31, SPL and HPS-first composition pass the matched
chain audit: 4,454 recorded inputs, actual firmware BSS end `0x5afc0`, final SPL
allocation guard after trace and before ATF, with its rejection branch and private
RAM cookie preserved. The FIT remains 8,051,232 bytes, below the unchanged 8 MiB
limit. FPGA package/build identity and core bytes are unchanged. See
[result.json](result.json) for exact sources, hashes and check receipts.

This prepares the next distinct bounded board attempt. It grants no scanned
menu frames, physical HDMI, full menu/core/menu cycle or ordinary package
admission. Publication throughput still needs measurement and improvement for
an interactive launcher; fixing deadline ownership does not establish that
performance. The continuing top-kit claim/spare remain held under the existing
factory-restoration waiver; the factory card and bottom kit are untouched.
