# Native Tenfoot hardware launch and return

One bounded procedure on **TRWU8CBL passed**, with no retry:
ZX81 → return → ZX80 → return → ZX81 → owned Quit. The actual native Tenfoot
App used its in-process runtime backend, real SD/FAT reader, secure configuration
path and FPGA identity endpoint. Launch generations were 1/3/5; return/Quit
generations were 2/4/6. The final native state is held with an empty identity.

Observer source: `4ecd73cfe`; native SDK: `c0cf35bac`; secure firmware/loader:
`faa14c06b`; FPGA producer: `7808f74c2`; board source: `79d616e85`. Every selected
native source byte matches the observer checkout. The firmware identity includes
the owned-installer input closure. The catalogue contains two distinct actual
sealed packages, without a new FPGA build in this procedure.

Qualified recovery and all seven fixed uploads/fresh SD readbacks passed before
native boot. All 14 applicable CPU checkpoints and the App Ready observation
passed. Launches took 71.733, 71.485 and 71.742 seconds; the two returns and final
Quit took 0.362, 0.313 and 0.313 seconds. Configuration, control and startup remain
within their existing bounds. Independent replay verifies the raw App/firmware
evidence for all eight key exchanges. No failure injection occurred on hardware;
software/firmware-model failure coverage is recorded in the preceding USB/runtime
validation.

Host-only setup stopped once at a catalogue proof/runner filename collision; the
regenerated proof matches all six catalogue files byte for byte. Archive checking
then caught valid internal directory symlinks. Corrected retention independently
read back 48,937 regular files and checked 59 relative internal links. Both stopped
setup records remain; neither caused a hardware retry or image rebuild. Combined
source-bundle restoration and strict fsck passed. The retained archive is under
`/home/deano/fes-retained/de25-tenfoot-core-cycle-20261104-r2/`; its exact dependency
archives, hashes and restoration requirements are in `retention.json`. External
toolchains/modules remain pinned, rather than exported as an offline SDK.

Cleanup independently confirms 15 MHz JTAG, original USB ownership/permissions,
no private JTAG process and a free UART. The board is parked after owned Quit.
The continuing exclusive claim and spare card remain; the original factory card
is untouched. No factory restoration was requested or performed.

This passes the selected-package **serial App lifecycle diagnostic**. It does not
qualify ordinary package/ROM/parts/media admission, physical framebuffer/core
execution, HDMI, USB hub/controller input or native Ethernet. Firmware and service
receipts retain their diagnostic-only acceptance flags. DE10-style readiness
remains incomplete; these physical gates and normal core/package integration
still require implementation and verification.
