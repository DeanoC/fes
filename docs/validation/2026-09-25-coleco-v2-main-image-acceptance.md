# Coleco v2 factory image and SGM acceptance

On 2026-09-25, the designated MiSTer Pi booted and confirmed a fresh
`native-integration-dev` release from FES main `3aa69308b52afcbf62c33503bd50ba383770f004`.
The factory-selected Coleco v2 shell launched and stopped through FogCast. An
independently routed Opcode SGM cart was then linked to that exact shell at
launch; an original BIOS-free cartridge reached its pass frame, produced both
programmed audio tones, stopped, and relaunched with the same composition.
This accepts these exact artifacts and this probe. It does not establish retail
SGM game compatibility or broader peripheral support.

## Source and release

- Clean FES source and all selected first-party components:
  `3aa69308b52afcbf62c33503bd50ba383770f004` (including factory v2
  selection from PR #218). The selected Coleco producer archive was reused
  from its verified source-closure cache at `07b2d0b3`; its payload and
  manifest matched the sealed v2 shell bytes exactly.
- `make check` and `make doctor` passed. Cold `make build` completed two
  independent rootfs passes. `make verify` passed reproducibility, structural
  checks, package validation, and QEMU packaging. The verified rootfs SHA-256
  was `4352d991a2bba5a2a09e4fb09f0a0225d7390896ae2aefb71feb3031ebc60e35`.
- `make release RELEASE_VERSION=coleco-v2-main-3aa69308-20260925` exported
  the sealed network release. The standard `fes-update` client acquired the
  kit lease, installed it, rebooted, and confirmed it. Boot ID changed from
  `4448dc24-d655-468e-a810-6a4a43ecf0b8` to
  `948d6338-1a12-4476-a435-d0ea7d4a6c7e`. The confirmed good image digest
  equals the release digest above; the previous image
  `5ec228ad94ef0e01ef82d35737ca516eb817c41be519ca2857ad67801bd8984a`
  remains named. No block device or bootstrap was written.
- The matching host and target agent both reported revision `3aa69308`.
  The package lifecycle smoke launched and stopped Pong, ZX81, and Coleco v2
  against their exact selected package IDs. ZX81 used its explicitly selected
  8 KiB machine ROM; Coleco used the BIOS-free probe cartridge. The smoke
  passed with `FOGCAST_CALL_TIMEOUT=90` because ZX81's first ROM composition
  exceeded the script's 30-second default. The first timeout still completed
  activation; it was observed and stopped before retry. This is a smoke-tool
  timeout limitation, not a failed FPGA launch.

## SGM route and composition

- Factory Coleco v2 package ID:
  `59cd2cd35c52227c49e5c69dc111a43bf3446856d8d03db3c553576aaf4da295`;
  shell RBF SHA-256
  `1886b9b1b6db14d8e96f5d53ea24991fe8e0b992b31a31578dabdace42383099`;
  BUILD_ID `059ec0389003099e7c74c2b77939fd7b`.
- SGM expansion ID:
  `3ff76ea041aca63fa86aa618c5e22074b6a937ce4cdd5877fafd4ac34d70ae4c`;
  archive SHA-256
  `f31694ef05e9f4a5df86be8c37b2c6b418976651076193cb4885939fc7129fe5`.
  The source-pinned `f7370550` HIP router used GPU 0, seed 3, and HeAP weight
  2000 for the frozen shell; the cart was routed independently at seed 3,
  weight 300. The cart reported 52.265720 MHz system, 85.770645 MHz pixel, and 213.720886 MHz
  audio against 52.224, 74.25, and 12.288 MHz targets. The system margin is
  narrow but positive. Its 38,384 non-ECC CRAM changes were all inside v2
  region `(1769,32,2806,1800)`, with zero outside changes.
- The Python build oracle and independent Go launch-time linker emitted the
  same 2,796,737-byte RBF, SHA-256
  `a294f347ff05db5ba89a73fd47f3895477798b8a1319f4b183003a1cf90b568b`.
  Composition ID was
  `18e2acb324acdc7ae0013624a0049a9a4bca7076eca947c553d89a649b6ed92c`.
  `go test ./...` in `sources/misteross/expansion` passed. The integrated
  SGM CPU/socket simulation reached the probe pass frame after 11,416,597
  cycles.

## Kit observations

The target was `73dc9f5f-1a12-4a95-a820-a9b4e600769a`. The normal FogCast
library entry selected the SGM archive and the original 1,261-byte probe ROM,
SHA-256 `33705344c6ae221a8ec3b9c862996b6136b9c236052c9c6517e9926eeb2a5317`.
Both launch responses reported `active`, the exact package/expansion/composition
identities above, and the same programmed payload digest. The first Stop
returned `idle` and freed the kit lease; relaunch and the second Stop did the
same. The final appliance state was confirmed good, raw idle, and lease free.

ShadowCast 3 captured the [pass frame](coleco-v2-main-2026-09-25/sgm-pass.png)
(PNG SHA-256 `d22f0537bb6385d27ffe10f360ac2b6736669cc32798628ceb2425663c0ccb66`).
The relaunch frame had the same pattern; capture noise accounted for the
different PNG bytes (90.6 dB PSNR). A five-second, 48 kHz stereo capture had
RMS 11,486 and strong peaks at 217 and 440 Hz, consistent with the programmed
SN and AY tones. The local WAV SHA-256 is
`f03932c3d60b056d0ee8b099f5595d2f301daf18ed586b340c43c822631c0be6`.
The temporary host process was stopped after the check; the image remains
installed on the kit.
