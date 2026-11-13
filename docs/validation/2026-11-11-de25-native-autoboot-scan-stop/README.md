# Native autoboot trial stopped at JTAG admission

Operator source `28309be398be7a4f8199e56cb42a1f62c47eb8f6`, observer code `50f9bc8cd`,
prepared native boot `548caa50395f3d94` from producer `303771739`.
This attempt failed. No automatic native startup or core cycle was observed.

The fresh recovery epoch programmed and booted successfully. Its one exclusive
session verified the previous FIT, thirteen staged sources and three installed
records, refreshed only `FES/UPLOAD.BIN`, and verified all seventeen files again.
The core sources and installed records were preserved; protected recovery files
were rechecked. The session closed at a verified recovery prompt.

The separate native epoch stopped at the initial vendor device-list check.
The vendor printed `Unable to read device chain - JTAG chain broken`, omitted
the expected FPGA ID, and nevertheless reported zero programmer errors. The
operator rejected that output. It issued no native programming grant or command;
the native UART capture is empty. Its scan reservation is retired and the
attempt will not be retried. Historical programming failures remain three.

Independent cleanup passes: both private services are absent, both role archives
match their saved hashes, clock policy returned to 15 MHz/automatic adjustment,
exact USB permissions were restored and the top UART is free. This establishes
host cleanup, not a fresh board boot or native Park. The test card now contains
the verified candidate FIT; the last observed board state was the recovery
prompt. The factory card and flash were preserved and no restore was requested.

Raw evidence is under `out/nano-autoboot-operator-20261111/`; curated hashes and
exact limits are in `hardware-result.json`. USB/power failure is not established.
Next: retain this failed attempt, then use a separate read-only diagnostic to
check board responsiveness and distinguish a real JTAG chain problem from
client enumeration state. Any future programming needs a reviewed correction,
fresh source/ownership/transport admission and a new bounded attempt.
