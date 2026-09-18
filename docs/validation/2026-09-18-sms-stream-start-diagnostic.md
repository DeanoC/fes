# SMS stream startup diagnostic — 2026-09-18

## Result and boundary

The designated MiSTer kit passed package admission, selected 32 KiB library-media
launch, active-session confirmation, and Stop back to idle after correcting
runtime startup ordering. This is a derived-image lifecycle diagnostic, not a
full reproducible-image or release acceptance. The operator subsequently
confirmed visible output and return to the menu, but gameplay controls produced
no visible indicator changes. Gameplay input acceptance therefore failed.

## Failure and correction

The first coherent diagnostic used runtime `3fe4b914` and FogCast `b606ca0`.
SMS activation failed after video initialization with
`FES GP command rejected with response 4`. The runtime attempted execution
release before FogCast could deliver the selected ROM. The streaming endpoint
correctly requires committed media before release.

Runtime `a6d658cd305c4a84860afc1f8b00a2798ee6e4f4` now neutralizes keyboard
input and explicitly holds reset when stream support has been verified.
Activation establishes the owned package/generation; successful media Commit
permits the existing transfer path to release execution. Legacy startup and
the RTL readiness check remain unchanged. FogCast
`2f75eb68116430e132be6d1a1562c718e4048188` selects this runtime in both its
input lock and host compatibility check. No revision bypass was used.

## Exact artifacts

- Target: designated MiSTer `192.168.10.84`, target ID
  `73dc9f5f-1a12-4a95-a820-a9b4e600769a`.
- Diagnostic image SHA-256:
  `4e02c6fba9a95ae1fae80308463abba00651f4d140eda42c73ce64874ae2e458`.
- Runtime binary SHA-256:
  `0b6275dce5eca4ea03e8d2ed159922f800b7b9c7bba4c6c6db7b7c57d0b184d8`.
- Agent binary SHA-256:
  `8b6a9d15f599ea3ae1a71b859d073964ad0a174eaee941c8d06dbf0e31c1e506`.
- Kit binary SHA-256:
  `6a01f5c15414aa81240ce2a059935cb1d6011b9cc34db9311c210b35fd1ffe0f`.
- SMS package ID:
  `6e172ee279a69d6c7326009c7a8a82ab496e56ce2cb5ac2f2e7e3252d7b194d7`.
- Sealed archive SHA-256:
  `fd7cce133d3fbbef952eebac9425cb71c2438a0730fd9a425a09838531ec28b6`.
- Interactive diagnostic ROM: 32768 bytes, SHA-256
  `411c33162658bf0bba55f5745565ee023c6bb6f5190a57f9a3b3ea5e2c484835`.
- New boot ID: `9202846c-efeb-42fc-95be-462d16414afc`.
- Library entry: `fpga-fes-sms-32-kib-diagnostic-07299fcf7ae2`, titled
  `FES SMS 32 KiB diagnostic`.

## Evidence

Powerboat integration checkout:
`/home/deano/fes/out/dev/library-client/fes`.

`out/hardware/sms32k-resetfix-20260918.B41h81/` retains the explicit artifact
manifest, assembly/extraction checks, filesystem check, deployment receipt,
`run-lifecycle.sh`, and successful `lifecycle.json` receipt. The lifecycle run
completed at `2026-09-18T06:50:49Z`, with retained selected media, generation 1,
flight `46e3ee10-346d-404c-a39a-935dc7ebd1a7`, then idle/detached Stop.
Its empty diagnostics list does not establish video or physical input.

The prior failure and original image are preserved under
`out/hardware/sms32k-20260918.JFKKCM/`. Deployment staged and hashed the new
image, renamed the old live loop image to a unique retained backup before
selecting the replacement, then verified boot and installed binary identities.
Factory package bytes and idle RBF were preserved; SMS remains library-admitted,
not an expansion of the factory image set.

Software checks: failing regression reproduced response 4 before the fix;
focused and full runtime tests passed after it; ARM cross-build passed;
independent review found no actionable issues. FogCast compatibility race tests
and native-runtime smoke tests passed. Parent `make check` and `make host`
passed with the corrected pins. Full-image build/verification remains separate.

## Operator input failure

A second launch reached active generation 2, flight
`5f85f240-8f82-48de-9ecc-ec4b7076e1fb`, with attached input. The operator saw
the diagnostic and returned to the menu, but D-pad/fire caused no visual change.
The final idle session recorded 164 input frames; this transport count does not
prove gameplay delivery. Source inspection found the host controller-to-matrix
translation gated exclusively on `fes.coleco`, while SMS uses the same first
five matrix bits and advertises keyboard rather than gamepad input. A separate
FogCast mapping correction and fresh-image input check are required. Preserve
this failed input result independently of any later corrected-image evidence.

## SMS input mapping correction and derived-image follow-up

FogCast `d9745ed746a1e8d0bde423151d08810248ce8815` extends the existing
controller-to-keyboard translation to exact core ID `fes.sms`, gated by the
verified keyboard interface. D-pad/left-stick directions and Fire1 map to
matrix bits 0..4; SMS Button B remains unmapped rather than becoming a second
fire input. Coleco retains its existing mapping. Runtime remains
`a6d658cd305c4a84860afc1f8b00a2798ee6e4f4`.

Independent source review of this correction found no actionable issue in the
exact-core/capability gate, SMS Button B exclusion, or reuse of the existing
mapped-state lifecycle. Affected tests cover direction/Fire1 press and release,
axis centering, overlapping keyboard/D-pad/axis holds, source reconnect,
Stop/new-generation cleanup, and rejection of wrong core IDs or missing/wrong
keyboard interfaces. `/tmp/fogcast-sms-input-race-final.log` records passing
`host` and `internal/hostapi` race-test results (two result pairs, with the
second hostapi result cached); `/tmp/fogcast-sms-input-related-race.log` records
passing `ui/kitlauncher`, `internal/input`, and `remoteinput` results. These
retained logs are software evidence, not USB-controller or target input proof.

The parent checkout was verified at `0711e75cd223a2c0e9911b78384846919258c7e8`
on `build/sms-media-v2`, with main's staged `sources/FogCast` selection at
`d9745ed746a1e8d0bde423151d08810248ce8815`. Follow-up receipts are retained in
`out/hardware/sms32k-inputfix-20260918.muS1pr/`:
`artifacts.json`, `verification.json`,
`deployment-05cb62d5e3fbb860fe30aaa6.json`, and `lifecycle.json`.

Exact SHA-256 identities from those receipts:

- Derived image `sms32k-inputfix-diagnostic-linux.img` (67108864 bytes):
  `287253fe6582a0918f2d1ab26683b768e77324939ff4719852b5da50709afeaf`.
- Artifact manifest:
  `0807de5a84032b51f24d152d50d1396a973964f9416f26f12550682afbee9a94`.
- Runtime binary (unchanged):
  `0b6275dce5eca4ea03e8d2ed159922f800b7b9c7bba4c6c6db7b7c57d0b184d8`.
- Agent binary:
  `b70a0583364b297ccf8b9f513c7a795862fa9ac4a079cdb34c68f9b5eddcfa51`.
- Kit binary:
  `502732cda68b8d24f4878adcebe81620ef7517f4b274e84d89a78814c71167f4`.

Verification records matching extracted binaries, `e2fsck_exit: 0`, unchanged
original/reset-fix baselines, six unchanged factory package files, and unchanged
idle RBF. The deployment receipt records matching installed binary hashes,
ready health, boot ID `553df27a-48f2-48b2-b68c-896255bdff0a`, and retention of
the prior image at
`/media/fat/linux/linux.img.sms32k-05cb62d5e3fbb860fe30aaa6.before`.
This remains a derived diagnostic, not reproducible release acceptance.

The completed `lifecycle.json` receipt at `2026-09-18T07:07:44.138772+00:00`
reports success in `lifecycle-only` mode: the same SMS package and explicit
32768-byte interactive ROM hash `411c33162658bf0bba55f5745565ee023c6bb6f5190a57f9a3b3ea5e2c484835`
were retained, generation 1 / flight `37b56a16-04af-4e30-9bdd-500033f148eb`
completed the launch/Stop check, and Stop returned idle with detached input.
The receipt has `diagnostics: []` and zero input frames. It does not establish
visible output or physical press/release response for the input-fixed image.

### Corrected diagnostic physical checks — SMS relaunch unverified

Following the lifecycle-only run, main launched generation 2 for the physical
operator check. The operator initially reported "all working", then explicitly
corrected the relaunch result: directions plus A/Fire1 press/release worked,
Select+Start return to menu worked, but the subsequent UI launch attempt failed with
`MISTER_UNAVAILABLE`. The correction supersedes the initial blanket report.
Main visually verified `hdmi-active.jpg` in the same
`sms32k-inputfix-20260918.muS1pr/` evidence directory: checkerboard, green
border, and four central plus-shaped tiles.

The confirmed passing legs are visible HDMI output, one-player directions/A
press and release, and return to menu. The UI launch attempt failed; an SMS
relaunch outcome and full physical acceptance are not established.
Input/menu/UI-error observations are operator-reported; HDMI inspection is
main-reported. They are separate evidence from the generation-1 lifecycle
receipt with zero input frames, not conclusions inferred from that receipt.
No second-player, Fire2, retail-game, mapper, or audio acceptance is claimed.

The earlier startup and input failures remain preserved above as prior-image
results. Full reproducible-image build/verification and release acceptance
remain pending, independently of the unverified SMS relaunch.
This documentation update read retained Powerboat evidence and recorded the
operator/main reports; it did not access or alter the kit/session.

Follow-up log evidence reported by main distinguishes the failed UI attempt
from an SMS runtime relaunch: Stop/menu completed successfully, followed by a
launch with `game_id=pong`, `system=pong`, and missing
`/usr/share/mister-runtime/cores/pong.rbf`. These facts do not establish an SMS
runtime relaunch failure or its root cause. The operator subsequently recalled
that Pong was highlighted ("iirc"), consistent with the logged selection. Record
this failed attempt as Pong, not SMS; the operator recollection remains qualified.
Main subsequently reported successful normal CLI Stop and an exact SMS relaunch
in progress. No result from that relaunch is recorded here. SMS UI relaunch
acceptance remains pending and is not established by successful CLI Stop.

### Final diagnostic UI acceptance

The exact SMS entry subsequently relaunched through the host API as generation
4, flight `e2316234-a234-4416-8dd7-b5e5d88d424f`, with input attached and ready,
without rebooting. After explicitly selecting **FES SMS 32 KiB diagnostic** in
the Kit menu, the operator confirmed: "yep working perfectly, did it 3 times
in a row and worked everytime". This completes the diagnostic physical legs:
visible output, P1 directions/A press-release, menu return and three consecutive
UI relaunches. The earlier Pong selection failure is retained above and is not
an SMS relaunch failure. Full-image build/verification remains a separate merge
gate; this acceptance applies only to the identified derived diagnostic image.

## Full-image follow-up: updated FPGA tools

The first full-image attempt at parent `f6faaef` successfully bootstrapped the
old authenticated HIP toolchains and built Pong/ZX81 packages, but stopped at
Coleco signoff: 48.4004 MHz achieved versus 52.00208282470703 MHz required.
No full-image success or verification is claimed for that attempt.

The next candidate selects merged misteross
`0825da5f277648009c2caa21c591e8b8bbca212a` (PRs #67/#68): ZX81 placement timing
weight 1000, nextpnr `0fad53a75a0218941c417ec6bb58bdede9070987` GPU repair,
and Coleco/SG-1000/SMS Yosys `e2d425dee148cc60c50f4e9b354a10d90eab15f4`
registered M10K correction. Coleco tries a fixed ordered seed list and still
requires final timing signoff. The older Coleco compatibility revision is
replaced, but the core-local toolchain lock is retained: repository-default
Yosys remains `ec34fcf38986217af9b5558936044b7197d968a7`.

Parent consistency passes for this selection. Fresh build/verification is
pending. This compiler/recipe change does not retroactively validate newly
produced FPGA packages on hardware; the working diagnostic kit is unchanged.

### Full-image verification completed

Parent `44c864ab5e56c1913b90ce45fc3fd945a6005a9d` completed `make build`
and `make verify` on Powerboat. Evidence is retained in
`out/hardware/sms32k-inputfix-20260918.muS1pr/full-image.1yUF9p/`.
Both independent image passes produced SHA-256
`6538ca1b5d61729f287a5aaeb5a72c14cf7f8bd6ae03bd87bc874056c65c75d9`;
the published image was independently hashed to the same value.
`verification.json` reports two-pass reproducibility, structural validation and
QEMU packaging all passing. QEMU log SHA-256 is
`934eec984cfcaa9caf3ee9237250d84fc5ccf0de2c10cf40534e41e0a9d03527`.
Historical baseline comparison is not asserted (`null`).

All three factory packages were built with the selected HIP recipes. Coleco
passed with seed 1: system clock 52.559658 MHz versus 52.002083 MHz required,
pixel clock 94.652161 MHz versus 74.250069 MHz required. This clears the earlier
timing failure and full-build landing gate. QEMU is a packaging check, not FPGA
emulation. The full image and its rebuilt FPGA packages have not been deployed;
the physical acceptance above remains specific to the derived SMS diagnostic.

### Subsequent full-image deployment and physical acceptance

The exact verified image above was subsequently deployed to the designated
MiSTer at 192.168.10.84. Boot ID changed to
`1e22fd4a-c72d-4331-8cc3-fd9316d61fc6`; installed runtime, agent and Kit hashes
matched direct read-only extraction from that image. Evidence is retained at
`out/hardware/full-image-20260918.8MiIie/`, including
`deployment-a6985413e27a39c8d85a23cc.json`.
The accepted diagnostic image remains intact as
`/media/fat/linux/linux.img.sms32k-a6985413e27a39c8d85a23cc.before`.

The existing library entries were explicitly moved to the new factory packages:

- Pong: `9be4b59993cfd0ac6b347da42decb340235c570d310901468778d6dad15d0120`.
- ZX81: `af84d2c7fd0ec920beb3214594688d2b17eb5724aba5ac917cfa37f359cc3a1e`.
- Coleco: `61714b569c705d2785e6820caac028ec96f6b5ff25395e39d27a6fdb13c3fc7d`.

Media selections were preserved. SMS package and 32 KiB ROM remained unchanged.
The factory harness passed launch, input delivery and Stop for all three cores;
`factory-captures/acceptance.json` records those legs. Its initial captures were
black, so those frames are not video proof. A later settled SMS capture
(`sms-settled.jpg`) visibly showed the expected checkerboard; the SMS lifecycle
receipt also passed. The Kit UI was restored after automated testing.

The operator then confirmed SMS display, input and return to menu, followed by
all three factory cores working, explicitly including a physical USB keyboard
for ZX81 and USB controller input for the others. This completes visible output,
physical input and menu-return acceptance for these exact packages on the full
image. Three consecutive SMS UI relaunches were established on the earlier
diagnostic image; that repetition count is not claimed for this full-image run.
No new SMS FPGA package, SG-1000 test, retail-ROM/mapper coverage or release
publication is implied.
