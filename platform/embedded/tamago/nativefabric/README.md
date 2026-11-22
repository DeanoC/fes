# Native contained-fabric endpoint

`nativefabric` implements the EL1 Go host side of the contained DE25-Nano FPGA
lifecycle: package/ROM/tape retention, SDM configuration transfers, owned
bridge/master containment, paired Tenfoot menu/core sessions, FAT32 catalog
installation and session-bound keyboard delivery.

For end-to-end launcher usage and SD image assembly, see
[../../nano/launcher.md](../../nano/launcher.md). Historical bring-up records
live under `docs/validation/*de25*`.

## Architecture and key types

| Area | Primary files / types | Responsibility |
| --- | --- | --- |
| Configuration transfer | `transfer.go`, `configuration_arena.go`, `prepare_configuration.go`, `retire_configuration.go` | Shared `START`/`WRITE`/`COMPLETED_WRITE`/`ISDONE` engine (`ConfigurationTransfer`), single-use pinned `ConfigurationArena` leases, per-load descriptor publication and fail-closed retirement. |
| Boot & bridge ownership | `hpscontext.go`, `cpuhooks.go`, `bridges.go`, `firmware_bridges.go` | `HPSContextVerifier`, `CPUHooks` and `FirmwareBridges` over the generated [owned-channel wire contract](../../../../sources/mister-packages/docs/embedded-bridges.md). |
| Program & menu sessions | `selected_program.go`, `selected_menu.go`, `program_backend.go`, `program_execution.go`, `menusession.go` | `ProgramLoader` (separates immutable boot/platform record from retained `.fcore`/ROM/tape selections), paired MENU ↔ core launch/return, SD containment/resume and `sd-checkpoint` preferences. |
| Staged catalog & installer | `program_startup.go`, `program_staging.go`, `program_staged_catalog.go`, `program_download.go` | `ReadProgramStartup` common startup reader, verified RAM-to-FAT32 installation (catalogue published last) and fresh-boot installed-record discovery. |
| Media & input | `rom.go`, `initial_tape_arena.go`, `initial_tape_publication.go`, `computer.go`, `program_keyboard.go` | Content-bound ROM linking, separate tape arena publication, Avalon/GP computer control and session-bound USB keyboard delivery. |

## Platform responsibilities

- **Configuration transfer & arena ownership:** `ConfigurationArena` keeps the
  full DMA mapping pinned until reboot and grants a fresh lease only after the
  preceding transfer verifies every completion, final digest and `Release`.
  `HPSContextVerifier.PrepareConfiguration` publishes the 640-byte per-load
  descriptor in the final 4 KiB page and verifies live EL1 mappings through
  [board/arm64map](../board/arm64map/README.md); uncertain outcomes permanently
  retire the transfer and retain memory until reboot.
- **Bridge & master containment:** `CPUHooks` enforces explicit `BridgeServices`
  snapshots (qualified bridge/control masks, hardware-drain mask, monotonic
  boot-scoped owner/generation). Active closures stop producers and require an
  independent hardware drain before any reconfiguration SMC. Control-only
  openings expose CSRs without releasing data masters; data release requires an
  explicit nonzero data mask and live build-ID verification.
- **Storage handoff:** `PreflightStoredConfiguration` validates retained
  archives while SD is still active so rejected selections leave storage
  usable. `Store.Contain` closes SD before `ConfigurationTransfer` reserves
  DMA; `ResumeStorageAfterReturn` reopens SD only after a verified same-owner
  computer/menu return.
- **EL3 firmware boundary:** `FirmwareBridges` communicates with the
  [controlled C Nano service](../../firmware/README.md#controlled-nano-bridge-service)
  via synchronous SMCs. Package metadata is an unsigned compatibility assertion
  only; it never authorizes hardware addresses, data masks or live boot proofs.

## Session-bound native keyboard input

`ProgramBackend.BindKeyboard` binds local USB input to an active play after
neutralizing all nine HID key rows. It requires an all-keys-up neutral report
before forwarding presses so launch keys cannot leak into the loaded core.
`ProgramKeyboard.Poll` drains bounded DWC2 boot-keyboard reports, handles
`Ctrl+Alt+Escape` return requests with neutral-report fences, and revokes the
binding on return, replacement or disconnect.

## Repeatable host check

```sh
python3 platform/embedded/tamago/nativefabric/verify.py \
  --reference out/reference/socfpga_sip_svc.c \
  --output out/native-fabric-check
```

Omitting `--reference` fetches and hashes the pinned public TF-A source blob.
`verify.py` compiles the prepared TF-A configuration functions and Verilator
RTL, generates synthetic package/ROM fixtures via the runtime compatibility
oracle, and runs `nativefabric` and Tenfoot model tests under `-race`.
`verify_producer.py` separately tests the standalone simulation producer
against this adapter.

Focused Go launcher/startup/preference tests:

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 -timeout=120s \
  ./nativefabric -run 'TestProgramLauncher|TestProgramStartup|TestProgramPreferences'
```
