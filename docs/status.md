# Unified status

`make status` prints a concise summary separating working-tree changes, latest observed remote
main, selected sources and external lock digests, CI evidence, built artifacts,
hardware qualification and deployment observations. It does not fetch, stage,
write receipts, rebuild, program hardware or query devices. Existing
`make source-status` behavior is unchanged. `--timeout` accepts more than zero
and at most 120 seconds per remote observation.

```sh
make status STATUS_ARGS="--offline"
# Full machine-readable identities, evidence and errors:
make status STATUS_ARGS="--offline --json"
python3 scripts/status.py --offline --json --output-dir out/native-integration-dev \
  --ci /absolute/path/ci-observation.json \
  --qualification /absolute/path/package-acceptance.json \
  --deployment /absolute/path/deployment-observation.json
```

Without `--offline`, only the existing source-status remote-main observation may
use the network. Offline freshness remains unknown, even if a local tracking
branch exists. Missing or malformed evidence produces an actionable unknown or
invalid result; successful report generation does not mean all states passed.
Evidence flags can be repeated. There is no implicit search for a newest receipt.

## Sources and builds

The selected snapshot is the actual FES HEAD and its module trees. Branch,
dirty paths and relation to observed main are separate. Policy/recipe/compiler
lock records show working and committed SHA-256 values separately; those digests
do not establish compiler qualification. The report includes native input
policy, image source locks, boot-media policy and locks selected by core recipes.

The default output is `out/native-integration-dev`; use `--output-dir` for another
explicit output location. Host and image receipt artifacts are rehashed, and
receipt source linkage is compared to selected HEAD. Host receipts must contain
both `fogcast` and `fogcast-api` and valid platform fields; image receipts must
contain `linux.img`. Valid old bytes can be
`bytes-verified` while `different-from-selected`. Even `matches-selected` describes
committed identity, not uncommitted working bytes. Input sidecars are bound using
the existing host/image fingerprint recipes when available; missing closure
remains unknown. When present, `verification.json`, `reproducibility.txt` and
`qemu-smoke.log` are separately checked through the existing image verifier.
The resulting `software_verification` state binds recorded structural, QEMU and
reproducibility results to the image bytes; missing evidence stays unknown and
invalid evidence does not erase the separate artifact-byte observation. A build
receipt is never hardware acceptance or deployment.

JSON files are limited to 1 MiB, each artifact to 4 GiB, and each receipt to 32
files and 8 GiB total. Paths must be regular files without symlink components;
artifact paths cannot escape the receipt output directory. Concurrent file
changes are reported as unavailable rather than verified.

## CI observations

The aggregate `integration` Actions job writes and uploads
`fes-ci-observation-<run-id>-<attempt>`. Download that artifact and pass its JSON
with `--ci`. It records `github.sha`, the actual tested checkout (which may be a
PR merge commit), the event base, and prerequisite job results. First branch
pushes with an all-zero base produce `null`, explicitly incomplete evidence.

```json
{
  "format": 1,
  "kind": "ci-observation",
  "source_revision": "1111111111111111111111111111111111111111",
  "integration_base": "2222222222222222222222222222222222222222",
  "observed_at": "2026-09-20T12:00:00Z",
  "checks": [{"name": "components", "result": "success"}]
}
```

`repository` and `run_url` are optional strings. Check results can be `success`,
`failure`, `cancelled`, `skipped`, `timed_out` or `unknown`. Status reports the
observation file digest and historical result; it neither authenticates the
file's author nor queries current Actions state or branch protection.

## Hardware and deployment

`--qualification` accepts the existing format-1 receipt emitted by
`package_acceptance.py`. The report retains its exact archive digest, package ID,
kit target, revisions, timestamp and `lifecycle-only` or
`lifecycle-input-diagnostic` scope. It rehashes the recorded absolute archive path
when available. A historical recorded pass remains separate from a missing or
changed local archive. Lifecycle acceptance does not imply HDMI/video, timing,
full-image acceptance or current deployment. Other receipt formats are explicitly
unsupported rather than guessed from filenames or prose.

`--deployment` accepts a supplied observation from an actual device inspection:

```json
{
  "format": 1,
  "kind": "deployment-observation",
  "kit": "designated-kit",
  "observed_at": "2026-09-20T12:00:00Z",
  "identities": {
    "image": {"image_id": "observed-device-image-id"},
    "agent": {"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
    "runtime": {"revision": "3333333333333333333333333333333333333333"}
  }
}
```

Each identity accepts `sha256` (64 hex), `revision` (40 hex), and/or `image_id`
(nonempty string). Omitted image/agent/runtime entries are explicitly unknown;
partial observations remain incomplete. Source comparison is made only for an
observed revision. An old restored agent or image is never labeled candidate
current based on a recent timestamp. These are supplied historical assertions,
not live observations or independently authenticated statements. Status never
records a deployment observation or infers one from an acceptance receipt.
