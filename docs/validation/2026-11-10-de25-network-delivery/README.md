# Nano export download and owned installation (software only)

The native download prototype reads the existing Nano export over HTTP. It uses
`FES/CORESEL.JSN` and `FES/UI.JSON`, then fetches only the selected archive,
metadata, ROM sources and optional tape. No new delivery format, programming
profile or firmware authority is introduced. Shared control-file decoders and
the existing package/ROM validator retain private content; the backend applies
its compiled platform admission before producing an opaque install plan.

Preparation touches neither local storage nor firmware. Installation remains a
separate operation under the existing Ready generation, input pause, menu
return, active SD lease and menu restoration. The downloader never starts an
inbound listener or installs automatically. The transport must honor request
contexts; cancellation supplies no DMA containment.

Origins are fixed numeric IPv4 HTTP addresses with an optional export prefix
and virtual Host. Redirects, transformed responses, length changes, corrupt
archive/metadata/ROM content and unknown catalogue IDs fail before device IO.
Controls are bounded to 8 KiB each, metadata to 16 KiB, each firmware source to
256 KiB and tape to the existing 16 KiB limit. Payloads total at most 64 MiB,
with a 100-second total download bound and bounded zero-progress reads. Package
identity and ROM linking use the normal canonical validation; no network source
or installed record grants activation or physical trust.

Tests use actual local TCP HTTP servers with injected native firmware/storage
boundaries. They verify URL filename escaping, fixed Host, no redirects or
transformations, truncated/oversized responses, cancellation, zero-progress
reads, close failures and content rejection before device changes. The combined
test closes the server after preparation, installs to a real FAT image through
owned storage, launches/returns, models a fresh boot, discovers the published
record and launches/returns again. Independent mtools readback checks the disk.

The Linux-free QEMU `programfetch` run separately uses the actual VirtIO/Lneto
TCP stack. It fetches exactly six files from the selected retained Nano export
and verifies package, build, metadata, ROM receipt and tape identities. Its
fresh FAT image is byte-identical before and after; the UART reports content
verification only, no platform admission, storage writes or hardware. Packet
capture, console, tool identities and all 2071 compiler input hashes are kept
in `out/nano-network-delivery-20261110/qemu-fetch-runtime/`. ELF SHA-256:
`9c37feef4db99b8c0f6bef5ba1d0646af93b3414886c827ea99a4e92295e6876`.
The initial attempt failed before guest boot because the host QEMU loader could
not find its bundled libraries. That result is retained separately; the passing
run uses the existing private library path with a fresh output directory.
The QEMU log also retains a missing optional RAMFB ROM warning; this diagnostic
checks HTTP content and disk identity and makes no display claim.

All seven selected checks pass: planner/whitespace checks, parent Python
regressions, generated consumer consistency and native firmware/FPGA lifecycle
integration. The latter uses real patched firmware C and Verilated endpoint RTL
with injected physical boundaries. The source snapshots remained unchanged.
All evidence and source digests are recorded in
`out/nano-network-delivery-20261110/validation.json`. This milestone does not
qualify physical native Ethernet, NIC stop/drain, SD writes, power-cut recovery,
visible HDMI or physical keyboard return. The Nano board still explicitly opens
SD read-only, uses volatile preferences and disables native networking. No kit,
card, firmware or FPGA was touched. Hardware testing remains paused until the
operator returns; the DE10-equivalent core-loading goal remains incomplete.
