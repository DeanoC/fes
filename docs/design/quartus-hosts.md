# Quartus hosts and the lease

Every Quartus run in FES holds one slot of its build host's **Quartus lease**
for its edition. The lease is a counted pool of `flock` slot files on each
host, sized to that host's RAM, so jobs from producers, fit scripts, Lander's
automation and manual runs share a host without running it out of memory.
Before the lease, most Quartus callers took no lock, and three unleased board
compiles OOM-killed each other on chip-30x0-ai-dev on 20 November 2026 while
Powerboat sat idle.

## The rule

- `locate_quartus` in `sources/misteross/scripts/quartus_tools.py` takes the
  lease after it authenticates `quartus_sh`. A producer or fit script that
  finds Quartus through it holds a slot without asking, until the process
  exits.
- A runner that fits several seeds takes `quartus_lease(edition)` once per
  seed, around the Quartus programs of that seed, and calls `locate_quartus`
  inside a lease so it does not hold one for the whole run. Parallel seeds in
  threads each take their own slot (`scripts/quartus_fes_i386.py`,
  `scripts/fit_i386_device.py`).
- Importing `quartus_tools` installs an audit hook. It refuses to start a
  licensed Quartus program from a process that holds no lease for its
  edition. It looks through `run_logged.sh`, `bash -c` strings and
  `os.system`. A licensed program is one whose directory's `quartus_sh` has a
  digest in `LICENSED_QUARTUS_SH`, which lists every committed
  `quartus.lock` digest, so test stand-ins are never leased.
- Not leased: `--version` and `--help` probes, and `quartus_pfg`, `quartus_cpf`
  and `quartus_pgm`, which convert or program an existing bitstream in seconds
  and a few hundred MB.
- A shell script, a manual `quartus_sh`, or a script whose bytes cannot change
  runs under `python3 scripts/quartus_jobs.py run --edition lite|pro -- …`
  (from `sources/misteross`).
  `run` holds the slot until every process of the job has exited, and `run` is a
  child subreaper, so a process orphaned anywhere in the job (left in the
  background, daemonized, or started by a producer with a scrubbed
  environment) is reparented to `run` and still counts. It stops the job at its time cap (`--cap-minutes`, default
  240): SIGTERM to every process, SIGKILL after a minute, exit 124.
  It prints a heartbeat every minute, also written to the run directory's
  `heartbeat` file. `run` and `remote` refuse a filesystem with under 30 GiB
  free (exit 75).
- `tests/test_quartus_lease.py` fails on any tracked script that could run a
  leased Quartus program unleased. Out of scope are tests (stand-in programs)
  and `docs/validation/` (frozen evidence of past runs).

## Pools

| Host | RAM | Kept back | Pro slots | Lite slots |
|---|---|---|---|---|
| chip-30x0-ai-dev (fes-build-3090) | 23 GB | 8 GB | `/tmp/lander-quartus-3090.lock` | `/tmp/fes-quartus-lite-1.lock`, `/tmp/lander-quartus-3090.lock` |
| powerboat | 62 GB | 24 GB | `/tmp/fes-quartus-pro-1.lock`, `/tmp/lander-quartus-powerboat-pro.lock` | `/tmp/fes-quartus-lite-1.lock`, `/tmp/fes-quartus-lite-2.lock`, `/tmp/lander-quartus-lite.lock` |
| any other host | | | `/tmp/fes-quartus-pro-0.lock` | `/tmp/fes-quartus-lite-0.lock` |

- Slots are tried in order. Each pool's last slot is the lock Lander's
  automation already takes as a whole-job mutex (`seal-remote.sh`,
  `lander-quartus-remote`). New jobs leave it to Lander while another slot is
  free, and a Lander job and new-scheme jobs never overlap on one slot. On
  chip both editions share that lock, because Lander takes it for both.
- A process whose ancestor holds a slot of the right pool already holds the
  lease. That covers `flock(1)`, which leaves its descriptor open in the
  command it runs, and a child of a leasing process (found in `/proc/locks`).
  So a producer that Lander runs under its flock does not wait on its own
  parent.
- Slot files are opened with `"a"`. They are never unlinked or replaced,
  because unlinking a held lock file would let a second job lock a new inode
  at the same path.

### Sizing

The budget for each host is: the sum of its distinct slots, each at the peak
of the largest edition using it, is at most RAM minus what is kept back.
`PoolTests` checks this.

| Job | Peak | Source |
|---|---|---|
| DE25 pc386 board fit, Quartus Pro 26.3 | 8.30 GB virtual (largest of the DE25 pc386 lane's board fits since 15 Nov) | `Peak virtual memory` in each run's `golden_top.*.rpt` on chip |
| DE25 ZX81 board fit, Quartus Pro 26.3 | 5.59 GB `quartus_fit` VmHWM | `/proc/<pid>/status` of Lander's 21 Nov baseline build on chip |
| pc386 machine fit on Agilex 5 (`fit_i386_device.py`) | 6.11 GB `quartus_fit` VmHWM | the same, on x87 slice 6's fit |
| DE10 pc386 Lite seal, Quartus Lite 17.0.2 | 5.65 GB virtual (largest of 18 seals since 17 Nov) | `Peak virtual memory` in `top.*.rpt` under `/data/projects/fes-pc386` on chip |
| pc386 machine fit on Agilex 5, leased run (`fit_i386_device.py --device de25 --design machine`) | **5.93 GiB RSS**; Quartus reports 7.79 GB virtual | `quartus_jobs.py run` on Powerboat, 21 Nov, commit 2290d767a |
| DE10 ZX81 Lite seal, leased run (`build_fes_zx81_quartus.py`) | **2.20 GiB RSS**; Quartus reports 2.74 GB virtual | the same |

Quartus reports peak *virtual* memory per stage, which bounds resident memory
from above. In the two leased runs, resident memory over the whole job, summed
across every descendant process, peaked at 76–80% of Quartus's reported
virtual peak. The DE25 pc386 board fit (8.3 GB virtual; its shell is not on
main yet) and the pc386 Lite seal (5.65 GB virtual) therefore come to about
6.6 and 4.5 GiB resident. `PEAK_RSS_GIB` is 9 for Pro and 6 for Lite: the
virtual peaks rounded up, which leaves room for what a 5-second sample misses.
chip's pool then fits one Pro job, or one Lander job, beside one Lite job
(9 + 6 = 15 of 15 GB). Powerboat fits two Pro and three Lite (36 of 38 GB),
leaving the rest for Deano's harness, the kits and GPU routes.

## Running on whichever host is free

```bash
cd sources/misteross
python3 scripts/quartus_jobs.py status            # this host's slots and holders
python3 scripts/quartus_jobs.py pick --edition pro
python3 scripts/quartus_jobs.py remote --edition lite -- \
    python3 scripts/build_fes_zx81_quartus.py --cache-root {cache} --package-output {run}/package
```

`pick` probes both hosts over SSH. It reads `/proc/locks` and `MemAvailable`,
so the probe contends for nothing. It chooses a host with a free slot and the
memory for one job, then the edition's preferred host (chip for Pro, per
AGENTS.md), then the most free slots and memory. A free slot on a host without
the memory for one job does not count. When no host is ready, `remote --host
auto` waits and probes again every minute; it never dispatches onto a host
below the memory threshold. `remote` takes only a commit on an `origin`
branch, because that is the remote the build host fetches.

`remote` builds only a pushed commit. On the chosen host it fetches that
commit into a blobless mirror this tool owns (`/data/projects/fes-quartus/mirror.git`),
then adds a detached worktree of it at
`/data/projects/fes-quartus/runs/<commit>-<time>-<label>-<random>/src`. Each run gets its
own clean tree. It is a git checkout, not a `git archive`, because producers
ask git for their tree and revision. The command runs there from
`sources/misteross` under `quartus_jobs.py run`, with `QUARTUS_ROOTDIR` and
`FES_TOOLCHAIN_CACHE_ROOT` set for that host. `{run}` becomes the run
directory, which also holds `job.json`, `job.log` and `peak-rss.json`, and
`{cache}` becomes that host's toolchain cache. Output never goes to `/tmp`:
Powerboat's `/tmp` is a shared 32 GB tmpfs. Run directories are not deleted
automatically; prune old ones by hand.

## Identity

The lease lives in `scripts/quartus_tools.py`. That file is in
`TOOLCHAIN_INPUTS` for both design-v1 and design-v2, so editing it moves no
design identity (`BUILD_ID`). Of the files this change edits, it is the only
one any registered producer's closure reaches. Producers needed no edit,
because `locate_quartus`, which every registered Quartus producer already
calls, takes the lease.

The guard test lists six files it cannot change and checks each pin:

- `review_audio_clocks.py`, `scanout/async_inputs.py`,
  `scanout/output_reset.py` and `scanout/video_outputs.py` are inputs of
  registered DE25 cores. Each runs `quartus_sta` on the project its producer
  just compiled, inside that producer's leased process.
- `link-proof/build.py` is bound by the committed DE25 combined-proof receipt.
- `prototypes/de25_board_control/seal.py` starts only `quartus_pfg`.

Run any of them standalone under `quartus_jobs.py run`.
