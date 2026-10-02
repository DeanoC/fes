# Kit 2 launcher idle follow-up — 2026-10-02

Scope: FogCast wheel/catalog work, label texture reuse and CPU-launcher Go
memory policy after PR #391's Kit 2 resource check failed. Base:
`71121449bd56950f47387440a27f9fa130b16d6f`. Selected source commit:
`96f85e35c9237c62ed3b89cf9e6db6c68be396d2`. This record adds documentation to
those tested source bytes. Issue #318 remains open for exact-image acceptance.
No runtime, FPGA, host API or shared wire changes.

## Causes and changes

A 41.32-second live grid CPU profile sampled 56.47 CPU-seconds (136.68% of one
core, including startup). Catalog filtering/copying accounted for 43.37%
cumulative CPU: wheel representatives, counts and prefetch repeatedly copied
or scanned the 3,656-title catalog before the paint gate. Removing copies alone
still left repeated scans at about 61% of one core in a longer mixed-scene run.
Wheel summaries now rebuild on catalog replacement and volatile-field refresh.
Prefetch reads representatives directly; tiles and footer construction follow
the paint gate. Identical complete wheel frames also reuse their rendered
revision on the bounded refresh. Motion bypasses reuse and forces a settled
frame, and scene changes reset the wheel cache.

Tenfoot keyed label textures by the entire source string. A changing lease
countdown could upload a new texture and invalidate the scene/settings backdrop
even when that countdown was clipped off screen. Labels now retain their source
and fitted visible text by slot, width and font size. Unchanged sources skip
fitting; invisible suffix changes retain the texture. Visible text and geometry
changes still replace it.

The baseline live heap retained about 14.75 MiB after collection, while RSS and
memory pressure were much larger. Artwork work in startup-inclusive profiles
therefore does not establish continual artwork re-decoding or a retained-cache
leak. The kit grid and tenfoot menu-display/linuxfb entry points now default to
a 96 MiB soft Go memory limit, honoring explicit `GOMEMLIMIT`. It collects and
scavenges temporary heaps sooner; it is neither an RSS cap nor a hard decode
allocation limit. Desktop SDL retains the usual runtime policy. The final live
settings heap retained 19.74 MiB, including its backdrop and menu buffers.

## Selected binaries and hardware

Kit 2 only: `192.168.10.85`, target
`67c5f4e2-d288-49bb-9049-39ecf39cf6f6`. Dual Cortex-A9, ARMv7/NEON,
800 MHz performance governor. Existing renewable target lease held by
`fes-318-idle-profile`; original launcher stopped during private diagnostics.
The existing host service and installed agent/runtime stayed running.

Build: Go 1.26.5, `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`,
`-trimpath -ldflags='-s -w'`.

| Binary | SHA-256 |
| --- | --- |
| fogcast-kit | `9abe33a74021e65aa5859fa498c9fa8c234efd75aecb63b4dfa0e9ca20c5d007` |
| fogcast-tenfoot | `312e35ba02729c6b602e77f4a9357b48daf411d9c9fd6a5952f6713f19d0582c` |

Private release-binary scratch occupied 27,244 KiB. Earlier unstripped profiling
staging briefly occupied 36,460 KiB, above the proposed 32 MiB scratch budget;
those binaries were removed before release-binary measurements.

## Resource measurements

Counters use aggregate `/proc/stat` and launcher process ticks over 30-second
windows. Process CPU is percent of one core; aggregate idle excludes iowait.
The full live catalog contains 3,656 titles. The wheel window starts 20 seconds
after startup and ends before its attract timeout. Tenfoot library starts after
45 seconds, with attract explicitly disabled. Settings follows the captured
navigation, then remains static for its resource window. CPU profiling is on.

| Release-binary scene | One-core CPU | Aggregate idle | MemAvailable at end |
| --- | ---: | ---: | ---: |
| Platform wheel | 13.60% | 91.26% | 349,996 KiB (342 MiB) |
| Tenfoot library | 6.88% | 94.67% | 335,684 KiB (328 MiB) |
| Tenfoot static settings | 7.74% | 91.32% | 374,396 KiB (366 MiB) |

These samples pass the proposed <=15% launcher CPU, >=80% aggregate idle and
>=256 MiB MemAvailable thresholds. The read-only collector also observed
load_1=0.26. This does not prove a 5 ms changed-frame operation or qualify every
animated scene. A longer grid sample crossing into attract measured 16.08% CPU
and 87.74% aggregate idle; keep it separate from the idle-wheel result. The CPU
profiles include startup, and some include attract/input; their overall means
are not the warmed resource windows above. No artwork-free steady-state claim
follows from an entire startup-inclusive profile.

## HDMI/input and software checks

Capture: ASUS 4KPRO `802B003090700329`, exclusively through its stable by-id node,
at the MENU output's 1280x720/60 mode. The final 30-second settings video shows
all 40 injected keyboard Down/Up focus transitions, zero multiple-highlight
frames, and 1,740 analyzed frames with one highlight (starting at second 1).
The library and wheel captures show their actual scenes. This is synthetic
evdev delivery, not physical-button or absolute input-latency proof.

The paired listener still reports `settings failed` for host administration,
as in the previous run. Local settings/navigation remain usable; administrative
saves and game launch/Stop are outside this diagnostic's acceptance.

Focused FogCast renderer/launcher/shared race tests and both ARM cross-builds
passed. `make check` passed on committed sources. The final affected runner
passed all 10 commands, including 611 parent Python tests (39 optional skips),
FogCast/appliance/shared-linker race suites and host UI tests. An intermediate
runner overlapped source edits and saw a temporary compile error; the stabilized
source rerun passed. The 3,656-title wheel query benchmark is 342 ns/op,
96 B/op, 3 allocations for the returned wheel item list/labels; representative,
play-count and last-played queries allocate zero temporary catalog copies.

## Restoration and limits

Original supervised launcher restored, live and on-disk SHA-256:
`7c62078ae6fd4a009e17f9481adf724add6a32428f9fc91891405b371a3cea65`.
Boot remains `da6bd708-6c4a-45ca-b600-d58a94d72b27`. Active root loop remains
`f449886fc026dbf678e7ab22ac14dd6d54c924485658e1015835f8fd3c9a127e.img`.
The generic collector reports bootstrap factory image `e1b687ea…`; that is not
the active appliance root image. No reboot or immutable-image update occurred.
All owned target files/input helpers and the private host config were removed.
Existing host service remains active, capture is free, lease is confirmed free,
and agent status is idle with no last error.

This is bounded exact-binary hardware evidence on the existing image, not formal
acceptance of a newly assembled appliance. Next integration step: review the
follow-up PR, select its committed bytes in an appliance image and repeat the
exact-artifact acceptance. Do not close #318 solely from this diagnostic.

Raw counters, profiles, binaries, videos, captures, analysis and SHA-256 inventory
remain under `out/fes-318/idle/` in the task worktree. The preceding failed run is
recorded at `out/fes-318/kit2/validation.md`.
