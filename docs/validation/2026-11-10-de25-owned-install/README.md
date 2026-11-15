# Owned native package installation (software only)

The native backend privately prepares uploads using the existing canonical
archive/ROM validator and compiled platform preflight, without SD or firmware
IO. Preparation is allowed during play. Installation requires the current Ready
generation and a new catalogue ID. Existing input pause, menu close, secure SD
return and menu restoration own the operation; no new firmware/FPGA authority
or programming-profile registration is introduced.

The store uses fixed content-addressed paths under `FES/CONTENT` and immutable
hash-named catalogue records under `FES/INSTALLED`. It never overwrites cached
files, verifies existing copies and reads new files back before publishing the
record last. The frontend advertises the entry only after menu restoration and
input resume. Startup discovers and validates records before menu construction;
normal launch still retains/admit-checks the package and verifies live identity.

Known pre-mutation space/protection refusals restore a healthy menu. Uncertain
IO, failed readback or changed ownership retire without replay; input stays
paused on uncertain transitions. A lost publication reply or later menu/input
failure can leave a complete record on disk without current frontend success.
No failure is claimed to roll back an already completed write. Limits are seven
files/64 MiB per prepared upload and 32 total frontend entries; each record is at
most 8 KiB. The work phase has a 100-second limit, with the existing independent
menu-restore cleanup bound. No existing timeout is extended.

Focused race tests cover private snapshots, existing-file conflicts/reuse,
cancellation, foreign/stale/running/duplicate requests, platform rejection before
IO, catalogue count/digest/path/schema checks, changed ownership, bad readback,
possible publication commit before a lost reply, readable cached catalogue
during copying, and input pause/write/resume failures. Real FAT images verify
independent readback and package retention. A combined modeled native test
installs to a real FAT image, launches/returns, constructs a fresh boot owner,
discovers the saved record and launches/returns again through the same native
implementation and firmware decoder. None of this executes on physical hardware.

The original compile/test-helper errors and all subsequent logs are retained.
Local work started on November 9 and continued into November 10; its output
remains at `out/nano-owned-install-20261109/`. All eight selected checks pass, including generated consistency, SD/FAT/store
race tests and actual C/RTL native launch/return integration. The test-only
cancellation addition also passes independently; production source bytes remain
unchanged. Source digests and all results are in the adjacent `validation.json`.
The full native ARM64 launcher cross-build passes from committed source
`da66e9b0ea40b33b408da79641abc4d2aa248b10`, based on `38b805e81d62ac87d1ea79f59d47c7b3ea9c01c2`.
All 2117 recorded compiler input hashes, including the installer/store,
match the current source. ELF SHA-256:
`0ef39393029a78f1f57864f2576de35c64809bf4faa6a000a8c0a61b8a19cb0e`. Startup includes installed-catalogue
discovery and still explicitly selects read-only SD and volatile preferences.
No new entry/BL31/FIT/SOF was assembled or deployed. Cross-compilation supplies
no physical write, network, core or boot acceptance.

Nano startup still selects read-only SD. The install API is in-process, with no
native network upload listener. Updates/uninstall, card write qualification,
power-cut recovery, physical keyboard/HDMI and full appliance acceptance remain
open. No kit or card was touched; physical testing stays paused until the
operator returns. The DE10-equivalent core-loading goal remains incomplete.
