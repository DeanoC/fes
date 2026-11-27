# Native SD launcher

This guide covers building, assembling, and using the opt-in Linux-free Tenfoot
SD launcher on DE25-Nano (`FES_EXPERIMENTAL_DE25_EMBEDDED=1`). For system
architecture, delegation, and launch/return diagrams, see
[../ARCHITECTURE.md](../ARCHITECTURE.md).

## How startup and persistence work

| Subsystem | Role in the native SD launcher |
| --- | --- |
| Startup boot | `--native-autoboot` runs guarded U-Boot `fesboot` after a 2-second UART abort window; SPL verifies `FES/UPLOAD.BIN` before entering BL31 → `entry` → `cmd/fpgaload`. |
| Startup reader | `nativefabric.ReadProgramStartup` reads bootstrap, installed records, menu selection, `FES/PREFS.JSON`, and optional staged installs through `packagestore.Store` while SD is open and before MENU construction closes SD. Missing/corrupt required files fail startup; no startup reader writes the card. |
| Tenfoot display | Paired FPGA MENU presents the 1280×720 `gfx.Software` renderer via `scanout` (two DDR slots) and restores it on every core return. |
| Input | Shared owned USB keyboard (`usbcontrol` + `dwc2`, GICv3 SPI 125 wakeup) drives Tenfoot and running-core keys; `Ctrl+Alt+Escape` returns to MENU; neutral all-keys-up fences prevent held keys crossing boot or return. |
| Installed library | `packagestore.Store.Install` writes content-addressed payloads (`FES/CONTENT/<sha256>.BIN`) and publishes catalogue records (`FES/INSTALLED/<sha256>.JSN`) last. Fresh boots discover installed records without rewriting bootstrap. |
| Settings persistence | Paired menu + compiled writable card plan selects `sd-checkpoint` preferences (otherwise RAM-only). Pending edits checkpoint to `FES/PREFS.JSON` via copy-on-write `fat.Volume.ReplaceFile` whenever the active SD lease is resumed (launch, return, install, or orderly Quit). |

## 1. Stage fresh launcher files

Use the sealed computer and menu exports matching your compiled board maps
(each contains `FES/CORESEL.JSN`; the computer export also contains
`FES/UI.JSON` and exact package/ROM/tape inputs). From the repository root:

```sh
GOWORK=off go -C platform/embedded/tamago run ./cmd/stage-installer \
  --export /absolute/computer-export --id zx81 --id zx80 \
  --menu-export /absolute/menu-export --output /absolute/new-launcher-staging
```

- Output directory must not exist.
- Emits verified `FES/MENU.FCP`, `FES/MENU.JSN`, `FES/MENUSEL.JSN`, and a
  64 KiB preallocated `FES/PREFS.JSON` (library open, attract off), keeping the
  bootstrap empty so first boot installs only on explicit user action.
- Do not overlay fresh staging onto an existing library card without backing up
  `FES/PREFS.JSON` (`preferences_recovery.snapshot_preferences` snapshots it
  read-only; `upload.py --purpose preferences` uploads bounded 64 KiB updates).

## 2. Build the matched native boot chain

Build all linked artifacts from the same checked contracts:

1. **Native SDK (`../tamago/run.py fpgaload --board de25-nano`):** pass the
   sealed computer project and firmware ROM, `--menu-project`,
   `--computer-control --computer-reload --computer-storage-return`,
   `--selected-programs --tenfoot-serial`, `--shared-usb-input boot-keyboard`,
   and `--writable-storage-plan` with `--sd-transfer-clock 25000000` (add
   `--initial-tape` when the package supplies tape, or `--shared-core-shell`
   for Spectrum 48K / C64). The writable plan is trusted build input binding
   the expected card CID, sector count, MBR, and protected recovery files.
2. **Entry stub & EL3 BL31:** build `platform/embedded/entry/` and
   `platform/embedded/firmware/` against the matched memory/boot contracts.
3. **U-Boot & HPS-first SOF:** build dedicated U-Boot with
   `--native-entry-build --native-autoboot`, then compose it with the sealed
   HPS-first project via `hps_boot.py` (see [README.md](README.md)).

Rebuild and re-admit the full matched chain after native code changes; reusing
an older FIT does not test new wiring.

### Stage a shared-shell catalogue

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

## 3. Build and verify the SD image

Run both commands from the same FES checkout with mtools on PATH. The
receipt binds the producer sources as well as the matched builds, recovery
kit and staging files at their original paths. The verify target checks
images assembled with this workflow.

```sh
export DE25_EMBEDDED_ARGS="--uboot-build /absolute/native-uboot \
  --entry-build /absolute/native-entry --native-build /absolute/native-sdk \
  --recovery-kit /absolute/recovery-kit --launcher-staging /absolute/new-launcher-staging"
FES_EXPERIMENTAL_DE25_EMBEDDED=1 make de25-embedded-image
make verify-de25-embedded-image
```

Assembly writes `native-launcher-sd.img` and `result.json` under a fresh
`out/de25-nano-embedded/`. Set `DE25_EMBEDDED_OUTPUT` for both commands to
choose a different fresh directory under `out/`. Verification checks the
recorded image digest, re-admits the boot chain and MENU, enumerates the FAT
directories, and compares the installed files, protected raw prefix and boot
sectors with the original inputs. It prints the verified SHA-256 and performs
its readbacks in temporary storage. For a return-trace recovery kit, pass
`--recovery-profile return-trace` and its optional `--recovery-source-root`
to both commands.

The SD image accepts the shared input mode compiled into its matched SDK and
entry. Choose `--shared-usb-input boot-keyboard`, `gamepad` or `multi` when
building the SDK. The `multi` launcher supports a USB hub. The optional
`--initial-tape` profile currently accepts `boot-keyboard` and `gamepad`.

## 4. Controls and clean-reboot persistence

- **Navigate & launch:** Arrows + Enter (on fresh media, first open
  **Settings → Updates → Systems → Install**, then **Open library**).
- **Return to MENU:** Press and release **Ctrl+Alt+Escape** (neutral-report
  fences prevent held keys from re-triggering launch or return).
- **Layout & shutdown:** **L** changes layout (buffered in RAM while MENU owns
  the FPGA and SD is closed). The next launch, return, install, or orderly
  **Q** (Quit) checkpoints `FES/PREFS.JSON` through the resumed SD lease via
  copy-on-write `ReplaceFile` and flushes before completion. **Q** parks the
  runtime cleanly without resetting the SoC—reboot only after Park finishes.
  Uncertain writes retire storage without retry or fallback.

## 5. Host verification and open qualification

Run the focused off-board launcher, startup, and preference persistence checks
(covering real FAT32 image assembly, package installation, Tenfoot App +
renderer + modeled USB keyboard across launch/return/layout/Park, and fresh
remount readback):

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 -timeout=120s \
  ./nativefabric -run 'TestProgramLauncher|TestProgramStartup|TestProgramPreferences'
```

An earlier matched R1 image cold-booted to Tenfoot twice, launched and
returned from ZX81 by keyboard, and retained the List layout. Factory
restore, a factory cold boot and native reinstall are still open. Each new
image needs its own board evidence.
[R1 trial](https://github.com/DeanoC/fes/pull/2026) ·
[Lifecycle qualification](https://github.com/DeanoC/fes/issues/1867)
Standard DE25 HIL uses Kit D25 (`TRWU8J8G`) on Linux; never access `TRWU8CBL`
without an explicit assignment and lease.
