# Native NES support

## Goal

Add NES as the next native FES system using the official MiSTer NES core and
the existing native launch path. A fresh FES checkout should be able to select
the exact NES source revision, build or validate its RBF bundle, install it in a
native image beside Mega Drive, Pong, and SNES, and launch an ordinary NES
cartridge through the existing FogCast target agent and libmister-runtime
daemon.

The first vertical slice is deliberately small: one NES core, one iNES/NES2
cartridge file, one standard controller, and clean Stop/relaunch. It proves the
cross-repository contract without changing the UI or creating another launch
backend.

## Current constraints

- FES remains the integrator and selects compatible child commits.
- `mister-packages` owns the NES source and system contracts and emits the
  checked-in C++ and Go consumers.
- `misteross` owns source checkout, Quartus rebuild, simulation/build evidence,
  and sealed RBF bundle export. It does not program hardware.
- `libmister-runtime` owns NES profile admission, cartridge preflight, FPGA
  programming, media delivery, controller packets, and idle recovery.
- FogCast owns the native target adapter and image-side core installation. The
  UI and catalog presentation remain with the separate UI work.
- The pinned upstream source is
  `https://github.com/MiSTer-devel/NES_MiSTer` at
  `9a63821173b6da4d6e95dcbe2e2a322ec8171144` (Release 20260823).
- Its checked-in upstream artifact is
  `releases/NES_20260823.rbf`, SHA-256
  `a4c023defa4f7856585e5dba429a3b61aee3e01eb3de2c731bb0036c12f11701`,
  size `3282472`, and Quartus project `NES.qpf`.

## Launch contract

The shared system package defines:

- system ID `nes`, expected observed core `NES`, and artifact `nes.rbf`;
- cartridge media role `cartridge` at MiSTer file index `0`;
- `.nes` as the only admitted extension in this slice;
- a 32 MiB bounded transfer;
- raw little-endian byte-pair transfer;
- one player with the standard NES A/B, D-pad, Select, and Start masks.

The runtime validates the file before programming. It requires the 16-byte
`NES\x1a` iNES header, a nonzero declared PRG payload, a declared payload that
fits in the supplied file and the 32 MiB bound, and no trainer bit. Both
iNES 1.0 and NES2 size encodings are accepted. The file is passed unchanged;
the FPGA loader owns mapper interpretation. A malformed or truncated file is
rejected before any FPGA mutation.

The native runtime does not expose NES save paths. FDS images, UNIF/UNF files,
NSF, trainers, cheat codes, light guns, four-player accessories, and
mapper-specific validation are outside this slice. The upstream core may
implement those features, but they are not part of the FES native contract
until they receive separate media and lifecycle designs.

## Artifact and image flow

`misteross/cores.lock` and `mister-packages/packages/source/nes_mister.yaml`
carry the same repository, commit, RBF path, digest, size, and project. The
existing generic fetch and Quartus rebuild lanes are extended to accept `nes`.
NES export uses the existing format-1 sealed bundle with `nes.rbf` and
`nes-rbf.toml`, recording the source revision, recipe digest, toolchain, and
artifact hash. A rebuild need not bit-match the official Standard Edition RBF,
but the upstream bytes remain hash-checked provenance.

The native image selector accepts the ordered system set
`megadrive pong snes nes`. It stages a source-built NES bundle into
`/usr/share/mister-runtime/cores/nes.rbf` and its sealed selection record beside
the existing cores. The default Mega Drive-only profile and historical
profiles remain unchanged; only the integration profile opts into NES.

## Runtime data flow

1. FogCast resolves an NES game to the existing native runtime adapter.
2. The adapter verifies the registered `.nes` path and sends the fixed
   `/usr/share/mister-runtime/cores/nes.rbf` plus one cartridge path over the
   existing Unix-socket protocol.
3. `libmister-runtime` resolves the generated NES profile, opens the RBF and
   cartridge, validates the iNES/NES2 header, programs the FPGA, streams the
   unchanged cartridge bytes at index 0, sends neutral input, and releases the
   core into the running state.
4. Stop uses the existing idle-RBF and recovery path. The next launch may be
   NES, Mega Drive, Pong, or SNES without a second daemon or a new coordinator.

No host UI code, public API shape, lease protocol, bootloader, or update
mechanism changes.

## Failure behavior

- Missing or wrong-extension cartridges fail during FogCast admission.
- Invalid magic, trainer, zero PRG, impossible NES2 size, or truncated payload
  fails in runtime artifact preflight with `invalid_request` before FPGA
  programming.
- Missing or mismatched `nes.rbf` fails with the same core/artifact checks used
  by the other native profiles.
- A program, media, video, input, or core-identity failure follows the existing
  native idle/recovery path and never claims a running game.
- NES Stop does not create or flush a save file. Existing SNES save behavior is
  unchanged.

## Verification

The milestone is complete in software when focused tests cover the package
schema/emitted consumers, lock and bundle admission, NES header acceptance and
rejection, generated runtime profile, FogCast native request validation, and
the four-system image selector. Parent `make check`, child tests, and
`git diff --check` must pass.

Hardware acceptance is a separate gate against an exact assembled image and
NES RBF hash. It should launch a known iNES title (for example a mapper-0
cartridge), demonstrate visible video and A/B/D-pad/Start input, Stop to the
launcher, and relaunch another native system. Until that evidence exists,
documentation must call NES software-supported or hardware-pending rather than
hardware-supported.

## Non-goals

- No UI, catalog artwork, or new network endpoint.
- No custom NES HDL or fork of the upstream core.
- No FDS BIOS/media path, UNIF/UNF conversion, NSF player, saves, savestates,
  cheat hardware, Zapper, Miracle Piano, SNAC, or four-player support.
- No cold rebuild of unchanged Linux, compilers, or base packages during
  development; reuse the existing native image caches and defer a two-pass
  image build until the selected component commits are stable.
