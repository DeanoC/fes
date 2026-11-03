# 2026-11-03: kit-hosted ST launch after #863 slice 2 (memory)

Kit re-measure of PR #1431 against the #864 spike and the #863 stop lines:
diskless ST launch under 60 s, and MemAvailable never under 200 MiB. Evidence
class: hardware diagnostic on Kit A. It is not acceptance.

## Method

The #864 method, repeated:

- **Bundle.** Scratch bundle `334fb6bb…` was built from PR head `754dbe0f1`,
  with `fogcast-api` added to the payload. That change was not committed;
  slice 5 ships `fogcast-api` as `fogcast-kit host`.
- **Install.** The bundle was installed and activated on the running image
  `f904961e…`, with no image change and no reboot.
- **Host.** `bin/fogcast-api -headless` ran by hand on `127.0.0.1:8797`, with
  a private config and state in `/run/fogcast/fesdata3/k863`. The config had
  one target, the kit's own agent on `127.0.0.1:8182`, and set `bundled_dir`
  to the release's `share/fogcast/firmware`. The shared Powerboat host was not
  used.
- **Launch.** The `fogcast` CLI on Powerboat reached the host through an SSH
  tunnel. It installed factory ST package `2dd2271c…` (21.2 MB; `rom-map.json`
  is 17.4 MB) and created a title on the bundled EmuTOS default (`etos192us`).
  Then it sent `POST /api/v1/session/launch` naming the target, and stopped
  after 30 s.
- **Sampling.** [`k863-sampler.sh`](k863-sampler.sh) wrote one sample per
  second: MemAvailable, `/tmp` use, the RSS of the host, tenfoot, agent and
  runtime, CPU ticks of the host and agent, system CPU, and mmcblk0 sectors
  written.
- **Restore.** After each run Kit A went back to its baseline software
  `89173748…`, verified, idle and lease-free.

The runs were on Kit A; the spike ran on Kit B. The two kits are
interchangeable test kits on the same image.

The launch time is the `POST /session/launch` round trip, the same boundary
as the spike's 107 s.

The first launch attempts (r1–r5) were refused before dispatch. The driver
created the title with the CLI's `firmware` flag, which is for packages with a
`fes.firmware.blob` slot. The ST takes EmuTOS through its ROM map instead.

r4 and r5 ran a debug build of the same `fogcast-api`, copied into the claim
directory, to log the masked cause; r4 never got as far as launching. Only r6
and r7 are measurements. Both used the bundle's own binary.

## Results

| | #864 spike | r6 | r7 | Stop line |
| --- | --- | --- | --- | --- |
| Diskless ST launch | 107 s | 63.6 s | 64.2 s | under 60 s: **missed** |
| MemAvailable minimum, launch | 141 MiB (whole run) | 277.8 MiB | 279.3 MiB | at least 200 MiB: **met** |
| MemAvailable minimum, whole run | 141 MiB | 223.1 MiB | 228.1 MiB | at least 200 MiB: **met** |
| Agent RSS peak | 110 MiB | 78.5 MiB | 63.3 MiB | |
| Host RSS peak, launch | 138 MiB (whole run) | 62.2 MiB | 60.9 MiB | |
| Host RSS peak, install and title | 138 MiB (whole run) | 132.4 MiB | 131.8 MiB | |
| `/tmp` peak | | 44.2 MiB | 44.2 MiB | |
| ST package install | 19.0 s | 22.8 s | 24.3 s | |
| Stop | 2.6 s | 4.1 s | 4.4 s | |

[`r6-samples.tsv`](r6-samples.tsv) and [`r7-samples.tsv`](r7-samples.tsv) hold
the samples. [`r6-results.json`](r6-results.json) and
[`r7-results.json`](r7-results.json) hold the run records; the launch window
in those records is given as kit uptime.

### Where the launch time goes

| Phase (seconds from the POST) | r6 | r7 | Busy |
| --- | --- | --- | --- |
| Host prepares the ROM input; nothing on `/tmp` yet | 0–16.9 | 0–17.4 | host about 0.9 core |
| Agent spools the body and stages it | 16.9–41.4 | 17.4–41.7 | agent about 1.1 cores, 26.8 / 25.9 CPU-s |
| Core load and boot after publication | 41.4–63.6 | 41.7–64.2 | agent about 0.3 core |

The kit was 91–92% busy across both cores for the whole launch. The host spent
13.9 / 13.4 CPU-s and the agent 39.3 / 38.6 CPU-s.

### Idle CPU

Kit A sat at about 60% busy across both cores before the launch, with the host
and agent at about 0.4 of a core between them.

A separate 60 s read-only sample was taken on Kit A's baseline software at
idle, on the same day and image:

| Process | Share of one core |
| --- | --- |
| `fes-kit-software run` (the image's software supervisor) | 46% |
| `mister-agent` | 30% |
| `fogcast-tenfoot` | 7% |
| `mister-runtime` | 0% |
| **System** | **41% of 2 cores** |

The supervisor had averaged 41% of a core over 43.5 hours of uptime (64,400
CPU-s). This is image code and was not changed by #1431. During a launch it
competes with host preparation and agent staging for the kit's two cores. Tracked in
#1438.

## Findings

1. **Memory meets the stop line with margin.** During the launch MemAvailable
   stayed at 278 MiB or above, against 141 MiB in the spike.
2. **The agent's RSS peak is not limit thrash.**
   - The agent's 64 MiB `GOMEMLIMIT` is soft and counts only the Go heap.
   - The 20 MB launch body is a read-only mapping of the tmpfs spool, so it
     counts towards RSS but not towards the limit.
   - On amd64 the agent's staging heap is at most 52 MiB, the `GOGC=off`
     upper bound. 52 MiB plus the 20 MB mapping plus the runtime is the 78 MiB
     seen in r6.
   - r7 peaked at 63 MiB, and agent CPU is the same in both runs.
3. **The launch misses its stop line by about 4 s.** Launch time fell from
   107 s to 64 s, against 47.5 s for a Powerboat-hosted launch.
   - The two ROM-map passes are CPU-bound: the host's prepare, about 17 s, and
     the agent's staging, about 25 s.
   - The idle supervisor and agent take about 0.75 of a core from those passes.
4. **Title creation is now the memory high point.** It took 52–62 s on the
   kit, and the host peaked at 132 MiB while doing it. That path was not
   profiled and is not on the launch path. MemAvailable still stayed above
   223 MiB. It belongs with slices 3–4 (kit-hosted catalog).
