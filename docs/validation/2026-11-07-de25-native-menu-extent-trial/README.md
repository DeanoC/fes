# Bounded exact-extent menu procedure, 2026-11-07

This is a fresh procedure for the
[repaired menu firmware and real opening order](../2026-11-07-de25-native-menu-firewall-fix/README.md).
The earlier zero-read firewall attempt is retained and consumed. It supplies
startup/configuration evidence, with no menu frame acceptance, and is not
replayed. [operator-source.json](operator-source.json) freezes this procedure's
exact helper and fixture hashes.

The matched firmware admits the exact 8 MiB menu observation while requiring
stable full-region coverage before DDR release. The observer expects readiness
and initial route records inside `open_begin`, matching the real firmware.
The FPGA package, native SDK/entry, buffer reservation and runtime selections
are unchanged. The separate repair record retains the broad source-suite
failure and its passing isolated/clean-main controls; it does not claim that
original command passed. All menu-specific and native integration gates passed.

This procedure uses the same top-only private JTAG, clock, recovery and
receive-only failure-tail helpers. In one owned recovery session it verifies
the spare, writes only the package and experimental FIT at the two existing
fixed destinations, then performs both fresh final SD readbacks. One native
boot must acknowledge three real Tenfoot frames in slots 0/1/0 and complete
independent firmware master-stop/bridge closure. It grants no physical HDMI,
general DDR or complete menu/core/menu acceptance.

The complete candidate check was measured rather than weakened: 82 context
workers completed in about 154 seconds. Its first 180-second host envelope
expired under variable host load, with no device operation. The fresh read-only
preflight has a 300-second host envelope. All validation calls remain present.
Device-phase limits remain 50 seconds for the loader prompt, 100 for storage,
45 for configuration/presentation/return, 240 for native work, 270 for its
process, 740 for the outer procedure and 850 including host cleanup.

The new attempt has its own durable fuse and fresh source, claim, USB/UART,
card and recovery proofs. First failure is terminal. Cleanup restores 15 MHz
and exact USB permissions and stops only its private server, independently of
the result. TRWU8CBL's spare and continuing claim may remain under the existing
factory-restoration waiver; the original factory card and bottom kit are
untouched. No physical controller action is required.

The one physical attempt stopped before the first frame command. Both uploads
and fresh SD reads passed. Native boot/configuration completed; all 8 MiB passed
the SMMU/firewall route observations, followed by control generation 5 and data
generation 6 (reset `5`). The real Tenfoot renderer produced the first frame.
CPU publication copied 3,686,400 bytes but exceeded the presenter's one-second
operation budget before START: zero command attempts, polls or accepted frames.
The measured presentation-to-return interval was 2,538,634 microseconds.

Independent firmware master-stop completed against the active data path, then
closed the bridges at generation 7/reset `f`. The arena remains retained until
reboot. Host cleanup independently passed: exact USB permissions, 15 MHz
readback, private server gone and UART free. The consumed attempt cannot be
replayed. This establishes memory-route opening and active-path closure for
this diagnostic; it establishes neither scanned menu frames nor physical HDMI.
The next repair separates bounded CPU publication from the hardware wait budget
while preserving ownership and integrity checks. See [result.json](result.json).
