# Kit-hosted fogcast-api spike (#864)

**Evidence class:** hardware diagnostic on Kit B. It is not acceptance.

| Item | Value |
| --- | --- |
| Source | `spike/864-kit-fogcast-api` @ `9be98690` (main `d1228da9` plus `fogcast-api` in the kit bundle) |
| Bundle | `101c1070c79c0eca14aedf270621f04673723c54aba5246bc9eecc347a9cc00a`, 67.8 MB (16 Oct bundle: 50.7 MB) |
| Base image | `5af6162b…` (unchanged, no reboot) |
| Restored | baseline bundle `f48b11f1…`, generation 72; kit idle and lease-free |
| Kit | Kit B, 492 MiB visible to Linux; about 400 MiB available at idle Home |

## Method

The PR bundle was installed and activated on the running image. Its `bin/fogcast-api` ran by hand with `-headless` on `127.0.0.1:8797`, with a private config and state on FESDATA3. The config had one target, the kit's own agent at `127.0.0.1:8182`, plus a one-ROM SMS library. The `fogcast` CLI on Powerboat reached the kit host through an SSH tunnel. It installed the factory ST package `2dd2271c…`, imported EmuTOS `etos192us.img`, created a title, bound `atari-st-firmware`, launched it and stopped it. Tenfoot stayed on the shared Powerboat host and was not pointed at the kit host. A 1 s sampler recorded [samples.tsv](2026-10-18-kit-hosted-fogcast-api/samples.tsv): MemAvailable, RSS of the kit host, tenfoot, agent and runtime, CPU, and mmcblk0 sectors written.

## Results

| Measure | Result |
| --- | --- |
| Start to first `GET /api/v1/games` 200 | 3.66 s |
| Kit host RSS at idle | 19.6–21 MiB, 7 threads |
| Kit host RSS peak (21 MB package install, then ST launch) | 138 MiB; agent peak 110 MiB at the same time |
| Lowest kit MemAvailable | 141 MiB (during launch); 394 MiB after Stop |
| Kit host CPU at idle | about 2% of one core (135 ticks in 60 s) |
| Kit host idle writes | about 32 KiB/min `write_bytes` (about 46 MB/day) |
| ST package install over the tunnel | 19.0 s |
| Diskless ST launch (`POST /session/launch`) | 107 s; Powerboat-hosted record: 47.5 s ([startup budget](2026-10-06-atari-st-startup-budget.md)) |
| Stop | 2.6 s; kit idle and lease-free |

HDMI showed the EmuTOS GEM desktop ([frame](2026-10-18-kit-hosted-fogcast-api/st-gem-via-kit-host.png)). Every launch, link and Stop went through the kit's own host and agent; no other computer was involved.

## Findings

1. The kit host is small at idle and starts quickly. Memory peaks are transient but large: both the kit host and the agent hold package-sized buffers during install and link. Streaming them would protect the kit's 492 MiB.
2. Launch took about twice as long as on Powerboat for the heaviest core. It was not profiled; digest and staging copies on ARM over SD are the likely cost.
3. The kit host has its own core-package store. It does not see the packages already installed on the kit (`/usr/share/mister-runtime/core-packages`, FESDATA3 core library), so a kit host needs those as its package source rather than a second copy.
4. Raw SMS ROMs are never launchable on a host. They play only as a core-package title with a bound ROM.
5. On this image the kit-local launcher can play only Pong and RAM test. SMS and SG-1000 need a cartridge, ZX81/Spectrum/ST need firmware, and Coleco needs a video part. The baseline Home showed the ST as "Needs its system firmware" after the kit host had just run it.
6. Not measured: tenfoot using the kit host, a full kit cold boot, and ZX81/Pong sessions.
