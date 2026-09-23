# Core developer workflow

Use this lane to prepare one described core for an **already-compatible**
platform. It never assembles `linux.img`, changes the factory package set,
updates target software, or silently falls back to Quartus.

## Ownership and source of truth

`config/core-recipes.toml` maps core IDs to the selected misteross producer,
authentication function and toolchain lock. It is versioned, strictly validated
data, not a list of shell commands. All current producers use HIP/nextpnr.
The existing image builder and the developer command consume the same registry.

Keep ABI, programming profile and interface declarations in the sealed package
manifest, generated from the owning shared/core definitions. Runtime compatibility
is negotiated by the existing host/agent/runtime path; do not copy capability
limits or controller mappings into this registry. Media is an explicit immutable
library selection, not a compiled-in host asset.

To add a compatible core, implement and test its producer in
`sources/misteross` and add its recipe row in the same reviewed FES commit. No new host/UI
allowlist is required. A new ABI or transport still requires separately reviewed
runtime support. A registry row alone cannot create that support.

## Prepare one package, without an image build

From a clean committed FES checkout on the development machine:

```sh
mkdir -p out/core-dev
make core-dev CORE_DEV_ARGS='prepare --core fes.pong --output out/core-dev/pong-001'
```

Choose a new output directory for each candidate. Preparation checks selected
module selections, takes the normal parent build lock and runs the existing
authenticated package resolver for **only** that core. Matching package/compiler
inputs are reused; a package miss runs its producer. Missing compiler prerequisites
fail with the existing toolchain setup guidance in [core packages](core-packages.md).
It does not invoke Docker/image assembly or contact a host API or kit.

The output contains a frozen `.fcore`, its selection and `prepared.json`.
The archive is checked against the resolved manifest/payload identity before
publishing that receipt. Partial failures have no success receipt. Do not edit
the frozen candidate to follow a newer branch; prepare a new candidate instead.

For format-3 SMS, prepare the package without a startup media blob:

```sh
make core-dev CORE_DEV_ARGS='prepare --core fes.sms --output out/core-dev/sms-001'
```

Its library entry must then bind the exact 32 KiB `cartridge-rom` through the
named-ROM selection API. The prepared-candidate acceptance adapter below
still supports only ordinary blob media and cannot select a format-3 ROM;
use the isolated host library and kit lease directly for SMS ROM diagnostics.
Preparation does not infer mapper support or hardware success from a filename.

## Exercise the frozen candidate

After obtaining exclusive kit use and authorization, use the existing isolated
host acceptance path via the prepared candidate adapter:

```sh
make core-dev-accept CORE_DEV_ACCEPT_ARGS='--prepared out/core-dev/pong-001/prepared.json \
  --host-binary /absolute/path/fogcast-api --expected-host-sha256 HOST_SHA256 \
  --container-image sha256:EXISTING_CONTAINER_IMAGE_SHA256 \
  --host-config /absolute/path/private/config.toml \
  --evidence-dir /absolute/path/new-acceptance-run \
  --expected-target-id AUTHORIZED_TARGET_ID \
  --expected-host-revision HOST_COMMIT --expected-agent-revision AGENT_COMMIT \
  --expected-runtime-revision RUNTIME_COMMIT \
  --new-entry-title "FES Pong development" --execute'
```

Replace placeholders with independently established identities. The adapter
rehashes the candidate files and supplies their exact identities to the existing
runner. You cannot override the candidate's archive/package/core/media fields.
Successful runs add `preparation.json` to the evidence directory, binding the
exact preparation receipt digest to that run. Source revisions and manifest
digests in the preparation record describe the earlier preparation checks;
the adapter does not independently re-attest build provenance. The existing
host package reader validates the imported archive against the expected package ID.
Target identity and software expectations are never discovered and blindly trusted.
No `--execute` means no acceptance execution. The original
[package acceptance guide](package-acceptance.md) defines the lease, version,
failure, cleanup and Docker-image requirements; all still apply.

For SMS stream-media diagnostics, explicitly add `--timeout 300 --runner-timeout 600`
as described there. No ambiguous mutation is automatically retried.

Acceptance imports the package/media into a private host catalog, performs
compatibility and selection checks, launches and stops, restarts the private
host, then relaunches and stops the same selection. It does not modify the live
host library. Imported media must survive that restart without reimport.

## Optional input diagnostic

### Build and play an original application: FES Catch

`fes.catch` is a small ROM-less game using the existing `fes.application` 1.0
ABI, `fes.gamepad` 1.0, fixed 720p60 video and signed stereo 48 kHz audio.
It needs no BIOS, cartridge, per-core host/runtime branch, or factory-image
entry. Its producer is registered in `config/core-recipes.toml` and uses the
same authenticated HIP tools and functional build identity as other recipes.

From a clean committed checkout, simulate and prepare it:

```sh
make -C sources/misteross sim-fes-demo
mkdir -p out/core-dev
make core-dev CORE_DEV_ARGS='prepare --core fes.catch --output out/core-dev/catch-001'
```

The simulation includes gameplay, audio CDC and the full shell: actual video
frames advance the game, mailbox gamepad state moves the paddle, and a catch
event reaches the board's I2S pins. Preparation performs synthesis, routing,
timing/electrical checks and package sealing; simulation alone does not produce
an RBF or prove board acceptance. `prepared.json` names the frozen archive and
its package identity. A new output directory preserves each candidate.

Install that archive using the ordinary library commands (substitute the
archive path and package ID from the preparation receipt):

```sh
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-install /absolute/path/catch.fcore
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-check PACKAGE_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-entry 'FES Catch' PACKAGE_ID
```

On the designated leased kit, select the resulting entry in the normal library
and Play. Left/Right move the blue paddle; opposing directions cancel. Catch
white targets for a short stereo chime and a point. The six yellow lamps at
the upper left show score bits worth 1, 2, 4, 8, 16 and 32 from left to right;
score saturates at 63. Three green bars show remaining lives. Three misses
end the game with a red background. A restarts; holding A does not repeatedly
reset play. Stop uses the usual runtime lifecycle, clearing gameplay and sound.
No progress is persisted by this example.

Use the frozen-candidate acceptance command above with the Catch preparation
receipt to verify import, launch, Stop and host-restart/relaunch separately from
visible gameplay and audible HDMI checks. Compatibility must advertise every
required interface before launch. Hardware acceptance is recorded separately;
the unit and board simulations are not physical controller or HDMI evidence.

To adapt the example, start with
`sources/misteross/cores/fes-demo/rtl/fes_catch_game.v`. The game consumes the
shared frame tick, normalized buttons and playfield coordinates. The thin
`fes_catch_core.v` connects shared video, and `fes_catch_audio.v` synchronizes
an event toggle into the audio domain before generating PCM samples. The
shared application shell owns transport, reset and board wiring. Keep package
identity/source closure in the producer, register another recipe for a new core
ID, and leave network/session handling in the existing host and runtime.

### ZX81 keyboard quick-start

With the selected toolchain/package cache available, prepare a new candidate:

```sh
mkdir -p out/core-dev
python3 scripts/core_dev.py prepare --core fes.zx81 --output out/core-dev/zx81-001
sha256sum examples/input/zx81-keyboard.json
```

The committed recipe presses and releases Shift: keyboard device 0, key kind 0,
code 256, on `fes.keyboard` 1.0. The code comes from the selected FogCast
`internal/zx81keys/matrix.go`; it is not a gamepad-to-keyboard mapping.
No external media is required for this ZX81 package. Shift alone need not
produce visible output; success does not prove FPGA key consumption.

After reserving the kit and checking its lease, use the acceptance command
above with `--prepared out/core-dev/zx81-001/prepared.json`, a fresh evidence
directory and title, and add:

```text
--input-events /absolute/path/to/fes/examples/input/zx81-keyboard.json
--expected-input-sha256 0e93f736f2d0418f2791ae702758e0ae21b6924e2380ba6cdc232e37b80f296b
--input-timeout 10
```

Verify the recipe digest yourself and retain the explicit accepted host binary,
container image, target and platform-revision arguments; do not discover and
blindly trust the installed versions. A successful run produces `receipt.json`
and `preparation.json`, plus each cycle's receipt. Check both cycles, host
shutdown/container cleanup, credential removal and the released kit lease.
This is a transport/lifecycle diagnostic, not a typing or physical USB test.

### Input recipe contract

For an authorized, exclusively owned kit test, add an explicit event recipe to
the acceptance command:

```text
--input-events /absolute/path/pong-gamepad.json \
--expected-input-sha256 INPUT_FILE_SHA256 --input-timeout 10
```

`examples/input/pong-gamepad.json` is a bounded press/release of gamepad D-pad Up
for a core advertising `fes.gamepad` 1.0. It is an explicit example, not a
core-name default. Inspect the file and calculate its SHA-256 before supplying
that digest. The command never silently selects an input recipe for a core.

The isolated host enables remote input only when this diagnostic is requested;
lifecycle-only runs keep it disabled. The same options are available on both
package-acceptance runners. The isolated
wrapper freezes one copy in its evidence directory and uses it in both lifecycle
cycles. Missing options preserve the previous lifecycle-only behavior.

Recipes are closed format-1 JSON documents with a versioned `interface` and an
explicit `events` array. Files are limited to 64 KiB and 64 events. Supported
event forms, balanced press/release pairs and neutral final axis state are
validated before any API mutation. No arbitrary sleeps or commands are allowed.
The runtime must advertise the exact requested active interface and the owned
input bridge must be attached and ready before dispatch.

The runner checks session and input-bridge identity before each event and while
observing counters. A changed owner, reconnect, regressed counter, timeout or
ambiguous request fails the diagnostic; events are never replayed automatically.
Existing owned-session Stop cleanup remains responsible for returning to idle.
The input endpoint does not accept a session compare-and-swap token, so these
checks are not atomic exclusion. Exclusive host/kit use is still mandatory.

## Evidence boundary and remaining work

Preparation proves a bound build artifact, not runtime compatibility.
Isolated acceptance proves the recorded package/media selection and lifecycle,
not visible pixels or controller behavior. It is not an image/release receipt.

Input receipts distinguish requested/acknowledged events from the observed
transport frame delta; one event need not produce one frame. Positive frame
progress can include heartbeat traffic and is not proof that the FPGA consumed
an event, that a physical USB
controller works, or that the display changed. Physical display/controller
confirmation remains separate. Do not substitute generic Coleco events for an
unfamiliar core. Independent execution by another team is also required before
declaring new-core onboarding routine.

## Recorded ZX81 diagnostic (2026-09-18)

Fresh package preparation on Powerboat took 4.75 seconds using the existing
HIP build cache, with no image assembly. It reproduced package
`af84d2c7fd0ec920beb3214594688d2b17eb5724aba5ac917cfa37f359cc3a1e`
and archive SHA-256
`c765295e0c3e4c2b7ae0157a881c0d8eed372b41a3b5cebcaf46fb89634d5208`.

The acceptance runner from FES `f2bf099740b1cc4d500d31d26a73accc1c876b89`
used the explicit keyboard recipe above. On target
`73dc9f5f-1a12-4a95-a820-a9b4e600769a`, both initial launch/input/Stop and
host-restart/relaunch/input/Stop passed: 2/2 events acknowledged and a frame
delta of 3 per cycle. The same private library entry survived the host restart.
Strict host/agent revision checks matched
`d9745ed746a1e8d0bde423151d08810248ce8815`; runtime matched
`a6d658cd305c4a84860afc1f8b00a2798ee6e4f4`.

Both host containers exited 0 and were removed, private credentials were
removed, and the kit lease returned to free. The boot ID and installed image
digest were unchanged. No deployment, reboot, or physical USB/display test
was performed. Event acknowledgement and frame progress remain transport
evidence, not FPGA key-consumption evidence.

Retained evidence paths on Powerboat:

- Preparation: `/home/deano/fes/out/dev/library-client/fes/out/core-dev/zx81-input-20260918-02/prepared.json`
- Acceptance: `/home/deano/fes/out/hardware/zx81-input-20260918.AuX9VN/acceptance/`

The acceptance directory contains the frozen input recipe, both cycle receipts,
the final `receipt.json`, and `preparation.json` binding the prepared candidate.
Preparation used the retained build-cache checkout at `27bc483`; its four
selected component revisions match the acceptance checkout. The build-cache
checkout and live host library were otherwise left unchanged.
