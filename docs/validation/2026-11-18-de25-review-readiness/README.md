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
accepting 19,999,999us and rejecting exactly 20,000,000us and 21/24/25 seconds.
Thirty focused tests pass, including window service acceptance below 25 seconds
and rejection at/above 25 seconds, plus paired ownership/final-drain observers.
The original runner supplied an explicit PYTHONPATH; it is now recorded with
that invocation. The portable invocation below was also run successfully from
the checkout root, overriding the inherited test path:

```sh
env PYTHONPATH=tests python3 -m unittest \
  test_nano_configuration_budgets test_nano_paired_tenfoot_trace \
  test_nano_window_reload_trace test_nano_window_service_trace -v
```

The GitHub fix review identified that missing environment in the original
record. This correction preserves the original argv/result and adds both its
actual environment and the separately executed portable command; no runtime,
observer or test implementation changes.

The complete older-head local run is deliberately stopped after the finding;
its interrupted verdict is retained, not called a pass. All 11 complete-PR
commands now pass on implementation/review head
`2a35a1b909cc0771d5ad81c33b4475c5e61b638e`, as does its GitHub integration CI.
The independent fix-only review is clean on that head. A saved-wire replay also
passes with explicitly pinned current host decoders and unchanged source838
native artifacts. The local status wrapper separately ends with 143 while the
owned actor continues. Its successful 11-command report is independently checked under the
original PID/start identity and unchanged 3600s cap; the wrapper itself is not
called successful. The later environment/evidence correction changes only this
Markdown and JSON; final-head CI and evidence verification are recorded on the
live PR and retained outside Git. These source/result identities are separate.

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
