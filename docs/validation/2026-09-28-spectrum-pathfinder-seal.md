# FES ZX Spectrum pathfinder: simulation and sealed shell

On 2026-09-28 the `fes.spectrum` 0.1.0 pathfinder passed its host simulations
and sealed a four-socket shell with the HIP/nextpnr producer. This record is
**host simulation and sealed-build evidence** only. No probe card was sealed,
and no kit, HDMI, audio or FogCast session test ran. Nothing here is hardware
acceptance. The core stays package-only.

## Scope

- ABI `fes.computer` 1.0. Machine: Z80 at a 3.5 MHz average, 48 KiB RAM,
  port `$FE` (border, beeper, keyboard, EAR) and built-in Kempston on `$1F`.
- Late-bound firmware: one exact 16,384-byte `spectrum-firmware` ROM at
  `$0000–$3FFF`, linked into 16 blank M10K lanes before download. Only the
  open diagnostic image was used. No Sinclair ROM is in git.
- Unit 0 plays a 1..65,536-byte `.tap` (`fes.media.spectrum-tape` 1.0,
  capability bit 5). Version 0.1.0 does not capture MIC writes.
- Sockets 1–4 (`fes.spectrum-bus.sockets/1`, optional
  `fes.expansion.spectrum-bus` 1.0) reserve the same column-24 CRAM bands as
  the Apple II sockets. No card archive was sealed in this run.

## Simulation

`make -C sources/misteross sim-fes-spectrum` passed on `ccafc7e9`:

- `sim-fes-spectrum-machine`: diagnostic signature `$A5`, keyboard row A,
  Kempston, probe id and scratch, 2168 T-state pilot edges, red border and
  the origin pixel.
- `sim-fes-spectrum-board`: mailbox identity (capability word `0x002f`), tape
  limits 1..65536, a 3-byte commit, and HID usage `A` on the Spectrum A key.

## Sealed shell

- Sealed at FES `ccafc7e9cb4662751cdceb293891c92cc591b7af` from a clean tree
  with `toolchains/spectrum.lock` (Yosys `e2d425de`, Mistral `7ed06e21`,
  nextpnr `0259c6dc`). Package ID
  `aa9760d46279aadad862a78d6f6cfc7a5dd48b837dd2019b0aa3ca20807cd770`, build id
  `f27d64978666b0b15492c4dd7e6c8f31`, archive SHA-256
  `a4dea0676cfac4d6d7d3fd22ff0d1f8d7dbda8b4844b98ad7ed62e4e51355a05`.
- `core.rbf` 2,535,808 bytes, SHA-256
  `2b2cbc9d2af02aad1d4eb9ff1efd034299bf8bad6e1ecbd9c00b93a8d3c06b7c`; ROM map
  SHA-256 `8f8a7da36b0e6854cb988075cf97257422d937dd21a58c7c4ee194865686b3f0`.
- The CPU/video RAM is a 64 KiB `ramstyle=M10K` array (the top 16 KiB is
  unused). The tape image is 64 KiB of `m10k_tdp`. Synthesis kept 16
  `MISTRAL_M10K` firmware lanes and 128 `MISTRAL_M10K_TDP` blocks.
- Placer seeds 5 and 4 left one and two overused wires. First-pass seed 2,
  HeAP weight 2000, routed on the RX 7900 XTX HIP backend and met every
  clock: system **57.97** / pixel **110.52** / audio **172.89 MHz** against
  **52.224 / 74.25 / 12.288 MHz**. Routed utilization: 4,649/83,820
  combinational cells, 1,915 flip-flops, 144/553 M10K.
