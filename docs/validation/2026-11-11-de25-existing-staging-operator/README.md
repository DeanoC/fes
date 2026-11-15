# File admission for installation from existing staging

The private operator at `out/nano-existing-staging-install-20261111` now has a
complete file/source admission for the matched fc2cec132701a0d8 native image.
The selected SDK, entry, firmware, U-Boot, sealed projects and recovery inputs
pass their existing validators. Admission checks 5,665 files and eight trees;
wrong-digest and lost-ownership controls reject. This is off-board evidence only,
not a new cold-boot, USB, installation or launch pass.

Its recovery phase reads the independently expected prior ZX81 record and all
fourteen staged files, without network transfer or SD writes. The actual
orchestration block runs with injected transports and the real read-receipt
checker in controlled tests. Bad prior content or a substituted recovery receipt
prevents the native transport from opening. A native failure ends the sequence
without another recovery or native attempt. The independent epoch and vendor
gate tests pass; qualified private-service, scan, one-use grant, clock restoration
and USB/UART cleanup primitives remain unchanged.

The new connection precondition binds any correction record to the retained
failed USB attempt and its console hash. It accepts a direct human report of a
verified upstream reconnection, not an unchanged-state assertion or elapsed time.
Temporary model records cover malformed, duplicated, oversized, symlinked,
wrong-kit and wrong-failure cases. No actual connection-resolution record exists.
Running the real preflight without that record rejects before any live preflight
or programming grant is created. A future independent USB diagnostic would need
its own admitted procedure; this record does not authorize such a diagnostic.

The procedure in operator-source.json is frozen against the tested private
helpers, public source trees, tool hashes and file-admission record. It denies
live authority. The pending correction and fresh root claim/source/60-second USB/
complete UART-owner checks remain mandatory. Recovery/source-read/native/outer/
host bounds remain 270/300/450/800/890 seconds, respectively. Neither USB nor its
500 ms connection requirement is disabled or extended to bypass the failed boot.

Only private operator/preflight preparation changed after `d4972027c`. Its passing
1,956-test parent result is reused for unchanged public implementation bytes.
Current tests cover the new correction and orchestration guards; prior passing
read, UART and two-core receipt models remain recorded with their scope. Full
hardware acceptance still needs the physical connection resolved, a separately
admitted two-core installation/launch/return attempt, and independent reboot
discovery without reinstalling. No board operation occurred during this step.
