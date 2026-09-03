# Architecture

## Current boundary

This repository has one runtime implementation split across three narrow
roles:

- `libmister-runtime.a` owns the public lifecycle API, state, validation, and
  profile model.
- `mister-runtime` owns the local Unix-socket protocol and delegates every
  hardware-changing request to that lifecycle API.
- `src/native` and `src/linux` contain the Linux hardware primitives and the
  production construction boundary. `CreateProductionHardware` owns
  `PosixArtifactOpener`, `LinuxMmio`, `SteadyClock`, `LinuxFpgaManager`,
  `LinuxSpi`, `CoreLoader`, `LinuxI2c`, `MenuVideoBringup`,
  `FixedVideoBringup`, `LinuxInput`, `NativeInputSession`, and
  `NativeHardware`. The dependency graph is:

  ```text
  LinuxMmio + SteadyClock -> LinuxFpgaManager + LinuxSpi
  LinuxSpi -> CoreLoader
  SteadyClock -> LinuxI2c
  CoreLoader + LinuxSpi + LinuxI2c + SteadyClock + LogSink + fixed recipe
    -> MenuVideoBringup
  LinuxSpi + LinuxI2c + SteadyClock + LogSink + fixed recipe
    -> FixedVideoBringup
  SteadyClock -> LinuxInput
  LinuxInput + LinuxSpi + SteadyClock + bounded delivery timeout
    -> NativeInputSession
  all of the above -> NativeHardware
  ```

  Its installed idle path is `/usr/share/mister-runtime/idle.rbf`. The one
  production profile is `megadrive`, whose image-owned core path is
  `/usr/share/mister-runtime/cores/megadrive.rbf`.

ADV7513 programming lives in `src/native/adv7513.hpp`. Register addresses,
bitfields, and CEA-861 AVI/VIC values are named from the public ADV7513
datasheet and Programming Guide. Analog Devices "must be set" bytes stay
under `adv7513::adi_required` because those internals are not documented.
`Menu720p60Recipe()` composes those named writes; the I2C byte sequence is
unchanged from the known-working Main table.

The library does not own a network API, catalogue, transfer cache, or host
session. A future target agent integration belongs outside this repository
and will call the daemon over `/run/mister-runtime.sock`.

## Lifecycle and ownership

The lifecycle states are `idle`, `starting`, `running_game`,
`running_development`, and `reboot_required`. Starting the runtime deliberately
asks the hardware boundary to establish idle. A game launch validates the
entire request against the one runtime-owned profile table before mutation; a
development launch validates its absolute RBF path without inventing a system
profile. Stop returns the hardware to idle.

Native idle admission performs this exact sequence:

```text
open locked idle RBF
  -> program FPGA and release bridges/core hardware reset
  -> toggle the FPGA core-ID strobe and sample GPI
  -> assert menu-core software reset over user-I/O SPI
  -> probe and require core identity MENU
  -> locate ADV7513 main map 0x39 (PD/AD low, Programming Guide 0x72) on /dev/i2c-0 through /dev/i2c-2
  -> apply the fixed ADV7513 initialization
  -> send the fixed 1280x720@60 timing and PLL words
  -> apply the fixed 720p ADV7513 mode registers
  -> release menu-core software reset
  -> emit the bounded ADV7513 wake edge and neutral core-input packet
  -> require ADV7513 HPD and monitor-sense status
  -> publish idle
```

When a game input session is open, `LoadIdle()` first prevents further input,
joins its worker, attempts the session's final neutral packet, and closes its
descriptors. At process startup there is no input session, so the same idle
path safely begins at the locked idle-RBF open. One absolute deadline bounds
each native stage. An idle failure enters `reboot_required`; it does not retry,
fall back, or reboot automatically.

Mega Drive launch performs this exact sequence:

```text
validate the complete profile request
  -> resolve and open exactly one FogCast Virtual Gamepad
  -> open and validate the locked Mega Drive RBF and every media artifact
  -> sort opened media by profile-owned index
  -> program the FPGA
  -> assert profile-owned core reset
  -> probe and require core identity MegaDrive
  -> apply the profile-owned initial status
  -> attach cartridge at file index 1 using little-endian byte pairs
  -> apply the fixed 1280x720@60 ADV7513 path and require link status
  -> send a neutral player-one map
  -> release core reset
  -> start the owned input worker with the new runtime generation
  -> publish running_game
```

Input resolution and all artifact opens complete before FPGA programming.
Neutralization succeeds before reset release, and the generation-bound input
worker starts before `running_game` becomes observable. Stop invalidates the
active generation, joins and neutralizes input through `LoadIdle()`, performs
the existing Menu idle bring-up once, and publishes `idle` only on success. A
second launch repeats preflight and creates a new generation, descriptor
session, and worker.

At most one hardware-changing operation is admitted. A concurrent external
lifecycle mutation is rejected as `busy`; external operations are not queued.
The one-shot hardware-fault notification is only deferred until the active
mutation releases that same boundary. After a mutation has begun, a failed
launch gets exactly one cleanup attempt. Cleanup success returns to `idle` with
the original error; cleanup failure returns `reboot_required`.
There is no retry loop, failover path, recovery coordinator, or second
ownership database.

`Runtime::Impl` is the sole `HardwareFaultSink`. `Hardware::SetFaultSink`
installs it before startup idle, and each admitted game hardware launch gets a
strictly increasing generation. The input worker callback only enqueues its
generation-tagged error and returns. Enqueuing an active-generation fault also
reserves that generation under the runtime mutex, so Stop is rejected as busy
until the drain owns cleanup. A private runtime drain thread admits the fault
through the same mutation boundary, then invokes `LoadIdle()` exactly once; it
never joins input from the input worker itself.
Cleanup success preserves the direct input fault in idle status, cleanup
failure publishes `reboot_required`, and stale generations perform no work.

Profiles own core identity, semantic media roles and indices, core and input
recipes, and allowed settings. Callers own selection and staging of absolute
paths. The production table contains only Mega Drive; test profiles are private
fixtures and cannot be selected by the production daemon. The canonical
software-versus-physical record is the [support matrix](docs/support-matrix.md).

The implemented Mega Drive path has no physical-hardware claim yet. Native
audio, save RAM, save states, six-button X/Y/Z/Mode input, multiplayer,
remapping, hot-plug recovery, and development-RBF loading/video acceptance are
outside this slice. Every other game system remains unsupported. The runtime
does not preserve a running game across restart and does not add conventional
Main, transient MGLs, or automatic legacy fallback.

## Protocol

Protocol 1 accepts exactly four operations over a local Unix socket, one JSON
request and one JSON response per connection:

- `status`
- `launch`
- `load_development_rbf`
- `stop`

Requests reject unknown fields. `launch` accepts a stable system ID, an
absolute RBF path, semantic media paths, and profile-declared settings.
Responses report `ok`, lifecycle state, execution type, system/core identity
when present, a direct error when present, and the runtime version.

The maximum request frame is 65,536 bytes including its newline terminator.
Decoded path strings reject embedded NUL bytes at the protocol, lifecycle,
profile, and POSIX artifact boundaries so the path validated by the runtime is
the path presented to the operating system.

`status` is the sole reconciliation mechanism for a response lost after
dispatch. The caller observes daemon state instead of guessing whether a
mutation happened or consulting another authority.

## Build and link closure

The canonical host build produces one production archive and one daemon. The
daemon links the whole archive so unresolved or accidentally omitted native
members fail at the final link. Acceptance guards audit the archive manifest,
exclude fake and historic symbols, exercise test and production header
dependency invalidation, verify incremental version embedding, and compare two
clean archive hashes for determinism. Host and 32-bit Arm production builds
use 64-bit file offsets for high-address MMIO mappings.
