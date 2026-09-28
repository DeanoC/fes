# Kit 2 menu-frame mutation diagnostic — 2026-09-28

The launch admission fix in FES commit `19d8e221` was tested on kit 2
(`02:46:43:9d:ac:d6`, `192.168.10.212`). The installed factory root image was
`e1b687ea77a3a87115530d2c27fd8373f732dba881c5d5676443e5bc8550e6b9`.
For this bounded diagnostic, the self-contained ARM `mister-runtime` binary
(SHA-256 `a9fa794a503f234712f934f53f9e296ecfce1ad6d84fa8fd7277298953267dee`)
was bind-mounted from `/tmp` over `/usr/sbin/mister-runtime`; the factory image
was not modified. The runtime and agent were restarted through their init
scripts while the kit lease was free.

With `fogcast-kit` running and the native menu presenting frames, the paired
FogCast host launched the installed FES Pong menu-release package six times.
Every launch returned HTTP 200 and `active`, with no menu pause. Each matching
Stop returned HTTP 200 and `idle`; the kit lease returned to `free`. The
operator confirmed the native menu was visible after the first Stop. This
reproduces the previously failing menu-to-Pong path with the patched runtime.

The runtime overlay was removed, the factory binary SHA-256
`8582bbffab791f3b98619cd5415d134bb1107c2ee7eeb29773463d53867ddc82`
was restored, and the runtime and agent were restarted. This is a temporary
hardware diagnostic, not exact-image acceptance or a release qualification.

Controller response was not established. A known-working DualSense and cable
worked on the other MiSTer, but kit 2 never enumerated the pad in `lsusb` or
`/proc/bus/input/devices`; no USB hotplug event appeared after reconnection.
The USB connection needs separate physical investigation.
