# Isolated System Console SDM configuration query: PASS

TRWU8CBL at source `e1932868a7ef121ab024778941b22a62600135bb` passed the corrected
source-bound query. Fresh claim/source/USB/UART, fixed clock, mandatory
vendor-first listing and forced scan preceded the System Console packet call.
The exact private config packet service was selected among two PHY endpoints
and one config endpoint on the same top-kit/private-port path. The single
explicit command was CONFIG_STATUS, with complete response:

`00006000 00000000 001a0300 c0000089 00000003 00000000 0000000f`

The main script and configuration were immutable copies, staged for UID 65534
outside the inaccessible worktree. Generated read-only Tcl configuration binds
absolute paths and the private cable, avoiding process-environment and raw-eval
context differences. The exact vendor Tcl 8.0 interpreter passes the packet,
full collector, partial data and stale-file/replay models. A direct result file
and journal establish completion independently of console redirection.

System Console ran in a private network namespace with a top-only owned JTAG
server and no direct console write access to Altera USB devices. The source
copies and published artifacts match hashes. Independent host cleanup verifies
no remaining namespace members, private server absent, restored 15 MHz/auto1,
client mode 0600, exact USB permissions and free UART. An initial post-audit
assumed only one discovered endpoint; its retained failure was corrected by a
separate audit selecting the single config endpoint while checking all three
paths against the same private top-kit cable. No hardware gate was relaxed.

No QSPI session, array/register read, image programming, flash/SD write, reset
or reboot occurred. CPU Park remains the prior native observation. The original
factory card/flash and other kit were preserved. Earlier failed attempts remain
failed. This is a configuration-query transport diagnostic, not bulk throughput,
flash backup, rollback, core-load acceptance or autonomous power-on proof.

The [documented packet service](https://docs.altera.com/r/docs/683101/26.3/quartus-prime-pro-edition-system-console-and-toolkit-tcl-command-reference-manual/packet_send_command)
and exact installed vendor read pattern provide the interface basis. Next is a
restricted QSPI read session: fresh AFh identity, aligned data reads with exact
header/count/byte-order checks, bounded complete-array passes and independent
comparison. No persistent flash change is authorized by this result.
