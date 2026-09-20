# Artifact identities

FES selects compatible component *sources*. A running system is a set of
**artifacts**: OS/arch-specific binaries, an image, cores, and an ABI snapshot.
This page names those artifacts, who builds them, and what is allowed to differ.
It does not change commands.

Fail closed on mismatch. Do not rewrite host config or target identity to make
an unmatched pair look current. Integration builds use committed FES modules.
Explicit local development snapshots are marked diagnostic and cannot satisfy
release checks.

## Roles (not machines)

A machine may play several roles.

| Role | Current examples |
| --- | --- |
| Library | `fogcast-api` catalog and ROM storage (Linux kit-host or Mac sofa) |
| Coordinator | same `fogcast-api` process; owns session/lease *client* |
| Surface | browser on loopback `:8787`, SDL `fogcast-tenfoot`, kit `fogcast-kit` |
| Input source | sofa HID, kit evdev pad (today forwarded through the host) |
| Output sink | FPGA HDMI, ShadowCast preview on the host, sofa window |
| FPGA target | `mister-agent` + `mister-runtime` on the DE10-Nano |
| Builder | Quartus, misteross OSS tools, Buildroot — never on the play path |

`selected_target` is the default host-session FPGA target, not a process
identity. `POST /api/v1/session/launch` may bind a different configured
target without rewriting that default. Two configured targets may play at
once; `GET /api/v1/sessions` lists live plays. `GET /api/v1/session` is the
foreground session. Surfaces attaching by session id is later session work.

## Artifacts

| Artifact | Typical OS/arch | Produced by | Locked by |
| --- | --- | --- | --- |
| Host API/CLI | linux/amd64 | FES `make host` | `host.json` (`os`, `arch`, binary hashes) |
| Host API (sofa) | darwin/arm64 | FogCast on a Mac (`build-fogcast-api` / signed app) | same receipt schema with `os=darwin`; not the FES Linux `host.json` |
| Tenfoot | darwin/arm64 or linux + SDL3 | FogCast `build-fogcast-tenfoot` | not a parent output |
| Target agent | linux/armv7 | FogCast binary, FES `image/` install | `mister_agent_sha256` in `build-inputs` |
| Kit launcher | linux/armv7 | FogCast binary, FES `image/` install | `fogcast_kit_sha256` in `build-inputs` |
| Runtime daemon | linux/armv7 | libmister-runtime via FES `image/` | `mister_runtime_commit` in `build-inputs` |
| Rootfs / appliance image | ARMv7 ext4 | FES `image/` recipe | `image.json`, appliance `image_sha256` |
| Idle RBF | FPGA bitstream | Distribution_MiSTer pin | `idle_sha256` |
| Catalog cores | FPGA bitstream | misteross bundles | `*_sha256` / selection records |
| Format-2 package | manifest + RBF | misteross | package id + payload sha |
| ABI snapshot | generated C++/Go | mister-packages | `make check` consumers |
| Bootstrap / kernel | locked boot | FES `platform/` plus media lock | `boot-media.lock.toml`, bootstrap evidence |

`make host` on Linux writes `host.json` with `os=linux` and `arch=amd64`. That
receipt cannot be presented as Darwin. A Darwin sofa receipt uses the same JSON
schema (`inputs`, `files`, `fes_revision`, `os`, `arch`) produced on a Mac with
FogCast `make build-fogcast` / `build-fogcast-api` (GOOS/GOARCH default to that
machine). `load_verified_host(..., os_name='darwin', arch='arm64')` verifies it.
The signed `FogCastHost.app` remains Darwin-only.

## On-wire identity

The target `GET /v1/health` object may include `artifacts` (omitted when the
agent has no closed record, for example a Main-backend diagnostic). Fields are
content hashes and revisions from the installed `build-inputs` file and, on an
appliance boot, the bootstrap ticket’s `image_sha256`. Health does not hash
live binaries on each poll.

The host `GET /api/v1/health` object includes a `host` identity (`version`,
`revision`, `os`, `arch`) and forwards `target.artifacts` when the target is
reachable.

Live connection compatibility requires the target API contract `v1`, not equal
FogCast or runtime Git revisions. Missing or unsupported API versions produce
`version_mismatch` and refuse admission. Package ABI, native runtime protocol,
media, input and persistence support are checked by the corresponding operation.
Artifact revisions remain provenance; missing provenance does not bypass API
admission. Configuration is not rewritten.

## What may differ

| May differ | Must match for a launch |
| --- | --- |
| Host, agent and runtime source revisions | Supported target API and operation-specific runtime/package contracts; selected package identity and target ownership |
| Working files vs committed module snapshots (working files are not the image) | Recorded FES selection, external native policy and generated package consumers (`make check`) |
| Capture device presence | Not part of the FPGA tuple |
| DHCP address | `target_id` (discovery); never a new identity minted by media |

## Build graph

```text
mister-packages  → abi-snapshot (checked-in consumers)
misteross        → core packages / RBF bundles
libmister-runtime + FogCast agent → target binaries
FogCast host     → host-app / tenfoot / kit (per OS)
FES              → selected tuple + receipts + media
```

Changing tenfoot must not require Quartus. Changing a core must not require a
Darwin sofa rebuild. Image assembly lives in FES `image/`; FogCast remains an
input for agent, kit and lock artifacts.
