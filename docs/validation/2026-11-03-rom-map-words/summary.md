# 2026-11-03: kit-hosted ST launch with the ROM map word decode

Kit re-measure of PR #1439, applied on top of #1431 (#863 slice 2). It is the
follow-up to the slice 2 re-measure, which missed the 60 s launch stop line at
63.6 s and 64.2 s; that record is `docs/validation/2026-11-03-kit-host-memory`
on #1431. Evidence class: hardware diagnostic on Kit A. It is not acceptance.

## Method

Same method, kit and package as the slice 2 runs r6 and r7. The one difference
is the tree:

- **Bundle.** Scratch bundle `26bb1565…` was built from #1431's head
  `e0d8364a3` plus #1439's two commits, as one local commit `485eb4f71` that was
  never pushed. `fogcast-api` was added to the payload and not committed.
  FogCast builds misteross's `expansion` from the tree, so the kit host and
  the agent both run the new decoder.
- **Install.** The bundle was installed and activated on Kit A's running image
  `f904961e…`, with no image change and no reboot.
- **Host.** `bin/fogcast-api -headless` ran by hand on the kit's loopback,
  with a private config and state on FESDATA3.
- **Launch.** Factory ST package `2dd2271c…` (17.4 MB ROM map), titled on the
  bundled EmuTOS default, then a diskless `POST /api/v1/session/launch` to the
  kit's own agent, and a stop after 30 s.
- **Sampling.** [`k863-sampler.sh`](k863-sampler.sh) took one sample per
  second.
- **Restore.** After each run Kit A went back to baseline software
  `89173748…`, verified, idle and lease-free.

## Results

| | #864 spike | r6 / r7 (#1431) | r8 / r9 (#1431 + #1439) | Stop line |
| --- | --- | --- | --- | --- |
| Diskless ST launch | 107 s | 63.6 / 64.2 s | **55.1 / 54.2 s** | under 60 s: **met** |
| Host prepare (POST until the spool appears) | | 16.9 / 17.4 s | 11.9 / 11.7 s | |
| Agent spool and staging | | 24.5 / 24.3 s | 20.0 / 19.4 s | |
| Core load and boot after publication | | 22.2 / 22.5 s | 23.2 / 23.1 s | |
| Host CPU during launch | | 13.9 / 13.4 CPU-s | 8.3 / 8.5 CPU-s | |
| Agent CPU during launch | | 39.3 / 38.6 CPU-s | 31.8 / 30.9 CPU-s | |
| MemAvailable minimum, launch | 141 MiB | 278 / 279 MiB | 276 / 273 MiB | at least 200 MiB: **met** |
| MemAvailable minimum, whole run | 141 MiB | 223 / 228 MiB | 225 / 248 MiB | at least 200 MiB: **met** |
| Agent RSS peak | 110 MiB | 78.5 / 63.3 MiB | 76.0 / 68.6 MiB | |
| Host RSS peak, launch | 138 MiB | 62 / 61 MiB | 74 / 82 MiB | |

[`r8-samples.tsv`](r8-samples.tsv) and [`r9-samples.tsv`](r9-samples.tsv) hold
the samples. [`r8-results.json`](r8-results.json) and
[`r9-results.json`](r9-results.json) hold the run records; the launch window in
those records is given as kit uptime.

## Findings

1. **Both stop lines are met.** The launch is 9.3 s faster (mean of r6 and r7
   against mean of r8 and r9). Most of the saving is in the two phases that
   decode the ROM map: the host's prepare is about 5.3 s shorter, and the
   agent's staging about 4.7 s.
2. **Host RSS during the launch is 12–21 MiB higher.** It peaks at 74–82 MiB,
   against 62 MiB before. The decode now allocates about a third as often, so
   the garbage collector likely runs less often within the shorter window. The
   host has no memory limit in this setup; slice 5 gives the kit-hosted
   process one. MemAvailable during the launch is within 6 MiB of r6 and r7.
3. **Core load and boot is unchanged at about 23 s,** and is now the largest
   phase.
4. **The remaining decode passes are unchanged.** The host still decodes the
   map twice and the agent once. The idle supervisor and agent CPU is #1438.
