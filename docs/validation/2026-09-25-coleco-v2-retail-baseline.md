# Coleco v2 retail-cartridge baseline

On 2026-09-25, the designated kit's already installed FES `3aa69308` image
ran two privately held retail ColecoVision cartridges on the factory Coleco v2
shell. This is a bounded compatibility diagnostic. The separate
[factory-image acceptance](2026-09-25-coleco-v2-main-image-acceptance.md)
qualifies the original SGM probe on that exact image; neither record establishes
general retail or SGM game compatibility.

## Selected matrix

| Cartridge | Reason for selection | Result on the factory v2 shell | Limit |
| --- | --- | --- | --- |
| Parker Brothers Frogger, 12,288 bytes | BIOS-dependent, sub-16 KiB standard cartridge; earlier [older-package gameplay evidence](2026-09-21-playable-audio.md) gives a comparison | Launched with the private BIOS and `blob` media, displayed a recognizable playfield. In a second launch with remote input enabled, keypad Start changed the display to the playing HUD, two Up events returned HTTP 200, and Stop returned idle. | The frame changes and accepted events do not prove sustained gameplay, collision behavior, physical controller input or audio fidelity. This is **not** Opcode's later SGM Frogger. |
| Coleco Donkey Kong, 16,384 bytes | Standard pack-in cartridge at the upper end of the mirrored `blob` path | Launched with the same BIOS and factory package; HDMI showed the ColecoVision/Donkey Kong title screen. Stop returned idle. | An input-enabled retry returned HTTP 500 and the temporary host exited with `session cleanup failed`. The cause was not isolated; gameplay remains untested. |
| Opcode Time Pilot, advertised 1 Mbit (128 KiB) | A [vendor-identified SGM-required cartridge](https://opcodegames.com/shop/colecovision/arcade-series/time-pilot/) to test the real expansion/game boundary | **Not launched.** No private dump was available. Its advertised cartridge capacity exceeds the package's 32 KiB `fes.media.blob-stream` maximum. | This is an admission/capacity gap inferred from the vendor specification and the package contract, not a measured FPGA/game failure. Full-cartridge mapping and any banking behavior need investigation before a game test. |

This three-title set separates a small BIOS-dependent cartridge, the standard
16 KiB boundary, and a real SGM title that current media admission cannot
represent as one unmodified cartridge image. The [core status](../core-status.md)
defines the current 16 KiB `blob` and 32 KiB `blob-stream` limits.

## Exact diagnostic context

- The selected factory package was
  `59cd2cd35c52227c49e5c69dc111a43bf3446856d8d03db3c553576aaf4da295`,
  BUILD_ID `059ec0389003099e7c74c2b77939fd7b`. Host and target agent both
  reported FES `3aa69308b52afcbf62c33503bd50ba383770f004`. The kit target
  ID was `73dc9f5f-1a12-4a95-a820-a9b4e600769a`.
- The private household BIOS was 8,192 bytes, SHA-256
  `990bf1956f10207d8781b619eb74f89b00d921c8d45c95c334c16c8cceca09ad`.
  Frogger was 12,288 bytes, SHA-256
  `c6ab01add1496516487dc9541301124a6f6418d3fc7be6a62bf7a256824a8f4d`.
  Donkey Kong was already in the host library as a 16,384-byte immutable media
  object `93f6ed7bd0d1a0ac751fbe09ce7011881939bc2f4c82d3bdfef7c617367f1ae4`;
  this run checked its library metadata but did not independently hash its
  original file.
- New, separate compatibility entries selected the exact factory package,
  `blob` media and required firmware. The existing entries were not changed.
  The first temporary host used the normal private configuration, which has
  remote input disabled. It established both boots and HDMI frames but could
  not accept input: `GET /api/v1/session/input` returned `INPUT_UNAVAILABLE`.
  A second temporary host used a private copy with remote input enabled;
  Frogger launched with input attached and accepted Start and Up events.
- ShadowCast 3 YUYV capture at 1280×720 showed Frogger's playfield (local PNG
  SHA-256 `3dc40a908bb4955c607a47f7dd721ed6d188fb3344932cd3a204bd43a9cccbda`),
  its playing HUD after Start (`0abf1f64f4476177afbc5009388f25ab6a6442a9611bf0164df2dc93447f403a`),
  and Donkey Kong's title (`cec01a16c18087a0d4299975389c43ef362b479c4b0e00771a146da7f8467632`).
  Captures and API JSON remain under the ignored
  `out/validation/coleco-compat-baseline/` directory of the test worktree.
  No proprietary ROM, BIOS or game screenshot is committed.

The Donkey Kong retry's HTTP 500 response body was not retained. After that
failure, the temporary host exited with `session cleanup failed`; the target
lease moved through `revoking` to `free` without takeover. No additional
launch was attempted. The host is stopped and the throwaway private config
was removed. The test did not re-check the target's raw idle state after the
host failure, so the final hardware claim is limited to the free lease.

## Next discriminating checks

1. Reproduce Donkey Kong's input-enabled launch in an isolated host with the
   full HTTP error body and target cleanup status retained. Stop after one
   failure; distinguish host/session cleanup from core execution before
   changing RTL.
2. Obtain a legitimately held SGM cartridge image and record its size, digest,
   layout and banking requirements privately. Define the cartridge address
   model and media admission required for a title such as Time Pilot before
   attempting kit execution. The existing SGM RAM/AY probe proves the module
   path, not that cartridge path.
3. For each candidate that boots, capture a stable title frame, Start response,
   a controlled input-visible change, audio, Stop and relaunch against one
   frozen package/image identity. Record failures at each boundary separately.
