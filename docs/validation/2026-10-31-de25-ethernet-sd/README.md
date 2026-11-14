# TRWU8CBL Ethernet download and SD scratch readback

The bounded recovery-loader trial **passed on TRWU8CBL**. The Nano booted its
matched SPL/strict BL31/U-Boot without Linux, obtained DHCP with an explicitly
assigned MAC, downloaded 65,573 bytes over Ethernet, verified SHA-256, wrote
`FES/NETTEST.BIN` to the spare SD and read back identical bytes into a separate
RAM buffer. The FAT boot FIT and the entire raw prefix before the FAT partition
matched their original hashes before and after the scratch write.

This qualifies that recovery-loader path for this board, spare card and session.
It does not qualify the native owned firmware, TamaGo Ethernet/SD drivers,
tenfoot, automatic boot-chain updating or persistence through power loss.
The original factory card and vendor-reference FPGA image were restored and
verified afterward; the top kit is released.

## Source and exact artifacts

The packaging fix is commit `37f9e68717d879b69180495431b1e7513120931b`, based on
`94cdf3b02f8af5273da26c669b01e575b2a22298`. The existing pinned U-Boot/SPL and
strict BL31 were reused unchanged. The corrected package receipt binds the
packager's exact committed source hash. This continues the
[secure handoff and recovery preparation](../2026-10-31-de25-trusted-handoff/README.md).

| Input | SHA-256 |
| --- | --- |
| Corrected 144 MiB SD image | `12ab480664451db28c93d6dacd9f01e1533b31979e5924879c728b909f8ac485` |
| Volatile recovery SOF | `f5ec8595a88a0bbc9205e9b50cfa00def22e3cc9e65ddb1af8dcde1ad42ba3ec` |
| FIT, FAT and raw copies | `71f413629a7714ab1a7bd4c6331969ba2670bdc447ddbc3bb8c103e52b525721` |
| Strict BL31 | `d5bc92fad82d8ee60ebfb0cda1efeea4fbb065dfec1c5af3aa73194e320cf121` |
| 65,573-byte network/scratch payload | `aaf7bb1f45972199ec04264630af2a3d5e992ee8aec8d60ed6f2b81c67bec207` |
| Protected first 16 MiB, before and after | `2586b521d2e8b69622c6c7d52156a540ef5a1c695ae410a38f2c1483054cad71` |

Only cable `DE25-Nano [3-11.1-iface0]`, FPGA ID `4362C0DD` and UART serial
`TRWU8CBL` interface `if02` at 115200 were used. The program operation targeted
volatile JTAG SRAM. No flash or factory-card image/boot settings were written.
The 4 GB spare was identified by capacity, USB path and a complete image-prefix
hash matching the card prepared by this task earlier. Prefix backups remain
private. The reader briefly reported no media; reseating restored its expected
3,980,394,496-byte capacity before the corrected write/readback.

## Initial failure and repair

The initial candidate reached the exact recovery SPL and 1 GiB DDR calibration,
then stopped because `/u-boot.itb` was absent from FAT. The vendor
`arch/arm/mach-socfpga/spl_soc64.c` selects filesystem boot whenever
`CONFIG_SPL_FS_FAT` is enabled. A configured raw sector of `0x200` does not make
that branch select raw mode. The original packager copied the FIT only to that
raw offset. No full U-Boot prompt, Ethernet operation or SD write was reached
in the failed attempt.

The corrected packager requires FAT partition 1 and the exact payload name,
copies `/u-boot.itb` to FAT, reads it back with mtools and compares every byte
through SHA-256. It still verifies the duplicate raw FIT, manifest, MBR, FAT
geometry and backup boot sector. The new regression executes real mtools,
checks both FIT copies and rejects a corrupted FAT-file readback. All five
focused recovery tests and all six affected checks passed before retry. The
second attempt uses the same SPL/SOF and a corrected card image; both attempts'
raw logs and receipts are retained.

## Bounded physical result

The passing trial ran from `2026-10-31T17:59:31Z` to `17:59:52Z`, within a
360-second overall cap and separate 30/90/45-second query/program/boot bounds.
Each loader command has a deadline and an independently checked result code.
It used the vendor manual prompt, with no automatic boot command or writes.

1. SPL loaded the FAT FIT, checked its image CRCs and booted the matching
   `2025.01-fes-nano-recovery-gd0f8813fd6bf` full loader. The live DDR bank,
   loader relocation, 3.7 GiB SD, partition geometry, boot prefix, boot FIT and
   card manifest passed before enabling the transfer/write sequence.
2. MAC `02:fe:25:08:cb:01` was assigned in the volatile environment, random MAC
   generation disabled. The driver negotiated the physical link and obtained
   DHCP address `192.168.10.211`; the host was `192.168.10.202`. This is not a
   persistent factory-Linux MAC/IP change. The DT's all-ones MAC warning is
   retained; the explicit environment address was used successfully.
3. A bounded host TFTP server accepted only the named octet download from the
   observed client address. The actual board acknowledged all 65,573 bytes.
   The loader independently hashed RAM at `0x82000000` and matched the expected
   digest. The server then closed.
4. `fatwrite` wrote exactly 65,573 bytes to `FES/NETTEST.BIN`. A separate buffer
   at `0x82100000` was filled with a different pattern, then populated using
   `fatload`. Its SHA-256 matched, and `cmp.b` reported all 65,573 bytes equal.
   The vendor block write path invalidates its software read cache; no claim
   of durability through card power loss is made.
5. SD reads and hashes again matched the first 16 MiB and FAT `/u-boot.itb`.
   No raw block write, boot FIT replacement or persistent environment save
   was issued. The loader was left parked at its prompt, and own UART,
   programmer and TFTP clients closed.

These results show that subsequent file transfers and SD file writes can use
Ethernet while the spare stays in the board. JTAG starts or restores the
matching loader; this is not a direct JTAG SD writer. The reserved raw slot has
no selecting SPL/updater yet. A protected complete boot-chain update remains
separate work; the scratch test does not establish an atomic update scheme.

## Verified factory restoration

After the user restored the original factory card, the exact reference SOF
`e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09` was loaded by
volatile JTAG. Raw factory boot output reached Ubuntu 22.04.3 LTS login. A fresh
authenticated UART session verified kernel `6.12.11-gd7d192a9ddd9`, root
`/dev/mmcblk0p2 ext3`, original card CID/capacity and Nano model, with new boot
UUID `8700edbb-81df-4107-9c04-c8d679cf0a60`.

The session logged out and closed. Exact top USB permissions were restored to
`root:root 0664`, the task's TFTP listener was gone, and the shared user JTAG
broker was left running. TRWU8CBL is idle and released in its authoritative
status file. No other kit or harness was touched. No physical power cycle was
requested; autonomous QSPI power-on boot was not retested. Normal factory
Linux login/shutdown accounting can change its root filesystem.

[evidence.json](evidence.json) binds the source, artifact, trial, recovery and
software-check receipts. Complete public diagnostic outputs are retained at
`/home/deano/fes-retained/de25-nano-ethernet-sd-20261031/`; private credentials
and spare-prefix backups are excluded. The previous firmware/toolchain outputs
remain in the separate trusted-handoff retention directory.
