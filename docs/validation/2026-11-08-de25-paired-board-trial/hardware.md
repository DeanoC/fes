# Paired native hardware stop on TRWU8CBL

Operator/source `c3cfb88bfb78532796a48086fc78174753794877`; one attempt,
no retry. The recovery boot, twelve fixed Ethernet/SD writes and twelve fresh
readbacks passed. Native cold boot, all fourteen CPU checkpoints, the shared
USB descriptor/pin proof, complete menu configuration and the full 8 MiB route
passed for the exact retained artifacts.

The menu acknowledged two frames (slots 0 and 1), with zero underflows. The
application was still loading its catalogue; it never reached a complete
launcher Ready with two titles. No launch/focus/stop/quit input was sent and no
computer was launched.

The native loop then reported a DWC2 interrupt-IN packet timeout. It retired
its session and retained resources. The independent secure callback reported
reader generation 1/cookie 1 drained, pending zero, and bridge closure at
generation 7/reset `0xf`. No successful native menu-return telemetry or final
Parked receipt was emitted; the strict menu parser correctly refuses to call
that a completed cycle. This is a partial diagnostic, not core-load acceptance.

Independent cleanup readback passed: exact original USB permissions, recorded
15 MHz JTAG clock, owned private server gone and UART free. The board remains
in native panic after the observed drain/closure; it has not been factory
restored or independently shown CPU parked. The spare stays in the top board;
the factory card is preserved, and the continuing DE25 claim remains owned.
No bottom-kit access occurred.

The next step is to reproduce and repair USB polling service across the
blocking menu path without disabling USB or weakening its timeouts, then
rebuild and re-admit the exact native chain before another bounded attempt.
The service-delay explanation is a hypothesis until source-level reproduction.
Native HDMI capture, real input controls and full appliance acceptance remain
open. Raw source-bound evidence is in `out/nano-paired-board-20261108`.
