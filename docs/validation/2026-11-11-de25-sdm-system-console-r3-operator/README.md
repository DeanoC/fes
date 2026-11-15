# Isolated System Console configuration packet query

One explicit CONFIG_STATUS query on TRWU8CBL uses the documented System Console
packet service. The exact installed vendor toolkit uses `packet_send_command
-format 32` on a claimed configuration packet path. This operator copies only
the small read pattern; it does not load the vendor GUI or generic flash tools.
The failed unsupported internal Tcl attempt remains failed and archived.

The whole attempt runs in a new network namespace with its own loopback and
claimed top-only JTAG server. No shared JTAG server is reachable there. The
System Console child runs as UID/GID 65534 with no supplementary groups; current
permission checks require no direct write access to any Altera USB device.
It uses a fresh private JVM home/temp directory and the source-bound private
client configuration. Installation startup RC bytes are frozen. It cannot fall
back to direct kit access. Its exact selected packet path must identify device
one, the top USB location, DE25-Nano, private localhost port and configuration
marker. Duplicate or foreign paths stop before an explicit command.

The complete seven-word CONFIG_STATUS response and source-matched ready values
must be observed once. The claimed packet service is closed once, including after
uncertain data. No QSPI session, array/register read, flash/SD write, image program,
controller handover, reset or reboot is enabled. One-use selection retires on any
failure, and no alternate transport or replay is permitted. Injected Tcl tests
cover exact arguments/path, duplicates, foreign port/device, partial/error data,
uncertain sends, close failures and no source-time IO. This is not a backup test.

Before entry, a fresh sixty-second parent/top USB observation, clean committed
source and complete frozen source/artifact/tool closure are required. Claim,
node, full-host UART inventory, fixed 3 MHz clock, mandatory vendor-first device
listing and fresh forced scan retain their existing checks. Each tool call is
bounded to 30 seconds; experiment 300 and host 330. Root host cleanup must restore
15 MHz/auto-adjust and exact USB permissions, restore client-config permissions,
stop only owned services, verify no task network-namespace members survive and
confirm the top UART is free. CPU Park is prior evidence, not reobserved here.

The installed [System Console packet command](https://docs.altera.com/r/docs/683101/26.3/quartus-prime-pro-edition-system-console-and-toolkit-tcl-command-reference-manual/packet_send_command)
and [SDM-over-JTAG application note](https://docs.altera.com/r/docs/683313/current/an-936-executing-sdm-commands-via-jtag-interface/executing-sdm-commands-via-jtag-interface)
provide the interface basis. The pinned installed toolkit, launcher, libraries,
namespace tools, scripts and previous results are in `operator-source.json`.
No future flash operation is admitted by this preparation. Factory media and
the other kit are preserved; whole-array backup and rollback remain open.
