# SMS stream startup diagnostic — 2026-09-18

## Result and boundary

The designated MiSTer kit passed package admission, selected 32 KiB library-media
launch, active-session confirmation, and Stop back to idle after correcting
runtime startup ordering. This is a derived-image lifecycle diagnostic, not a
full reproducible-image or release acceptance. Physical HDMI/controller and
operator Stop/relaunch checks remain pending.

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
