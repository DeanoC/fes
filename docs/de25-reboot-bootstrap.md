# DE25 persistent HPS-first bootstrap

The next reboot milestone replaces temporary JTAG bootloader delivery with an
HPS-first image in QSPI. Linux stays on the existing FES SD. FPGA cores remain
separate Linux-loaded artifacts. This is a proposed boot change, not deployed
or qualified QSPI startup.

The [cold-reset investigation](validation/2026-10-30-de25-reboot/README.md)
records working factory-QSPI reboot and stalled temporary JTAG reboot.
[The HPS-first proof](https://github.com/DeanoC/fes/pull/1303) then boots the
matching HPS-only RBF through JTAG and loads ZX81 from Linux, running BASIC
through the actual board CPU. Those exact files establish a useful candidate;
they do not establish QSPI programming, cold boot or repeat-reboot acceptance.

FES owns this off-board packaging and integration procedure. misteross owns
its compiled inputs; libmister-runtime continues to own physical lifecycle.
No automatic flash updater, target daemon or change to the production runtime
is introduced. The normal-kit goal still includes managed software, MENU and
catalog launch after this boot gate.

## Prepare without a board

Use the retained HPS-first producer's `hps/` directory containing
`linux-programming.json`, its complete SOF, HPS JIC and core RBF. The vendor
rollback directory comes from the verified FES Linux build's
`vendor-boot/factory-boot/`, independently pinned by
[the vendor lock](../image/de25-nano/vendor.lock.toml).

```sh
python3 scripts/de25_bootstrap.py prepare \
  --programming-dir /absolute/hps-first-build/hps \
  --vendor-boot /absolute/linux/vendor-boot/factory-boot \
  --quartus-pfg /absolute/quartus/bin/quartus_pfg \
  --output /absolute/de25-bootstrap
python3 scripts/de25_bootstrap.py verify --bundle /absolute/de25-bootstrap
```

The tool verifies captured producer hashes and HPS-first selection, checks
locked vendor rollback bytes, derives an HPS-only recovery RBF from the exact
complete SOF, and requires the resulting core RBF to match the selected core.
It inspects both JICs for MT25QU128 and the candidate/recovery metadata for
matching HPS IO hashes and HPS_FIRST sections without a core section. It
publishes a closed file set only after verification. `bootstrap.json` binds
all file hashes and preserves the producer/computer identity. Verification
rejects missing, changed, extra or symlinked payloads. These checks establish
file consistency, not a signature or hardware acceptance.

The package contains:

| File | Purpose |
| --- | --- |
| `linux-boot.hps.jic` | Proposed persistent HPS-first QSPI image |
| `recovery.hps.rbf` | Matching HPS-only volatile JTAG bootstrap |
| `linux-complete.sof` | Matching full volatile JTAG recovery image |
| `linux-boot.core.rbf` | Separate core for Linux loading |
| `golden_top_hps.jic` | Unchanged locked Terasic BSP rollback image |
| `vendor-readme.txt` | Vendor setup/version instructions |
| `linux-programming.json` | Original producer receipt, unchanged |
| `*-info.log`, `conversion.log` | Actual Quartus conversion/inspection output |
| `bootstrap.json` | Closed package identities and unqualified status |

The vendor rollback JIC is not a readback of the current board. The board's
printed OPN suffix remains unconfirmed under its fan; tool metadata and the
successful volatile run do not identify every SKU sharing its JTAG ID.
No preparation command runs a programmer, opens a kit or writes a device.

## First approved trial

Boot-flash programming needs Deano's explicit approval under
[the shared-kit rules](../AGENTS.md#shared-kits-and-powerboat-read-first).
Approval must cover the bottom board, the exact candidate, a pre-write flash
readback attempt and restoration if the trial fails. Claim its authoritative
status file first; preserve the approved FES SD and original factory SD.
Leave the top board unchanged.

1. Reverify the package, board USB serial/topology, cable, running FES card,
   UART-derived SSH key and boot UUID. Complete any activity, release all
   masters, sync and shut Linux down cleanly. Observe `Power down` before
   initializing a flash-loader helper. A flash-loader helper changes the
   volatile FPGA configuration even during readback.
2. Attempt the installed programmer's supported JIC Examine flow using a
   private working copy of the locked vendor JIC and an explicit matching
   device identity. Preserve the resulting raw readback, tool log and hash.
   Validate readback metadata before selecting it for recovery. If readback
   is unsupported, refuses the part or cannot establish a recoverable image,
   stop before a flash write and report the limit. Do not substitute the
   resource-package SOF/JIC for the BSP image or guess flash offsets.
3. Once that gate passes, program and verify the approved candidate on the
   exact cable/device. The installed `--initcfg` option requests configuration
   after flash programming; this trial must establish whether it starts Linux
   without a manual power cycle. The proposed programming form is below.
4. Capture UART and inspect the new boot UUID, expected FES rootfs/card,
   firmware version and pinned SSH identity. Check an ordinary Linux reboot
   from this HPS-only startup before loading any FPGA core.
5. Load the exact matching modules/core through Linux. Hold/reset before
   RUN and observe ROM readiness before keyboard input. Run BASIC LOAD/RUN
   and capture HDMI. Reboot again, reacquire the new boot epoch and repeat
   Linux core loading. Require three successful consecutive software reboot
   cycles, including one after core execution, with no JTAG redelivery.
6. On failure, capture evidence and recover with the packaged HPS-only RBF
   or complete SOF through the already proven volatile JTAG path. Keep old
   clients and uncertain DMA allocations out of the new hardware epoch.
   Restore the verified pre-write readback (or separately approved locked
   vendor rollback) using the same explicit program/verify/post-configure
   path, then verify baseline Linux and factory configuration before release.
7. On success, record this exact QSPI image as the new approved baseline
   only with Deano's agreement. Keep vendor rollback and the pre-write
   readback. No acceptance transfers to later rebuilt cores or bootstrap
   artifacts automatically.

The following is an operator review example, **not executed by preparation**.
Use a verified absolute package path and the claimed board's exact cable:

```sh
quartus_pgm -c 'DE25-Nano [3-11.3-iface0]' -m JTAG --initcfg \
  -o 'pvi;/absolute/de25-bootstrap/linux-boot.hps.jic@1'
```

This changes QSPI boot flash. `I` initializes the serial flash loader, `P`
programs and `V` verifies; do not add erase/force options or perform this step
without the approval and pre-write gates above. `--initcfg` is documented for
after programming, not as a standalone reset/recovery command. A successful
programmer return alone is not boot acceptance.

The [Altera Linux boot example](https://altera-fpga.github.io/rel-26.1/embedded-designs/agilex-5/e-series/013B/boot-examples/ug-linux-boot-agx5e-013b/)
documents separate HPS-first QSPI and HPS-RBF generation. The
[JTAG configuration debugging guide](https://docs.altera.com/r/docs/813773/26.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/debugging-guidelines-for-the-jtag-configuration-scheme)
explains boot-information redelivery after JTAG startup. These are flow
references; the DE25 package retains its own Terasic board/flash/boot inputs.

Passing this trial removes manual power cycling from ordinary tested reboot
and core-development operations. It does not by itself qualify failed-load
DMA quiescence, warm reset, watchdog fault recovery or managed MENU lifecycle.
