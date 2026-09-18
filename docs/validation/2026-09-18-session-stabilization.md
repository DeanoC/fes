# Session stabilization diagnostic

FES selects FogCast `88a2307` for explicit Stop admission and diagnostic fixes
(FogCast #267). Runtime, misteross and shared-definition pins are unchanged.

## Failure and fix

An instrumented host reproduced the immediate Stop HTTP 500 on the designated
kit with `error.stop_stage=admission_backoff`. The host rejected Stop before
dispatch because background discovery backoff remained active. Evidence was
saved before an explicit recovery Stop. Explicit Stop now makes fresh admission
checks instead of honoring that timer; protocol, identity, ownership, status
and timeout checks remain. Pending-package recovery uses the same explicit-Stop
exception; activation cleanup does not. No hardware mutation is replayed.

The fixed host passed launch and immediate Stop with HTTP 200 active then idle.
Regression tests reject wrong target identity and unsupported API without any
Stop mutation. Affected service, host API and Kit race tests passed, as did
independent source review. UI reconnect clears labeled Host unavailable errors;
valid runtime quiesce/programming phases remain visible.

## Persistent diagnostic image

A copy of the known-good image was updated with only the Kit binary and its
build-input hash. This is not a full rebuild of current FES component pins or
release/reproducibility acceptance. The actual agent, runtime, idle and FPGA
packages remain those of the baseline; their provenance is not relabeled.

- Baseline image: `6538ca1b5d61729f287a5aaeb5a72c14cf7f8bd6ae03bd87bc874056c65c75d9`.
- Derived image: `02fef352cc9209d6176d42701c7eef091cda4b24820967f573dd4d233a84d1c3`.
- Kit binary: `76d01df0ddd60ae643a1666ff5976c2bc5bb2062ac187e8107b97a4ea217b93c`.
- Host binary: `38aa60af39587465df3c68e8144813bbd167fc714be793a4ef614b103ec5094e`.
- Agent binary: `b70a0583364b297ccf8b9f513c7a795862fa9ac4a079cdb34c68f9b5eddcfa51`.
- Runtime binary: `9608cc234d8644ddd645544a493556c54b39126db25396c83d80763c7368fd38`.
- Target ID: `73dc9f5f-1a12-4a95-a820-a9b4e600769a`.
- New boot ID: `af78e27d-d28e-4a87-989b-d4c18a1896a7`.

Extracted bytes, root-owned executable mode, filesystem check and post-reboot
image/binary hashes passed. The host reports target ready. No executable overlay
remains; the Kit change survives reboot. After this reboot, the operator confirmed
three consecutive controller launch/return-to-menu cycles for the browser-created
Pong entry. The Kit dispatch/result log independently records all six operations
as HTTP 200 for the exact entry ID, and final host session status is idle.

The old image is retained on the card as
`/media/fat/linux/linux.img.before-session-stabilization`. Rollback is an explicit
idle maintenance operation: preserve the current candidate, restore that exact
baseline as linux.img, sync and reboot. Do not overwrite a mounted backing inode.
After physical acceptance, the host service was stopped (inactive/dead),
autostart remained disabled, and the target lease was verified free. The
exclusive reservation was released to the coordinating team.

Private evidence and source snapshots live on Powerboat under
`/home/deano/fes/out/hardware/session-stabilization-N3jJhn/`.
