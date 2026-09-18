# Browser library and protocol compatibility diagnostic

## Scope

FogCast #258 adds browser package/media management over existing host APIs.
FogCast #260 replaces live Git-revision equality with target API `v1`
admission. Exact source pins, image locks, artifact hashes and release receipts
remain provenance. Package ABI, native protocol, input, persistence and media
limits retain their operation-specific checks.

No FPGA compilation, agent/runtime/Kit replacement, image rebuild, SD write or
reboot was performed for this diagnostic. This is not image reproducibility or
release acceptance, and the Pong leg does not exercise media import.

## Artifacts actually exercised

- Powerboat host source: `115f82cba3e52bf73074efb94753eb66a4b04749`.
- Host binary SHA-256:
  `43fd18d618dab13f92af798687282bc67af7dd8b7120ba50e9b8296100ea57b4`.
- Target: `73dc9f5f-1a12-4a95-a820-a9b4e600769a`, `192.168.10.84`.
- Agent revision: `d9745ed746a1e8d0bde423151d08810248ce8815`.
- Runtime revision: `a6d658cd305c4a84860afc1f8b00a2798ee6e4f4`.
- Boot ID: `1e22fd4a-c72d-4331-8cc3-fd9316d61fc6`.
- Installed image SHA-256:
  `6538ca1b5d61729f287a5aaeb5a72c14cf7f8bd6ae03bd87bc874056c65c75d9`.
- Pong package:
  `9be4b59993cfd0ac6b347da42decb340235c570d310901468778d6dad15d0120`.

The host uses a normal full revision stamp, not a development version or
spoofed agent revision. Health reports both differing revisions and `ready`.
Later source revisions must not inherit exact-binary acceptance from this run.
The parent candidate selects `c8deb0fb4a2ebd3a55ba476d18d9fc9c65012579`,
which additionally fixes explicit multi-target admission after review. Its
full Go suite, affected race tests and parent consistency/host build are
separate software evidence; it was not substituted under the operator's live
physical-check session.

## Observations

The real Chromium browser submitted the immutable package archive and created
`Browser protocol smoke Pong 20260918` through the management panel. Its ID is
`fpga-browser-protocol-smoke-pong-20260918-7f28e9c7967b`. The existing five
entries and their package/media selections were preserved. This labelled
diagnostic entry is intentionally retained.

The running Kit refreshed its own `launcher-cache/catalog.json` with that exact
ID and launch eligibility. Launch through the common host session API reported
the exact active package and attached input. A second launch/Stop cycle passed.
Physical display/controller/Kit-menu acceptance remains pending operator
confirmation; catalog persistence alone does not establish visible rendering.

Failures are retained, not erased from the acceptance record:

1. The first browser evidence collector lost its upload response body after
   HTTP 200. A read-only package query reconciled identity; the upload was not
   replayed. A subsequent browser run created the entry and verified it through
   the authoritative inventory.
2. Two legacy plain-Pong launches failed because
   `/usr/share/mister-runtime/cores/pong.rbf` is absent in the package-only image.
   The resulting recovery error blocked initial entry creation. Normal owned
   Stop cleared it; explicit package compatibility and entry creation then
   succeeded. No image or runtime change was made.
3. The first immediate automated Stop returned HTTP 500 while the session
   remained active. Its response body was not retained by the initial script.
   A deliberate later owned Stop succeeded, and the next launch/Stop cycle
   passed. The cause is not established; neither an empty JSON body nor the
   keyboard-neutralization log alone explains it. This is not an all-green
   lifecycle acceptance claim. Follow-up:
   [FogCast #261](https://github.com/DeanoC/FogCast/issues/261).

## Software checks and evidence

Full FogCast root Go tests passed with a 90-second per-package timeout. Race
tests passed for the changed service/protocol/host API and target/runtime/Kit
packages. Independent review covered invalid/missing protocol rejection and
preservation of provenance. A pre-existing tenfoot test cleanup deadlock was
fixed separately without weakening assertions. FES consistency and selected
host build passed. Parent regression tests passed (403 tests, 36 delegated
skips, with the delegated platform/image checks run by `make test`).

Powerboat evidence is under:

```
/home/deano/fes/out/hardware/browser-library-protocol/
  run-4ckI9m/
  browser-20260918/
  browser-resume-20260918/
  browser-recovered-20260918/
```

This private evidence includes catalog backups; it is not a portable release
artifact. The host service remains autostart-disabled. It is temporarily active
for the operator's physical check and must be stopped and the kit released
after that session.
