# Audited core-package source keys

The eight `native-integration-dev` FPGA producers use the committed
`sources/misteross/scripts/source_closures.json` manifest. Each entry lists the
files or directories that can affect that producer: its Python imports,
explicit HDL and constraint inputs, recipe, ABI definition, selected toolchain
lock, the toolchain recipe files hashed into the shared toolchain cache key
(`scripts/bootstrap.sh`, `lockfile.py`, `toolchain_cache.py`), core-local
sources, and only the shared RTL it uses. The functional record
hashes the tracked files below those roots, the existing execution digest
(tool binaries, controlled environment and GPU inputs), and
`parameters.source_closure_mode = "audited-v1"`. Producers without a manifest
entry keep the original broad `scripts/` and owning-module roots and produce
byte-identical records.

A narrowed build enforces the declared roots. Python read opens and directory
listings and successful compiler opens and `getdents` listings under the
misteross source root must be covered. Generated `build/` outputs and temporary
files outside the source tree are exempt. A Python `open` of a directory or a
missing path is not a read: the audit event carries no `dir_fd`, so
`shutil.rmtree`'s descriptor-relative opens would otherwise resolve against the
working directory. A producer run as `python scripts/build_x.py` (`__main__`)
resolves to its `build_x` manifest entry, so the sidecar record it writes uses
the same roots as the canonical record FES derives by importing it. The existing executable-Markdown
policy and clean-source and historical-record validation still apply. An
uncovered read fails the build before package sealing; it never results in a
new cached package. A cached package is still checked against its immutable
manifest, payload, build record, original source revision and current
functional key on selection.

## Refresh a closure after a failure

On the Linux GPU builder, run the producer with an explicit read log. For
example, for Pong:

```sh
cd sources/misteross
python3 scripts/source_closure.py record --producer build_fes_pong \
  --record /tmp/pong-reads.jsonl --record-only
python3 scripts/source_closure.py derive --producer build_fes_pong \
  --record /tmp/pong-reads.jsonl
```

`--record-only` sets `FES_SOURCE_CLOSURE_RECORD_ONLY=1` and uses the broad key,
so a diagnostic artifact cannot populate the narrowed cache key. Normal
builds enforce the closure even when `FES_SOURCE_READ_RECORD` is set. Put the
printed roots into `source_closures.json`, inspect each new read, and run
`python3 scripts/source_closure.py check`. `static --producer NAME` bootstraps
an import and pinned-input closure without a GPU. It can be too narrow; a
recorded build identifies omitted dynamic reads. `derive` collapses files to a
directory only if all tracked files below that directory were read or listed.
Simulation and testbench directories need investigation rather than automatic
inclusion.

## Nightly comparison

On a clean Powerboat checkout, run the parent comparison under the existing
FPGA lock. It resolves each narrowed package, then force-builds a broad-key
package with read recording in a separate snapshot and private artifact cache.
It fails if either the RBF/payload SHA256 differs or the broad rebuild reads
outside the narrowed roots. The JSON report records keys, hashes, durations
and uncovered paths. A plan can be printed without a GPU:

```sh
python3 scripts/core_key_nightly.py --out /path/to/reports --dry-run
~/bin/fes-lock fpga -- nice -n 19 python3 scripts/core_key_nightly.py \
  --repo /path/to/clean/fes --out /path/to/reports
```

A sample **systemd user** timer (install and paths are operator choices):

```ini
# ~/.config/systemd/user/fes-core-key.service
[Unit]
Description=Compare narrowed FPGA core keys with full rebuilds
[Service]
Type=oneshot
ExecStart=%h/bin/fes-lock fpga -- /usr/bin/nice -n 19 /usr/bin/python3 /path/to/fes/scripts/core_key_nightly.py --repo /path/to/fes --out %h/fes-core-key-reports

# ~/.config/systemd/user/fes-core-key.timer
[Unit]
Description=Nightly FPGA core-key comparison
[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true
[Install]
WantedBy=timers.target
```

This changes cache selection only. Release and HIL evidence remains bound to
its exact artifact. Package selection continues to record `core_rbf_sha256`,
`payload_sha256`, and `functional_inputs_sha256`.
