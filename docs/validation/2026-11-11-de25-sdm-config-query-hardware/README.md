# Internal Tcl configuration query rejected

One bounded source-bound attempt on TRWU8CBL reached the selected Quartus Pro
26.3 library. The saved trace independently confirms that exact library was
mapped. The vendor-first and forced-scan gates passed. The Tcl command then
rejected `-cable` with exit code 3 before producing a successful SDM response.
No configuration query is qualified, and this attempt is terminal. The injected
model established the wrapper's restrictions, not actual vendor argument support.

The installed package's metadata already warned that no executable currently
supports it. This result rules out assuming that loading the internal package
provides its apparent argument contract. No live retry or alternate invocation
was attempted. The next approach uses the documented System Console packet
service and the matching installed vendor toolkit's read-only call pattern.

No QSPI session, array read, flash/SD write, image program, reset or reboot was
part of the script. Independent cleanup confirms the owned service absent,
restored 15 MHz/auto-adjust, exact USB permissions and free UART. An initial
post-audit assumed a generic exit code 1 and rejected the log; the corrected
separate audit checks the actual retained tool code 3 and passes. Neither audit
performed hardware commands. All logs are preserved. CPU Park remains the prior
native observation. Successful AFh identification and inferred 16 MiB capacity
are unchanged; no backup, rollback or autonomous power-on qualification exists.
