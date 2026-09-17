# FES SMS package-only kit launch/Stop HIL

Status: GREEN. This is package-only physical hardware-in-the-loop evidence
for `fes.sms`. It claims only sealed-package admission, launch, and Stop
lifecycle behavior on the designated kit. No media, input, HDMI, image,
FAT, or appliance diagnostics were run or claimed.

## Scope and exact identities

The tested archive was the user-designated sealed artifact:

| Item | Value |
| --- | --- |
| Core | `fes.sms` |
| Package ID | `c9f2f7d71e77ab6153ddf7224d2beabede6258c1265bb3bfcfa2b5641e1de1b4` |
| BUILD_ID | `7088fdb52c3ea75b636d13d07a46587a` |
| Archive | `/Users/clawzai/tmp/fes-sms-package/fes.sms.c9f2f7d7.fcore` |
| Archive SHA-256 | `f172d94d64f1c0d9301f244cb7c3c671961948b271895ecc8b3f3223f3bdc4b2` |
| Target ID | `73dc9f5f-1a12-4a95-a820-a9b4e600769a` |
| Target address | `http://mister.lan:8182` (`192.168.10.84`) |

The archive manifest reports core `fes.sms`, package build ID
`7088fdb52c3ea75b636d13d07a46587a`, and payload SHA-256
`b95fb1af1825ed62cb7a71f076f0a7ec7aba4d9547584b9cada7fb9bfb03878e`.

## Temporary host and live platform

The isolated-container route was unavailable on this workstation: the host is
Darwin arm64, Docker had no pre-existing Linux image, and no Linux host
executable was available. A new headless temporary API was therefore started
on loopback at `http://127.0.0.1:56888` with a private owner-only config and
private HOME. The config selected exactly one target, the designated target
above. The long-lived `127.0.0.1:8787` API and `fogcast-api.service` were not
used or enabled.

The temporary host binary was built from FogCast revision
`2047f4c258e0ae5efb8eb0315a5225504181703d`; its SHA-256 is
`ea4b5807955b90e671a1aaed532157013f40371db90c4730cee41744ad20f11e`.
The preflight health response at
`/tmp/fes-sms-kit-hil/pre-acceptance-health.json` reported:

| Revision/identity | Observed value |
| --- | --- |
| Host revision | `2047f4c258e0ae5efb8eb0315a5225504181703d` |
| Agent revision | `2047f4c258e0ae5efb8eb0315a5225504181703d` |
| Runtime revision | `f700e342023e21f5917857326a0c533d621905b8` |
| Target boot ID | `fc3efd01-18b4-471c-90eb-b9d6f6d517b2` |
| Target state | ready |

The temporary credential-bearing HOME and TMPDIR were removed after the API
stopped. No persistent host configuration or component pin was changed.

## Lease evidence

The initial `kit.py status` was free, generation
`f8a7189bae7b879c40e43a1bf19b6a2a648a6b33641d0f80542faff9cbf9deb3`. The
run-specific reservation session then held generation
`7af81368542e913fd772b3f383bcba7db0cefad0bcedf1311e330a6c0f7f49f2` for
owner `Caster@ai-dev-mac`, purpose `fes.sms package HIL launch+Stop`, and was
explicitly released. Its release entered `revoking`; the follow-up status was
free at generation
`99862067aeae1387b10e1c68b40ed3bb9ebc5ce1827ca351ad24886bcd10025e`.

The package runner's physical launch/Stop was then admitted through the
temporary host's application-owned lease (`fogcast@ai-dev-mac`). A separate
`kit.py` lease cannot remain held concurrently because the target agent is the
single lease authority and the host must claim the lease for its own mutation
requests. The pre-run status was free. After Stop and temporary-host shutdown,
`/tmp/fes-sms-kit-hil/lease-release-proof.json` reported:

```json
{"state": "free", "generation": "bd70201b8cbe434a24a4d1c7824beb6fda9f8ac40695877d78e21e651b953b0c", "expires_at": "0001-01-01T00:00:00Z", "expires_in_ms": 0}
```

Thus the kit was left idle and the lease was released. The reservation and
release transcript is retained in `/tmp/fes-sms-kit-hil/kit-session.log`.

## Package-only acceptance

The acceptance command used `--execute`, `--new-entry-title 'FES SMS kit HIL'`,
the exact archive/package/core/target identities above, and the live host,
agent, and runtime revisions above. It passed with new library entry
`fpga-fes-sms-kit-hil-24127d519add`.

Receipt: `/tmp/fes-sms-kit-hil/receipt.json`
Receipt SHA-256:
`44f62ee210ca109f5d0872c16f7318901fb51d6be9520eca8d5dd3102d52b46b`

The receipt records `success=true`, `mode=lifecycle-only`, compatible package
admission, a new selection, and one owned session:

- session ID `395f422a-49b5-4d2f-be52-8dbebcf016f5`;
- flight ID `22d08075-e6de-4a57-9cd7-ff1e72fad0b2`;
- generation `1`; and
- the expected package and target IDs.

Stop confirmation reports the same session and flight, target state `idle`,
the expected target ID, and `shutdown_reason=session_stop`. Diagnostics are an
empty list. The runner log is `/tmp/fes-sms-kit-hil/acceptance.log`.

The temporary API emitted `fogcast-api: session cleanup failed` during its
final process shutdown after the successful runner Stop. This did not leave an
active session or lease: the receipt's Stop confirmation is idle, the host
process stopped, and the independent post-shutdown kit status is free with no
owner. No additional hardware mutation was attempted.

## Verification and handoff

The focused package acceptance tests passed:

```text
python3 -m unittest discover -s tests -p 'test_package_acceptance.py' -q
Ran 33 tests; OK
```

The isolated-wrapper unit suite was not the hardware path; its one macOS
path-normalization expectation remains unrelated to this successful temp-API
run. No FES source, parent pin, image, media, or service state was changed.
