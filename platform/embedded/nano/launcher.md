# Native SD launcher

This is the opt-in, Linux-free Tenfoot path. It reuses the selected-program
runtime, paired FPGA menu, shared USB keyboard, owned FAT32 installer and native
U-Boot `fesboot` command. Keep `FES_EXPERIMENTAL_DE25_EMBEDDED=1` selected when
building the experiment. Linux appliance defaults are unchanged.

## What is already implemented

| Part | Implementation and evidence boundary |
| --- | --- |
| Native startup | `--native-autoboot` selects the guarded `fesboot` command after a two-second UART abort window. The [recorded phase-one test](../../../docs/validation/2026-11-11-de25-phase1-startup-hardware/README.md) boots automatically from SD after volatile JTAG delivery. |
| Tenfoot display | The paired menu presents the actual 1280×720 software renderer and restores it after an owned core return. |
| Keyboard | Shared owned USB input supplies menu navigation and running-core keys. A fresh Ctrl+Alt+Escape requests Stop and returns to Tenfoot. Neutral-report fences prevent held keys crossing boot or return. |
| Installed library | The installer publishes verified content and a catalogue record last. Fresh startup discovers the records without reinstalling or rewriting the bootstrap. [Earlier physical installation/discovery evidence](../../../docs/validation/2026-11-11-de25-installed-discovery-trial/README.md) is bound to its recorded image. |
| Launcher settings | A paired menu with an explicitly compiled writable card plan selects `sd-checkpoint` preferences. Launch, return, installation and orderly Quit checkpoint pending settings only while the existing SD owner is active. Other configurations select RAM preferences. |

`ReadProgramStartup` is the common native startup reader. It reads bootstrap,
installed records, the single menu selection, preferences and optional staged
content through the active package store before FPGA menu construction contains
SD. Missing/corrupt required settings or menu files refuse startup. A rejected
optional install source is quarantined only while storage ownership remains
healthy; it cannot hide a failed lease. No startup reader writes the card.

## Prepare fresh launcher files

Use the computer and menu exports belonging to the selected compiled board
maps. Each export contains `FES/CORESEL.JSN`; the computer export also contains
`FES/UI.JSON` and its exact package/ROM/tape inputs.

From the repository root:

```sh
GOWORK=off go -C platform/embedded/tamago run ./cmd/stage-installer \
  --export /absolute/computer-export --id zx81 --id zx80 \
  --menu-export /absolute/menu-export --output /absolute/new-launcher-staging
```

The output must not exist. The command verifies the selected content and single
menu package/metadata, emits fixed `FES/MENU.FCP`, `FES/MENU.JSN` and
`FES/MENUSEL.JSN`, and creates a 64 KiB preallocated `FES/PREFS.JSON` containing
valid initial JSON and whitespace padding. Initial preferences open the library
and disable attract mode. The existing empty bootstrap and explicit Install
catalogue are retained; first boot does not silently install a core.

This prepares ordinary host files. It does not build a boot chain, write a
device or grant hardware admission. Prepare the exact matched native boot
chain as below, then assemble the regular SD image. Do not overlay
this seed on an existing library: replacing `PREFS.JSON` would reset its settings.
The allocation survives later shorter JSON writes and supports the full existing
64 KiB preference bound without FAT allocation during checkpoints.

## Stage a shared-shell catalogue

The opt-in SDK accepts the Spectrum and C64 build, package and ROM options in
the same invocation when `--shared-core-shell` is selected. Supply
`--spectrum48-tape` and `--c64-d64` to add their media, and use
`--initial-tape --zx81-tape GAME.P` to add a ZX81 tape to the primary held-ROM
package. The combined output is under `export/FES`. Its selection and UI
records index ZX81, Spectrum and C64 in that order when all three are present.
Each entry binds its own sealed package, ROM receipt and media. The separate
Spectrum and C64 exports are still available for single-core staging.

The combined export can select all three entries in one
`stage-installer` invocation or be used as the playable catalogue on a
read-only shared-shell trial image. The menu and matched boot chain are
separate inputs. The catalogue and its filenames supply no firmware authority.

A matched TRWU8CBL diagnostic boot (boot ID `91c8655b9adace10`, 24 November
2026) launched ZX81, Spectrum 48K and C64, then relaunched C64 in the same
boot. The C64 disk uploaded in about 3.03 seconds on both runs and execution
was verified about 3.95 seconds after FPGA activation. The USB keyboard
launched all four sessions and returned the first three. The final C64
return and Park used UART. [#2207](https://github.com/DeanoC/fes/issues/2207)
tracks the earlier C64 launch stall and
[#2206](https://github.com/DeanoC/fes/issues/2206) tracks keyboard return and
Park. This capture used experimental SD boot with volatile JTAG startup.

The SDK's optional `--lifecycle-trace` records launch checkpoints and the
Ctrl, Alt, Escape and menu Quit states relevant to USB shortcut diagnosis.
It leaves the normal build default off and excludes ordinary typed input.

## Select the matching native boot

The existing native SDK builder (`../tamago/run.py fpgaload --board de25-nano`)
needs the sealed computer project and firmware ROM, `--menu-project`,
`--computer-control --computer-reload --computer-storage-return`,
`--selected-programs --tenfoot-serial`, `--shared-usb-input boot-keyboard`, and
`--writable-storage-plan` with `--sd-transfer-clock 25000000`. Use
`--initial-tape` when the selected package supplies tape. The writable plan is
trusted build input binding the independently identified card, MBR and protected
recovery FIT/manifest; an SD catalogue does not select or modify it.

Build the corresponding native entry and owned firmware using their checked
memory/boot contracts. Select `--native-autoboot` when building the dedicated
U-Boot with `--native-entry-build`, then compose it with the sealed HPS-first
project using `hps_boot.py`. Rebuild and admit the full matched chain after
native code changes. Reusing an earlier diagnostic FIT does not test the new
preference wiring.

The native SPL seals the exact experimental FIT at `FES/UPLOAD.BIN`.
Automatic U-Boot startup is distinct from autonomous power-on: the board also
needs a compatible HPS-first bootstrap delivering that exact SPL. Existing
Linux or factory QSPI is not assumed to provide it. The [boot-flash
preparation](../../../docs/validation/2026-11-11-de25-bootflash-trial-preparation/README.md)
and power-on observer cover separate prerequisites; this launcher preparation
performs no QSPI change or power-on qualification.

## Assemble the regular SD image

After the full matched chain passes source and file admission, run:

```sh
python3 platform/embedded/nano/launcher_image.py \
  --uboot-build /absolute/native-uboot --entry-build /absolute/native-entry \
  --native-build /absolute/native-sdk --recovery-kit /absolute/recovery-kit \
  --launcher-staging /absolute/new-launcher-staging \
  --output out/native-launcher-image
```

The assembler uses the existing complete native FIT/entry checker and recovery
kit verifier. It requires autoboot, shared USB keyboard, the paired compiled
menu, the SDK receipt selected by the entry, and the trusted writable plan's
exact protected recovery files and MBR. It rejects extra, aliased or protected
staging destinations and requires the menu files to match that SDK's assets.
Use the same menu export for staging and compilation.

Only a fresh directory below `out/` is written. `native-launcher-sd.img` is a
copy of the recovery template with `FES/UPLOAD.BIN` and the closed launcher file
set added. The assembler reads back every added file, verifies both recovery
files, the protected raw prefix and both FAT boot sectors, and rechecks inputs
and native admission before publishing `result.json`. A failed operation leaves
its partial regular-file output for diagnosis and publishes no success receipt.
It never writes a block device or supplies a QSPI bootstrap. The image still
requires separately admitted card provisioning, matched SPL delivery and native
boot/reboot qualification.

For the separately verified Ethernet return-trace recovery package, select
`--recovery-profile return-trace`. If it was produced in a retained worktree,
also name that worktree with `--recovery-source-root /absolute/producer-root`.
The verifier reads its pinned original producer files and build receipt; it
executes no code from that root. The current binary, PHY, trace, console, FIT,
SPL, protected-prefix and image checks still apply, and the package is checked
again before a result is published. Standard recovery remains the default;
neither profile falls back to the other.

When preparing an existing test card, `preferences_recovery.snapshot_preferences`
can read the fixed `FES/PREFS.JSON` through a freshly admitted recovery session.
It reports absence explicitly and otherwise retains bounded bytes with a fresh
memory hash and protected-file checks. It never writes or seeds settings.
The uploader has a separate fixed `preferences` purpose bounded to 64 KiB;
the operator must independently preserve existing settings and authorize any
seed or replacement. Fresh staging defaults must never overwrite an existing
library's preferences implicitly.

## Use and clean-reboot persistence

With the keyboard released at startup, select a title using arrows and Enter.
On fresh media, first use **Settings → Updates → Systems → Install**, then
**Open library**. Running-core keys stay in the core. Press and release
**Ctrl+Alt+Escape** to return to the menu. A held chord or a missing neutral
report cannot trigger another launch or return.

**L** changes the launcher layout. The edit remains pending in RAM while the
menu owns the FPGA and SD is contained. The next launch, return, installation
or orderly **Q** checkpoints it through the exact resumed SD lease and flushes
before completion. Q leaves the runtime parked; it does not itself reset the
SoC. Reboot only after the owned shutdown finishes. Fresh startup then reads
installed records and saved preferences from FAT again. A power cut while an
edit is pending can lose it; clean-reboot durability is not power-loss atomicity.
Uncertain writes retire storage without automatic retry or fallback.

## Validation and remaining qualification

`TestProgramLauncherKeyboardFATShutdownAndFreshBootPersistence` creates a real FAT32
image with the launcher producer, installs two verified packages, then runs the
actual Tenfoot App, renderer, menu presenter and USB keyboard adapter through
both launches/returns, layout changes and final Park. A newly opened image,
filesystem, store, loader, service and App discover both installed entries and
apply the saved layout, without reinstalling. Independent mtools readback
checks saved bytes and the unchanged empty bootstrap. USB hardware, MMIO and
secure firmware observations are modeled in this test.

Run the focused checks from `platform/embedded/tamago`:

```sh
go test -race -count=1 -timeout=120s ./nativefabric \
  -run 'TestProgramLauncher|TestProgramStartup|TestProgramPreferences'
```

The paired host observer requires the emitted `preferences=sd-checkpoint` mode
when the checked SDK record selects writable storage, and `preferences=ram`
otherwise. This identifies the selected policy; it is not an SD write or reboot
receipt. Historical captures require their matched source/policy decoder.

[Issue #1867](https://github.com/DeanoC/fes/issues/1867) stays open for repeated
and injected failures, actual keyboard/HDMI behavior on the new artifact,
power-on/reboot evidence, recovery and vendor reset/DMA guarantees. The image-assembly tests use real recovery-template/FAT writes and independent
readback with native source admission injected. They are separate from a full
matched-chain preparation. This work supplies no fresh hardware pass. Current shared-kit rules select J8G for DE25
HIL and forbid accessing CBL; the historical CBL records above are evidence,
not permission or a current kit-state assertion.
