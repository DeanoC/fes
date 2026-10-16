# Supported kit software acceptance runner

The [supported command](../kit-software-acceptance.md) passed an explicitly
authorized API lifecycle diagnostic on the designated `.84` kit. The runner
exited zero. [Result](2026-10-16-kit-software-acceptance/result.json),
[baseline](2026-10-16-kit-software-acceptance/baseline.json),
[final ready state](2026-10-16-kit-software-acceptance/final-ready.json) and
[source/test identities](2026-10-16-kit-software-acceptance/evidence.json) are
sanitized public records. No credentials, private maintenance records, operator
argv or frozen archives are published.

## Exact source and validation boundaries

Hardware executed source `509d6fbb`; the runner SHA-256 is
`f45f5389ed171ee057eb4254ee09d853c60530558ea6627f40206e81a59cde3c`.
The subsequent rebase is `7cb817ab`, onto dependency `99dfe381`; the runner,
focused tests, command guide and README are byte-identical to the hardware source.
The original dependency base was `211fa7c8`.

The six-command parent test run passed 898 tests with one skip in 242.393 seconds
at source `1d762cbf`. The final budget correction at `509d6fbb` passed all 24
focused runner tests in 15.002 seconds and the hardware diagnostic below.
`make check` passed again on rebased `7cb817ab`. These are separate source-bound
results, not a claim that the earlier parent run executed the later correction.

## Observed kit transitions

Base image remained
`32f16cd2eba9befefaef7f6c94ee70f5c830b3c231e859222df7c7aee7b3b993`.
Baseline selected bundle `7d71e90f…` was generation 6 on boot `e9c755b2…`.

- Held installation, competing-client conflict, exact duplicate/conflict/stale
  generation checks, cancellation, retained idle Stop and requester-process exit
  passed. The competing client is an independent HTTP client, not a claim about
  two physical workstations. Retained Stop did not launch a game.
- Changed candidate
  `4ae0826c94159b09778d83ccf1c4ef84b7672772a86a8d8b89505743eb102b28`
  accepted at generation 7 without changing boot or base.
- The explicitly configured reboot command ran through the queued-maintenance
  path. Persistent reselection accepted once at generation 8 on new boot
  `c59b9142-a6c2-44e4-beb4-6f0f503b0461`, preserving the base and candidate.
- Explicit failing-candidate recovery returned a matching `recovered` receipt,
  preserving that selected bundle, generation 8 and new boot. Each accepted or
  recovered transition recorded ten seconds of stable readiness, idle runtime
  and free admission. Receipt/phase observations do not count every process start.
- The external local probe reported busy while the grant was held. Its temporary
  helper was verified absent after reboot. No successful local launch is claimed.
- Supported core inventory identities and selections remained unchanged,
  including `fes.coleco` package `cf41dc16…`. This is not byte-persistence or
  Coleco execution evidence.

The final public observation is ready, idle and free, with the candidate still
selected/active at generation 8 and no pending/unreleased operation or fault.
There was no test-driven restoration of the prior release.

Input and HDMI stages were explicitly **skipped**. This diagnostic establishes
the recorded software API lifecycle behavior; it does not claim overall hardware,
physical input, video or core-execution acceptance.
