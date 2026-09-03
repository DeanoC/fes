# Native Mega Drive hardware baseline

Acceptance date: 2026-09-03

Result: PASS

Hardware-supported native systems: 1 (`megadrive`).

This result covers the defined one-player Mega Drive vertical slice on the
designated MiSTer Pi: native idle, public Sonic 2 launch, exact running
identity, visible 1280x720@60 gameplay captured at 1920x1080, D-pad movement,
jump, Stop to idle, immediate relaunch, and unchanged legacy rollback.

## Authorities and reproducible artifacts

- FogCast source: `483f794c285b38ca452d7fcc9ee6afb61302b782`
- `libmister-runtime` source: `443b603de991b56b5f4d0d11c5bc88a3f83fad13`
- Native-dev image SHA-256: `4d23a561906f5dcdc2b5fcd51a82eef8a9be1102f88c3c9487a1425cffeb599e`
- Legacy-prod image SHA-256: `f1ede775a78f6cec02216a21d0cf21656b3d85a00d1458e9f1ff381b5720e87b`
- Legacy-dev image SHA-256: `d7a9c02462891db4af0c0e3049c5331e6a2ef3d9a993acdf8cd991a482480ea2`
- Idle RBF: Distribution_MiSTer `f7bde4becb452ca28f604ad9802bbed5c6b58e01`
  `menu.rbf`, 2,452,588 bytes, SHA-256
  `821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934`
- Mega Drive RBF: MegaDrive_MiSTer
  `7365a137cfd8fa6f041e964d8b953159c0ec42d9`
  `releases/MegaDrive_20260603.rbf`, 4,296,864 bytes, SHA-256
  `0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839`

Native, legacy-prod, and legacy-dev images were each built twice from fresh
independent work directories. Each pair was byte-identical. Structural,
library-closure, input, immutable-manifest, and QEMU packaging gates passed.
The exact reproducibly built native image—not a diagnostic derivative—was
deployed for acceptance.

Installed native identities were:

```text
/media/fat/linux/linux.img                 4d23a561906f5dcdc2b5fcd51a82eef8a9be1102f88c3c9487a1425cffeb599e
/usr/sbin/mister-runtime                   f100441df7b50df5aa3a7659ea41773ad36501dabafc678688fe444e120e9098
/usr/sbin/mister-agent                     53f6bbbf48c2402ae0d278f73343c3f36d78b1dae1f5849afa3ef07f89c2cd8f
/usr/share/mister-runtime/build-inputs     c8cd5ab2059dfadf87883d1ca2fd057736d8734d4fb7524ced2eb810ab9d20f4
/usr/share/mister-runtime/idle.rbf         821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934
/usr/share/mister-runtime/cores/megadrive.rbf
                                            0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
FogCast host executable                    b8cda43722e331a7e970357a018ffea290095b8e8dddb7e35033d480939b469c
```

The native boot ID was `5e715b7b-fee0-43f9-ad45-08f1b9c8e647`. Direct
predicates proved exactly one image-owned runtime and agent, no conventional
Main process, no `/dev/MiSTer_cmd`, FPGA manager `operating`, the fixed idle
and Mega Drive artifacts, and one persistent `FogCast Virtual Gamepad`.

## Physical launch, video, and control evidence

The accepted catalogue record was:

```text
game_id       = megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1
system        = megadrive
expected_core = MegaDrive
observed_core = MegaDrive
content_size  = 1048576
content_sha256= 193bc4064ce0daf27ea9e908ed246d87ec576cc294833badebb590b6ad8e8f6b
```

Each accepted launch used the public host API and the shipped 60-second
launch bound. Runtime state was `running_game`, execution was `game`, and
`last_error` was null. The ShadowCast 3 receiver with serial `KT044001`
captured collision-proof RGB24 PNGs at 1920x1080. These five pre-input
gameplay frames were opened individually:

```text
layer4-gameplay-001.png  b11eaa517e824d70b095ea84141f961002bfec4e95485d9d1f0e88c5d1f18eb0
layer4-gameplay-002.png  cf9f79a5fe61274e51f3505884b0282abab0ca74ca64f993882ffb973d3a6a3f
layer4-gameplay-003.png  bca149a90005ca8747d7892838ad6fba338e1e7073bc04ca96062bbd404dc364
layer4-gameplay-004.png  edbf6f3655fbd94ae423b5f23e68466ed7532366b37d434b5e926c45cf0ecd5b
layer4-gameplay-005.png  3b307f4d2e972585b00b21b6547de6b5f711d9b506c7f15f0e7524c3a575dece
```

All five show recognizable Sonic 2 Emerald Hill gameplay with no red/green
raster and no receiver no-signal output. A subsequent five-frame sequence was
captured while the production input path held D-pad right and pressed A. It
shows rightward displacement, ring collection, and Sonic airborne; its hashes
are `cf9790bd...f88c`, `eda38d19...3fab`, `715aa004...f88a`,
`08801dc1...4857`, and `241975ac...4cd4`. The input session sent 320 frames,
reported no sequence gaps or state resyncs, detached cleanly, and released
held state once.

The formal sequence and two consecutive stabilization passes produced six
uninterrupted launch/input/Stop cycles on the same native boot. Every cycle
passed its fresh title gate, one Start press, gameplay capture, direction and
jump, input detach, and Stop to visible native idle. Final target logs recorded
exactly six agent Launches, six runtime Launches, six agent Stops, six runtime
Stop transitions to idle, and six input attach/stream/detach sequences. No
receiver reset, ADV7513 reset, reboot, source change, or retry occurred during
acceptance.

## Legacy rollback and final state

After native acceptance, the exact freshly reproducible legacy-dev image was
deployed once. On new boot `5040bc74-b4e8-4f21-8f43-f953bf77ed7b`, the
unchanged smoke passed:

```text
target smoke passed: megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1 -> MegaDrive -> MENU
```

The final target has the exact installed legacy-dev image hash
`d7a9c02462891db4af0c0e3049c5331e6a2ef3d9a993acdf8cd991a482480ea2`,
one `/media/fat/MiSTer` process, `/dev/MiSTer_cmd` present, FPGA manager
`operating`, and `CORENAME=MENU`. The host is idle and capture is released.
Five final 1920x1080 frames were opened individually and show the stable
MiSTer MENU.

## Scope

Audio, saves, six-button input, multiplayer, remapping, hot-plug recovery,
development-RBF loading/video, every other system, running-game restart
preservation, conventional Main, transient MGLs, and automatic legacy fallback
remain unsupported by the native path.

The full uncommitted evidence set is retained outside Git at:

```text
/home/deano/fes/task-tmp/task9-native-megadrive-final.bwU5ti
```
