# Retained DE25 headless image: native boot and automatic modules

On 2026-11-05, bottom board TRWU8J8G cold-booted the retained image from producer
`0da63d1044cc7ecf9908efba52af17b3e55e6271`. Its 3,984,588,800 bytes were written
to the user-designated spare and verified by a complete device readback after
flushing the block buffers. Both hashes were
`0d703468eeec8bddec2f591d7acd4cd3c674f94878ec589bca56a3fda22a4171`.
The current working FES card was authenticated in the board before the write,
kept aside during the trial and restored afterward. No FPGA or QSPI was programmed.

The [boot log](boot-uart.log) reports `FPGA drivers: ready` during init. The
[selected service](../../../image/de25-nano/rootfs/overlay/etc/init.d/S15fpga)
matched the reviewed PR bytes, hash
`b479a4473ee45b6c3daebbbc5c7f1f875c361393f8e6ce613e8f39a317f1c1d3`.
[Authenticated observations](modules-network.log) confirm all four modules
loaded without a manual modprobe: `stratix10_soc`, `of_fpga_region`,
`fpga_region` and `fpga_bridge`. Their installed bytes match every hash in the
[module lock](../../../image/de25-nano/kernel-modules.lock.json). The manager
name is `Stratix10 SOC FPGA Manager`, and `region0` exists. Kernel release is
`6.12.11-gd7d192a9ddd9`, architecture AArch64, boot
`0483ff1b-30bb-4f10-9cf1-c377c572e044`.

The initial DHCP discovery missed the link. The selected background client
continued without intervention and configured `192.168.10.203/24`, a default
route and DNS; its process and `inet dhcp` configuration were observed. There
is no separately retained DHCP server lease record. The SSH key was obtained
over the exclusively owned, serial-number-selected physical console before
SSH authentication. The unmodified original producer's first-boot collector
and verifier passed; see [receipt](first-boot/receipt.json).

The running writable root does not provide a whole-image identity marker.
The full image hash is a pre-boot readback observation; the first-boot receipt
binds selected observed files. This proves one physical coldboot and automatic
module initialization. Software reboot of this exact legacy image, MAC
stability, managed FES/runtime, DMA/display, audio and PR acceptance are not
established by this check. Later managed-image evidence stays separate.

After clean shutdown, the first working-card restoration stopped in SPL with
an SD timeout (`-110`). The user reseated that card; it then booted the named
managed FES image `6de6a39230ce4bc364f34b7c606a5ed2c218d1315906c7e7f8b1ef9bbb566076`
and bundle `a14a53cd33e2c51d7639701363bf95cc7781f62b8207c0e2bab1653276504700`,
generation 34. Their identity, original card CID and owner kernel hashes were
authenticated at new boot `2ae639a5-0925-4fdf-9a2e-e8c295299130`. The first
execution claim was refused during startup; the separate settled observation
passed. The board was ready, idle and lease-free, with no maintenance or owned
UART/SSH session remaining. The top board was untouched.

[Evidence](evidence.json) binds the logs, selected identities and restoration.
Private credentials and console authentication logs are not included.
