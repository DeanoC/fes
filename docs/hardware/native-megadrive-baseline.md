# Native Mega Drive hardware baseline

Acceptance date: 2026-09-04

Result: PASS

Hardware-supported native systems: 1 (`megadrive`).

This result covers the defined one-player Mega Drive vertical slice on the
designated MiSTer Pi: native idle, public Sonic 2 launch, exact running
identity, visible 1280x720@60 gameplay captured at 1920x1080, D-pad movement,
jump, Stop to idle, immediate relaunch, and unchanged legacy rollback.

## Authorities and reproducible artifacts

- FogCast source: `cd85971bf0bffe36e69c381917f618620b901726`
- `libmister-runtime` source: `443b603de991b56b5f4d0d11c5bc88a3f83fad13`
- Native-dev image SHA-256: `95c9b4671e0d19781a6428d2168ab631453740215b194519f12788ade03c7c2e`
- Legacy-prod image SHA-256: `c18d156d8fc42e8eae2e7ea02ad1c32e44b1aff3963be23c44f52a74365c1b2e`
- Legacy-dev image SHA-256: `91f9870a1d2988ffec3e9cda22dea7f729633bbfc26bdd6c35f5776b4abff055`
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
/media/fat/linux/linux.img                 95c9b4671e0d19781a6428d2168ab631453740215b194519f12788ade03c7c2e
/usr/sbin/mister-runtime                   f100441df7b50df5aa3a7659ea41773ad36501dabafc678688fe444e120e9098
/usr/sbin/mister-agent                     d5cfab61a45e79205a5aab922bbed46b4905968a665f8432f1121a426dcb60c1
/usr/share/mister-runtime/build-inputs     f0459411042f6be0de7d7048dd73c75954363d27c1369061388d6ce2a340e318
/usr/share/mister-runtime/idle.rbf         821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934
/usr/share/mister-runtime/cores/megadrive.rbf
                                            0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
FogCast host executable                    0c27b78d9117ae013fb8f483d2939843d06673c8044f7325b464f359907c8ee6
```

The native boot ID was `cae04297-cbf1-44b1-8eb0-7f06ca603d77`. Direct
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
layer4-gameplay-001.png  6ba1606c401234376adededb80a1bfc586185ad4ba2919abde2ef9ff1d107fc8
layer4-gameplay-002.png  e6c650e83bf871b6dc343700ccc07a085120621b2184aaae6f2f0f2906f50761
layer4-gameplay-003.png  691e0c3fc347c0ad8a8fa2796519212921bbcbe4a18d014f96c9363e9da0b027
layer4-gameplay-004.png  0264c5af8a7b73caedb746f393929eaf9307b4551237a5b909e5afff56f7e866
layer4-gameplay-005.png  74f1d2743cff98b7960e8bf985a9fb022b5bbb05317c9f1f690e7771f0bdebd6
```

All five show recognizable Sonic 2 Emerald Hill gameplay with no red/green
raster and no receiver no-signal output. A subsequent five-frame sequence was
captured while the production input path held D-pad right and pressed A. It
shows rightward displacement, ring collection, and Sonic airborne; its hashes
are `699ad3f8...b755`, `61b38b4a...cc06`, `33351613...88ba`,
`cabb9238...d8f9`, and `fd86e9c6...cf88`. The input session sent 406 frames,
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
deployed once. On new boot `d5dcb3d5-268b-4b42-a56c-d8d458e35711`, the
unchanged smoke passed:

```text
target smoke passed: megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1 -> MegaDrive -> MENU
```

The final target has the exact installed legacy-dev image hash
`91f9870a1d2988ffec3e9cda22dea7f729633bbfc26bdd6c35f5776b4abff055`,
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
