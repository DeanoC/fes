# Atari ST frozen video boundary: shell rebuild and Kit A diagnostic

The shell change fixes the failure in
[nextpnr #165](https://github.com/DeanoC/nextpnr/issues/165): the unused request
FF output was vacant, but frozen shell routes consumed its exits inside the
video CRAM fence. The rebuilt shell reserves permanent identity-LUT loads for
all 32 request bits inside that same fence before routes are frozen.

The exact sealed Direct and Scanlines parts both passed native loading and
HDMI observation on Kit A on 2026-10-07. This qualifies these named artifacts
in a developer diagnostic; it does not qualify an assembled current appliance
image or audio, input and floppy behavior.

## Build and containment

The FPGA source selection is FES `0a80d4d2a3f4ad183a630c766453145a13739eda`,
based on `4ab1d84b4967394d6f4edf4580e2f7dcb29cf096`.
`make core-dev prepare --core fes.atari-st --output out/dev/st-egress-002`
built the shell and both sealed parts through the authenticated HIP lane.
Yosys is `886afa63953e97407153e9f4aae25fcedb639696`, nextpnr is
`3d4a5b352b4edb478b744b82cc61333353751a80`, and Mistral is
`7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`. The separate nextpnr preflight
change is diagnostic; these successful FPGA artifacts use the original pins.

| Artifact | Identity |
| --- | --- |
| Base package | `2edf5b3ea70a354147ce8b86941a4fcfc9f82aef5ba94033e6a24c3f11967128` |
| BUILD_ID | `d46dfe8d1df7329e8ab6084d80556dae` |
| Shell RBF SHA256 | `3b94077c7c7b82e44931844788dfde1604b35e2220eee4ad28365572b66518ff` |
| Direct part | `75e153be59308b643ae51987ec41e2e856aa3e8f92af20b76cf34198cf666acc` |
| Scanlines part | `f7f0239a4fb78ae200776ec370ae7653b1d2b1365c1e64d92d97b7b61a64fb21` |

Shell seed 1 achieved 74.839096 MHz pixel, 56.497173 MHz system and
222.321030 MHz audio, against 74.250069, 52.224773 and 12.288032 MHz
requirements. Both seed-4 part routes passed timing and preserve all 93
boundary FF/clock anchors, the socket map `fes.atari-st-video.socket/1`, and
half-open CRAM rectangle `(1769,3442,2806,5162)`. Direct changes 2,186 bits
inside the rectangle and zero outside; Scanlines changes 3,489 inside and
zero outside. The physical producer layout becomes `fes.atari-st-video.parts/2`;
the existing Go/C++ composition contract remains `fes.atari-st-video.parts/1`.

## Native hardware test

Kit A's installed image `5032c2d2282da26e79e6f7efc47f9f2a1aa9abca4aad4ca6f19817e82d18ce4a`
and runtime/agent revision `5a58053229b5e1772defe5f9bdc34c72f659db71` predate
ST support. Initial raw development loads passed, but they did not run normal
computer startup; their black HDMI captures were inconclusive.

For the native test, ST-aware runtime and agent binaries from FES
`4ab1d84b4967394d6f4edf4580e2f7dcb29cf096` were temporarily bind-mounted
at idle/free using the existing service supervisors. Their SHA256 hashes are
`91e4f34b2bf91dff6caf68c00b8065eb5ef5748321577b2984f5445f2979ad5c`
(runtime, static ARM with whole-archive pthread) and
`b847aabcd54e2dbf3bf6156967dd2a14de0b5c95eeb7aa4ecf2cedbfd3455c96`
(agent). Recorded build receipts and observed executable hashes bind those
binaries; the health artifact record still describes the installed image.
An isolated host built from `0a80d4d2a` imported the original shell and sealed
parts and linked the 196,608-byte EmuTOS 1.4 firmware through the normal API.
ROM SHA256 is `8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc`.
Required interfaces were retained and the package reported compatible.

Both launches reached active through `fpga_native`, with the exact part,
composition, linked ROM and observed BUILD_ID verified at launch, 12 seconds
and 32 seconds. Direct composition is
`2958c3c498a7796defdd6eaa43d0b8f7831bee6bff152095ec8d81157f167d76`;
Scanlines composition is
`f9a8f12b59bc97bec0672471594f35fd12c3673bf3db786d0974584e86bff586`.
Both Stops returned idle without recovery or reboot.

[Direct capture](atari-st-video-egress-2026-10-07/direct.png) shows a clear
EmuTOS desktop, text and icons.
[Scanlines capture](atari-st-video-egress-2026-10-07/scanlines.png) shows the
same desktop with horizontal dark rows. ShadowCast 3 captured 1920×1080
MJPEG at requested 30 fps. The blank header's row means are uniformly 234
for Direct and range from 123 to 200 for Scanlines. Capture scales the native
720p output, so these observations establish modulation and readable video,
not bit-exact RGB or exact half-brightness. The
[video receipt](atari-st-video-egress-2026-10-07/video.json) retains composition
and capture hashes. Full session and restoration receipts remain in the task
worktree's ignored `out/dev/st-egress-hardware/` directory.

## Restoration

The temporary host container, private bearer configuration, executable bind
mounts and target binaries were removed. Original live/disk executable hashes
were restored: runtime
`69e5c6476620db8cba638d2b93675d99d34981eaa30e9027482c7ef061f7f5cc`,
agent `dd91591c8eb6a9728bb1036cbeaba4025e52de5c747402c2c980bdb046986754`.
Kit A is ready/idle with a free lease. Boot
`841fda80-26fd-4153-98f7-45cf902f0736` and installed image are unchanged.
The normal host service and operator library were undisturbed; Kit B was not
programmed. The next integration check is selection and testing in a current
FES appliance image after review and merge.
