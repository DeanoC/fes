# DE25 retained USB observations around selected core loads

Source `adcb08ee40a7ec223622e8e01a19e1a388a85eb8`, base `20126e07e16d58a2b30c89c7ab9c695aa32a84b1`.

The native serial Tenfoot diagnostic now emits checked shared USB ownership and report counts before and after core launch/return. The observer requires one retained USB lease, live reports between transitions, no delivered reports while paused, and the owned Stop on quit. It verifies the standard HPS context and complete pin observations before accepting USB startup. These observations grant no additional device authority.

Model coverage joins actual C ownership/configuration traces to the shared observations for ZX81/ZX80/ZX81 transitions, both input protocols, corrupted ownership/generation, missing or reordered boundaries, and existing native failure paths. The real seven-file uploader is exercised with stateful card and ownership failures. Native catalog validation uses the actual two sealed packages. The matched application, entry, secure firmware and U-Boot were rebuilt; compiled call paths and shared firmware linkage were checked. The JTAG composition preserves every FPGA core payload byte.

`result.json` records exact checks and retained early failures. The first observer fixture incorrectly required an EL1-only final checkpoint; the first disassembly assertion incorrectly assumed typed polling was inlined. Both are retained separately from the corrected passing checks.

Hardware has not been run for this image. Idle report counts would establish continued delivery, not physical button behavior. Display remains serial state only. Ordinary core/ROM/media admission, physical video and input, native Ethernet, and full DE10-normal-kit readiness remain unproven.

The original sealed FPGA projects remain prerequisites, retained in `/home/deano/fes-retained/de25-tenfoot-native-boot-20261104-r3/matched-candidate.tar.gz` (SHA256 `1479d5fa439c77457a3c19959a97b810b8ce5a6aa0672bb4567519f2d7229d17`). No FPGA recompilation or new timing qualification is claimed.
