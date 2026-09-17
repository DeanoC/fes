# Herd shared-fixture update — REQUIRED SMS sim test

HOLD: do **not** refresh `cores/fes-sms/generated/stream-exchanges.json` until Herd shared fixes land (stronger portable vectors: two-chunk golden, CRC across chunks, ordinal reset, repeated prior-chunk rejection, fixture metadata validation, explicit SMS long→short tailFF). Wire opcodes unchanged.

**REQUIRE for this branch (do not wait on Herd fixtures for this):**
`make sim-fes-sms` loads a 32 KiB non-0xFF image, then a shorter image, and
checks that unused mapped bytes `N..0x7fff` read `0xFF`.

Continue LIVE RTL/sim/package against canonical media-stream.md. Package-only / off kit. Powerboat sync + additive .vh regen already done.
