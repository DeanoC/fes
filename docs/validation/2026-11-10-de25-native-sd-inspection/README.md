# Fresh SD-only inspection procedure

Base: `8e07258b8`. The compiled inspector and its SD-only DDR boot chain remain
unchanged. The host decoder now explicitly selects the inspection record and
requires exactly one bounded plan after the original card read and before
containment. It binds CID/capacity to that read, checks the exact plan fields
and format, and requires the complete resumed read and final SD containment.
Unknown, duplicate, misplaced, malformed, foreign-card and already-written plans
fail. Default storage decoding cannot silently accept the new record. No parser
projection, synthesized history or skipped marker supplies a success.

The 59 focused trace tests pass, including fragmented plan lines and legacy
read-only behavior. All six selected checks pass, including 1,857 parent tests
(three existing skips) and generated consumer consistency. The private operator
models exercise its actual decoder wiring and single-upload receipt checker,
including nine negative identity, protection, readback and boundary cases.

Before preparing this procedure, a fresh five-second read-only UART observation
matched the old returned menu2/owner `bccd55323262a6e3`, bridge17/reset5 and
consecutive frames6382–6384. A separately consumed one-byte App Quit completed
its real master-stop/reader-drain/bridge closure18/reset15 and App Park3, followed
by the shared USB stop. Exact termios restoration and independent raw firmware
record checks pass. CPU park was not observed. This is cleanup of the active
menu; the preceding failed timed display capture remains failed.

`operator-source.json` freezes the 2,410-file SD-only input set, exact helper
code, source-tree membership, external tool aliases and vendor scan binary.
This is preparation only. Full candidate/source validation is repeated before
native device access; no paired/USB cached admission is reused. The existing
qualified top-only private server, fixed3MHz/auto0 clock policy, fresh vendor
scan and consumed-once programming grant remain required for each program.
Original clock/auto settings and USB permissions must be restored, and the
private service stopped. A reconnect or failed gate stops the attempt.

The hardware sequence is one qualified recovery boot, one fixed Ethernet upload
of `FES/UPLOAD.BIN`, a fresh independent readback, then one matched native boot
and `fesboot`. Recovery updates that single file. The native diagnostic only
reads the recovery manifest, MBR and final eight sectors, contains/resumes the
same card, and ends SD-contained. Its plan still needs independent review
before a separate writer is built. No native SD write, core launch, ordinary
admission or full appliance qualification is claimed by this procedure.

Raw helpers, models and preparation results are under
`out/nano-sd-inspect-trial-20261110/`. The hardware attempt has not started in
this source record, and no live preflight or programming grant is archived as
permission to execute it later.

## Hardware result

[The single hardware attempt](hardware-result.json) passes on TRWU8CBL for the
matched inspector build `4d1d7cc52d07940d`, compiled from `8e07258b8` with host
observer `91e248fda`. Recovery boot, the single boot-FIT upload, its independent
fresh SD readback, complete cold/ATF/EL2/EL1 proof, native reads, contain/resume
and final SD containment all pass. The host restores 15MHz/auto1, exact USB
permissions, stops its private JTAG service and leaves the UART free. There
are no new programming failures. CPU park was not observed.

The [independent plan review](real-plan-review.json) matches CID/capacity to the
previous retained physical spare-card capture. Its MBR hash matches the qualified
recovery image, whose sole FAT partition spans sectors32768–294911. The proposed
eight-sector scratch starts at7774200, outside that partition and the declared
144MiB boot image. Both recovery and native U-Boot use volatile environments.
The original tail contents were hashed, never dumped or modified by native code.

The recovery loader wrote `FES/UPLOAD.BIN`; the native inspector wrote no SD
sectors. The captured plan is build input for a separate writer and grants no
live operation. Native writes, FAT persistence and ordinary installation remain
unqualified. Full raw evidence is under the private trial directory named above.
