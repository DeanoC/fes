# Native SD launcher wiring and persistence

Implementation `23dc5a01f` starts from merged main
`a14663bd9f6bfdd4d9024d7c6a0b8f0f843e937a`. This is software and regular-file
preparation evidence. No kit, card, serial/JTAG interface or QSPI was accessed.
[Issue #2006](https://github.com/DeanoC/fes/issues/2006) tracks this wiring
milestone; [#1867](https://github.com/DeanoC/fes/issues/1867) stays open for
whole-system qualification.

## Audit and change

Main already supplies the guarded two-second native autoboot policy, paired
FPGA menu and complete Tenfoot App, shared keyboard neutral fences and
Ctrl+Alt+Escape return, owned installation and fresh-boot installed-record
discovery. Those implementations and earlier physical records do not establish
new-artifact power-on, HDMI/input, persistence or full appliance qualification.

The native entry previously selected `LoadProgramPreferences(..., false)` for
every boot. `ReadProgramStartup` now reads required manifests and preferences
through the active store before menu construction. Paired builds with a trusted
compiled writable-card plan select the existing owned SD checkpoints. Other
builds retain RAM preferences. Optional install-source quarantine still requires
healthy storage; required corruption and ownership failure stop startup.

`stage-installer --menu-export` adds the checked single menu, its selection and
a 64 KiB preallocated default preferences file to the existing fresh installer
output. Package sources cannot overwrite the preferences seed. Existing output
refuses; no tool silently installs a core or overlays an existing library.

`nano/launcher_image.py` composes these files and the matched experimental FIT
into a copy of the verified recovery template. It requires the entry-selected
SDK, keyboard, paired menu, autoboot and matching protected writable plan. It
pins the exact menu assets and inputs, creates nested FAT directories, reads
back every added file, and verifies the recovery files, raw prefix and both FAT
boot sectors before publishing a result. It writes only a fresh regular-file
output and never provisions a card or bootstrap flash. See the
[operator guide](../../../platform/embedded/nano/launcher.md).

## Validation

- The full affected command passed its first seven scopes, including **2,920
  parent tests (21 skips)**, planners, whitespace, Go command inventory and
  generated consumers. The eighth scope stopped while downloading the pinned
  upstream TF-A blob because DNS resolution failed. The invocation remains
  recorded as failed; it is not relabelled green.
- That failed native integration scope was separately rerun through its existing
  `--reference` option using the exact retained upstream blob
  `3c223c1498e3a412e075e4f921cd78acd3d0fcd6`, SHA256
  `92607ef0382fd57501c38f254b04c246e94fff1f1d60bb97e3d3d1284dbbf17a`.
  Existing blob/SHA checks are unchanged. All 42 integration commands pass in
  216.352 seconds: actual firmware C and control RTL, all four native race-test
  partitions, native tape and the Tenfoot model/endpoint integration. The source
  snapshot contains 590 entries. No producer timeout or ownership predicate changes.
- The joined Go regression runs the real Tenfoot App, renderer, menu presenter,
  input adapter and FAT implementation. USB reports drive layout changes,
  keyboard launch/core key release/Ctrl+Alt+Escape return for two installed
  titles, and orderly Quit/Park. A newly opened image/filesystem/store/loader/
  service/App discovers both records and applies saved preferences without
  reinstalling. Independent mtools readback checks saved bytes and the unchanged
  empty bootstrap. Hardware, MMIO, secure observations and SD ownership are
  modeled; this is not a physical reboot.
- The 46 focused observer/installer/autoboot tests pass. A later 30-test final
  host run covers the new image assembler, paired observer and autoboot; counts
  overlap. Image tests use real recovery-template/FAT writes, readback and
  preservation, with native source admission injected. They cover changed
  SDK/FIT bindings, protected plan/menu mismatch, missing/corrupt/extra/aliased
  staging, preallocation and strict JSON types, changed admission inputs and
  failed readback without a published success receipt. The new assembler tests
  were added after the full parent run and are covered by these focused runs.
- `make check`, command inventory, documentation links/support matrix and
  whitespace pass. A real retained ZX81 package/ROM/menu passes the actual
  staging CLI; existing output is refused without changing its ten files.
- The current selected native command compiles with the pinned AArch64 TamaGo
  compiler and retained generated resource constants. ELF: **16,162,991 bytes**,
  SHA256 `e61f8b0a6b66e4e654977602ed6f17749bea524c9ac749550ddc8de2ef6196be`.
  Its 1,810-entry first-party Go/module source map is unchanged across the
  compile. This is compile-only evidence: no new matched SDK/entry/BL31/SPL/FIT/
  SOF chain or real new-image assembly/admission is claimed.

The first joined-test compile failure (a test incorrectly used the void
software-device `Close` result) is retained separately; it executed no test.
The corrected run and final native integration pass. Prototype logs and source
snapshots live in the task's ignored `out/operations/`; retention is recorded in
`validation.json`.

## Remaining hardware boundary

Automatic U-Boot execution does not supply a persistent HPS-first bootstrap.
The new runtime needs a fresh matched boot chain and the exact selected SPL
must be delivered before `FES/UPLOAD.BIN` can boot from SD. A full image does
not replace that step. No QSPI write or autonomous power-on is qualified here.

The next native trial must cover fresh SD startup, visible Tenfoot, real keyboard
installation/launch/return, a settings edit, orderly shutdown and an independently
observed reboot/discovery, with source-matched artifacts and verified restoration.
Current DE25 HIL rules select J8G; a Linux package smoke does not exercise this
native boot chain, and CBL is not an available fallback. Keep repeated/failure,
physical display/input, power-loss and vendor reset/DMA work under #1867. No
hardware waiver, qualification claim or Lander handoff is made by this record.
