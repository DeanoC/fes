# Nightly core qualification

The qualification job in the [nightly workflow](../.github/workflows/nightly.yml)
takes the event's main SHA after full core simulations, FPGA producer checks
and the PC/XT COM run pass. It uses the protected main runner environment.
A missed night leaves the previous selections readable. The next run compares
each registered board/core product recipe with its last successful inputs.

The worker derives the producer's canonical selection record. It includes
source closure, tool/constraint/recipe inputs and evidence dependencies.
Video companions currently use the whole misteross tree as a conservative
invalidation key. A changed cell runs the existing authenticated core-dev
resolver. A prepared candidate survives a failed or delayed kit run. An
unchanged retry uses its original SHA and package identity. Different inputs
prepare a new candidate. Candidates enter the qualified store only after
applicable hardware acceptance. Initial startup visits every registered cell.

## Configure the worker

Use the repository variable `FES_QUALIFICATION_RUNNER` to select a provisioned
self-hosted runner label (default `fes-build`). Choose a worker with the
authenticated toolchains required for cache misses. Set the following Actions
environment variables for the protected runner:

| Variable | Value |
| --- | --- |
| `FES_QUALIFICATION_STORE` | Absolute persistent directory outside checkouts. The builder and acceptance workers require a trusted shared filesystem with atomic rename, fsync and flock. Release consumers need read access. |
| `FES_QUALIFICATION_CACHE` | Absolute producer cache holding the authenticated OSS and Quartus build inputs. Provision the normal GPU/Quartus leases and tool installations on the build worker. |
| `FES_QUALIFICATION_CONFIG` | Absolute path to the release owner's JSON configuration. |

Each named release specifies a board, an ordered scope including MENU and the
SHA-256 of its software release manifest. That manifest should identify the
exact OS, runtime, drivers and Tenfoot binaries. Separate Linux and experimental
embedded targets need separate identities and admission evidence. For example,
this is a single-machine development scope:

```json
{
  "releases": {
    "de10-zx81-dev": {
      "board": "de10-nano",
      "software_sha256": "<64 lowercase hex digits from the release>",
      "cores": ["fes.menu", "fes.zx81"]
    }
  }
}
```

For either Linux board's MENU-only bootstrap release, configure
`linux_bootstrap_artifacts` as a map from release name to the absolute path of
the existing bootstrap artifact directory. Set that release's `software_sha256`
to the digest of its input `artifacts.json`. The job validates the complete
artifact directory and runs the image packer's MENU admission check against
the actual ARM runtime. A compatible selection publishes automatically after
hardware qualification. Releases with playable cores use the whole-set
admission worker described below.

## Qualification and admission workers

The coordinator writes atomic `requests/<sha256>` JSON files. The filename
hashes the canonical `request` object (sorted compact JSON plus a newline).
Locations are supplied separately so the same evidence travels between hosts.
The store is a trusted release input. Limit write access to the farm and
release operators.

* Core requests identify the exact main SHA, board, input key, prepared receipt,
  package and archive. `locations.FES_NIGHTLY_PREPARED` names the staged input.
  Run the cell's applicable acceptance under its designated lease and verify
  the restore. Use A/B on DE10 and TRWU8J8G on DE25 Linux. DE10 pc386 cells
  get exact-artifact DE10 kit HIL like other tier-1 cells.
* Release requests contain the exact software identity, board and entire
  ordered package selection. The release worker checks those bytes against the
  platform runtime, required interfaces/drivers, linked companions and any
  coupled shell or firmware constraints together. Report PASS or INCOMPATIBLE
  for that tuple. An admission error or absent worker cannot qualify a set.

A worker can submit evidence asynchronously:

```sh
python3 scripts/nightly_qualification.py --store "$STORE" record \
  --verdict /path/to/verdict.json --evidence /path/to/retained-evidence
```

The verdict contains `request_sha256`, `evidence_sha256` (digest of the
supplied raw evidence) and `verdict`. For hardware PASS it also contains `kit`
and `restored: true`. A worker may replace a FAIL after rerunning the exact
request. The next nightly consumes the verdict. For synchronous operation,
configure optional absolute executables `accept` and `admit` in the same JSON.
They receive canonical request JSON on stdin, the store in `FES_NIGHTLY_STORE`,
and the prepared path for core requests in `FES_NIGHTLY_PREPARED`. They retain
the raw report in `evidence/<sha256>` and emit the same verdict JSON on stdout.
Exit zero with a bound verdict; reserve exit 3 on admission for an incompatible
set. The existing kit baseline/image timers do separate work. Keep their
recovery selections and credentials in their existing stores.

## Release consumption and retries

The job checkpoints individual cells. A failed route, HIL or unavailable
worker leaves its previous qualified object in place and reports a failing
nightly. The coordinator checks complete board snapshots newest first. It
publishes the newest compatible complete set for each release and keeps
historical immutable sets. Independent readers see complete generations.
Use the release's expected software digest to resolve once, then keep the
returned set identity in both OS and Tenfoot release evidence:

```sh
python3 scripts/nightly_qualification.py --store "$STORE" select \
  --release de10-zx81-dev --software-sha256 "$SOFTWARE_SHA256" \
  --catalog-output "$NEW_CATALOG_DIR"
```

The command verifies the selected objects and evidence and prints the immutable
set path. The optional catalog output uses the existing FogCast catalog
publisher for the selected playable cores. Each row's `prepared` path is
relative to the store. For MENU, use the sibling `menu-package/` directory and
the selection TOML named in `prepared.json` as the two MENU inputs to
[image artifact pack](image-artifacts.md). Preserve the existing software
bundle and image checks. A missing first baseline or an incompatible release
has no usable pointer for its software digest.

For a farm retry, check out the same tested main SHA and run the workflow's
`run --config ... --sha ...` command. The lock serializes writers, and an
older invocation cannot rewind a newer checkpoint. Pending cells, including
cells outside today's release scope, are visible in `state.json`. Retain the
objects, verdict evidence and sets used by supported releases and rollback.

### Powerboat and chip deployment

The chip build runner keeps the persistent store at
`~/.local/share/fes-qualification/store`. Its protected config selects
`bin/nightly-kit-client.py` for `accept` and `bin/nightly-release-admit.py`
for `admit`, using absolute paths under that same parent directory. The client
submits a request digest to Powerboat over SSH. Powerboat fetches the named
request and sealed inputs from chip, checks the frozen main and Actions run,
then calls the existing HILQ driver. Both hosts require batch-mode SSH to
their configured peer. Powerboat also needs read access to the repository's
Actions runs through `gh`.

Install the two client-side executables from `scripts/powerboat/` on chip.
Install `nightly-kit-worker.py`, `nightly-de10-smoke.py` and
`nightly-menu-smoke.py` together in Powerboat's
`~/.local/share/fes-qualification/bin/`. HILQ owns the kit claims and restore.
DE10 tries B then A. DE25 uses the serial-bound D25 watcher. A digest-specific
run directory retains the raw kit evidence and verified sealed package for
retries. Two failed attempts hold the candidate for investigation.

For the configured DE10 MENU and ZX81 development release, place the checked
bootstrap under `software/<artifacts.json sha256>` alongside the store.
The admission worker validates MENU with the release runtime, builds the
FogCast catalog and checks whether the playable kit smoke used all six
selected software binaries, the running runtime and Tenfoot, and the same
MENU firmware. Exit 4 leaves a release awaiting matched evidence and lets
the coordinator consider earlier complete sets. The release owner may also
submit an exact request-bound verdict using `record` after a separate
release-matched kit run. The bootstrap-only release uses its existing
Linux MENU admission path.
