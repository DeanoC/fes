# Two-core trial stopped at USB startup

The single bounded TRWU8CBL attempt at operator `dc2b92830`, using the matched
fc2cec132701a0d8 native image compiled from `e4e695d5b`, failed before reaching
the installer. Recovery and native programming completed, and the matched
secure native cold boot passed. All fourteen staged files uploaded and passed
independent SD readback. The independently expected 754-byte prior ZX81
installed record matched before and after staging. Those reads have
capacity/layout/protected-file scope; they do not prove a native writable CID
admission or two-core persistence across a subsequent boot.

Native startup then reported USB driver failure: register 0x440 read 0x1000
while waiting 500 ms for connection bit 0. Source inspection identifies this
as the powered root-port connection wait, before the port reset/enumeration
sequence. The recorded StartupFailure says stop_attempted=true and
stop_verified=true. The existing disconnected-startup Go control passes with
the same register/value/mask signature, one verified firmware stop and no
enumeration or retry. This is consistent with an absent upstream USB connection;
it does not establish whether the cause is cabling, hub power, the PHY or timing.
All eighteen selected DWC2, USB-control and relevant board/entry USB source files
match the earlier single-core image that passed physically. This comparison
does not establish the cause of the current connection failure.

The trace contains no sd_open_begin checkpoint or App command. The compiled
main calls openProgramUSB before opening SD storage. There were no native
installs or core launches. The operator stopped on the first failure and retained
its two-second receive-only tail; no retry, timeout expansion, synthetic receipt
or USB-disabled replacement was used. The staging writes remain on the spare
card. Neither the overall attempt nor normal final Park is reported as passed.

An independent root read-only host audit confirms both private programming
services are absent, their transport archives verify, clock restoration was
15 MHz with automatic adjustment enabled, USB permissions match their original
values, and the UART has no owner. Both programming operations completed, so
the historical transport-programming failure count remains three. That host
cleanup evidence does not prove a new board-level final Park. The board remains
stopped at failed startup with the claim held and the original factory card
untouched. The user has been asked to check the powered hub's upstream connection
without changing board power.

Private trace, phase receipts, programming grants and failed-attempt audit are
in `out/nano-catalog-install-trial-20261111`. Retention uses a separate capsule
from the prepared operator, preserving the failed physical baseline and its
exact source. Before another hardware attempt, resolve the connection evidence
and make a fresh bounded admission; this failed native proof must not be replayed
as a passing install test. Two installs, both launch/return cycles and independent
reboot discovery remain unverified.
