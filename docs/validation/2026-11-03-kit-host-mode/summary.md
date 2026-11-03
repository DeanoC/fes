# 2026-11-03: fogcast-api kit mode on Kit A (#863 slice 4)

Kit check of `fogcast-api -kit`. Evidence class: hardware diagnostic on Kit A.
It is not acceptance.

## Method

- **Software.** Kit A stayed on baseline software `b3444abf…` and image
  `f904961e…`: no bundle install, no image change, no reboot. Slice 4 changes
  only the host, and slice 3's agent-side changes are not needed for kit mode.
- **Host.** The branch's ARMv7 `fogcast-api` at `b5a0be1af`, run by hand as
  `fogcast-api -kit -kit-state /run/fogcast/fesdata3/k863s4/fogcast-host
  -headless -listen 127.0.0.1:8797`, with the kit's real
  `/media/fat/fogcast/agent.toml` and the image's
  `/usr/share/fogcast/config.toml` as the template. The state root is a private
  directory under FESDATA3, not the default `fogcast-host`, so nothing is left
  for a later first start to find.
- **Steps.**
  1. Start the host and check the config it generated.
  2. List the kit's packages, then title the ST package (already in the durable
     store since the slice 3 check), launch it diskless and stop it.
  3. Measure the host's idle CPU over 60 s.
  4. Stop the host, append a `[[libraries]]` row to the generated config as a
     settings edit would, and start the host again.
- **Restore.** The driver, [`k863s4-run.py`](k863s4-run.py), checked the agent
  token without printing it, removed its directory and processes, and confirmed
  the baseline software, an idle lease-free kit and no leftover `fogcast-api`.
  [`k863-sampler.sh`](k863-sampler.sh) took one sample per second.

## Results

| Check | Result |
| --- | --- |
| Agent wait | Reached the agent in 54 ms (76 ms on restart) |
| Generated target | One row `kit`, `http://127.0.0.1:8182`, enabled, Kit A's own target ID `73dc9f5f…`, the agent's token |
| State | On FESDATA3 under the given root; the tmpfs fallback was not used |
| Managed keys | `[core_packages] kit = true`, `folder_watch_seconds = 0`, 300 s upload timeout, firmware `/usr/share/fogcast/firmware` |
| Libraries | `sms-main` from the template; after the restart also the added `k863s4-extra` |
| Durable store open | 23.7 s and 23.6 s; the package list answered 503 until then |
| Title of the ST package | 28.6 s |
| Diskless ST launch | **46.8 s** (stop line under 60 s: **met**) |
| Host CPU / RSS peak during launch | 8.8 CPU-s / 80 MiB |
| Agent CPU during launch | 21.5 CPU-s |
| MemAvailable minimum, launch / whole run | 256 / 246 MiB (stop line at least 200 MiB: **met**) |
| Host idle CPU, watch off | 1.9% of a core over 60 s |

[`k1-results.json`](k1-results.json) is the run record,
[`k1-samples.tsv`](k1-samples.tsv) the samples (launch window in kit uptime) and
[`k1-api.log`](k1-api.log) the host's log.

## Findings

1. **Kit mode configures itself from the kit.** The host found its agent,
   learned the right target ID before serving, and launched with no hand-written
   config.
2. **A settings edit survives a restart.** The managed target row was rewritten
   and the added library was kept.
3. **The 24 s durable-store open is still there** (see the slice 3 record). It
   is the larger part of the host's start-up, and it locks the agent out for
   that time.
