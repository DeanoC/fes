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

## 3. Assemble the SD image

After the matched chain passes source and file admission:

```sh
python3 platform/embedded/nano/launcher_image.py \
  --uboot-build /absolute/native-uboot --entry-build /absolute/native-entry \
  --native-build /absolute/native-sdk --recovery-kit /absolute/recovery-kit \
  --launcher-staging /absolute/new-launcher-staging \
  --output out/native-launcher-image
```

`launcher_image.py` copies the recovery template into a fresh `out/` directory,
writes `FES/UPLOAD.BIN` and the closed launcher file set into
`native-launcher-sd.img`, reads back every added file, verifies protected
recovery files, raw prefix, and FAT boot sectors, and publishes `result.json`
only on full verification (it never writes to a block device or QSPI). Pass
`--recovery-profile return-trace` (with `--recovery-source-root` if needed) to
select the Ethernet return-trace recovery template instead of standard recovery.

## Prototype a C64 disk set

Add `--c64-disk-swap` to a shared-shell Tenfoot build with paired HDMI, the shared USB boot keyboard, an initial
`--c64-d64`, and the sealed native C64 package containing the text overlay.
Add one to three extra read-only 35-track D64 files to `FES/` and
create `FES/DISKSET.JSON` with the exact catalogue game ID and SHA-256 for
each extra disk. For a catalogue entry named `c64` and a second disk named
`SIDE2.D64`, run this from the host directory containing `FES/`.

```sh
disk_sha=$(sha256sum FES/SIDE2.D64 | cut -d ' ' -f 1)
jq -cnS --arg sha "$disk_sha" \
  '{game_id:"c64",disks:[{name:"SIDE2.D64",sha256:$sha}]}' >FES/DISKSET.JSON
```

The startup reader validates and retains the set in RAM before the paired menu
contains SD. Tenfoot's existing in-play selector receives the set through the
same API as the Linux backend. A swap binds to the captured session, target,
package and native generation. It uses the owned unit-0 mailbox under the
five-second control limit. A failed transfer leaves the native service retired
with SD contained.

While C64 runs, press Ctrl+Alt+M to open Tenfoot's media selector. Use Up and
Down to select a disk, Enter to insert it, or Escape to dismiss the selector.
The native shell draws a text panel over the active C64 video. The panel uses
one M20K for two text buffers and switches between them at vertical blanking.
The CPU writes through the existing control bridge, so the panel requires no
FPGA DDR access.

The host test exercises Tenfoot and its keyboard controls with a regular
FAT32 image and a modeled mailbox. The native C64 RTL simulation checks the
panel on the active video stream, both disk commits, and continuing
instruction fetches. The [TRWU8CBL prototype result](../../../docs/validation/2026-11-25-de25-c64-live-selector/README.md)
records both swaps on the earlier BRAM-backed C64 package. The SDRAM-backed
package requires a fresh fit and board run.

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

Remaining physical qualification (repeated/injected failures, standalone SD
power-on, physical HDMI/keyboard behavior on matched artifacts, and QSPI
factory rollback) is tracked in [Issue #1867](https://github.com/DeanoC/fes/issues/1867).
Standard DE25 HIL uses Kit D25 (`TRWU8J8G`) on Linux; never access `TRWU8CBL`
without an explicit assignment and lease.
