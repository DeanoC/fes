# 2026-11-03: the kit host reads the kit's own packages (#863 slice 3)

Kit check of `[core_packages] kit = true`. Evidence class: hardware diagnostic
on Kit A. It is not acceptance.

## Method

Same kit, ST package and launch as the slice 2 runs r8 and r9
(`docs/validation/2026-11-03-rom-map-words`), with three differences:

- **Bundle.** Scratch bundle `48704279…`, built from this branch at
  `bcbc116a9` with [`build-863s3.sh`](build-863s3.sh). Its agent and runtime
  are main plus slice 3. The bundle was installed and activated on Kit A's
  running image `f904961e…`, with no image change and no reboot.
- **Host.** The branch's ARMv7 `fogcast-api`, copied to the kit and run by hand
  on its loopback with a private config and state on FESDATA3. The config sets
  `[core_packages] kit = true`, so the host reads the kit's packages instead of
  its own store.
- **No host upload.** The host received no package. It listed the kit's
  packages, titled the ST package on the bundled EmuTOS, launched diskless and
  stopped after 30 s.

[`k863s3-run.py`](k863s3-run.py) is the driver (the s4 version) and
[`k863-sampler.sh`](k863-sampler.sh) took one sample per second. After each run
Kit A went back to baseline software `b3444abf…`, verified, idle and lease-free.

## What Kit A holds

Image `f904961e` ships one factory package, the menu (`343e6200…`). Before
these runs its durable store held one package, Coleco (`cf41dc16…`). It held
no ST package. In s2 the ST package `2dd2271c…` was installed into the durable
store through the agent's own `POST /v1/core-packages`, the way a package
reaches a kit. It took 44.0 s and is still installed, because the store has no
delete.

## Runs

| Run | Host | Result |
| --- | --- | --- |
| s1 | `bcbc116a9` | Listed exactly the kit's packages (menu and Coleco) in 0.4 s. No ST package on the kit, so no launch. |
| s2 | `bcbc116a9` | After the agent install, the host listed the ST package with no upload. Title in 47.6 s, launch in 55.9 s. |
| s3 | `c8fb7eead` | Stopped at the first list, which returned 503. The background open was still verifying the new ST package, and the driver did not wait. |
| s4 | `99dfcdffc` | The open took 24.3 s, logged by the host; the list returned 503 until then and then answered in 4.5 s. Menu left out of the list. Title in 28.9 s, launch in 46.2 s. |

The durable store held the same two archives before and after each launch, so
the host wrote nothing to it.

## Launch

| | r8 / r9 (host's own store) | s2 | s4 | Stop line |
| --- | --- | --- | --- | --- |
| Diskless ST launch | 55.1 / 54.2 s | 55.9 s | **46.2 s** | under 60 s: **met** |
| Host CPU during launch | 8.3 / 8.5 CPU-s | 18.1 CPU-s | 8.5 CPU-s | |
| Agent CPU during launch | 31.8 / 30.9 CPU-s | 22.3 CPU-s | 21.1 CPU-s | |
| Host RSS peak, launch | 74 / 82 MiB | 116 MiB | 75 MiB | |
| Agent RSS peak, launch | 76.0 / 68.6 MiB | 59 MiB | 61 MiB | |
| MemAvailable minimum, launch | 276 / 273 MiB | 224 MiB | 264 MiB | at least 200 MiB: **met** |
| MemAvailable minimum, whole run | 225 / 248 MiB | 223 MiB | 243 MiB | at least 200 MiB: **met** |

[`s2-samples.tsv`](s2-samples.tsv) and [`s4-samples.tsv`](s4-samples.tsv) hold
the samples, and `s1`–`s4-results.json` the run records. The launch window in
those records is kit uptime.

## Findings

1. **The kit host serves the kit's packages without an upload.** It titles and
   launches a package that only the agent installed, and lists exactly what the
   kit holds, less the menu.
2. **s2's host cost came from re-verifying on every read.**
   `kitpackages.Store.Read` hashes the unpacked members and decodes the 17 MB
   ROM map each time. The host now reads through `Store.ReadCached`, which
   trusts the stat fingerprint as List does. s4 is back to the host's own-store
   CPU and memory.
3. **The s4 launch is 8–9 s faster than r8/r9.** That is a single run; the
   saving is not broken down by phase here.
4. **The durable store takes 24 s to open with the ST package installed.**
   Opening takes the store's lock and verifies every installed package. For
   that whole time the host answers 503 `CORE_PACKAGE_STORE_UNAVAILABLE`, and
   mister-agent cannot select or launch, because it waits on the same lock.
   The cost grows with the durable library. Once slice 5 starts the host at
   boot, it is a boot-time cost on the agent. It is open, not fixed here.
