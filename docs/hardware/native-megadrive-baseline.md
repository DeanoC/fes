# Native Mega Drive hardware baseline

Acceptance date: 2026-09-04

Result: PASS

Hardware-supported native systems: 1 (`megadrive`).

This result covers the defined one-player Mega Drive vertical slice on the
designated MiSTer Pi: native idle, public Sonic 2 launch, exact running
identity, visible 1280x720@60 gameplay captured at 1920x1080, D-pad movement,
jump, Stop to idle, immediate relaunch, and unchanged legacy rollback.

## Authorities and reproducible artifacts

- FogCast source: `889c777f0b7fe6fc662a9d8c7d57834c986239a7`
- `libmister-runtime` source: `443b603de991b56b5f4d0d11c5bc88a3f83fad13`
- Native-dev image SHA-256: `7591ee6a943fabd3e40134f662bb76e242bffa201716ee0d4201a7859d31e480`
- Legacy-prod image SHA-256: `260e36b8eeeaa87238b9e6c2f4c7975f86f45f131fe1e1f8c1f649453d7ac780`
- Legacy-dev image SHA-256: `54210721b1419b80d4a15ca415e0a9068a081c1a01b4675dc64eed8f868e7a79`
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
/media/fat/linux/linux.img                 7591ee6a943fabd3e40134f662bb76e242bffa201716ee0d4201a7859d31e480
/usr/sbin/mister-runtime                   f100441df7b50df5aa3a7659ea41773ad36501dabafc678688fe444e120e9098
/usr/sbin/mister-agent                     5746965d6f4d88461b5795abb3a8eb2e488ebdc0ff3b2cf2697a74b8fcf4f3d2
/usr/share/mister-runtime/build-inputs     079d91b0ff9befaedd9bb46a5e01e23b5f133a76fbc8647637c430603fa9398d
/usr/share/mister-runtime/idle.rbf         821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934
/usr/share/mister-runtime/cores/megadrive.rbf
                                            0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
FogCast host executable                    93a041191209a75d34b6af2f5c8b753742323e731edcf3738d7a0bcdef9c34bf
```

The native boot ID was `71760e31-a96f-487b-a3f4-b7ef915794c2`. Direct
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
layer4-gameplay-001.png  3c885e4e3cf08909e86a7563743e7f355bf4dadd9674d762f595761e23c6d8b3
layer4-gameplay-002.png  508b44821ee4a9c29b8cc3e9a7b33738b3455efae51e8f9f4e1d42a77f8da911
layer4-gameplay-003.png  508e99be8a2ca8b8533a63d271a70bfe08309c2ffc41058f6b8edc7ffc77600c
layer4-gameplay-004.png  fb2d265be207185160999420281e1c2135660c5fc8e2e8ba06236ae7d334ff87
layer4-gameplay-005.png  d3b513da30ec72c4c44b34800012f07b84523ed6ac61cbbf842e19fe4c30279b
```

All five show recognizable Sonic 2 Emerald Hill gameplay with no red/green
raster and no receiver no-signal output. A subsequent five-frame sequence was
captured while the production input path held D-pad right and pressed A. It
shows rightward displacement, ring collection, and Sonic airborne; its hashes
are `f7eec6c8...10e7`, `d6f1e78e...d0f2`, `b8afb58b...a373`,
`16308b38...761e`, and `fd2df219...8daa`. The input session sent 406 frames,
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
deployed once. On new boot `e006a65e-0890-4ba8-987a-d42d3bd8f5c6`, the
unchanged smoke passed:

```text
target smoke passed: megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1 -> MegaDrive -> MENU
```

The final target has the exact installed legacy-dev image hash
`54210721b1419b80d4a15ca415e0a9068a081c1a01b4675dc64eed8f868e7a79`,
one `/media/fat/MiSTer` process, `/dev/MiSTer_cmd` present, FPGA manager
`operating`, and `CORENAME=MENU`. The host is idle and capture is released.
Five final 1920x1080 frames were opened individually and show the stable
MiSTer MENU.

## Scope

Audio, saves, six-button input, multiplayer, remapping, hot-plug recovery,
development-RBF loading/video, every other system, running-game restart
preservation, conventional Main, transient MGLs, and automatic legacy fallback
remain unsupported by the native path.

The accepted identities and SHA-256 values above are the durable evidence
record. Raw controller transcripts and captures were retained outside Git for
the acceptance run and are not required to reproduce the locked images.
