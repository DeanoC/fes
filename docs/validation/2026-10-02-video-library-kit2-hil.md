# Library video selection — Kit 2 diagnostic

Normal library Play passed household direct/scanline selection on Kit 2 on
2026-10-02, with and without SGM. A missing profile used built-in direct output.
Changing the preference preserved the active FPGA generation; the next launch
used the new part. Video assets, preference and CPU selection survived a host
restart. All six final Stops returned idle and released the existing kit lease.

This is an exact-artifact normal-library diagnostic for the named format-2
Coleco shell. The factory package/profile still uses built-in direct output.
It does not qualify an appliance image, other cores or raster standards,
ROM-linked format-3/4 parts, CRT/DDR/overlay processors or pluggable audio.

## Software and selected FPGA bytes

The base is FES `85d038130e92d709a414cc09fdd168ab1b071fa4`. Clean software
`5612e8603cf0d531c69c6ef8ca57e14d0f723d72` supplied the ARM agent/runtime and
the committed production `fogcast.Open`/`hostapi.New` used by the isolated host.
The small [host driver](video-library-kit2-2026-10-02/host-driver.go.txt) sets
explicit private Paths and listens on loopback port 18787; it does not change
production lifecycle behavior. Its source and binary are separately hashed
in the [build receipt](video-library-kit2-2026-10-02/build-receipt.json).
The later `4314a052b` changes only race-test request counters.

| Item | Identity |
| --- | --- |
| Kit 2 | `67c5f4e2-d288-49bb-9049-39ecf39cf6f6` |
| Boot | `da6bd708-6c4a-45ca-b600-d58a94d72b27` |
| Installed image | `f449886fc026dbf678e7ab22ac14dd6d54c924485658e1015835f8fd3c9a127e` |
| Shell package | `3a7f36570018f0be35a99a47b68912637849275c8ca5ad8ba07384f7a8f3af28` |
| Shell BUILD_ID | `0fff6c5fc1fed9054db6451398182d4b` |
| Diagnostic runtime SHA256 | `9560b75d896e01c13f633abeba6887137537c8170e00b40ead9f8b27dcca34e7` |
| Diagnostic agent SHA256 | `3977f9e83386abc47ea3623ff846fb550eccbd213b1baff22a052818527423e5` |

The shell, direct part, scanline part and SGM asset are the unchanged sealed
FPGA bytes from the [previous video-parts diagnostic](2026-10-02-video-parts-kit2-hil.md).
Their timing and containment evidence remains in the
[host seal](2026-10-02-video-parts-seal.md). No FPGA rebuild was performed.

## Library path and selection

The isolated host has its own catalog, package store and settings overlay,
and configures only the user-designated Kit 2. The existing workstation host
and Kit 1 were left untouched. Imports used the public package, media, video
part and CPU expansion APIs; Play used `POST /api/v1/session/launch`.
The host composed selected parts, then the target independently recomposed
`POST /v1/library/core/parts`. Native `load_parts_library_core` admitted the
library data namespace and reused the physical lifecycle. This shell has
volatile core data; storing host selection does not make Coleco saves persistent.

| Generation | Final case | Composition ID |
| ---: | --- | --- |
| 2 | Missing scanline profile → built-in direct | none |
| 3 | Direct | `bb0ef695a80ade98e76170ffb8bc3f457d01f072e71e30e119913b21841efdd8` |
| 4 | Scanlines | `5f1c6d9df0f14085a2c6db51eb068a3a752395e5b228522c43ae530a4f55f375` |
| 5 | Direct + SGM | `74417e1a3246818d8fd992a7ef5dbabbc252af4c6273f7462cd861f5d081273e` |
| 6 | Scanlines + SGM | `9243b55b8f283942efc70a824cf94ef7758a78ddd1100decf6026ebf93b346e5` |
| 7 | Direct relaunch after host restart | `bb0ef695a80ade98e76170ffb8bc3f457d01f072e71e30e119913b21841efdd8` |

Every receipt matched the entire selected tuple, including shell, parts,
linked payload SHA256/size and layout. Library cartridge delivery followed
the active base package generation through the existing blob-stream path.
The open Graphics I and SGM probe ROMs are unchanged from the prior diagnostic.
The [result receipt](video-library-kit2-2026-10-02/result.json) contains exact
launch/Stop tuples, resolution, restart persistence and restored identities.
One preliminary fallback load at generation 1 was stopped and released before
the final run after correcting a host-response assertion in the diagnostic
driver; no product source change was needed.

## Captured video and audio

The ASUS 4KPRO used its stable by-id path, YUYV422 1280×720 at requested 60fps,
with five 180-frame lossless FFV1 captures and six seconds of stereo 48kHz PCM
per selection. Independent local-file analysis checked all 900 frames.
After 2–4 initial black acquisition frames per clip, all 886 settled frames
retain the expected grid and bounds x384–895, y168–551. Each clip has one
50ms acquisition interval; settled decoded timestamps use 16/17ms intervals.

The [direct](video-library-kit2-2026-10-02/direct.png) and
[scanline](video-library-kit2-2026-10-02/scanlines.png) pictures retain their
geometry and even-row values. Dominant odd/even channel ratios are about
0.4933 for the green border and 0.4930–0.4931 for red squares. Decoded capture
variants differ by at most one RGB unit; direct, SGM and relaunch settled
frame variants agree. The settled [direct SGM audio](video-library-kit2-2026-10-02/direct-sgm.flac)
and [scanline SGM audio](video-library-kit2-2026-10-02/scanlines-sgm.flac) retain
SN near 216.784Hz and AY near 440.397Hz. Audio acquisition is initially quiet
for about 1.85–1.98 seconds;
comparison uses the common settled 2.5–5.5s window. Its stereo AC RMS is about
10,736 signed-16 units in both SGM clips; graphics baselines have zero AC RMS.
The [independent verdict](video-library-kit2-2026-10-02/capture-verdict.json)
and [analysis](video-library-kit2-2026-10-02/capture-analysis.json) retain
thresholds, capture hashes and acquisition observations.

Capture color/range conversion and audio filtering limit these measurements;
USB timestamps do not directly measure FPGA HS/VS. The observations establish
the intended visible filter and retained tones, rather than bit-exact FPGA
RGB/PCM or general game compatibility.

## Restoration and handoff

The temporary binaries used bind mounts and the existing supervisors with
SIGTERM at idle/free. All final Stops and asynchronous lease releases completed
before service restoration. No reboot was requested. Original live and disk
hashes match: agent `2de33b804ccd03c7bbb695cf3fc460d7277e31bded24f2cacb9c59b965d89699`,
runtime `a6c24668d3e98ab067f0835371733ab895905919ba630e60c409cd3c678b50a2`.
Boot/image identities are unchanged, the original agent revision is restored,
the kit is ready/idle/free, own target staging/binds are removed and capture
handles and the isolated host are closed. Full clips/operator logs remain in
the ignored task `out/hardware/video-selection-kit2-20261002/`.

Shared FPGA/GP/status definitions are unchanged; FogCast catalog schema 17
adds separate exact-shell video mappings and household settings persistence.
All 11 [affected-software commands](video-library-kit2-2026-10-02/software-tests.json)
passed on `4314a052b`, including full host,
appliance and expansion race tests, UI tests, native runtime tests and parent
regressions. `make check` passed 16 generated consumers and 33 fixture copies.
One locked U-Boot test lacked its cached artifact. Separate required Chrome/CDP
integration cases exercise video preference saving, reload persistence and
visible missing-profile fallback through the production browser UI.
The next integration step is a selected factory video-shell/part build lane
and its own appliance-artifact acceptance.
