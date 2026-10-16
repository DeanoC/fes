# Kit diagnostic active software identity — 2026-10-16

[PR #735](https://github.com/DeanoC/fes/pull/735) separates active bundle identity from base-image provenance in host health and diagnostic admission. Source revisions remain provenance; API/interface contracts and pinned bundle/base identities decide diagnostic admission.

## Sources and artifacts

The compiled host and bundle source is `0fc0f1f902d002b00ae1e1a543da2d26c284a9f7`. Final runner source is `211fa7c8ac890e9c8a73c1e587d0a6bf51b57d6d`. Only four script/test/documentation files changed between them: requested timeout propagation into private host configuration and removal of obsolete image/overlay guidance. No Go source changed.

The tested host SHA-256 is `97a16d81126eb4d4e7897df3cfbb4f057dafe999112bc93c40ba74bd700a3933`. Bundle `4ae0826c94159b09778d83ccf1c4ef84b7672772a86a8d8b89505743eb102b28` was built for retained base `32f16cd2eba9befefaef7f6c94ee70f5c830b3c231e859222df7c7aee7b3b993`, but was **not installed or activated during this validation**. Later, separately authorized testing may change the kit selection. The [build receipt](2026-10-16-kit-diagnostic-identity/bundle-build-receipt.json) retains exact source, toolchain, archive and member digests.

## Hardware diagnostic

The [initial diagnostic](2026-10-16-kit-diagnostic-identity/initial-hardware-receipt.json) and [final 60-second-budget diagnostic](2026-10-16-kit-diagnostic-identity/final-budget-hardware-receipt.json) both passed two isolated Pong cycles. Each cycle acknowledged 2 of 2 input events and advanced the input frame counter by 3. All private host containers exited successfully and were removed cleanly.

Both runs used the existing selected bundle `7d71e90f32da8d0ff7ce26472539bc067648abb0c5fc865654dc4aec8f1c04d0`, whose agent/runtime provenance is `b463f445b34eed9dbeabdbae5d92b63d5867a33d`, on base 32f from source `9af8dbb2b8d77f622a4414d8969efe5cb5f3cb5e`. The coordinating operator recorded unchanged boot `e9c755b2-b341-479c-b2dc-5b295f33f26d` and selection generation 6. Receipts separately record new-host, active-bundle and base provenance. Declared contracts were host/target API v1, runtime protocol 2, `fes-kit-maintenance-v1` and `fes-kit-software-v1`.

An intervening long-title attempt failed before launch: its generated catalog entry ID produced HTTP400 on GET, with zero completed cycles and clean container cleanup. The [failure summary](2026-10-16-kit-diagnostic-identity/long-title-failure-summary.json) preserves that separate catalog rejection; it is not counted as a passing run.

This is host health/tooling hardware evidence against retained target software, not deployment of the new target agent. Input acknowledgments and counter increments do not prove physical input consumption; no new HDMI acceptance is claimed. Public target-health redaction has host-test and ARM-build evidence only.

## Host checks

- Exact-source 0fc `make test-changed` completed all 8 planned commands with exit 0: parent 951 tests, 39 skips, 830.218s; host Go and UI lanes passed.
- Focused Python groups passed 62, 35 and 3 tests; the mixed-case fixture was corrected. Final runner 211 passed 36 isolated tests in 21.752s, 16 core-development tests and consistency checks. Initial mid-edit parser failures were corrected before those reruns.
- Package race checks passed: fogcast 477.348s, targetclient 7.277s, corrected hostapi rerun 7.391s, httpapi 1.976s. The original combined invocation exited 1 because a new paired-listener fixture omitted configured target settings; it remains a failed invocation despite the corrected passing rerun.
- The slow-inventory regression passed with race checks in 2.163s: selected-target inventory releases foreground lifecycle admission before waiting upstream, while its configured deadline remains bounded. Paired reads retain authenticated kit scope.
- ARMv7 `mister-agent` and `fogcast-api` builds passed with CGO disabled. Consistency and documentation-link checks passed.

The [validation record](2026-10-16-kit-diagnostic-identity/validation.json) contains source boundaries, receipt hashes and check results. Public evidence was scanned for credential-bearing fields and secret-value patterns. Preparing this record made no kit calls or software selections.
