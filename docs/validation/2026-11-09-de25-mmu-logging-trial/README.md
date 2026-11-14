# Bounded late-MMU decode-recording test

The new matched image is `2ed0757748552e49`, built from the
[contained format6 diagnostic](../2026-11-09-de25-mmu-logging/README.md).
Its final FIT is 8387136 bytes, within the unchanged 8388608-byte seal/upload
limit. The initial oversized builds were never uploaded and remain retained.
This preparation does not claim hardware qualification.

The frozen procedure selects the exclusively assigned top TRWU8CBL and its
installed spare card. The factory card remains aside, the bottom kit is
untouched, and the user's factory-restore waiver remains in effect. The prior
58a43a2afe6818d7 image and all earlier consumed operations remain retired.

There is one attempt with separate durable recovery/native reservations and
fresh scans, the qualified executable probe, original 6M test/15M restoration,
thirteen verified uploads and fresh SD reads. The three-launch/four-menu/seven-
configuration exercise runs only if complete native boot and memory proof pass.
The first failure stops commands, retaining only the original two-second passive
tail and independent cleanup. The 450-second native, 800-second outer and
890-second host caps and the one-second tape deadline remain unchanged.

The selected logging control requires a zero baseline and fresh CPU/MMU/master
scope; only decode detection is enabled. Normal or terminal cleanup checks
restoration to0 and preserves sticky status. Missing, interrupted or failed
restoration cannot grant admission. Reliable core loading and full appliance
acceptance remain open until physically verified.

The [physical result](hardware-result.json) now records the terminal SError and
a new CCU decode syndrome at the interrupted OCRAM stack address. The
[field extraction](status-diagnosis.json) and [analysis](analysis.md) distinguish
the logged transaction from the unproven exact instruction. Control restoration
and independent host cleanup pass. This image is consumed and retired.
