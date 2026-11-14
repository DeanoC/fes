# Native board selection and first Nano drivers

Scope: FES-owned exploratory native base on `codex/de25-embedded-os`, based on
`7703ab25b0c08d7ae81d39d98d5550db96c7d09a`. The result is the commit containing
this record. Evidence is host testing, AArch64 QEMU execution and Nano
cross-compilation only. No physical board, kit, FPGA, GPU or Powerboat was used.

All native applications now select peripherals through `board`. QEMU retains
its actual emulated-device drivers. Nano selects separate UART/counter boot
hooks, profile-owned RAM and read-only SDHCI PIO against firmware's initialized
SDHC card. It excludes QEMU controller implementations and refuses unavailable
USB, Ethernet and display providers. Physical FPGA activation remains unavailable.
No shared schema, generated contract or production Linux component was changed.

The [board contract](../../../platform/embedded/tamago/board/README.md) specifies
selection, firmware ownership, source identities and bring-up requirements.
Current DWC3 assumptions were corrected to DWC2 against the published Agilex 5
SoC device tree. Historical evidence files were preserved.

## Checks

- `GOWORK=off go -C platform/embedded/tamago test -race ./...`: passed;
  the final SDHCI test changes were also rerun with `test -race ./sdhci`.
- `python3 -m unittest discover -s tests -p 'test_embedded*.py'`: passed;
  malformed, overlapping, unaligned and QEMU-addressed handoffs reject.
- SDHCI register/card model: multi-sector LBA/data integrity, read-only policy,
  capacity exposure, readiness, CRC/R1 errors, missing completion, inhibited
  command, frozen-clock poll bound, unchanged failed-sector buffers, and
  poisoned-device reuse/flush refusal all passed.
- Native QEMU Play: all eleven launch/return/failure cases passed, with actual
  tenfoot USB navigation, stale ownership checks and application cleanup before
  QMP shutdown. Disk hash stayed unchanged.
- Native QEMU browsing: actual VirtIO HTTP/catalog, USB navigation and FAT32
  preferences survived a fresh boot. The UART diagnostic backend was also tested.
- Native QEMU `storage`: FAT32 BOOT.JSON read/hash passed and disk hash stayed
  unchanged under the ordinary-file disk lease.
- Nano `probe`, `storage`, `fabric`, `packages`, `admission` and `tenfoot`:
  cross-built with the synthetic unit-test handoff. AArch64 ELF segments and
  executable entry fit the supplied RAM; QEMU controller implementations were
  absent from selected sources. This handoff is **not** a board boot recipe.
- Missing Nano profile: rejected before output creation; there is no fallback.
- `make test-changed PYTHON=out/de25-os-tools/venv/bin/python
  TEST_CHANGED_ARGS='--base 7703ab25b0c08d7ae81d39d98d5550db96c7d09a'`:
  passed, including parent regressions and generated consumer consistency.
- `make deadcode`: passed. `git diff --check`: passed.

The compact [evidence receipt](evidence.json) binds selected native sources,
compiler/ELF identities, full local receipt hashes and outcomes. Full logs,
receipts and screenshots remain under the named worktree `out/` paths; no
hardware acceptance or portable binary release is attached to this record.
The initial test-changed attempt lacked the Python test environment; the
successful run above used the existing provisioned environment. An initial
host-test import-name collision and assembler halt-loop issue were corrected
before final passing checks.

Next integration gate: confirm delivered Nano revision, console routing,
matching firmware and all reserved RAM; boot the serial runtime probe, then
read disposable synthetic FAT32 media and compare BOOT.JSON's hash. Cold SD
initialization/writes, DWC2, MAC/PHY and FPGA display/activation follow as
separate driver and hardware qualification steps. This change cannot yet run
a complete tenfoot session on the physical Nano.
