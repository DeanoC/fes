# Coleco two-player keyboard input diagnostic

This follows [cartridge delivery and paired graphics](2026-09-12-coleco-media.md).
Scope: the existing keyboard-row adapter, not native gamepads, full Coleco
keypad/joystick emulation, audio, sprites or retail cartridges.

## Source and artifacts

The FES base is `691cdad5a78fd43db5c21e3c4eccdbb2f4d59b8e`; the misteross
base is `69c58237c0a1cc933140f95ebfff32626c9f455a`. Changes are confined to the
open cartridge generator, tests, Makefile and documentation. RTL, build recipes,
constraints, shared ABI, FogCast and runtime are unchanged. Consequently this
test deliberately uses the previously sealed exact `69c5823` FPGA packages:

| Lane | Package ID | RBF SHA-256 |
| --- | --- | --- |
| OSS | `11f79d4b74216c4ee741e65931b1c37219691545e462e032a5e51fbd9974a134` | `4efdfc0670e757792314ee75ace1c724aa24fafcec7d443e94c9bb79a6c75382` |
| Quartus | `5c9b705cf9e4b2819caa09eecf06e0bd9dee36047ac0fc8e034707bccfb6648f` | `821bc900d619171b13d52f66a7471028c8e5dc77414e5b469428e1d89d482138` |

This is new cartridge acceptance on those artifacts, not a newly compiled RBF
or acceptance inherited from their earlier static picture.

Result: misteross `2a640cf93f21d4fc4db64ef1ded37119204e90dc`, branch
`feat/fes-coleco`. FES selects it only in `feat/fes-coleco-integration`; the
main checkout, other component pins and shared contracts are untouched.

`make coleco-diagnostic` adds `input.rom` (1837 bytes, SHA-256
`3083d8e4621d2a515980d49933cceead6195b16cc9f8c34373c9d0f8cb8de39f`),
`input-16k.rom` (16384 bytes, SHA-256
`d4f617b29d066a0bf7acd8001ed3b37f7a5e571b7a9e981ab929ebab641d1058`)
and `input.ppm`. The original 989-byte graphics cartridge is byte-identical.
The MIT generator supplies all code/data; no downloaded ROM or BIOS is used.

The CPU initializes VRAM and two previous-input bytes in CPU RAM, polls FC/FF,
and repaints only a changed player's panels. There are two rows of five solid
panels, ordered Up/Right/Down/Left/Fire, orange released and green pressed. The
names describe diagnostic bits, not full Coleco controller semantics. Player 1
uses existing matrix row 0 (Shift/Z/X/C/V); player 2 uses row 1 (A/S/D/F/G).
Other rows are ignored. Preview-only `--row0`/`--row1` change the reference
picture, never cartridge contents or live input.

## Verification and test setup

The focused Python suite increased from 13 to 15 tests and passes. The new CLI
test first failed because `--interactive` did not exist. The independent board
oracle checks real CPU-driven HDMI output after GP input commands; it does not
derive expected pixels from the generator, cartridge or VRAM.

`make sim-fes-coleco VERILATOR=build/toolchain/install/bin/verilator OBJCACHE=`
passed the complete default and OSS suites with production `TV80_REFRESH=1`.
Each lane checked 81 exact interactive frames: 27 states on compact, full-size
and compact cartridges, plus the retained three static graphics frames. States
cover every bit's press/release on both players, mixed bits, ignored row 7,
held reload neutral/restore, and reset-only neutral/restore without a new
MEDIA_BEGIN/COMMIT. Monitors require repeated reads of both controllers, no
HALT, initialization of all VRAM through CPU I/O, no duplicated VDP writes and
no repaint while input is unchanged. Both lanes rejected the static cartridge
in interactive mode for halting instead of polling. The simulation log is
`misteross/build/sim/fes-coleco-interactive.log`.

The temporary host uses unchanged FogCast `c761cff0d9e7878d90eb3dee24ba96010acb46de`
and runtime `2629c6e1a896663b3e06688462624c3fac67ba67`. The leased updater confirmed
diagnostic image `77dccfd1f6ff2ea013ff186e31d6030bd5cdd566d1bc22985d05652fa3a2dd3e`
on boot `f41cbe00-7600-4918-904a-a62f39f39102`. This is the previously documented
dynamic-runtime derivative, not a cold reproducible release image.

Two operator-script setup findings are retained separately from compiler issues:

- The private host configuration had remote input disabled. A mode-0600 copy
  under ignored evidence enables only `[remote_input].enabled`; the original
  configuration and normal host are not modified.
- `core-load` automatically attaches eligible keyboard input. A redundant
  explicit attach was rejected as busy; the diagnostic now verifies that
  existing attachment instead. Both failed attempts returned the core to idle.
  An additional request before the replacement host was ready failed without
  loading a core; retry followed its health-ready response.

No new Yosys/nextpnr/Mistral workaround is required. The existing documented
RAM latency and inference accommodations remain unchanged. Review also added
reset-only HOLD/RELEASE coverage (distinct from reupload) and ensured the
Makefile emits the documented neutral preview.

Local evidence is under `out/dev/fes-coleco/evidence/`: `run_input_hil.py`,
`input-*-hil.log`, per-lane `input-oss/` and `input-quartus/` API responses,
events, captures and comparison results. Capture uses the previous uncompressed
YUYV recipe and stable-interior color/geometry check; this is not bit-exact RGB
capture or gameplay acceptance. The private copied configuration is not tracked
and must not be published with evidence.

## Exact-artifact hardware results

Both sequences completed successfully on the diagnostic boot above: OSS
generation 4, Quartus generation 5. The host reported the expected package and
build IDs and an attached, ready keyboard input session. Inputs traversed the
running host's input-event API and existing leased target bridge/runtime;
there was no direct GP, runtime-socket or JTAG access.

Each compiler lane passed 27 captures: neutral; each of ten controls pressed
and released; mixed row values 26/13; unrelated row-2 Q held; full-size and
compact reuploads while mixed keys remained held; detach neutral; reattach
neutral. Media reloads retained the package generation and restored the held
matrix. Both final Stops returned idle with input detached.

All 27 paired PNGs are byte-identical between Quartus and OSS. Neutral/released
SHA-256 is `22a29522788a55aa3fd3614a31c2825ef5bb4f1b369d7118baac52bc26711e9e`;
mixed/held-reload SHA-256 is
`606fd67210847d7324204f76a16d322f9b4cff43bdda8a3c9f95da4d123b3c7c`.
All comparisons pass the documented stable-interior color/geometry threshold.
This establishes keyboard-event-to-CPU-to-video behavior, including release and
reload, for these exact artifacts. Reset-only HOLD/RELEASE is separately tested
in simulation, not claimed as a host hardware operation.

Independent review found no remaining actionable issue after the preview and
reset-only test fixes. No cold FPGA or appliance build was required or claimed.

## Restoration and handoff

The leased rollback completed and confirmed original image
`d1733d3fdcc97c4669f07576c3f449ba72f05e6ed77fbcf3b0a029830d98d760` on boot
`ce81ab0a-014c-447d-beb6-b6247cbd5061`: raw idle ready, trial false, pending
empty, corrupt false. Diagnostic/recovery images remain retained. Only the
temporary port-8797 host was stopped; the normal host and private configuration
were preserved. Restoration evidence is `input-rollback.log`,
`input-final-lease.json` and `input-restored-host.json`.

Final parent `make check` passes (14 generated consumers, 11 fixture copies,
four copied source pins). The 15 focused tests also pass from the newly selected
integration component, and whitespace checks pass. The previous milestone's
broader parent/image tests are not represented as rerun for this documentation
and FPGA diagnostic-only update. No push or main-branch merge was performed.

Next: expand actual Coleco controller semantics and VDP behavior, with fresh
functional tests. Physical gamepads, full keypad, sprites, interrupts and audio
remain outside this completed keyboard-driven slice.
