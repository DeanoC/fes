# Kit runtime manifest

Dev kits A and B keep a record of every file that is not the stock copy.
Agents change those files only through `scripts/fes`. Copying a binary, a
package file, `launcher.json`, or `agent.toml` onto a kit any other way is
drift: the next watcher cycle flags it, and HIL evidence that skips
`fes kit status` does not say what was running.

The kit root filesystem is read-only. The manifest and stock backups therefore
live on the writable FAT volume, not under `/var/lib`:

| Record | Path |
| --- | --- |
| Manifest | `/media/fat/fes/kit-manifest.json` on the kit |
| Stock backup | `/media/fat/fes/stock/<image-sha256>/<component>` on the kit |
| Drift baseline | `/media/fat/fes/baseline.json` on the kit |
| Named baseline | `/media/fat/fes/baselines/<name>.json` on the kit |
| Hold marker | `~/tmp/KITA-HELD-BY-<who>` or `~/tmp/KITB-HELD-BY-<who>` on Powerboat |

`~/tmp` is the existing hold directory. A marker named `KITA-HELD-BY-<who>`
means kit A is claimed. Restore renames it to `KITA-RELEASED-BY-<who>`.

Tracked components:

| Name | Path | Default |
| --- | --- | --- |
| `mister-runtime` | `/usr/sbin/mister-runtime` | tmpfs bind, then restart runtime, agent, and launcher |
| `mister-agent` | `/usr/sbin/mister-agent` | tmpfs bind, then restart agent and launcher |
| `fogcast-kit` | `/usr/sbin/fogcast-kit` | tmpfs bind, then restart launcher |
| `fogcast-tenfoot` | `/usr/sbin/fogcast-tenfoot` | tmpfs bind, then restart launcher |
| `launcher.json` | `/media/fat/fogcast/launcher.json` | persistent FAT replace; `--why` required |
| `agent.toml` | `/media/fat/fogcast/agent.toml` | persistent FAT replace; `--why` required |

A file under `/usr/share/mister-runtime/core-packages/<64-hex-id>/` can be
named by that absolute path. It is a tmpfs bind and restarts the runtime,
agent, and launcher. `--persistent` on a `/usr` file rewrites the read-only
image and also requires `--why`. `--temporary` keeps a FAT file on a tmpfs
bind instead of replacing it.

The kit.py lease is still the FPGA session lock. Release it before swap or
restore, because those commands restart services and a restart drops the lease.
This tool does not reboot the board.

## Commands

From the FES checkout:

```sh
python3 scripts/fes kit claim A --by 700 --for 'PR 700 pad smoke'
python3 scripts/fes kit swap A mister-agent /absolute/path/mister-agent
python3 scripts/fes kit swap A launcher.json /absolute/path/launcher.json --why 'PR 700'
python3 scripts/fes kit status A
python3 scripts/fes kit restore A
```

`--by` is a slug (`700`, `lander`, `reimage`): letters, digits, `.`, `_`, and
`-`. Claim refuses when another `KITA-HELD-BY-*` marker exists, when the
manifest names another holder, or when a tracked path is bind-mounted and the
manifest does not record it.

Swap keeps the first stock bytes for the booted image in
`/media/fat/fes/stock/<image-sha256>/` and does not overwrite that backup on
a later swap of the same image. A different image gets its own directory, so
an older backup does not block a swap after the image hash changes. The
manifest entry records the path, stock hash, current hash, who, when, why,
and `tmpfs` or `persistent`.

`status` prints the image version and sha256 from the appliance release, the
boot id, the hold, and each component's current hash against stock and, with
`--main-hashes`, against main. Before the first claim or watch there is no
stock baseline, so stock is `unrecorded`. `--json` prints the same report.

`restore` puts every manifest entry back to its stock backup, checks the
hashes, unmounts tmpfs binds, restarts the affected services, removes stray
`mister-agent`, `mister-runtime`, `fogcast-kit`, and `fogcast-tenfoot`
processes whose executable is under `/tmp/fes-swap/` or a deleted mapping,
clears the manifest entries, and releases the hold. The boot id must be
unchanged. A changed boot id still restores the files, then the command exits
4 and the hold is released.

```sh
python3 scripts/fes kit baseline A clean
python3 scripts/fes kit restore A --baseline clean
```

`baseline` snapshots every tracked file, including a core-package file the
manifest currently records. A file that is absent is stored as missing.
`restore --baseline` installs that snapshot. A fixed file the snapshot marks
missing, or omits, is removed. An active swap the snapshot does not keep is
cleared back to its stock backup before the hold is released. Components
that match a trusted stock backup are cleared; components that differ stay
as recorded swaps so the watcher does not treat them as drift. Stock is the
manifest entry or the image backup already on the kit. A drifted file with
neither is not copied into the backup; the named baseline bytes are saved
as stock instead.

`restore --force` runs without a hold marker. Use it when the marker was
removed and the manifest still lists swaps, or when a tracked path,
including a core-package file, has a bind mount the manifest does not
record. Those abandoned binds are unmounted, the affected services restart,
and the hold is not released while a tracked mount remains. If an unmount
fails, binds already removed in that attempt are mounted again and the
services are started before the command returns the error.

## Drift watcher

`python3 scripts/fes kit watch --once` hashes both kits, compares them with
the manifest and the drift baseline, and appends to
`~/tmp/fes-kit-drift/watch.log`. A cycle with no drift baseline logs
`UNVERIFIED` and does not treat the current bytes as stock, so an edit made
before the first cycle stays uncertified. The baseline is written only when
`--main-hashes` matches every present tracked file and nothing is drifted or
bind-mounted outside the manifest. Later cycles flag a hash or bind mount
the manifest and that baseline do not record.
`--main-hashes FILE` also prints `STALE` when a hash differs from main.
`--fail-stale` makes that a failing exit. A main-hash file is `sha256
component` or `component sha256` per line.

Install the hourly timer on Powerboat from
[fes-kit-drift.service](../scripts/systemd/fes-kit-drift.service) and
[fes-kit-drift.timer](../scripts/systemd/fes-kit-drift.timer). The unit runs
as `deano` and uses the documented development-image login. It does not use
an agent.

```sh
sudo cp scripts/systemd/fes-kit-drift.service scripts/systemd/fes-kit-drift.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now fes-kit-drift.timer
```

## HIL

Hardware evidence for a kit run includes `python3 scripts/fes kit status <kit>`.
Claim before changing files, swap instead of copying, and restore before
calling the run done. The lease steps in [overlay HIL](hil-overlay.md) stay;
file installation goes through this tool. The kit addresses and the restart
rules are in the FogCast [development guide](../sources/FogCast/docs/DEVELOPMENT.md).
