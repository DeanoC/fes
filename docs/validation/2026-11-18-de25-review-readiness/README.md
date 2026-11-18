# PR1952 review and validation preparation

The independent full review of `ce975a4c2b366b247b32d3fe5b7ece601824713c`
finds one P2: fixed `fpgaload.program()` probes keep their strict 20-second
owner deadline, but computer reload, window reload and initial window-service
trace checks inherited the service profile's 25-second limit. Synthetic
20/21/24-second fixed-probe traces were incorrectly accepted. The GitHub Codex
review of that same head reports no major findings; the independent finding is
still repaired before handoff.

The host observers now select the producer's limit. Both computer reload
cycles and the fixed window reload use 20 seconds. Initial window-service
startup also uses 20 seconds; later WindowService launches retain 25 seconds.
No runtime, firmware, ownership, integrity or time-limit change occurs.
[Before/after replay and focused checks](review-fix.json) show fixed probes
accepting 19,999,999us and rejecting exactly20,000,000us,21/24/25seconds.
Thirty focused tests pass, including window service acceptance below25seconds
and rejection at/above25seconds, plus paired ownership/final-drain observers.

The complete older-head local run is deliberately stopped after the finding;
its interrupted verdict is retained, not called a pass. Fresh full-PR checks,
CI and a review of the fix must finish on the resulting head. Their terminal
receipts and source identities are recorded on PR #1952 and retained outside
Git; this document does not assert those results in advance.

The [successful CBL serial cycle](../2026-11-18-de25-completion-cost/README.md)
uses native artifact source838 and remains separate from this host-only fix.
A fresh saved-wire replay can check the new observers against those unchanged
physical records; it cannot create a new hardware trial or an exact-head HIL
pass. Native code, firmware and core payloads are unchanged by this fix.

Ordinary J8G HIL and restore are still a separate gate. The managed runner
currently exercises Linux packages and cannot run this opted-in native boot
chain. Its kit also has a recorded claim. Neither a Linux package smoke nor
the CBL diagnostic silently waives that gate. A scoped waiver from Deano or an
applicable J8G native test is required before merging. Lane and readiness are
controlled by the live PR, after the user-requested review cycle and remaining
validation. Issue #1867 remains open for broader reset/DMA/failure qualification
and source-matched standalone historical receipt replay.
