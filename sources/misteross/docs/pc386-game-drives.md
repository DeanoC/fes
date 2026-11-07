# pc386 game drives

Reusable FreeDOS hard-disk images for `fes.pc386` simulation and kit runs. Each
holds user-supplied DOS programs. Runs used to rebuild a game disk by hand in a
per-run scratch tree. They now name a prebuilt drive by path, SHA-256 or name.

The drives hold commercial games and the DOS Benchmark Pack. **They never enter
the repository, a PR, an issue or an upload.** The repository holds only:

- the builder, [`scripts/build_pc386_game_drive.py`](../scripts/build_pc386_game_drive.py);
- the manifest, [`cores/fes-pc386/sim/game-drives.toml`](../cores/fes-pc386/sim/game-drives.toml): titles, source paths, SHA-256s, launch commands and keys;
- this page.

## Where the drives live

The store is on Powerboat at `~/fes-drives/pc386/` (`FES_PC386_DRIVES`
overrides it). It is private to the account: the directory is mode 0700.

| Path | What |
|---|---|
| `<sha256>.img` | a drive, named by its content SHA-256 |
| `<sha256>.json` | its record: geometry, the FreeDOS base, each title's launch, keys, source hash and file count, free bytes |
| `by-name/<drive>.img`, `by-name/<drive>.json` | symlinks to the latest build of each drive |
| `sources/dosbench/` | the DOS Benchmark Pack v1.5, fetched from the NAS share (`FES_PC386_DOSBENCH`) |
| `sidecars/<sha256>` | config files a game's own setup wrote, which a title can put over its copy |

eXoDOS zips are read in place from
`/mnt/emuhd/Games/PC DOS/Collections/eXoDOS 6.04 Full/eXoDOS/eXo/eXoDOS`
(`FES_PC386_GAMES` overrides it). The builder checks each zip and folder
against the manifest's SHA-256 before it copies anything.

## The drives

Each drive is the FreeDOS games image of
[`build_pc386_freedos_image.py --profile games`](../scripts/build_pc386_freedos_image.py):

- HIMEMX, UMBM, CuteMouse with MOUSEDEF, FESENV for `BLASTER`, and MEM;
- `FDCONFIG.SYS` and `AUTOEXEC.BAT` unchanged;
- 16 heads and 63 sectors per track;
- one FAT16 partition with 2 KiB clusters.

Each title's folders go into their own `C:` directory. A drive is at most 260
cylinders: 128 MiB, the most the ATA disk window holds
(`fes.media.ata-disk`, and the kit agent's limit).

Built 2026-11-07 from the manifest with SHA-256 `3fc4306cedf128ac…`:

| Drive | SHA-256 | Size | Titles | Free | What |
| --- | --- | --- | --- | --- | --- |
| `real` | `b460ca550cc7a8b8df525e7fbb258b03c18acb1f82e22289dcdb0c54b335dff2` | 260 cylinders, 128.0 MiB | keen4, prince, civ, jill, stunts, vikings, starcon2, wolf3d, ultima6, jazz, pinfan, wacky, hocus, monkey2, wc1, falcon3, xwing | 25.4 MiB | Real-mode and 386 real-mode games, EMS users and sound setups |
| `xms` | `64f68024ffbd114bed1070770267100fe8415f625c1e3cd4266270468da0a39f` | 260 cylinders, 128.0 MiB | ultima7, strike, uw1, frontier, moo, darklands, tnt | 12.5 MiB | XMS and unreal-mode games and other own 386 memory managers |
| `pmode` | `70713edb73d554dec709af5d4ff75e14ce67af0cf6e6aa09f9fc53293336f986` | 260 cylinders, 128.0 MiB | doom, heretic, duke3d, descent, raptor, omf, rott, machines, hotspeed | 16.1 MiB | DOS/4GW and other CPL-0 DOS extenders |
| `dpmi` | `bb659032ad7006bd951c5b147e2ee47b6557c10f11483a2f9aa7824e3d407c50` | 260 cylinders, 128.0 MiB | jumpbump, angband, nethack, tyrian, cyberdogs, tcircle, screamer, tv | 31.1 MiB | Ring-3 DPMI hosts (CWSDPMI, Borland RTM and 32RTM) and FPU users |
| `fpu` | `03e430f9df0637d29707e62a30e94f02fd4f06350ea5d6abdde94bf8970a9f12` | 260 cylinders, 128.0 MiB | gp2, usnf | 54.0 MiB | FPU-required simulations |
| `bench` | `4fbeb7d9d7391e25e5b54393d4fcb85ffc74fb2c3717ea4e0dae838828a331ef` | 80 cylinders, 39.4 MiB | 3dbench, pcpbench, topbench, speedsys, chkcpu, cachechk, si8, chrisb, quakes | 18.9 MiB | Non-game programs: PhilsComputerLab's DOS Benchmark Pack v1.5 tools |

The census record,
[2026-11-07 pc386 compatibility census](../../../docs/validation/2026-11-07-pc386-compat-census/summary.md),
lists every title with its bucket and result.

## Building

```sh
python3 scripts/build_pc386_game_drive.py --download            # every drive
python3 scripts/build_pc386_game_drive.py --download real xms   # some drives
python3 scripts/build_pc386_game_drive.py --list                # drives, title counts, latest builds
python3 scripts/build_pc386_game_drive.py --resolve real        # a drive's image path and record
```

`--download` fetches the pinned FreeDOS packages and NASM the first time. The
build is reproducible, so a rebuild from the same manifest, sources and
FreeDOS lock gives the same SHA-256. To make that true:

- file names are upper-case 8.3 names, with no long names;
- each file keeps its zip time; folder sources take the image's fixed time;
- directories and files are written in sorted order.

Names that are not 8.3, such as eXoDOS's long-named manuals, are skipped and
counted in the record. Source files pass through a private 0700 work directory
in the store, which is deleted at the end.

To add a title, append it to a drive in the manifest. Give its zip and the
zip's SHA-256 (`sha256sum`), and the folders to copy. Take the launch command
from the eXoDOS launcher: `!dos/<game>/dosbox.conf` and the game's `run.bat`,
in `Content/!DOSmetadata.zip`. When eXoDOS's Sound Blaster choice copies a
config folder over the game (`copy .\sb16\*.*`), add a second `copy` entry for
that folder. Keep each drive under 128 MiB: the build fails if the files do not
fit.

## Simulation

The game suite boots a drive, types the title's launch command at the `C:\>`
prompt, and sends its keys, timed from the launch:

```sh
python3 scripts/sim_fes_pc386.py --suite game --game wolf3d                 # the store's drive that holds wolf3d
python3 scripts/sim_fes_pc386.py --suite game --game doom --drive pmode     # by name
python3 scripts/sim_fes_pc386.py --suite game --game doom --drive 70713edb  # by SHA-256 prefix
python3 scripts/sim_fes_pc386.py --suite game --game duke3d --cpu rtl --rtl-at-prompt --frame-every 1
```

- `machine_tb` reads the drive into memory and never writes it back, so runs
  can share one drive.
- `--rtl-at-prompt` boots FreeDOS on the oracle and hands over to
  `fes_i386_cpu` at the prompt (`machine_tb --rtl-at-prompt`). The proven boot
  then costs oracle time only.
- `--frame-every S` writes the captured frame every S simulated seconds.
- `--instruction-census` writes the oracle's instruction census, which counts
  CPL changes and gates.

The report goes to `build/sim/fes-pc386/game-<id>[-rtl]/report.json`. With
`--cpu rtl`, its `rtl` section lists the stops and the protected-mode
stretches the oracle ran. Wolfenstein 3D and Ultima VI, the acceptance titles,
must also play the OPL2.

## Kit runs

A kit runner resolves the drive with the builder's helpers. It then stages the
drive by SHA-256 and launches with the geometry from the drive's record:

```python
from scripts import build_pc386_game_drive as drives
image, record = drives.resolve('real')      # a path, a SHA-256 (or 8+ digit prefix) or a name
# stage: POST /v1/library/disk-images with X-FogCast-Disk-Image: record['sha256'] and the image as the body
#        (skip it when /v1/library/disk-images/probe says the digest is staged already)
# launch: POST /v1/library/core/load with X-FogCast-Disk-Image: record['sha256']
#         and X-FogCast-Disk-Geometry: drives.geometry(record)   # '260/16/63'
```

The agent keeps a staged drive under `/media/fat/fogcast/disk-images/` by
digest, so relaunching reuses it. A 128 MiB drive needs that much free FAT
space, plus room for the run's disk delta. The kit rules apply:

- claim the kit in `~/kit-status/kitX-STATUS.md`;
- remove the staged drive and the run's game records afterwards;
- log the restore.

Type the title's `launch` text through the agent's input stream at the
prompt. Kit keys are the runner's own: the manifest's keys are simulation
times.
