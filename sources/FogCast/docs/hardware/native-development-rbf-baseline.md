# Native development RBF hardware baseline

Acceptance date: 2026-09-04

Result: PASS

Hardware-tested: yes

Accepted consecutive cycles: 2

This baseline covers the existing MiSTer-compatible development ABI on the
designated MiSTer Pi. It proves one bounded raw upload through the public API,
native programming with HDMI intentionally down, honest development state,
Stop to visible idle, and a subsequent normal catalogue launch. It does not
promise useful video or input from arbitrary RBFs and does not extend support
to non-MiSTer or generalized RBF ABIs.

## Authorities and reproducible artifacts

- FogCast source: `1fc2ef075b3d80ede1a25592b2155ccc256a56af`
- `libmister-runtime` source: `4398f41bf504329e5c9b21f916cb37952bfb4cc7`
- Native-dev image SHA-256: `2bcfea7ef52dafa718edba4cfc15bb7ffbc2c9e83a6790be7e8d840dc008d730`
- Legacy-prod image SHA-256: `078ba05de5acf39a182d86912673bbec1ad73f50aa2e4191100eff025129ff56`
- Legacy-dev image SHA-256: `c6a025f55d79df2af9e9c6ab0503b212ebf9dd3ec1ab3360cb300a64a023c4a7`
- Upstream development fixture:
  `/home/deano/fes/misteross-rebuild/build/current/megadrive.rbf`, 4,306,912
  bytes, SHA-256
  `195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e`
- Sonic 2 content: 1,048,576 bytes, SHA-256
  `193bc4064ce0daf27ea9e908ed246d87ec576cc294833badebb590b6ad8e8f6b`

Native-dev, legacy-prod, and legacy-dev were each built twice in independent,
network-isolated build directories after the canonical source fetch. Each
pair was byte-identical. Independent hashes, structural verification, clean
filesystem checks, exact manifests, and QEMU smoke all passed. The exact
reproducible native image above—not a diagnostic derivative—was deployed once.

Installed native identities were:

```text
/media/fat/linux/linux.img                 2bcfea7ef52dafa718edba4cfc15bb7ffbc2c9e83a6790be7e8d840dc008d730
/usr/sbin/mister-runtime                   72dd916858b82590b7126238dd714565cbd6130ee09d79fc9accfb8cd9e4ea64
/usr/sbin/mister-agent                     75531dc7115a0ddc2fefc3234762f2c4da66573ede9cfa3bd008b57947a823e0
/usr/share/mister-runtime/build-inputs     2404e6770011718b7de13ab818b150f6117702c63fabe7fd71f8586630e2246c
/usr/share/mister-runtime/idle.rbf         821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934
/usr/share/mister-runtime/cores/megadrive.rbf
                                            0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
FogCast host executable                    5743c2d2dfea636dba54681caddb6a8f1e71ef85909b60d0865fa28a7aa7f058
```

Packaged development RBF: no

The native boot ID was `85b19a3a-c81b-4290-8d3a-322da4967176`.
Direct checks proved one image-owned runtime and agent, no conventional Main
or command FIFO, FPGA manager `operating`, exact installed hashes, one virtual
gamepad, an idle public session, and five individually inspected idle frames.
The ShadowCast 3 receiver was `/dev/video0`, serial `KT044001`; captures used
`ffmpeg -nostdin`, with no receiver or ADV7513 reset.

## Two-cycle physical result

Both cycles ran on that same native boot without a deploy, reboot, upload
replay, Launch replay, input replay, receiver reset, manual correction, or
source change. Each cycle proved:

1. one upload of the exact upstream fixture;
2. public `fpga_development` and runtime `running_development` with null game,
   system, and expected-core identity, optional observed core `MegaDrive`, and
   ADV7513 main output powered down;
3. one explicit development Stop to the exact visible native idle pattern;
4. one public launch of
   `megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1`;
5. a fresh visible Sonic 2 title, one Start press, five inspected Emerald Hill
   gameplay frames, then visible rightward movement and an airborne jump;
6. one input detach and one explicit game Stop; and
7. five inspected frames showing the restored native idle pattern.

The cycle-2 upload correctly issued one idempotent target Stop guard because
the host retained the prior native execution selection. The already-idle
runtime did not transition. Exact cumulative endpoints were:

```text
cycle 1 upload                    target Stop 0  runtime Stop 0
cycle 1 explicit development Stop target Stop 1  runtime Stop 1
cycle 1 explicit game Stop        target Stop 2  runtime Stop 2
cycle 2 replacement/upload guard  target Stop 3  runtime Stop 2
cycle 2 explicit development Stop target Stop 4  runtime Stop 3
cycle 2 explicit game Stop        target Stop 5  runtime Stop 4
```

Final logs recorded exactly two development uploads, two catalogue launches,
two explicit development Stops, two explicit game Stops, the one target-only
replacement guard, two runtime development loads, two runtime launches, four
runtime Stop transitions, and two input attach/stream/detach sequences.

Generic development video/input guarantee: no

The recognizable Mega Drive observation validates this fixture and the
lifecycle around it. The raw endpoint carries no profile, so it does not
guarantee video, audio, media, controls, saves, or settings for another RBF.

## Legacy rollback and final state

The exact reproducible legacy-dev image was deployed once after native
acceptance. On distinct boot `183c8bef-5edc-4534-9bc9-2991cb25d3fe`, the
unchanged smoke passed:

```text
target smoke passed: megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1 -> MegaDrive -> MENU
```

The installed image hash matched the value above. One conventional
`/media/fat/MiSTer` process, the legacy agent, `/dev/MiSTer_cmd` FIFO, FPGA
operation, and `CORENAME=MENU` were verified; native runtime processes were
absent. Five individually inspected frames showed the stable visible Menu.

Final target state: exact legacy-dev image, visible Menu, capture/input/helpers released

## Scope and evidence

Browser file picker: no

Generalized RBF ABI: no

The accepted capability is only the existing MiSTer-compatible development
load/Stop lifecycle. The fixture remains an external from-source test input,
not a packaged or production-pinned development artifact. Browser selection,
non-MiSTer standalone images, custom-core ABIs, and universal development
video/input behavior remain deferred.

Immutable acceptance evidence is retained at
`/home/deano/fes/task-tmp/native-development-rbf-acceptance.jW7uM4` with
manifest SHA-256
`003c9c4741d4be17e39f4318a42f4adb0f20aaa7304937e179d6866ec410c5cf`.
Earlier stopped harness iterations remain separate and are not counted as
accepted physical cycles; in particular, a prior anomalous duplicate target
Stop observation is preserved as non-contract evidence.
