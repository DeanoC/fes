# Ethernet on the paired C64/MENU launcher

On TRWU8CBL the retained 4 GB C64 card uses PIO SD, launcher boot ID
`af4407e48b21634e`, FIT `be9a5f82a0ff769676e75a64583b19f68f7fbcb5896405c5a574512d91c6fcbb`
and BL31 `d63e6026c7bcd9741e1055ca1ea829164cdf5e14675b5431fd5ec9f74a31e413`.
The SPL authenticates the size and SHA-256 of the SD FIT before it passes
control to firmware. A production trial can preserve this card and QSPI by
booting a separately prepared PIO card with its matching volatile phase-one
SOF. The new card needs the matched firmware, Ethernet-enabled launcher and
provisioned FogCast target UUID and bearer. [C64 startup and handback](README.md) ·
[Native FIT check](../../../platform/embedded/nano/spl_cold.c)

## Memory ownership

The following half-open physical ranges come from the retained C64/MENU
entry, SDK and BL31 receipts under Powerboat's
`out/c64-picker-fault-requalification/` in the `de25-c64-selector-pr`
worktree. The entry receipt SHA-256 is
`172832a94221054f31860c1af7264bef57c5c650bb35cd566646036d17754101`.
The SDK receipt is `4e883e4b3bae89bc7455cdd4a3f2fa86a653b33c6bad1a8fa07ae49d69f86a73`
and the BL31 receipt is `22c1a200180d430323e12d3838e829f0595f50f404fd2ddf9aceccf0f0411028`.

| Range | Owner or reservation |
| --- | --- |
| `0x80000000–0x82000000` | Firmware and initial U-Boot reservation. The matched BL31 image ends at `0x8005f000`. |
| `0x82000000–0x84000000` | SPL FIT load and recovery scratch |
| `0x84000000–0x94000000` | Native runtime RAM, including the entry decoder |
| `0x94000000–0x94010000` | SDM SMMU tables, command queue and event queue |
| `0x94800000–0x95000000` | FPGA configuration DMA source |
| `0x95000000–0x95800000` | Reader and MENU reservation |
| `0x95800000–0x95a00000` | Tape and metadata |
| `0x95a00000–0x95c00000` | Optional SD ADMA allocation in the current producer |
| `0x95c00000–0x95e00000` | Candidate Ethernet pool, 2 MiB aligned |
| `0x96080000–0x98080000` | Maximum native entry staging envelope. This C64 build ends at `0x96861000`. |
| `0x99000000–0x9a000000` | Current matched producer's verified FIT copy |
| `0xb0000000–0xc0000000` | High SPL/U-Boot memory reservation |

SDK and entry build checks at `74b301c0d` compare the proposed Ethernet
pool with the compiled configuration and reader ranges, selected tape,
SDM control memory, boot reservations and the full entry staging envelope.
The emitted EL1 and EL3 mappings still require verification. The production XGMAC API currently uses
identity DMA addresses. Restrict SID 1 access to the actual descriptor and
packet pages within the pool. The standalone diagnostic's physical frame
and descriptor pages at `0x94800000` overlap the C64 configuration source.
[Saved FIT geometry](../../../platform/embedded/nano/fit_layout.py) ·
[Native entry exclusions](../../../platform/embedded/entry/build.py)

## First MENU and later handoffs

The paired launcher reads startup files over PIO SD, programs MENU and
presents its first frame before `startProgramNetwork` runs. At Ready the UI
generation is zero, the first SDM transfer has retired, and its configuration
source is retained. Secure source release for reuse happens later, during a
program transition. The cold SDM gate requires a completely uninitialized MAC,
so registering a contained MAC early would already reject this first load.
[Tenfoot startup](../../../platform/embedded/tamago/cmd/fpgaload/tenfoot.go) ·
[First MENU frame](../../../platform/embedded/tamago/nativefabric/program_menu.go) ·
[SDM ownership](../../../platform/embedded/firmware/nano_sdm_source_platform.inc)

SDM retirement sets global Non-secure abort, disables translation and both
queues (`CR0=CR0ACK=0`), and records the consumed command pointer. Its table
still contains a translated SID 10. Enabling that table for Ethernet would
also re-enable SDM access. The next SDM load checks the previous retirement
receipt, table and exact queue pointer. Separate tables need a common EL3
owner that accounts for every intervening command and invalidation. A
software-supplied replacement pointer cannot satisfy that requirement.
The MENU reader's sealed route is **Secure SID 1**. Its window gate checks
both banks disabled and Non-secure global abort. MAC0 is **Non-secure SID
1**. MAC Stop in the standalone diagnostic leaves its bank enabled with an
abort entry, a different hardware state. [SDM setup and rearm](../../../platform/embedded/firmware/sdm_input_probe_setup.c) ·
[SDM retirement](../../../platform/embedded/firmware/source_retire.c) ·
[Reader proof](../../../platform/embedded/firmware/nano_sdm_reader.c)

### Matched trial

1. Build a PIO measurement image with the ordinary MAC supplier disabled.
   Capture paired, read-only EL3 snapshots after the first MENU frame, at
   C64 launch before source reuse, and on return to MENU. Record boot owner
   and epoch, role and source generations, both banks' controls and status,
   Non-secure table/queue bases and producer/consumer pointers, SID 1 and
   SID 10 entries, MAC reset/SID routing, and reader/SD/USB ownership.
   Compare them with the corresponding retirement, reader and release receipts.
2. Qualify a bounded switch using disjoint protected table memory. Show
   complete MAC stop/drain, disabled-and-aborting Non-secure control between
   owners, the inactive stream denied, acknowledged invalidations, and a
   fresh consumed queue position at each switch. The MAC grant must preserve
   the running MENU reader, leave the Secure bank unchanged and receive a
   matching reply. Repeat through C64 and back to MENU under the same boot
   owner, then verify the normal Park closure. [EL3 MAC lease](../../../platform/embedded/firmware/nano_mac.h) ·
   [ProgramService transitions](../../../platform/embedded/tamago/nativefabric/program_network.go)

The supported cold chain has no RAM-only input for a new launcher. SPL reads
and hashes `FES/UPLOAD.BIN`, and U-Boot verifies its preloaded entry. The
recovery TFTP tool writes its verified upload to FAT. A separate trial card
can preserve the original. Before linking the writable launcher SDK, obtain
that card's driver-format CID (30 hex digits) and 512-byte sector count.
The recovery template supplies the MBR and protected recovery file sizes
and hashes. The builder assembles and reads back FAT in a regular image
file, so the card's prior FAT cluster layout is unnecessary.
[SPL FIT verification](../../../platform/embedded/nano/spl_cold.c) ·
[U-Boot handoff](../../../platform/embedded/nano/uboot.c) ·
[Recovery upload](../../../platform/embedded/nano/upload.py) ·
[Image admission](../../../platform/embedded/nano/launcher_image.py)

## Wire qualification

After a qualified TX, the next bounded hardware test is an independent
host-to-CBL unicast frame with fresh bytes, a single RX descriptor and a
writable buffer inside the reserved pool. Compare the hardware length,
first/last/error bits and bytes with the emitted frame. Check the adjacent
guards. Run both directions, stop them, observe fresh channel stop
acknowledgements, drain both MTL queues, fence/revoke SID 1 and check for
late writes or SMMU events before reuse. Repeat after a fresh grant.
Full launcher validation also crosses MENU → C64 → MENU with PIO SD and USB
active as applicable, followed by mDNS discovery and authenticated target
status from a separate FogCast host.
