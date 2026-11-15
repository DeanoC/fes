# Measured duration for complete main-array backup

The predecessor recovered its earlier uncertain flash session, then read7,892,992
bytes in1927 complete packets before its450-second soft stop. That prefix and
the unique acknowledged CLOSE independently replay correctly; host cleanup and
retention passed. It remains an incomplete attempt and will not be resumed.

Measured hardware throughput makes16MiB a roughly15–25minute operation, despite
much faster injected models. This new admission changes only host read-time
allowances: data soft stop3300seconds, hard console3600, experiment3700 and host
3800. Other commands remain30seconds and each output file64MiB. No native boot,
FIT/configuration arena, RAM map, core-load or runtime limit changes. The wrapper
supports this explicitly bounded job rather than its earlier900-second maximum.

The exact full-array Tcl reader, byte codec, raw collector, immutable staging and
stream verifier are unchanged. The root packet guard changes only its soft-stop
clock constant. The full16MiB/4096-read vendor Tcl model, eighteen failure cases
and independent corruption cases remain bound by hashes; the actual Tcl owned
soft-stop/ownership-loss tests pass again for this guard. Selective tracing is
retained and its selected JNI mapping events must appear in the audit.

Each of two fresh sessions reads bytes0 through0xffffff from the beginning,
using4096 fixed1024-word READ3a packets. Each packet requires root PRE/POST
ownership grants. CONFIG_STATUS, OPEN, chip0, AFh20bb18 and two zero status reads
precede data; acknowledged unique CLOSE and unchanged CONFIG_STATUS follow it.
All complete replies are streamed to a journal and little-endian16MiB binary.
Independent replay checks every request/header/count/data byte,4096 chunk hashes,
the qualified windows and whole-file SHA256. Two complete hashes and byte streams
must match. Partial prefixes are preserved as failures and never resumed.

Each pass uses a new network namespace/top-only private server, fresh60sUSB,
source/claim/node/UART/3MHzauto0/vendor-first/forced-scan gates. The first pass
must complete replay and cleanup before the second is admitted. The console is
UID65534 without direct Altera USB write permission and reads root-owned immutable
inputs. A soft stop permits one CLOSE while owned; loss of ownership blocks IO.
The hard limit still handles an unresponsive packet. Cleanup checks namespace
and server absence,15MHz/auto1, exact USB permissions/config bytes+mode600 and
free UART. CPU Park is not reobserved.

No flash/SDwrite, erase, generic opcode, controller handover, image programming,
reset or reboot is admitted. Factory media and the other kit stay preserved.
A complete main-array backup still excludes OTP/security. Usable rollback and
explicit flash-change approval remain required before autonomous boot testing.
