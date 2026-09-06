# Pong and SNES integration

Status: implementation started; neither new system is hardware-supported.
The user approved Pong first, followed by SNES, on 2026-09-06.

## Outcome

FES selects a compatible system that can launch Mega Drive, a ROM-less Pong
game, and SNES, stop each to idle, and switch between them without rebooting.
Keep the existing native lifecycle and component ownership. Whole-system image
ownership migration is separate.

## First deliverables

1. Make package-generated definitions usable for multiple systems and empty
   media. C++14 headers must coexist in one translation unit; no zero-length
   arrays. Go must emit identity without fabricating a cartridge index.
2. Implement deterministic digital-control Pong game logic and simulate reset,
   paddle movement, collisions, scoring, video and sound before a hardware build.
3. Connect Pong to a pinned MiSTer-compatible wrapper, then add its package,
   production runtime profile, host launch and image artifact selection.
4. Integrate a pinned upstream SNES core with verified cartridge transfer and
   complete one-player SNES button mapping.

## Pong contract

Use stable system ID `pong`, core identity `Pong`, artifact `pong.rbf`, and no
external media. Use ordinary profiled launch with an empty media object;
raw development-RBF loading deliberately leaves HDMI down and cannot substitute
for a playable system. One player controls the left paddle with Up/Down against
a simple opponent. Start begins a game; reset restores deterministic idle game
state. Render paddles, ball and scores, and generate a short collision/score tone.

The existing upstream Arcade-Pong implementation is analog-paddle oriented.
Implement a small digital-control game in misteross, using a pinned compatible
MiSTer wrapper for board/video/HPS integration. Do not create a second hardware
control protocol or put reset sequencing in FogCast.

## Shared contracts and ownership

- mister-packages owns system identity, media rules, reset words and input masks.
  Keep shared generated C++ declarations guarded and identical between system
  headers. Empty media emits a null pointer and zero count. Go omits the
  cartridge constant when absent; existing Mega Drive symbols retain meaning.
- misteross owns Pong RTL, wrapper/source pins, simulation and source-built RBF
  provenance. Confirm wrapper compatibility before claiming a deployable core.
- libmister-runtime owns profile admission, FPGA/video/audio/input lifecycle and
  cleanup. Reuse one profile table and daemon. Extend button recipes for SNES
  based on the upstream wire contract; preserve Mega Drive input mapping.
- FogCast owns ROM-less game discoverability, launch requests and network input.
  Empty media is allowed only for a registered ROM-less system. Unsupported
  systems and unexpected content still fail before a physical transition.
- FES owns matching consumer definitions, artifact selection and integration.
  Expand the existing bundle interface without discarding hash/provenance checks.

## SNES scope

Use `snes` and the upstream SNES core. Inspect its source and Main reference
for ROM headers, transfer indices, status bits and any required auxiliary data
before defining the package. Start acceptance with a basic supported game;
do not claim special-chip, save-state or complete-library compatibility from it.
SNES already has a FogCast product row, but is explicitly rejected by the native
adapter today.

## Validation and handoff

Use focused software tests and Verilator before Quartus. Reuse compilers and
base packages. Keep source work in `out/dev/pong-snes/`; pinned `sources/`
remain clean until compatible component commits can be selected. Uncommitted
worker tests are not parent-image evidence.

Physical acceptance requires the exact artifact identities and visible playable
output/input, audible sound, Stop, relaunch and cross-system switching on the
designated kit. Mega Drive's prior acceptance does not establish native audio.
One operator performs kit work. Record actual results and limitations; do not
mark Pong or SNES hardware-supported from simulation alone.
