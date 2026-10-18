# Kit resource limits

The local host and shell on a DE10-Nano share one small board with the runtime.
This note records the idle baseline from 2026-10-02, the collector
measurements on kit B from 2026-10-04, the proposed budgets for that host and
shell, and the read-only collector that measures them again.

The baseline was read on the kits at about 07:10 Europe/Sofia. Later
comparisons should use the collector so the units match the definitions below.

## Baseline

Both kits are DE10-Nano boards: 2× ARM Cortex-A9, 492 MiB RAM, no swap, no CMA.
`/` is a read-only ext4 loop. `/media/fat` is exfat, 1008 MiB, mounted read-write
with `sync`. `/tmp` is a 246 MiB tmpfs.

| | Kit A `192.168.10.84` | Kit B `192.168.10.85` |
| --- | --- | --- |
| Image | `c41e58c1`, tenfoot launcher | `f449886f`, legacy `fogcast-kit --menu-display` |
| Load | 0.01 | 1.5 |
| CPU | 95% idle; `fogcast-tenfoot` 2% of one core | `fogcast-kit` 45% of one core while idle |
| Memory | MemAvailable 410 MiB | MemAvailable 434 MiB |
| `/media/fat` | 75% used, 248 MiB free | 17% used |

## Kit B measurements, 2026-10-04

Kit B (`192.168.10.85`) ran image `24c93192…` (0.2.0-main-84dcb29f.1) with the
grid shell `fogcast-kit --menu-display`. The collector was
`scripts/kit-diagnostics-ssh.sh` from main. Times are Europe/Sofia (EEST).
Each column has two runs.

| | Idle (09:00:48, 09:01:18) | Data Storm playing (09:02:20, 09:02:36) |
| --- | --- | --- |
| `load_1` | 0.40 / 0.55 | 1.02 / 0.91 |
| `cpu_idle_percent` | 58 / 59 | 75 / 74 |
| `fogcast-kit` CPU (% of one core) / RSS | 37 / 42%, about 67 MiB | 13%, 65–70 MiB |
| MemAvailable | 408 / 409 MiB | 357 / 358 MiB |
| `/` (read-only ext4 loop) | 83% used by `df`, 18.7 MiB free | same |
| `/media/fat` | 66% used, 342 MiB free | same |
| `/tmp` | 4 KiB used | same |

Earlier runs on images `5af41062` (idle) and `1e7485f2` (idle and play) are in
[#359](https://github.com/DeanoC/fes/issues/359#issuecomment-5966189267) and
[its follow-up](https://github.com/DeanoC/fes/issues/359#issuecomment-5967709655).

Against the proposed budgets below, at idle:

| Budget | Result |
| --- | --- |
| CPU: `cpu_idle_percent` ≥ 80, `load_1` < 0.50, no `fogcast-*` > 15% | Miss: 58–59% idle, `fogcast-kit` at 37–42%; `load_1` 0.40 and 0.55 ([#389](https://github.com/DeanoC/fes/issues/389)) |
| Memory: MemAvailable ≥ 256 MiB | Pass: 408 MiB idle, 357 MiB during play |
| `/media/fat`: ≥ 128 MiB free | Pass: 342 MiB free |
| `/tmp`: scratch < 32 MiB | Pass: 4 KiB used |

The image id above comes from `/media/fat/fogcast/releases/boot.json`. The
collector on that image reported the factory bootstrap image instead; see
[#504](https://github.com/DeanoC/fes/issues/504).

**Root percentage.** The build reported rootfs use at 78.41%, while `df`
reports 83% on the kit. They measure different things. The build check
(`image/scripts/check-rootfs-headroom.sh`) divides used blocks, block count
minus `dumpe2fs` free blocks, by the configured 128 MiB image size. `df`
reports Capacity as used / (used + available), rounded up. Its Size leaves out
ext4 metadata, and its Available leaves out the blocks reserved for root. The
smaller denominator gives the higher `df` figure. The root is read-only, so
neither figure is space the host can write.

## Collector units

`scripts/kit-diagnostics.sh` prints `key=value` lines, schema
`fogcast.kit-diagnostics.v1`. Integer counters that do not fit in a 32-bit
shell are subtracted as decimal strings. Percentages use the resulting small
deltas.

- `cpu_idle_percent` is the idle field of the aggregate `/proc/stat` `cpu`
  line over the sample window, nearest percent. iowait is not counted as idle.
- A process `cpu_percent` is percent of one core over that same window.
  100 saturates one Cortex-A9. Only processes whose name starts with `fogcast-`
  and that appear in both samples are listed, highest CPU first, then RSS,
  then name, then pid. The name is the basename of cmdline argv0 when `tr` is
  available, otherwise `comm` (15 characters).
- `free_kib` is the Available column of `df -P -k`. `used_percent` is that
  command's Capacity with the percent sign removed.
- `cpu_sample_seconds` is the live sleep, default 1, allowed range 1..5.
  It is 0 when `proc/stat.sample2` supplies the second sample. That file is a
  test fixture, not a live path.
- `cma_present` is 0 and the CMA sizes are 0 when `CmaTotal` is absent.
- A missing mount is `present=0` with empty sizes. The report still exits 0.
  Missing uptime, load, cpuinfo, meminfo, the aggregate cpu line, or `df`
  exits 1.

Image id is the first readable token from:

1. `/etc/fes-image-id`
2. `/etc/fogcast-image-id`
3. `/usr/share/mister-runtime/image-id`
4. `/etc/os-release` `FES_IMAGE_ID`, then `IMAGE_ID`, then `BUILD_ID`
5. `image_sha256` in `/etc/fes/factory.json` (64 hex digits)
6. `image_sha256` in `/.fes-bootstrap/etc/fes/factory.json`
7. the first standalone 7–40 hex word in `/proc/version`

If none of those exist, `image_id` is empty and `image_id_source` is `none`.
The id is not shortened. `fes_revision` in those factory files is the source
commit, not the image. The first 7–40 hex value is `source_revision`, including
when an earlier file already supplied `image_id`. It is empty when neither
file has that field. `sed` is used only for the factory JSON. Without it, the
factory image id and `source_revision` are skipped.

Keys, in order: `schema`, `image_id`, `image_id_source`, `source_revision`,
`uptime_seconds`, `load_1`, `load_5`, `load_15`, `cpu_count`, `cpu_idle_percent`,
`cpu_sample_seconds`, `mem_total_kib`, `mem_available_kib`, `swap_total_kib`,
`swap_free_kib`, `cma_present`, `cma_total_kib`, `cma_free_kib`, then `present`,
`size_kib`, `used_kib`, `free_kib`, and `used_percent` for `fs.root`,
`fs.media_fat`, and `fs.tmp`, then `process_count` and for each process
`process.N.pid`, `process.N.name`, `process.N.cpu_percent`, `process.N.rss_kib`.

## Proposed budgets

These are proposals for the kit-local catalog, the shell, and a headless host
(`fogcast-api --launcher-config --headless` plus `fogcast-tenfoot`). They are
not enforced by the image.

**CPU.** At idle, with the shell up and no title or video decode running:
`cpu_idle_percent` at least 80, `load_1` below 0.50, and no `fogcast-*`
process above 15 percent of one core. Kit A's recorded idle (95% idle,
tenfoot at 2%) meets this. Kit B does not. See the finding below.

**Memory.** `MemAvailable` at least 256 MiB at that same idle. Do not add swap
to hide growth. CMA is absent; do not plan the framebuffer or the catalog cache
as CMA. The tighter measured idle, kit A at 410 MiB available, leaves 154 MiB
above this floor.

**`/media/fat`.** Keep at least 128 MiB free for catalog growth and ROMs. The
filesystem is exfat mounted `rw,sync`, so writes are slow and hit the card.
Kit A is the tight card (248 MiB free), which leaves about 120 MiB of new
catalog and ROM bytes above the reserve. Do not put diagnostics, caches, or
scratch files on `/media/fat`. Do not modify `u-boot.txt`.

**`/`.** The root is a read-only ext4 loop. The local host must not need to
write there.

**`/tmp`.** The tmpfs is 246 MiB, half of RAM, and it is the same memory
`MemAvailable` reports. Shell and host scratch on `/tmp` should stay under
32 MiB. Do not stage ROMs or the catalog database there. Filling the tmpfs is
an out-of-memory event.

## Kit B idle CPU

Kit B on image `f449886f` was idle in `fogcast-kit --menu-display` and still
showed load 1.5. `fogcast-kit` was at 45% of one core, which is under half a
core. Kit A on the newer image, running tenfoot, was at 95% idle. That idle
load is open and is filed as [#389](https://github.com/DeanoC/fes/issues/389).
This collector does not change the launcher.

On 2026-10-04, with image `24c93192…` and the grid shell, kit B's idle load
fell to 0.40–0.55. `fogcast-kit` still used 37–42% of one core and the board
was 58–59% idle, so the CPU budget still misses.

## Run

The collector only reads. It does not create a file, restart a service, or
write `/media/fat` or `u-boot.txt`.

From a FES checkout, pipe it over ssh. The remote command is `sh -s`. Nothing
is copied onto the kit, and `known_hosts` is not updated:

```sh
sources/FogCast/scripts/kit-diagnostics-ssh.sh 192.168.10.84
```

The stock kit login from the [development guide](DEVELOPMENT.md) is:

```sh
FES_KIT_DIAG_SSH='sshpass -p 1 ssh' \
  sources/FogCast/scripts/kit-diagnostics-ssh.sh 192.168.10.84
```

`FES_KIT_DIAG_SSH` is an optional client prefix, split on spaces. The wrapper
unsets `FES_KIT_DIAG_ROOT` and `FES_KIT_DIAG_SAMPLE_SECONDS` before it execs
ssh, so a client `SendEnv` rule cannot point the kit at a workstation fixture.
Leave those variables unset on a live kit.

`kit-diagnostics.sh` is also the script to run on the kit, with `sh`. The
wrapper does not install it. Putting a copy on the kit is a separate authorized
step; use a private `/tmp` path, not `/media/fat`.

## Tests

`scripts/tests/kit-diagnostics_test.sh` runs the collector against a fixture
root (`FES_KIT_DIAG_ROOT`) with fake `/proc` and `df.txt`. It also checks that
the ssh wrapper's remote command is `sh -s` and that a successful run does not
change the fixture. `make test` in `sources/FogCast` runs it with the other
operator-script tests. Focused run:

```sh
make -C sources/FogCast test-kit-diagnostics
```

The test does not contact a kit.
