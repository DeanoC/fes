# Coleco sprite line-bank read correction

CrackPots v1.0 exposed missing vertical columns in player and insect sprites
on the MegaCart Coleco package. The registered sprite walker advanced its
column or magnified repeat while remaining in its write state. The next
priority/collision decision therefore used the previous address's RAM data.
Returning to the read state for each pixel removes that dependency on stale
metadata and same-port read-during-write behavior.

- [Before: gaps through the player and insects](coleco-sprite-read-2026-09-26/before.png).
- [After: complete sprites during gameplay](coleco-sprite-read-2026-09-26/after.png).

## Source and artifact identity

Base FES commit: `5ef46ff31746d7569c52e8e56ac51a2485a0cc3b`.
The feature was tested as an uncommitted diff on `fix/coleco-sprite-read`.
`scripts/dev_snapshot.py` captured the candidate as development-only source
`00d2db0ac2ccddece96eca1262782074494eee25`; the normal MegaCart producer ran
against that frozen source on GPU 0 using the existing locked tools.
This is diagnostic evidence, not committed integration or appliance acceptance.

- Package: `2fd36e75d2ca4a35c5182ef4f2536a95ef7fa83d2815598cd989f7c7f3b04e87`.
- ROM map: `4fc795387ccaaec552a8c8e0581544ac2b9c19f348e650279f275b6981802af1`.
- Programmed RBF: `206ceafd63da2f3aecce97a01d6d8132ec3122a158be6933984ac7af5ea7c542`, 2,811,432 bytes.
- Host/agent/runtime/kit diagnostic binaries: the verified artifacts from
  `40c30b09b6af0f5b9c4af444ff519bc0e56220bf` used in the
  [two-ROM diagnostic](2026-09-26-coleco-megacart-two-rom-hil.md).

The author's [CrackPots v1.0 download](https://electric-dreams.itch.io/crackpots)
is 131,072 bytes, SHA-256
`86dad62fd125f1e10fa6b26536a3712d14e061c63c435f85d84d6f462def3351`.
The privately supplied BIOS is 8,192 bytes, SHA-256
`990bf1956f10207d8781b619eb74f89b00d921c8d45c95c334c16c8cceca09ad`.
Neither ROM is included in this repository. No SGM was selected.

The GPU route passed on seed 3 / HeAP weight 2000. Final system, pixel and
audio clocks reached 53.1096, 106.1008 and 169.2334 MHz respectively, exceeding
52.2248, 74.2501 and 12.2881 MHz constraints. The producer also validated the
reserved expansion socket and blank ROM map.

## Validation

The existing OSS unit suite passed before the change. The new regression
failed the old RTL at `size=8 scale=1 x=40 expected=1 actual=2`, reproducing
stale metadata corrupting priority. It passes with the fix and checks every
column of a full line for 8x8/16x16, normal/magnified sprites, separated
foreground pixels, solid background sprites and transparent overlaps.
The full changed-file suite passed all 23 selected commands, including normal
and OSS Coleco board cases, SG-1000 normal/OSS/linked-ROM paths and SMS
normal/OSS tests. Parent regressions passed 569 tests with 37 expected skips;
working-tree consistency checked 12 generated files and 27 fixture copies.
Independent review found no blocker; the worst-case walker remains below its
800-clock budget within a roughly 3,300-clock logical scanline.

Physical observations on the designated kit:

- Intro, title, instructions and game scene render.
- Player and insect sprites no longer exhibit the captured column gaps.
- Injected left/right move the player; Fire drops pots and score reaches 30.
- HDMI audio capture has nonzero 48 kHz stereo output. Listening quality and
  physical gamepad operation were not assessed.
- The target's exact programmed receipt matches a separate host-side ROM link.

Both launch/Stop cycles passed with identical ROM-link receipts. Original
on-disk and running service hashes, boot identity and target configuration were
restored; the kit finished ready, idle and free. Private host, target staging
and credentials were removed. Full shared-core simulation results are recorded
in [the evidence](coleco-sprite-read-2026-09-26/evidence.json).
The installed appliance image and factory package selection are not changed.
The shared VDP change affects Coleco, SG-1000 and the SMS legacy TMS path;
there are no ABI, schema or generated-consumer changes.

This hardware diagnostic uses a development snapshot; committed-source
consistency is a separate gate run after committing the reviewed feature.
`make check` correctly refused the development snapshot and dirty module.
No cold image, factory promotion or appliance acceptance is claimed here.
