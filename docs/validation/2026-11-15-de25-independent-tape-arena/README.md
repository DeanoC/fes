# Independent tape arena prototype and original-FIT restoration

The [warm-core trial](../2026-11-15-de25-warm-core-and-tape-stop/README.md)
passed MENU and warm ZX81 transfer/retirement/continuation, then correctly
refused initial-tape publication into its retained configuration reservation.
The completed source and metadata must remain protected. This prototype adds a
separate publication mechanism; it does not select it in a board image yet.

`InitialTapeArena` owns an independent pinned mapping and requires a new,
privately validated completed configuration for each publication. It serializes
both leases, rechecks the protected source/header/ROM, leaves configuration
memory unchanged, and cannot release that source through tape completion.
Copied tape handles share private state. A copied configuration owner or aliased
backing memory rejects before access. A new generation alone cannot bypass a
consumed program lease. Uncertain publication retains its mapping; source
mutation also retires the configuration lease. A trusted platform caller still
owes live ownership and CPU/firmware mapping checks around every SMC.

The candidate layout is `0x95800000..0x95a00000`: a 16 KiB payload at its start
and an independent metadata page at `0x959ff000`. Geometry checks use the actual
existing paired-program SDK resource map and native-entry envelope. They retain
the entire 8 MiB configuration arena and both 4 MiB MENU slots; heap, firmware,
FIT, readback, entry and other declared boot reservations remain excluded.
Payload capacity for FPGA configuration is unchanged. Geometry supplies no
physical reservation, live mapping or firmware authority.

Thirteen targeted Go tests and 51 subtests pass with the race detector,
including the existing configuration/retained-source guards. The new publisher
joins the actual generic C tape protocol engine at 1, 512, 917 and 16,375 bytes
without changing retained source/metadata. Nine Python layout/selection tests
pass, rejecting missing, changed, aliased and overflowing resource descriptions.
Compiled Go constants agree with the Python layout derived from the retained
SDK/entry records. These are host models and protocol-engine tests; the fixed
hardware supplier and CPU/EL3 mapping have not been changed. Source/readback
archive: `9bd9125f9b88705694e024a9ba729b0c11726541b6efcfab9c7eb1b5e18e6ee2`.

**Still pending:** select the independent arena through the board hook, map and
verify it at CPU and firmware, switch the fixed tape supplier and bind the
memory contract through all matched producers/admission. Remove the borrowed
configuration-storage publisher when that replacement is complete. No new boot
image, physical tape-load result, full launch/return/Park or general core-load
acceptance is claimed by this prototype.

After the human's fresh reset for consumed trial `16fe97ce…`, restoration-only
`fbe754ce00877f8ecc61c4b911742b318c6b8701c2d80f05ca6da07976a4d564`
passes in 267.984 seconds. Independent replay verifies fresh recovery, one exact
original-FIT write and all 17 before-and-after file readbacks. Original FIT:
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`.
Restoration archive `ae977c7ee0df5d9348daff14c93e8203d1509a77b70f582dbc98f8d67b8be14b`
verifies 79 entries and independently closed owned processes/UART/adapter
handles. USB permissions and JTAG auto/15M settings are restored. The top kit
is parked in the recovery loader with the original FIT on the spare and its
claim held. The old native runtime was not booted; the failed diagnostic was
not retried. QSPI, factory media and the bottom kit were untouched.
