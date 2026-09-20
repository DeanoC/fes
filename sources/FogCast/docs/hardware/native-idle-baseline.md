# Native idle hardware baseline

Acceptance date: 2026-09-02

Result: PASS for the idle-only native baseline and legacy rollback on the
designated Pi. Native game and development-RBF support remain intentionally
unsupported.

## Authorities and reproducible artifacts

- FogCast source: `493c94538541ea3acfbb84fb12645a8c3b081712`
- `libmister-runtime` source: `c71733238bba066705e3d08d64876c4ad6b218ff`
- Locked runtime commit: `c71733238bba066705e3d08d64876c4ad6b218ff`
- Idle source: Distribution_MiSTer commit
  `f7bde4becb452ca28f604ad9802bbed5c6b58e01`, `menu.rbf`
- Idle artifact: 2,452,588 bytes,
  SHA-256 `821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934`
- Prod image SHA-256: `4f8c3b1164bebc9e60b31563c6ed009892dd2e52f0ed4452ba869f45efce3d0f`
- Dev image SHA-256: `535d264254e861bd2a60819abd450d0ec30edb63400d2d10f8b9f2de7efe216b`
- Native-dev image SHA-256:
  `b65efb8fc583a40cfa85c0d249872a02b2afcbe2fff8effe5988460481ab4074`

Each image completed two independent reproducibility passes with
`source_date_epoch=1751459412`; run 1 and run 2 matched for every variant.
The native reproducibility record SHA-256 is
`8fc6a8e836b5b0a5068cdefbdf565ac666e89677d857d4a5e1eb330f21fd3f70`.
The native manifest (514 entries) SHA-256 is
`14aa6f8704614c7300c7790d950474fe2562627c6eb19b91264d66e0bdfbddfe`; the
14-entry library report SHA-256 is
`85b3ddf6aebd25a58a8ea736d081e028fd7bdab35a4567be67f01a07848dd926`.

Manifest identities are:

```text
/usr/sbin/mister-agent                 76f68a9e4cabb0e7208abde74296cd860d0ffa385bc82649ff1cff410a6c6b8c
/usr/sbin/mister-runtime               e0921c79e48341dee456070703d2d55009c07fe3891b238007088077cd31a6e5
/usr/share/mister-runtime/build-inputs 657906d23526b28a50289726fa2c586a969e2dd87a1a0040b59102f975ec48eb
/usr/share/mister-runtime/idle.rbf      821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934
```

## Native lifecycle and direct predicates

The fresh native image was deployed once. The unchanged lifecycle smoke passed:

```text
native runtime smoke passed: boot 6f2656f4-885c-42b2-a2dd-36369295d4b8
  -> 8b0737e2-aad2-4821-ad4f-9dcbc7e7c32e, idle -> idle
```

It proved the single native runtime and agent, no conventional Main or FIFO,
exact build-input bytes, idle Stop, explicit reboot, changed boot ID, and fresh
ready/idle. Direct post-smoke diagnostics recorded FPGA manager `operating`,
core `MENU`, and the native runtime video record:

```text
recipe=menu_720p60 bus=/dev/i2c-1 address=0x39
power_before=0x10 power_after=0x10 link_status=0xf8
```

The native runtime log reached `core_sync`, `core_probe`, `hdmi_init`,
`video_timing`, `core_release`, `hdmi_wake`, `core_input`, `hdmi_verify`, and
`idle`, all with `error=none`. The direct diagnostics log SHA-256 is
`7d30593949d606f193b4784f68dc7d735ec0d86274e844b77174771f71bd679d`.

## HDMI capture

Capture directory (ignored, not committed):
`build/output/target-image/native-dev/evidence.sync.uW48Ll`.

The V4L2 report SHA-256 is
`942c19773ed5132829a6640e645c25104509e22082ecd82a043cdc88088256f0`.
The reviewed capture completed normally in 5.015834939 seconds with status 0,
`frame=5`, `progress=end`, and exactly five numbered files. Every file is a
valid RGB24 PNG at 1920x1080 and was opened individually. The images show the
black/white analogue-style MiSTer boot static raster, matching the known Main
boot reference; there is no red/green diagnostic pattern and no ShadowCast
`No HDMI Signal Detected` screen. The static raster is valid boot video, not a
corrupted raster or a no-signal condition.

The capture files are:

```text
idle-001.png  0ca446b11865ab6c8d90a2584cd0dffc7c275fbd4680c7920670b666683799c5
idle-002.png  eb49f6b82f59447c1617f3098ba4acb54c9aa42900d7441d63588f106bbacfed
idle-003.png  d0a87980b05640e60200ae858acb000a2a316bc882ff8ae4c00adcf82a7831d1
idle-004.png  54ff9c9376a89bb74aa3e68523c057033b2c3872fba0d74c03b9da3067eb5ccf
idle-005.png  e19101530942160b6964ebb30a2dda3e9fc343c5ecec1805f4fbd1f6ca698df1
```

The capture manifest SHA-256 is
`1056a95e457a829759ef5fd9faff50fb982be49e407f015c36d8ca91d4c1cfa6`.
FFmpeg emitted the retained MJPEG decoder diagnostics in `ffmpeg.stderr`,
but all five output PNGs decoded independently and showed the same valid
boot-static signal; no no-signal OSD appeared.

## Legacy rollback and final state

The freshly rebuilt legacy dev image (`535d264254e861bd2a60819abd450d0ec30edb63400d2d10f8b9f2de7efe216b`) was deployed after native testing. The live catalogue selection was the launchable Mega Drive record:

```text
id              = megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1
canonical_title = Sonic The Hedgehog 2
title           = Sonic The Hedgehog 2 (World) (Rev A)
```

The existing smoke passed:

```text
target smoke passed: megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1 -> MegaDrive -> MENU
```

Final target health is ready/idle on boot `f6535ed7-ab73-4e7a-a172-537a3d21cd35`,
with one `/media/fat/MiSTer`, one FAT-side agent, the command FIFO present,
`CORENAME=MENU`, and FPGA manager `operating`. Host status is `{"state":"idle"}`.

## External evidence

Full logs, manifests, capture files, and hashes are retained outside Git under:

```text
/home/deano/fes/task-tmp/fogcast-sync-final.jAi8na
/home/deano/fes/FogCast-POC/build/output/target-image/native-dev/evidence.sync.uW48Ll
```

The final-state log SHA-256 is
`69c2108d52e23ba02a5686744707f04407c007e88874817c0d777db3a813d289`.
