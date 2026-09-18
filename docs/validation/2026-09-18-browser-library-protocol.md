# Browser library and protocol compatibility diagnostic

## Scope

FogCast #258 adds browser package/media management over existing host APIs.
FogCast #260 replaces live Git-revision equality with target API `v1`
admission. Exact source pins, image locks, artifact hashes and release receipts
remain provenance. Package ABI, native protocol, input, persistence and media
limits retain their operation-specific checks.

No FPGA compilation, agent/runtime/Kit replacement, image rebuild, SD write or
reboot was performed for the initial browser diagnostic below. A later host/Kit
diagnostic deployment is recorded separately at the end. This is not image reproducibility or
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
The initial parent candidate selected `c8deb0fb4a2ebd3a55ba476d18d9fc9c65012579`,
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
At this stage physical display/controller/Kit-menu acceptance was pending;
the later operator confirmation is recorded below. Catalog persistence alone
does not establish visible rendering.

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

## Later diagnostics and latest component selection

FogCast #260 and #263 are merged. On the diagnostic host/Kit build containing
the #263 changes, the operator confirmed controller launch of the browser-created
Pong entry, return to menu and relaunch. The correct entry ID also reproduced an
admission failure while the target retained an earlier runtime error; explicit
Stop cleared it. No wrong-entry dispatch was demonstrated.

Deployed diagnostic binary SHA-256 values:

- Host: `0288a8bda3e826e545c37e5106d10045c10661ad609b0f06991232059410d160`.
- Kit: `8e869915c18963181c947bb4eadeed600e9e09468b95d6463cde40d368a4c483`.

The Kit binary is a temporary bind-mounted overlay that disappears on reboot.
The original image, target agent, runtime and FPGA packages were unchanged.
Evidence and rollback notes are in Powerboat
`/home/deano/fes/out/hardware/menu-launch-errors/deploy-bdB1Da/`.
Immediate automated Stop reproduced #261 once on this build; explicit recovery
Stop and a subsequent launch/Stop succeeded. #261 remains unresolved. The new
retained-error wording has regression coverage, not deliberate deployed fault
injection. The two diagnostic follow-ups remain FogCast #264 and #265.

The refreshed integration candidate selects:

| Component | Revision |
| --- | --- |
| FogCast | `76179c6` (latest merged #263 plus runtime-lock alignment, #266) |
| libmister-runtime | `8c4b690964ca2af06581e4cf11df22d33f48e1e0` |
| misteross | `ea1fd3e488dfee7a6e056b2a871a4dbf835fa3d8` |
| mister-packages | `fdc4ece2e1fa87035ddca8cd147c621e7edcce3b` |

The runtime merge commit has the identical Git tree as the previous `a6d658c`
pin. FogCast #266 aligns its lock and provenance constant with that merge commit.
misteross now includes the merged recipe-clock scoring, Coleco/ZX81 placer search
and two-HIP-device search changes. No new FPGA compilation, image build or
exact-artifact hardware acceptance is claimed for this refreshed combination.
