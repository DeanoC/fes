# Provisional flash windows; CLI cross-check stopped

The frozen `fb1830181` operator completed its System Console sequence on
TRWU8CBL. It read all three selected 4 KiB windows with exact 1025-word replies,
AFh identity 20bb18, zero status bytes and unchanged CONFIG_STATUS. A single
CLOSE was acknowledged. Twenty-two root PRE/POST grants, full raw responses
and little-endian binary bytes independently replay correctly. The first two
windows contain structured nonuniform data; the final window is all FF.

The subsequent CLI session stopped at its first CONFIG_STATUS invocation:
`Can't scan JTAG chain. Error code 87.` It returned 3 with no SDM response.
No CLI OPEN or array read followed. The vendor AJI header calls 87
AJI_BAD_JTAG_CHAIN; that name alone does not establish a physical cable defect.
The complete attempt is failed, the selected windows remain provisional and
no complete backup or usable rollback is claimed. No retry was issued.

Independent host cleanup passed: no private namespace members or JTAG server
remain, 15 MHz/auto-adjust 1 and exact USB permissions are restored, the private
client config is still mode600 and the top UART is free. No image programming,
reset/reboot, flash or SD writes occurred. Prior native Park remains the last
CPU-state evidence; it was not reobserved. The spare card remains installed,
factory media is preserved and the bottom kit was not operated on.

Next isolate the transport boundary with a fresh CLI-only private server and
its own mandatory vendor-first listing and forced scan, then cross-check the
saved windows only if all gates pass. This is a separately prepared experiment,
not a replay of the stopped reservation. A second failure stops that experiment.
Two complete independent main-array reads and concrete rollback preparation
still precede any request for approval to change boot flash.
