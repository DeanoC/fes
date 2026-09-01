# Native Idle Video Bring-up Design

Date: 2026-09-01

## Status

Approved design for the first native MiSTer idle display baseline. This is not
a game-support claim. Production profiles remain empty and the native support
matrix remains at zero supported systems until later milestones add and
physically accept them.

## Problem

The native runtime now programs the Cyclone V FPGA correctly. On the physical
MiSTer Pi, the reviewed lifecycle smoke passed from idle through Stop, reboot,
changed boot ID, and fresh idle. The FPGA manager reported `operating`, the
installed runtime and agent matched the image, and conventional Main and its
command FIFO were absent.

Direct HDMI evidence still failed. Two frames were black and the next two were
the ShadowCast `No HDMI Signal Detected` card. The runtime had nevertheless
reported ready/idle.

The cause is an incomplete lifecycle boundary. `NativeHardware::LoadIdle()`
currently stops after opening and programming `menu.rbf`. Working Main
continues through menu-core user-I/O setup, FPGA video timing/PLL programming,
and ADV7513 transmitter setup before it considers the display usable.
libmister-runtime currently performs none of those post-program operations.

The accepted physical evidence is under:

```text
/home/deano/fes/task-tmp/fogcast-native-baseline.OpddDB/
  task7a-acceptance-20260901T185328Z
```

The failed native image SHA-256 was
`d50d5c465fe911f72cbbad149a095728a8bb8a2bd278aefab5da5f81bc403e71`.
Its lifecycle smoke changed boot ID from
`db27fee4-4d71-4faf-bded-5445e91e10f6` to
`871a3b66-e3de-4a10-aab3-cf2042b33ef1` and returned idle to idle.

## Goal

Make native idle truthful and visible by owning one fixed, known-working-in-Main
1280x720@60 menu-core video path inside libmister-runtime. Runtime may publish
ready/idle only after the menu core, FPGA video generator, and ADV7513 have all
been configured successfully.

## Non-goals

- No EDID mode selection.
- No MiSTer.ini parsing or alternate video modes.
- No scaler, filters, gamma, framebuffer terminal, HDR, CEC, VRR, direct
  video, analogue output, or hot-plug service.
- No import of Main's user-I/O, video, configuration, menu, or scheduler
  subsystem.
- No retry, fallback to Main, background recovery, durable coordinator, or
  new runtime state.
- No native game or development-RBF video claim. Those flows remain unchanged
  and unsupported unless separately designed and accepted.

## Considered approaches

### Runtime-owned fixed video component — selected

Add a small video boundary to libmister-runtime, backed by the existing SPI
transport and a small Linux I2C adapter. It configures exactly one immutable
720p mode and makes video success part of idle admission.

This keeps state authority honest: a runtime that reports idle also owns the
hardware work required for that idle state.

### Image startup helper — rejected

An init script or separate helper could configure video after the runtime
starts. This would split hardware ownership and permit the runtime to report
healthy while the helper or HDMI link had failed, reproducing the current
false-ready problem.

### Port Main video/user-I/O — rejected

This provides broad compatibility but imports configuration, EDID, scaler,
hot-plug, OSD, framebuffer, and legacy behavior that is outside the idle
baseline. It recreates the coupling this repository was created to remove.

## Architecture

`NativeHardware` receives one additional dependency:

```text
VideoBringup
  BringUp(expected_core, absolute_deadline) -> Error
```

That signature describes the ownership boundary rather than freezing a public
ABI. The concrete result includes the failing/completed phase, observed core,
selected bus, and diagnostic register values. The component uses the existing
`LogSink` for its named phase records; none of these details enter the public
daemon protocol.

The interface is implemented by a native component composed from:

- the existing `Spi` boundary;
- a new small `I2c` boundary for byte-register access;
- one immutable `VideoRecipe` for 1280x720@60;
- the existing monotonic `Clock`.

The FPGA manager remains responsible only for safe FPGA programming,
containment, bridge release, and hardware core-reset release. HDMI and
menu-core behavior must not be added to `LinuxFpgaManager`.

Production composition owns the Linux I2C adapter and constructs the video
component beside `LinuxSpi` and `CoreLoader`. Fakes remain test-only and may
not be selectable or linked into the production daemon.

## Idle data flow

The canonical idle sequence becomes:

```text
open locked menu.rbf
  -> program FPGA and release bridges/core hardware reset
  -> assert menu-core software reset over user-I/O SPI
  -> probe and require observed core name MENU
  -> locate ADV7513 main map at address 0x39
  -> apply fixed ADV7513 baseline initialization
  -> send fixed 1280x720@60 timing and PLL words with UIO_SET_VIDEO
  -> apply fixed 720p ADV7513 mode registers
  -> release menu-core software reset
  -> require ADV7513 HPD and monitor-sense status
  -> publish idle
```

The software reset uses the existing user-I/O protocol rather than inventing
a new hardware register path. It sends the canonical 16-byte status vector
with bit 0 asserted before video setup and the all-zero vector to release the
menu core afterward. It does not load or save a user configuration.

The probe uses the existing core-name command and requires the exact canonical
name `MENU`. A different, missing, malformed, or unreadable identity fails
bring-up. This prevents applying the menu-only recipe to an unknown core.

The SDRAM-size command is deliberately excluded. It is not part of video
timing or transmitter initialization, and the locked menu baseline does not
need a public memory-configuration contract. It may be added only with direct
evidence in a later design.

## Fixed video recipe

The production recipe is immutable and named `menu_720p60`. Its timing is the
known-working Main mode 0:

| Field | Value |
| --- | ---: |
| Active width | 1280 |
| Horizontal front porch | 110 |
| Horizontal sync | 40 |
| Horizontal back porch | 220 |
| Active height | 720 |
| Vertical front porch | 5 |
| Vertical sync | 5 |
| Vertical back porch | 20 |
| Pixel clock | 74.25 MHz |
| CTA VIC | 4 |
| Pixel repetition | none |

The exact `UIO_SET_VIDEO` transaction is generated once from the pinned
known-working Main algorithm and committed as literal production data. Main's
20 logical mode items expand to 26 payload words because its six 32-bit PLL
items are sent low-word then high-word; including command `0x20`, the wire
request contains exactly 27 16-bit words. Production does not carry Main's
mode-selection or floating-point PLL search algorithm. A provenance test
records Main source commit `cc5eb4bfc4cb2887dd6ab8364bff7010d7c6978c`
and verifies every literal wire word against the reviewed oracle.

The ADV7513 setup is likewise fixed data derived from working Main defaults:

- RGB 4:4:4, 8-bit, style 1;
- ordinary HDMI mode with HDCP disabled;
- full-range SDR output;
- 16:9, VIC 4, no pixel repetition;
- the exact Main mode-0 sync encoding, including ADV register `0x17 = 0x62`;
- transmitter powered on;
- Main's required/vendor initialization writes and default RGB CSC values.

Dynamic HDR, CEC, EDID, audio-rate, limited-range, DAC, interrupt, and
hot-plug configuration are excluded. Registers whose fixed default writes are
required to make the transmitter operate remain in the literal baseline
table even when their broader feature is out of scope.

## Linux I2C boundary

The production adapter uses Linux `/dev/i2c-0` through `/dev/i2c-2`, matching
the bounded hardware search used by working Main. It:

1. opens each existing bus with close-on-exec;
2. selects slave address `0x39`;
3. performs one detection read;
4. keeps only the first responding descriptor;
5. offers byte-register read and write operations;
6. closes the descriptor deterministically.

It uses kernel I2C ioctls directly and adds no external runtime library or
shell-command dependency. Missing devices, short/failed transfers, invalid
outputs, and deadline expiry map directly to `io_failed` with a named phase.

All operations share the caller-owned absolute video deadline. There is no
unbounded scan or sleep.

## Link verification

After the fixed recipe and software-reset release, the component reads
ADV7513 register `0x42`. Bits 6 and 5 must both be set, matching Main's HPD and
monitor-sense predicate. A bounded poll is permitted within the one absolute
deadline because transmitter lock is asynchronous; it is not a retry of the
mutation sequence.

The final log records:

- selected I2C bus and address;
- register `0x41` before and after initialization;
- final register `0x42` value;
- observed core name;
- completed recipe identity.

No background monitor is started. Link loss after successful startup is a
future feature.

## Error and state model

`NativeHardware::LoadIdle()` logs distinct phases:

```text
preflight
program
core_reset
core_probe
hdmi_init
video_timing
core_release
hdmi_verify
```

Opening or FPGA-programming failure retains its current behavior. Any failure
after successful FPGA programming returns the existing direct I/O error with
`mutation_attempted=true`. `Runtime::Start()` maps that to `idle_failed` and
the existing `reboot_required` state. Runtime never publishes ready/idle after
a video failure.

There is no cleanup attempt, retry, alternate mode, or fallback. Reboot is the
existing recovery contract.

## Testing

### Unit tests

- Idle success requires exact `open -> program -> video -> idle` ordering.
- Video is never called after artifact or programming failure.
- The menu software-reset assertion precedes core probe and all video writes;
  release follows the full recipe.
- Only exact core name `MENU` is admitted.
- The complete fixed SPI request is asserted word-for-word.
- The complete fixed ADV7513 write sequence is asserted register-for-register.
- Every I2C bus-open/select/detect/read/write error returns at its first phase.
- Every SPI/reset/probe/timing/release error prevents idle.
- Link status requires both HPD and monitor-sense bits before the deadline.
- Deadline expiry is bounded and deterministic.
- The production composition contains the real video component.

### Mutation checks

At minimum, deliberate changes to one timing word, one PLL word, one ADV
required write, reset/release order, core identity, and link predicate must
each fail a named test.

### Repository gates

- Fresh host unit and integration suite.
- ASan/UBSan and relevant TSan suites.
- Archive, active-tree, history, deterministic-build, and dependency
  invalidation checks.
- Pinned ARM EABI5 hard-float build, final-link closure, and production-symbol
  audit.
- No fake backend, Main mutation symbol, or external I2C userspace library in
  the production binary.

## Integration and physical acceptance order

To avoid repeatedly rebuilding unaffected images before the uncertain HDMI
gate, acceptance is ordered as follows:

1. Review and merge the runtime change.
2. Pin the exact merged runtime commit in FogCast, review, and merge that pin.
3. Build native-dev twice from the merged pin and require identical images.
4. Run native structural, ELF/library, QEMU packaging, and exact-input gates.
5. Deploy native-dev and run the unchanged lifecycle smoke.
6. Record core identity, selected ADV bus/address, key ADV registers, fixed
   recipe completion, runtime/agent identities, and direct logs.
7. Capture exactly five one-per-second HDMI PNGs using a frame-count command
   with an external bounded timeout. Do not combine a five-second duration
   cutoff with a five-frame requirement.
8. Open and inspect all five frames. Require stable geometry and a present
   HDMI link; black output is acceptable only when signal presence remains
   stable. A capture-card `No HDMI Signal` frame fails.
9. Only after native HDMI passes, build legacy prod/dev twice from the same
   final FogCast commit and run their structural checks.
10. Restore the fresh legacy dev image and run the exact Sonic The Hedgehog 2
    Mega Drive launch, `MegaDrive` core observation, Stop, and `MENU` return.
11. Update hardware truth documentation and milestone status last.

If any physical gate fails, preserve exact evidence, restore legacy/Menu, and
make no pass claim. Diagnose the named failing phase before changing code.

## Acceptance criteria

- Native lifecycle smoke passes without Main or its FIFO.
- Runtime reports idle only after the fixed video component succeeds.
- The observed core identity is `MENU`.
- ADV7513 is found, initialized, and reports HPD plus monitor sense.
- Five direct HDMI frames show a stable present signal with consistent
  geometry and no corruption.
- Fresh legacy restoration and the Sonic 2 launch/stop/Menu gate pass.
- Documentation continues to report zero native game systems; only the native
  idle hardware baseline becomes supported.
