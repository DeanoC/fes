# Package-specific fetch expectations after the distinct-package stop

The [failed board procedure](../2026-11-07-de25-distinct-launch-trial/README.md)
loaded ZX81, returned, then loaded ZX80. Its reused runner's separate
`during_play` callback still asserted address zero at selection index one,
an assumption from the preceding `JP 0000` ROM. That callback was outside the
shared decoder and was not covered by the outer lifecycle fixtures. The
saved ZX80 address is 612; no further board command followed the stop.

`TenfootClient` now accepts an optional `fetch_address` in the trusted selected
program's observer data. The value must be a strict 16-bit integer and is
copied with the rest of the program. An explicit expectation, including zero,
is checked inside the existing launch transaction; mismatch retires the client
and prevents another command. A program without that fixture-specific
expectation keeps the existing bounded held/run counter and address checks,
package/build/generation binding, configuration ordering, firmware evidence
and shared USB rules. Selection index does not imply a CPU address.

Focused tests pass for normal nonzero fetches, a valid zero-address fixture,
mismatched expected addresses, immutable caller data and invalid expectation
types/ranges. Existing malformed/missing/stale fetch and shared USB cases pass.
The actual stopped capture also replays off-board: four exact command windows,
two launches and one return, including the real complete boot and C transfer
observations. A negative control executes the original callback extracted from
the frozen runner and reproduces its failure. Explicit-zero, foreign-identity
and missing-fetch controls reject the real capture without a fifth command.
The [replay receipt](replay.json) keeps these boundaries explicit.

This changes the host observer only. It changes no SDK/firmware/FPGA code,
configuration limit, deadline or physical admission. No board was accessed
for the repair. The old attempt stays consumed; it has no second-return,
third-launch or Quit proof, and the board is not claimed parked. A fresh
single-use operator must remove the obsolete callback, bind the repaired
decoder and preserve the prior failure count before a new hardware attempt.

Working evidence is in `out/nano-fetch-observer-repair-20261107/`. The failed
procedure and original source pack were independently retained/read back at
`/home/deano/fes-retained/de25-distinct-launch-stop-20261107-r1` before changing
the observer. General package, display/input/media/native-network, DDR-drain
and timing acceptance remain open.
