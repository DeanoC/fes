# Matched boot chain for distinct native computer packages

A fresh native SDK, entry, BL31, SPL/FIT and HPS-first SOF now agree on the
version-two boot-configuration contract and the existing owned CPU/SD/USB/FPGA
resources. The actual ZX81 and ZX80 packages and their 8/4 KiB firmware pass
retained admission and modeled launch/return against this SDK. This record
prepares a physical test; it does not establish physical boot or activation.

The SDK/runtime implementation is
`d07593783a9ebd18436856e5f83898a979209dd2`. Entry/firmware/loader/composition
ran from `c9d797a1aaa516def6e99b19fde7be9cfaa2207c`; its additional tracked
change is the test decoder's fixed staging limit and validation records.
The boot FPGA remains the qualified predecessor's sealed ZX81 payload from
`6c6dd62b503f143448a65e17ba346e410187e960`. The independently sealed ZX80
payload is from `36a38da8e9f792a7404ca91000826bb8f3b65153`. Their distinct
identities and compatibility investigation are recorded in the
[package validation](../2026-11-07-de25-distinct-computer-packages/README.md).

The normal native checker accepts the new chain and rejects the old complete
FAT chain against the current sources. Fresh audits check the linked CPU array,
both firmware memory reservations, whole-file seals and all 24 retained
low-level object hashes. The CPU array remains a 256-byte initialized DDR
object; the loader and FIT retain the same 2 MiB firmware reservation. The
compiled old/new SDK source comparison identifies exactly four changed inputs:
the two HPS metadata derivation files, native selected-program loader and
default-off SD clock prototype. No compiled input was added. The boot device,
control, memory and ROM maps remain identical. The faster SD mode is unselected.

The matched FIT is 8,219,184 bytes, SHA-256
`c841ea0a2b14041f5d8760c4f29f4bd29bb2c2c5620978c4f2cc0623fa424184`.
The [result](result.json) binds the SOF, source checks, audit receipts and all
nine fixed upload files. The extra file is the independent alternate FPGA
package in the recovery loader's existing `FES/PKGALT.BIN` slot. Native
startup, launch, transfer, control and whole-attempt deadlines are unchanged.
PFG composition held the existing shared Quartus lock as the normal user.

Working files are in `out/nano-distinct-boot-chain-20261107/`, with its SDK and
catalog in `out/nano-distinct-native-20261107/`. The next step is a newly frozen
nine-file upload and three-session ZX81 → ZX80 → ZX81 operator, including its
fault checks and complete source/device admission. The consumed predecessor
operator must not be replayed. Four FPGA signoff findings, physical DDR drain,
HDMI/launcher/input/media/native-network behavior and full core-load acceptance
remain open. No board, SD, UART or JTAG operation occurred here.
