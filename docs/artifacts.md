# Artifact identities

FES selects compatible component *sources*. A running system is a set of
**artifacts**: OS/arch-specific binaries, an image, cores, and an ABI snapshot.
This page names those artifacts, who builds them, and what is allowed to differ.
It does not change commands.

Fail closed on mismatch. Do not rewrite host config, pins, or target identity to
make an unmatched pair look current. Parent builds use selected gitlinks, not
uncommitted worktrees.

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

Today one `selected_target` and one coordinator process bind these together.
That 1:1 coupling is the later session work, not this inventory.

## Artifacts

| Artifact | Typical OS/arch | Produced by | Locked by |
| --- | --- | --- | --- |
| Host API/CLI | linux/amd64 | FES `make host` | `host.json` (`os`, `arch`, binary hashes) |
| Host API (sofa) | darwin/arm64 | FogCast on a Mac (`build-fogcast-api` / signed app) | same receipt schema with `os=darwin`; not the FES Linux `host.json` |
| Tenfoot | darwin/arm64 or linux + SDL3 | FogCast `build-fogcast-tenfoot` | not a parent output |
| Target agent | linux/armv7 | FogCast image recipe | `mister_agent_sha256` in `build-inputs` |
| Kit launcher | linux/armv7 | FogCast image recipe | `fogcast_kit_sha256` in `build-inputs` |
| Runtime daemon | linux/armv7 | libmister-runtime via image recipe | `mister_runtime_commit` in `build-inputs` |
| Rootfs / appliance image | ARMv7 ext4 | FogCast recipe, FES assemble | `image.json`, appliance `image_sha256` |
| Idle RBF | FPGA bitstream | Distribution_MiSTer pin | `idle_sha256` |
| Catalog cores | FPGA bitstream | misteross bundles | `*_sha256` / selection records |
| Format-2 package | manifest + RBF | misteross | package id + payload sha |
| ABI snapshot | generated C++/Go | mister-packages | `make check` consumers |
| Bootstrap / kernel | locked boot | FES media lock | `boot-media.lock.toml` |

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

When both sides advertise a comparable runtime commit or FogCast revision and
they disagree, host connection state is `version_mismatch` (the target stays
reachable). Launches, development loads, and package activation are refused.
Configuration is not rewritten. Missing artifacts (Main-backend images, or
binaries built without git ldflags) stay compatible until they advertise
identity.

## What may differ

| May differ | Must match for a launch |
| --- | --- |
| Surface UI build vs last cold image, if the protocol is unchanged and the operator accepts diagnostic use | Agent, runtime commit, installed cores, and ABI snapshot the host was selected against |
| Uncommitted component worktrees vs parent pins (worktrees are not the image) | Parent gitlink, FogCast runtime lock, and generated package consumers (`make check`) |
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
Darwin sofa rebuild. Image assembly still lives in the selected FogCast recipe
until that migration completes; FES orchestrates it and records the tuple.
