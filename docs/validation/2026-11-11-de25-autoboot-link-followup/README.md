# Read-only follow-up to the rejected native JTAG scan

The failed trial remains failed and archived separately. Its recovery epoch
successfully refreshed only the native FIT and preserved seventeen checked
files; native programming never began. This follow-up grants no programming
permission and does not establish automatic startup.

The top kit responded to one carriage return with memory-information output
and a fresh `SOCFPGA_AGILEX5` prompt. That establishes a responsive CPU console;
it is not a fresh boot proof. The UART was closed and independently found free.

In a separate owned private service, the unchanged qualified forced-scan probe
verified FPGA ID `4362C0DD`; the following vendor device list also reported that
ID. A second fresh service then reported the ID in vendor-first order, verified
it with the forced probe, and reported it again afterward. Neither session
programmed the FPGA, requested a CPU/FPGA reset, wrote SD data or reused the
failed programming reservation. Both restored 15 MHz/automatic adjustment,
stopped their private server and restored exact USB permissions.

The link is readable in these observations. The original cause remains
unproven: the later vendor-first success prevents attributing recovery solely
to command ordering or client-cache initialization. The observations are not
an automatic retry or qualification of the failed attempt.

Next: freeze and admit a new bounded automatic-start trial, preserving the
mandatory vendor/forced-scan identity gates. The spare card now contains native
FIT SHA-256 `d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`.
A read-only recovery admission can verify its seventeen staged/installed files;
the old refresh operator expects the previous FIT and must not be replayed.
Native boot `548caa50395f3d94` and both installed launch/return cycles still need
hardware evidence. No factory restore, card swap or power cycle was requested.
