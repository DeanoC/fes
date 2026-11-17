# DE25 baseline final USB failure and host-only reset

Later hardware state and candidate results are recorded in [USB recovery and warm preparation stop](../2026-11-14-de25-usb-recovered-warm-stop/README.md). Board-state statements below describe this earlier checkpoint.

Following the second fresh human-confirmed physical reset, read-only restoration `ac4945e957b76ce474ae436dedfbbffb0a4a40829a99ac7b6a5f2c8931b14bcd` verified all seventeen original files through independent nonce-bound saved-wire replay, with zero SD writes or network commands. Exact original FIT `d79a8e…` and its sealed native boot passed. The repaired expected-word image was not programmed.

The native baseline issued 51 commands. Independent prefix replay verifies the first 50 and two core launches. The final Quit was issued, but USB Close failed with `usbcontrol: firmware status 4`, followed by a retained-ownership panic. The final menu drain appears in the trace; the full final application Park receipt is absent and is not inferred from that drain. No enclosing host deadline expired (487.083 seconds total).

The failed state was preserved, rather than immediately requesting another physical reset. All eight owned processes stopped; independent root checks verified UART free, original USB identity/inode/permissions and auto/15 MHz JTAG settings. [Baseline result](baseline-result.json) and [baseline retention](baseline-retention-proof.json) record the actual failure. Archive `2ba2b7d30f5da13f6128a661e5bb9c7acd9e9c4874e0444b4f084eb593bd5bc4` independently verifies all 146 entries. The replay explicitly stops before the unverified Quit and cannot fabricate a successful final receipt.

## Requested host-side USB reset

The human requested trying either/both USB sides instead of another power cycle. A separate one-attempt capsule was bound to the actual failed state, source hash, continuing top-kit claim, exact device inode and a root scan proving no UART/top-adapter FD remained open. Its dry check opened no device. Its source and exact plan were retained before execution.

The capsule issued one `USBDEVFS_RESET` to TRWU8CBL's programming/serial USB adapter, vendor/product `09fb:6026`. It never selected the parent hub or bottom kit, programmed an FPGA, wrote SD/QSPI, or reset HPS USB0. The syscall returned, the top device remained enumerated throughout forty observations over twenty seconds, and the bottom kit's complete USB identity/inode snapshot was unchanged. A fresh root FD scan found no remaining top-adapter or UART handle.

[Exact plan](host-adapter-plan.json), [actual result](host-adapter-result.json), and [retention proof](host-adapter-retention-proof.json) describe the scope. Archive `20685e9f65b69cabaf967ff0be194890ffb3ea7eb198e483c3e87de5876a4b06` independently verifies all 16 retained entries, including the capsule source, plan, attempt, syscall-issued record, result and supervisor logs. No physical power cycle occurred between the failed baseline and this adapter reset. No subsequent JTAG programming or native command was issued.

This proves a contained host-adapter reset and continued enumeration. It does not prove a JTAG programming repair, restart the on-kit keyboard USB controller, or recover the panicked runtime. Board recovery is explicitly false in the capsule result. The existing failed baseline procedure remains consumed and stopped.

## On-kit recovery still needed

The separate native USB0 controller is the DWC2 block at `0x10b00000`. The source maps its module and packet-RAM resets to PER0 bits 3 and 11 (`0x808`). Normal firmware USB Stop first checks owner/epoch/generation and PIO scope, halts enabled channels, removes port power/reset signaling, checks AHB idle, flushes FIFOs and asserts those resets with independent stopped-state readback. It retains ownership if any step fails.

The current runtime has panicked and cannot invoke that normal stop again. The native driver and shared USB client deliberately retire their old instances after uncertainty; a USB reset alone cannot turn them into fresh owners or restart the application. The installed `quartus_hps --help=operation` exposes device/flash operations, not a generic USB-peripheral reset. No flash or debugger command was attempted.

The next on-kit recovery experiment needs an explicit trusted entry that can reset USB0 after fresh CPU/PIO/master ownership checks, then establish new device enumeration and application/lease state. It must preserve the old failed objects and source reservations, reject foreign DMA/IRQ or changed boot ownership, and grant no FPGA/SMMU reuse from a USB reset alone. That path is not implemented or physically qualified by this record.

Current spare card contains the exact original FIT. The last observed native state is the final USB ownership panic; after the host-only reset its runtime state has not been re-admitted. All our hardware handles are closed and the continuing top-kit claim remains held for on-kit recovery investigation. QSPI, factory card and bottom kit remain untouched. No complete baseline Park, repaired-image hardware acceptance or Linux-equivalent repeated loading is claimed.
