# C64 bounded functional diagnostic

This is an exact-package hardware diagnostic on Kit A, not factory-image,
retail-software or disk-write acceptance. Implementation base: FES `2378bf24`.
The frozen shell was produced from `e90c2213`; probe artifacts below were built
from `c92d70f9`, before the subsequent synthesis-snapshot cleanup safeguard and
documentation changes. No default shell RTL, toolchain lock or shared ABI changed.

## Selected artifacts

- Package: `bda3a02c8bb4bd747de2ee615bd5b635e804d4e5f724272b8d1c11f0418a5d8d`.
- Package archive SHA256: `261f156e927b57a535e2e8085652d8e597c1f4db85951d3d03258c0b61bc2b9c`.
- Shell RBF SHA256: `d43dca1c09f7763b0485af365976998619b02c79f04d3b6d939081c14d1c46db`.
- Socket 1 ROM probe: `1863c2e921505a17351f54bf3f162eab5da5d4c9edc224bb2eb04e6f592545f2`.
- Socket 2 I/O probe: `06076f50260cabed6ac159b4b574e15ba6eeb773c684af11515eefbbb151a184`.
- Full original firmware SHA256: `698f0e61288b9bcb944fd1912abd75d29edae98b3ddca5ed00b16feb7d08193f`.
- Synthetic 174,848-byte D64 SHA256: `2ba3d30f6384121156d2b429ab8d464f437b6524fd0e459135e458dda41756a0`.
- Observed programmed payload SHA256: `73ed6a9a994ccaaa44ed86e8dfe97c96e0265225e3927878dd7b8c7bada41eb8`.

Tools remained C64-pinned Yosys `5391eeb1`, Mistral `8fcc4cb4`, nextpnr
`0c5ed400`. Each card used seed 3 with a 900-second route bound. Earlier failed
attempts were retained, not counted as passing artifacts or performance samples.
Shared GPU activity means elapsed routing times are not comparative evidence.

## Routing and final analogue timing

Both card routes finished normally. Socket 1 changed 3,672 CRAM bits and socket
2 changed 4,099, with zero bits outside their respective fences and unchanged
ORAM/PRAM headers. Containment is separate from timing admission.

Both reports have `final_analogue_model=true`. Values below cover every required
clock, not merely the critical CPU clock; setup/hold are worst slack in ns.

| Clock | Required MHz | Achieved MHz | Setup | Hold socket 1 / 2 |
| --- | ---: | ---: | ---: | ---: |
| System | 52.224 | 53.444496 | +0.437 | +0.729 / +0.679 |
| Pixel | 74.250 | 74.382622 | +0.024 | +0.904 / +0.904 |
| Audio | 12.288 | 225.937637 | +76.954 | +0.911 / +0.911 |

## Host and simulation checks

- `python3 -m unittest tests.test_c64_slot_card tests.test_c64_firmware`: 12 tests pass.
- `go test ./internal/hostapi -count=1`: passes.
- `make sim-fes-c64 VERILATOR=/usr/bin/verilator`: CIA, color RAM and both machine
  cases pass using Verilator 5.032. Machine success at 1,620,917 cycles initially
  ready and 1,817,563 cycles with delayed media and input.
- `make check`: 18 generated consumers, 34 fixture copies, zero copied source
  pins match. `git diff --check` passes.

The cached newer Verilator initially rejected existing CPU declaration warnings;
the passing simulation used the explicitly named system Verilator, not a hidden
warning suppression. No shell reseal was needed for firmware/probe-only changes.

## Kit observation and corrections

Kit A target `73dc9f5f-1a12-4a95-a820-a9b4e600769a` retained agent/runtime
`5a58053229b5e1772defe5f9bdc34c72f659db71`. An isolated diagnostic host used
`51167005c5ad60696d50755788b9ded7d24815d5`; the new host idempotency code was
qualified by host tests, not by this older host binary. No image deployment or
reboot occurred. All programming went through the existing agent/runtime lease.

Launch already creates a ready input stream. The earlier duplicate attach
caused HTTP 500; it was not evidence of failed input hardware. The corrected
runner reuses that stream, confirms disk unit 0 ready, then sends joystick Up
and keyboard HID usage A (`0x1004`, not legacy code 2).

The first combined-card test failed stage 5 despite passing route and timing.
The pinned merger ignored constant OB inputs, leaving EXROM and fixed signature
bits at vacant zero. Aliased input/output nets also caused an undriven I/O
response during an earlier build. Independent identity ALUT2 response drivers
preserve constants and fanout; read-qualified drive avoids driving write cycles.

With the corrected cards, HDMI visibly waits at stage 7, advances to stage 8
after joystick Up, and turns green after HID A. Reaching green follows both
cartridge signatures, I/O scratch, D64 `BOOT` byte comparisons, CIA1 IRQ and
CIA2 NMI checks. The final 48 kHz stereo capture contains 143,914 frames; the
left channel AC RMS is about 1,912 and rising-crossing estimate about 248 Hz,
consistent with the diagnostic's nominal 249.7 Hz pulse, not DC-only output.

Final frame SHA256: `70b9cc0bb47334a4ae891d12c887088534d220642f917980229a967a8cb3682e`.
Raw operator evidence is retained under the isolated remote checkout
`/home/deano/fes/out/dev/codex-c64-functional-2cbef297/out/functional-hil-response-drivers`
and locally under the task worker's ignored `out/functional-hil-response-drivers`.
The synthesis/route/containment reports remain in the separate remote producer
checkout `/home/deano/fes/out/dev/codex-c64-functional-c3913825`.

Stop completed; the normal host was restored active and enabled, the session
idle with input detached, and the kit lease free. The private container and
generated credential configuration were removed.

## Disk boundary and next step

Atari ST's initial library disk is validated and uploaded while execution is
held, before CPU release. Its writable sector/save path also binds durable
records to the game and base media. C64 currently inserts a read-only D64 after
launch. The operator gate fixes diagnostic ordering; it does not implement ST's
pre-release mount or write/save contract. No C64 disk-write capability is claimed.

Review and merge this coherent FES change before considering separate factory
integration. A new image/profile needs its own exact-artifact acceptance; the
historical image and this diagnostic do not qualify it automatically.
