# Kit runtime manifest

This file-overlay workflow applies only to maintenance-aware images that do not
use the stable kit software supervisor. Managed images reject claim, swap,
baseline and restore plans before mutation. Use a private diagnostic host and
[immutable kit software releases](kit-software-releases.md); return to a prior
installed bundle through explicit generation-checked activation. Config-only
temporary overlays are not supported on managed images. The unmanaged path
remains for bootstrap adoption and is not an alternative managed update path.

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
| Task marker | `<hold>/KITA-HELD-BY-<who>` or `KITB-HELD-BY-<who>` on the workstation |
| Maintenance credential journal | `<hold>/.fes-kit-maintenance/KITA.json` or `KITB.json`, private to the invoking user |
| Target maintenance reservation | `/run/fogcast/kit-maintenance.json`, private and scoped to the current boot |
| Target transaction lock | `/run/fogcast/kit-maintenance.lock` |

The hold directory is `FES_KIT_HOLD_DIR` when that variable is set. Otherwise
it is `$XDG_STATE_HOME/fes-kit` when `XDG_STATE_HOME` is set, then
`~/.local/state/fes-kit`. The directory is created mode `0700`. It is used
only when the current user owns it, it is not a symlink, and neither it nor
an ancestor is group- or world-writable. A sticky ancestor such as `/tmp` is
allowed when this user owns the next component. An explicit path that fails
these checks is an error. A default candidate that fails is skipped.

Temporary storage is not the hold directory, so a workstation reboot does
not drop the maintenance credential. A private journal and task marker left
in the previous default `$TMPDIR/fes-kit-<uid>` are moved into the selected
hold directory when that file is not already there. A legacy directory or
file that is a symlink, is owned by someone else, or is group- or
world-writable is left in place and is not used. Drift snapshots use
`FES_KIT_DRIFT_DIR` when that variable is set. Otherwise they use
`~/.local/state/fes-kit/fes-kit-drift`, including when `XDG_STATE_HOME` or
`FES_KIT_HOLD_DIR` puts the hold directory somewhere else, so a manual watch
and the timer share one history. A cycle whose own drift directory has no
snapshot yet still reads that canonical snapshot when a home directory can
be resolved. A missing home does not block a watch that sets
`FES_KIT_DRIFT_DIR`. A marker named `KITA-HELD-BY-<who>` records a temporary
diagnostic task. It does not fence target requests or other hosts. Restore
renames it to `KITA-RELEASED-BY-<who>`. The target maintenance reservation
is the authority while files and services change.

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

The kit.py lease is the FPGA execution lock. Release that session before a
swap/restore. The tool claims a grant, promotes it into target maintenance after
cleanup, and retains the reservation across service and agent restart. Each
successful transaction ends maintenance after the target confirms idle. This
tool does not reboot the board. Ordinary claims, takeover and mutations are
refused during maintenance, including kit-local launch requests.

The maintenance client reads `target_id` from `agent.toml` when that key is
set. When it is omitted, the client uses `/media/fat/fogcast/target-id` if the
agent saved one, and otherwise the target id in authenticated health. Health
must report the same id as a configured or saved identity. A swap of
`agent.toml` that changes the target id can finish on the same boot: the
client records one successor id in the private journal after the original
reservation still matches, then ends that reservation. A different boot, a
different reservation, a health id that disagrees with the new config or saved
file, or a further id change does not finish and keeps the journal.

The installed agent must support `fes-kit-maintenance-v1`; see the one-time
[adoption procedure](kit-sharing.md). Replacement and restored agent executables
must advertise that same contract with `--print-maintenance-contract`. A stock
agent without it cannot be restored through this workflow. Missing API support
never enables an unguarded SSH swap.

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
manifest names another task, or when a tracked path is bind-mounted and the
manifest does not record it. A random task identity is retained in both the
local marker and remote holder; reusing the same `--by` label does not let an
older task swap or restore a newer task's files.

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

`restore` is for temporary overlay diagnostics. Accepted appliance updates remain
installed and do not use this command. Managed software uses [persistent kit-software releases](kit-software-releases.md)
and refuses this file-overlay command.

`restore` puts every manifest entry back to its stock backup, checks the
hashes, unmounts tmpfs binds, restarts the affected services, removes stray
`mister-agent`, `mister-runtime`, `fogcast-kit`, and `fogcast-tenfoot`
processes whose executable is under `/tmp/fes-swap/` or a deleted mapping,
clears the manifest entries, and releases the hold. The boot id must be
unchanged. A changed boot ID is rejected before mutation and keeps the task
marker. Inspect the current boot and use `restore --expected-boot-id UUID` only
when deliberately restoring the retained diagnostic files on that exact boot.

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
record. If a recorded task has no matching local marker, additionally pass
`--expected-manifest-sha256 SHA256` from the inspected status to bind that
explicit recovery to the current manifest. `--force` alone cannot restore
another recorded task. Those abandoned binds are unmounted, the affected services restart,
and the hold is not released while a tracked mount remains. If an unmount
fails, binds already removed in that attempt are mounted again and the
services are started before the command returns the error.

## Failed transactions and concurrency

The local credential journal is written privately before mutating API requests.
An interrupted or failed transaction retains the target reservation. Inspect
`kit status`, repair the cause, and rerun the explicit swap/restore to resume the
same operation. To finish after independent repair without repeating a file
change, run `python3 scripts/fes kit maintenance-end A` on the workstation that
holds the journal. Matching completion still requires clean target idle. A
missing credential journal cannot authorize clearing another operation.

SSH apply takes the target transaction lock and checks the exact maintenance
operation, generation and boot before any service stop or file change. It also
compares the manifest digest observed during planning. Concurrent or stale plans
are rejected; they cannot overwrite a newer task's manifest. Incoming paths are
unique per invocation. The lock covers validation, changes, rollback and service
start; child supervisors do not inherit it. API completion takes the same lock.

The temporary task marker remains between transactions so ordinary leased tests
can run. Each later swap/restore requires fresh target maintenance admission.
Removing the agent or its configuration, or restoring an agent without the
maintenance contract, is rejected before service shutdown.

## Drift watcher

Managed images report `MANAGED software` and direct the operator to
`fes-software --action status`. Their watcher does not compare inactive factory
binaries with a working bundle, write stock baselines, or attempt restoration.

`python3 scripts/fes kit watch --once` hashes both kits, compares them with
the manifest and the drift baseline, and appends to
`~/.local/state/fes-kit/fes-kit-drift/watch.log` (or `FES_KIT_DRIFT_DIR`).
The timer uses that same canonical directory, not the hold directory selected
by `XDG_STATE_HOME`. A cycle with no drift baseline logs
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

Managed kit evidence records `fes-software --action status` with the exact
base-image, bundle and core identities. On unmanaged images, hardware evidence
includes `python3 scripts/fes kit status <kit>`. Claim before changing files,
swap instead of copying, and restore before
calling the run done. The lease steps in [overlay HIL](hil-overlay.md) stay;
file installation goes through this tool. The kit addresses and the restart
rules are in the FogCast [development guide](../sources/FogCast/docs/DEVELOPMENT.md).
