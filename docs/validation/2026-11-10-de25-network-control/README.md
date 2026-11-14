# Native network traffic gate and explicit DMA controls

The existing link now shares its mutable ownership across copied handles and
can bind one `network.Controller` with trusted `DMAControl.Stop/Start` callbacks.
Construction performs no hardware IO. Missing callbacks or a second supplier
reject. There is no default drain implementation or replacement supplier.

Pause first closes the software gate, then waits for every in-flight driver
Receive/Transmit. Only afterward may the trusted supplier stop that exact
device and independently prove all DMA/transactions drained. Repeating a
successful pause while closed makes no additional callback. Resume keeps the
gate closed until the paired platform restart completes. Neither operation
allocates or frees descriptors, reattaches the global socket provider or
reconfigures the IP stack; DMA memory stays reserved until reboot.

Each phase passes a context bounded to five seconds; suppliers must honor it.
A lost/failed completion, cancellation after gate closure or link failure keeps
traffic unavailable without retry. Cancellation while a driver call remains
pending cannot establish DMA containment and never invokes the drain callback.
Pre-cancellation rejects without changing a healthy link. Copying either handle
cannot replace the exclusive supplier, split control state or replay restart.
The native socket provider rejects new calls while paused; existing sockets
retain their application deadlines. Short pauses may preserve a connection;
long FPGA configuration is not claimed to preserve it.

Focused race tests cover pending RX and TX, ordering before hardware callbacks,
already-closed pause, paired restart, concurrent controls, incomplete/duplicate
suppliers, copied/fabricated handles, cancellation, platform failures and link
failure. An actual HTTP exchange crosses the real XGMAC descriptor driver,
Lneto ARP/IPv4/TCP and an independent gVisor HTTP peer. It then repeats three
stop/restart/request cycles and checks no driver register or descriptor-memory
access while paused. These hooks control only the synchronous register model;
they supply no firmware or physical drain proof.

The Linux-free QEMU package-fetch regression passes with the changed link:
exactly six export files, matching selected package/build/ROM/tape identities
and an unchanged FAT disk. All 2072 compiler input hashes remain unchanged.
QEMU ELF SHA-256:
`49afa16b29fa96c8b8953042ef627bf725068be64813b53c4c84e8db14504839`.
This run verifies ordinary content delivery, not pause/resume of real VirtIO
DMA. The optional missing RAMFB ROM warning remains in the log; no display is
tested. The model supplies pause/resume evidence separately.

Artifacts, source snapshots, commands and results remain under
`out/nano-network-control-20261110/`, indexed by `validation.json`.
All six affected checks pass, including parent Python regressions and generated
consumer consistency. The focused network/XGMAC race tests and QEMU regression
pass separately; source and artifact hashes have been verified unchanged.
The full Nano ARM64 launcher cross-build passes from committed source
`b595f610a2b1dae85a6b90144d9f3b28dbabd407`, based on
`b190ab76dab12eecc2aa820220f6055820222c55`.
All 2119 compiler input hashes match the current source. ELF SHA-256:
`f22403975f6f09492fec9d1fe64e19bf23c6e2932855653989518e6e11b0c501`.
The generated network profile stays disabled, SD stays read-only and preferences
stay volatile. No matched entry/BL31/FIT/SOF was rebuilt or deployed.
No kit, card, remote builder or programmer was touched. Physical testing remains
paused until the operator returns. Nano networking is still disabled; the
current board has no qualified NIC lease/drain supplier. Runtime integration
must also update the secure boot ownership contract: `FES_NANO_PRODUCERS_HPS`
currently includes EMAC0..2 among the stopped producers. Enabling a controller
without a qualified lease would contradict that accepted boot state. Integration
must cover all fabric/firmware lifecycle paths, including presentation failures,
retirement and final shutdown, before enabling it. This change does not bind
callbacks to only launch/return and leave those other paths unguarded.
Physical Ethernet, native SD writes, keyboard/HDMI and ordinary DE10-equivalent
core-loading acceptance remain open.
