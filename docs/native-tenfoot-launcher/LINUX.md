# Linux SDL3 build path

`make build-fogcast-tenfoot` builds `cmd/fogcast-tenfoot` with `-tags sdl3` on
Linux as well as Darwin. Mac remains the primary sofa target. Linux uses the
same public host API client; there is no second launch path.

## What this guide covers

1. **Linux SDL3 build** for `cmd/fogcast-tenfoot` / `host/tenfoot` with `-tags sdl3`.
2. **Makefile / `TENFOOT_CGO_ENV`** that follows `uname -s`: Darwin keeps the
   Homebrew deployment-target flags; other hosts get `CGO_ENABLED=1` only.
3. **Docs** for distro packages, native-on-Linux build/run, and an honest split
   of what the Mac mini can prove vs what needs a Linux box.
4. Existing sofa behavior (browse layouts, safe-area, attract stills-or-video,
   GPU park, launch/stop) is unchanged on Darwin aside from attract video.

## Non-goals

- Host API redesign (loopback `Host` allowlist stays in the API). Tenfoot
  smoke may send `Host: 127.0.0.1:<port>` so a container can prove the client
  against the existing loopback API.
- Bundled ffmpeg / extra cgo video libraries. Linux attract video is optional
  `ffmpeg` on PATH; missing ffmpeg keeps the stills fallback.
- Further #110 / residual polish.
- New sofa layout modes beyond grid / shelf / list.
- Full CI matrix for Linux tenfoot.
- Cross-compiling a GUI SDL3 binary from macOS without a Linux sysroot.
- Kit / MiSTer, Grok Bot coding, Caster work.

## Inventory

| Area | Now |
|------|--------|
| `Makefile` `TENFOOT_CGO_ENV` | Darwin: `MACOSX_DEPLOYMENT_TARGET=11.0`, `-mmacosx-version-min=11.0`, `CGO_LDFLAGS_ALLOW`. Else: `CGO_ENABLED=1`. Override with `make TENFOOT_CGO_ENV='…'`. |
| `make tenfoot-cgo-env` | Prints the env the tenfoot target uses on this host. |
| `make build-fogcast-tenfoot` | `$(TENFOOT_CGO_ENV) go build -tags sdl3 … ./cmd/fogcast-tenfoot` |
| `host/tenfoot/sdl.go` | `//go:build sdl3` + `#cgo pkg-config: sdl3` |
| `host/tenfoot/run_stub.go` | `//go:build !sdl3` stub |
| Fonts (`label.go`) | Mac system fonts first; Noto/DejaVu Linux paths; embedded Go Regular fallback |
| Prefs | `os.UserConfigDir()` → Mac `~/Library/Application Support/FogCast/tenfoot.json`; Linux `$XDG_CONFIG_HOME/FogCast/tenfoot.json` or `~/.config/FogCast/tenfoot.json` |
| CI | No Linux SDL3 job (out of scope) |

## What the Mac mini can prove vs a Linux box

| Check | Where | Status |
|-------|--------|--------|
| Darwin `make tenfoot-cgo-env` includes `MACOSX_DEPLOYMENT_TARGET` | Mac mini | Proven |
| Darwin `go test ./host/tenfoot/` | Mac mini | Proven |
| Darwin `make build-fogcast-tenfoot` | Mac mini | Proven |
| Darwin `make tenfoot-smoke` vs `:8787` | Mac mini | Proven (kit may be unavailable) |
| Linux `TENFOOT_CGO_ENV` is `CGO_ENABLED=1` (no Darwin flags) | Linux `uname` | Proven: fake-`uname` on mini + debian:sid container both print `CGO_ENABLED=1` |
| Linux `pkg-config --modversion sdl3` + `make build-fogcast-tenfoot` | debian:sid **container** on mini (aarch64) | Proven: `libsdl3-dev` → `sdl3` 3.4.16; ELF linked to `libSDL3.so.0`. This is a Linux userspace compile, not `GOOS=linux` from Darwin cgo. |
| Linux `-smoke` vs host API | debian:sid **container** on mini using `-api http://host.docker.internal:8787` | **Proven** (this wave): `-smoke` sends `Host: 127.0.0.1:8787`, no `403 HOST_NOT_ALLOWED`, same launch JSON as Darwin (`MISTER_UNAVAILABLE` because the kit is down). `-api-host` / `FOGCAST_API_HOST` override. This is a client Host header, not a host API change. Native same-box `http://127.0.0.1:8787` on Deano’s Linux box is still the living-room check. |
| Linux windowed/fullscreen on X11/Wayland | Linux box with a display | **NEED** — cannot fake on the mini |
| Linux attract video decode | Linux box with `ffmpeg` on PATH and a display | Code path landed: optional ffmpeg CLI (`host/tenfoot/attractvideo`) when `ffmpeg` is on PATH; otherwise skip the video download and use stills. debian:sid container with ffmpeg ran `go test ./host/tenfoot/attractvideo`. GUI video on a real display is **NEED**. Darwin still uses AVFoundation. |

Cross-compile from Mac (`GOOS=linux go build -tags sdl3` on Darwin) is **not**
provided. CGO + SDL3 needs a Linux compiler, headers, and `sdl3.pc`. A Linux
container on the mini can compile; that is not a Mac-native cross toolchain.

## Dependencies (Linux)

`#cgo pkg-config: sdl3` is unchanged. Distro packages must provide a module
named **`sdl3`** (file `sdl3.pc`). If a distro uses another `.pc` name, set
`PKG_CONFIG_PATH` to a wrapper/`sdl3.pc` rather than changing the cgo line
(that would break Homebrew on Mac).

Do **not** require Homebrew on Linux.

### Debian / Ubuntu

```sh
sudo apt-get update
sudo apt-get install -y build-essential pkg-config libsdl3-dev
pkg-config --modversion sdl3   # must succeed
```

| Distro | `libsdl3-dev` in archive? |
|--------|---------------------------|
| Ubuntu 25.10 (questing) and newer | Yes (universe); `.pc` is `sdl3` |
| Ubuntu 26.04 LTS (resolute) | Yes (universe); `.pc` is `sdl3` |
| Debian testing / sid | Yes; `.pc` is `/usr/lib/<triplet>/pkgconfig/sdl3.pc` |
| Ubuntu 24.04 LTS (noble) | **No** official `libsdl3-dev`. Use a newer distro, Fedora, or build SDL3 from source. A PPA is not documented here. |
| Ubuntu 22.04 LTS | **No** |

Optional fonts already searched: `fonts-dejavu-core` and/or `fonts-noto-cjk`
(`DejaVuSans.ttf` / `NotoSansCJK-Regular.ttc` under `/usr/share/fonts/…`).
Missing fonts fall back to embedded Go Regular.

Optional attract **video**: install distro `ffmpeg` (and `ffprobe`, usually the
same package). Tenfoot does not link libav. If `ffmpeg` is missing, attract
skips the video download and uses stills.

Go: same major as `go.mod` (cgo-enabled). Distro `golang-go` is often too old;
install the official toolchain if `go version` is below the module’s `go` line.

### Fedora

```sh
sudo dnf install -y gcc make pkgconf pkgconf-pkg-config SDL3-devel
pkg-config --modversion sdl3   # must succeed
```

`pkgconf-pkg-config` is the `/usr/bin/pkg-config` shim Go's `#cgo pkg-config: sdl3`
uses; `pkgconf` alone does not install that command on a minimal Fedora.
`SDL3-devel` provides `pkgconfig(sdl3)` (`/usr/lib64/pkgconfig/sdl3.pc`).
Fedora 42+ ships it. Optional: `dejavu-sans-fonts` / `google-noto-sans-cjk-fonts`.
Optional video: `ffmpeg`.

### Arch

```sh
sudo pacman -S --needed base-devel pkgconf sdl3
pkg-config --modversion sdl3
```

Optional video: `ffmpeg`.

**Verified on the mini:** debian:sid `apt-get install libsdl3-dev` →
`pkg-config --modversion sdl3` reports **3.4.16**, and
`make build-fogcast-tenfoot` links `bin/fogcast-tenfoot` to
`/usr/lib/aarch64-linux-gnu/libSDL3.so.0`.

Fedora `SDL3-devel` and Arch `sdl3` are from distro indexes (`pkgconfig(sdl3)` /
`sdl3.pc`), not executed on this mini. **NEED:** Deano’s native Linux box for
`dnf`/`pacman` and GUI smoke.

## Build

### Native on Linux (preferred)

```sh
pkg-config --modversion sdl3   # must succeed
make tenfoot-cgo-env           # expect: CGO_ENABLED=1
make build-fogcast-tenfoot
```

Expect `bin/fogcast-tenfoot` linked against system SDL3. `uname` is not Darwin,
so the Makefile must not inject `MACOSX_DEPLOYMENT_TARGET` or
`-mmacosx-version-min`.

### Darwin (unchanged)

```sh
# Homebrew sdl3 + pkg-config
make tenfoot-cgo-env           # includes MACOSX_DEPLOYMENT_TARGET=11.0
make build-fogcast-tenfoot
make tenfoot-smoke
```

### Cross-compile from Mac → Linux

Not supported. Do not set `GOOS=linux` on Darwin for this binary.

A Linux **container** on the mini can compile (Linux `gcc` + `libsdl3-dev` +
Linux Go). That is native Linux cgo inside the container, not a documented
product workflow. Prefer `make build-fogcast-tenfoot` on a real Linux box.

## Run (Linux)

Host API must already listen (default `http://127.0.0.1:8787`):

```sh
bin/fogcast-tenfoot
bin/fogcast-tenfoot -fullscreen
bin/fogcast-tenfoot -layout shelf
bin/fogcast-tenfoot -no-attract
bin/fogcast-tenfoot -smoke -no-attract -api http://127.0.0.1:8787
# container / host-gateway against the Darwin loopback API:
bin/fogcast-tenfoot -smoke -no-attract -api http://host.docker.internal:8787
# equivalent explicit Host (sofa runs do not rewrite unless this is set):
bin/fogcast-tenfoot -smoke -api http://192.168.10.230:8787 -api-host 127.0.0.1:8787
```

`-smoke` implies `-no-attract`. When `-api` is not loopback/`localhost`, `-smoke`
sets the HTTP Host header to `127.0.0.1` plus the URL port so the host API
loopback allowlist does not return `403 HOST_NOT_ALLOWED`. That does not bind
the API to a LAN address and does not change the host API.

Use a session with a real display (X11/Wayland) for the windowed/fullscreen
path. Headless agents may get SDL dummy video (same fallback as Mac headless);
cover grid / gamepad / host launch can still exercise logic where SDL allows.
Attract video on Linux needs `ffmpeg` on PATH **and** a display for GUI proof.

Prefs path on Linux: `$XDG_CONFIG_HOME/FogCast/tenfoot.json` or
`~/.config/FogCast/tenfoot.json`.
