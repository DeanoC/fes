# Read-only programmer access at the qualified clock

The installed Quartus 26.3 help documents `quartus_pgm -c <cable> -a` as displaying
the devices connected to a cable. A separate diagnostic invoked that exact
read-only form once through a fresh TRWU8CBL-only private server, with the
qualified clock policy setting 6M and restoring 15M afterward. No image, CDF or
programming operation was supplied, and no HPS reset or card write was attempted.

The programmer returned zero, listed the expected `4362C0DD` device and reported
zero errors/warnings. This distinguishes successful current read-only programmer
access at 6M from the prior programming-time scan failure. It does not reproduce
or explain error 87, establish SOF programming readiness, or qualify a full native
cycle. A fresh server/session differs from the failed recovery-to-native session;
that difference is a diagnostic lead, not a demonstrated cause or fix.

[result.json](result.json) binds the command, raw output, installed help and
independent cleanup audit. Current wrapper/executable hashes are recorded for
subsequent investigation. They do not retroactively identify the exact loaded
libraries/environment of the earlier failed programmer process.

Independent checks verify 6M during the query, 15M restoration, exact original
USB permissions, owned private-server exit and no UART owners. All raw records
remain under `out/nano-jtag-programmer-diag-20261108`. The earlier diagnostic's
reporting failures and original failed image attempts remain preserved separately.

No programming was retried; the image's two-attempt allowance remains consumed.
The claim is retained. No current FPGA/CPU state, native Park, return/shutdown,
physical display/input or normal core-load acceptance is inferred. Further work
must diagnose the programming path before a new programming procedure is proposed.
