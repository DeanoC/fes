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

### Pair the launcher with FogCast

In FogCast target settings, prepare the named target's ID. Create a private
JSON file containing that lowercase UUID as `target_id`, the same target's
bearer credential as `token`, and a numeric TCP `port` (for example, 8080).
Pass its absolute path to `stage-installer --agent-config` alongside the
menu export. Keep the resulting staging directory and SD image private.
The image assembler reserves `FES/AGENT.JSN` for this per-board record.
Omit the option for a local-only launcher.

The matched Agilex 5 boot firmware opens an SDM crypto session to obtain a
fresh boot ID. The serial marker `FES_NANO_TRNG ready bytes=128` confirms
the boot entropy pool is ready. If the launcher reports
`boot-entropy-unavailable`, MENU remains usable locally and FogCast discovery
is disabled for that boot.

On an Ethernet-enabled launcher, FogCast browses `_fogcast._tcp.local.`
when a bound target's address changes. It authenticates health, matches the
target UUID, then reads `/v1/kit/lease` and `/v1/status`. The local launcher
reports owner `native-tenfoot`. On the matched C64 PIO build, select
`--paired-ethernet --network-dhcp` in the SDK and `--nano-sdm-trng` and
`--nano-mac-menu-supplier` in the matched boot chain for discovery in MENU.
For reads during play, also select `--c64-network-scope` in the SDK and
`--nano-mac-c64-scope-supplier` in the boot chain. This binds access to the
fitted C64 package and an advancing CPU fetch counter. A host can then observe
idle MENU, active `fes.c64` and returned MENU under the same boot ID with
successive lease generations. The original CBL recovery card runs the
local-only C64 launcher. [Native Ethernet qualification](../tamago/network/README.md#de25-nano-target-discovery)

## 2. Build the matched native boot chain

Build all linked artifacts from the same checked contracts:

1. **Native SDK (`../tamago/run.py fpgaload --board de25-nano`):** pass the
   sealed computer project and firmware ROM, `--menu-project`,
   `--computer-control --computer-reload --computer-storage-return`,
   `--selected-programs --tenfoot-serial`, `--shared-usb-input boot-keyboard`,
   and `--writable-storage-plan` with `--sd-transfer-clock 25000000` (or
   `50000000` for SD High-Speed when supported by the card), plus optional
   fast-loading flags `--arm64-features`, `--sd-bus-width 4`, `--sd-multi-block`,
   `--sd-cmd23`, and `--sd-adma2` (add `--initial-tape` when the package
   supplies tape, or `--shared-core-shell` for Spectrum 48K / C64). `--sd-adma2`
   maps a disjoint 2 MiB uncached DDR pool at `0x95a00000..0x95c00000` and
   requires `--profile platform/embedded/nano/sd-dma-profile.json`
   (`sd_control.service = "fes-owned-sd-dma-v1"`), which `entry/build.py` and
   `cold_profile.py` propagate so `build_terasic.py` compiles BL31 with
   `#define FES_NANO_SD_DMA 1` and verifies `fes_nano_sd_platform_enable_dma` in
   `bl31.elf`. The writable plan is
   trusted build input binding
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

The native C64 picker extends the scanout control aperture in previously
unused access directions. It reads tagged picker status at `0x0c` and
writes picker data and commands at `0x64` and `0x68`. Scanout command
writes, build ID reads and first-fault reads still use the scanout endpoint.
The runtime requires the tagged picker status before it writes any overlay
data.

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

An earlier matched R1 image cold-booted to Tenfoot twice, launched and
returned from ZX81 by keyboard, and retained the List layout. Factory
restore, a factory cold boot and native reinstall are still open. Each new
image needs its own board evidence.
[R1 trial](https://github.com/DeanoC/fes/pull/2026) ·
[Lifecycle qualification](https://github.com/DeanoC/fes/issues/1867)
Standard DE25 HIL uses Kit D25 (`TRWU8J8G`) on Linux; never access `TRWU8CBL`
without an explicit assignment and lease.
