# Consolidated development validation — 2026-09-20

Base: `3a58ef102e0dc8836fc9cfe970a8373dbf01028b`.
Tested implementation: `e58bbe878d0c41cff8cf106dafb29d2da40a5ba1`.
Later documentation commits do not change the identity of the tested artifacts.
This is local validation; no remote cutover, remote CI run or permanent image
deployment is claimed.

## Development workflow

The history-preserving import retains all four original component histories as
ancestors and records their trees in `config/source-imports.toml`. First-party
modules have no gitlinks. External compiler and upstream-source locks remain.

Focused parent/module tests, contract generation, HDL simulations, workflow lint
and independent reviews passed. The final ZX81 placement-policy correction
passed 74 focused producer/search, parent-recipe and identity/export tests. It
preserves the original seed order, adds bounded weight fallbacks, and seals the
effective policy; clock requirements and compiler selections are unchanged.

A local two-team branch rehearsal integrated a contract change with generated
Go/C++/RTL consumers and an independent documentation change with zero conflicts
or internal pin PRs. This was one operator simulating two branches; it does not
measure live team lead time or imply a remote PR occurred.

Actual Markdown addition/edit/deletion rehearsals reused identical Pong package,
manifest and RBF bytes with zero producer invocations. A fresh Git transport
clone retained required history without object alternates, passed `make check`,
and authenticated/reused the shared compiler/package cache without copying an
installation or compiling. Post-publication fresh-clone verification remains a
cutover step.

## Built artifacts

All three sealed packages passed their recipe's resource and timing checks:

| Core | Package ID |
| --- | --- |
| Pong | `722d0c008bdecc91fb170002420cceca4ba5403bbdaf6f27f079991f231b3e12` |
| ZX81 | `7e97b75a40c1652baa837d35aa1716a53baf03326240fb7dbfeaa0880d6e169c` |
| Coleco | `1a242c70b2dbbe9db0bd965ff41fc0663e41bb8f605038727d8f170bfae18f7f` |

ZX81 closed at 54.5613 MHz system / 130.5313 MHz pixel. Coleco closed at
54.3596 MHz system / 96.0154 MHz pixel. Their constraints remain approximately
52 MHz system and 74.25 MHz pixel.

`make build` and `make verify` exited successfully. Both independent rootfs
builds have SHA-256
`42b1629917397d241f03a1683fe87595b38e5ce914d42944148e44d6f4768268`.
Structural checks and QEMU packaging smoke passed. The image is not hardware
boot-qualified by this record.

## Exact-package/software hardware diagnostic

The designated kit temporarily ran binaries extracted read-only from that exact
rootfs. Their on-disk and running executable hashes were checked. A private host
and library exercised the exact Coleco package under the existing target lease.

| Artifact | SHA-256 |
| --- | --- |
| Host API | `7b00ef2999891f8f42770e70dc50f07cebbe02b99e44fb8e341e4aba2c4ebf43` |
| Runtime | `a2ea079df6a9a0b0db35a367c42f65835cdc004461dedfccb5ceb088e22a404c` |
| Agent | `bbf61fda0b3126c73d1ff23e291f99ad4a505d09d904cb7b62ee119b6016fa85` |
| Kit | `ac2fefe682ff3e92fce269d115dbf694726bccd25ff8bde1f917bcc291c2a312` |
| Coleco RBF | `6cd6b450ec2e75f36cd3444756ee730c862a8b896416f1d992cde378466ff282` |
| Coleco archive | `d0dd672a754d7994d67a8f8103cbbc58e04577beed720c8ec02c2767ed6c769b` |

32 KiB, 24 KiB, 32767-byte and legacy media launches passed; Stop and 32 KiB
relaunch passed. All five HDMI captures matched 664/664 reference samples.
Input attached, but this is not physical controller/hotplug acceptance. Each
library launch reprogrammed the FPGA; same-generation stale-tail coverage
remains simulation evidence.

An additional lifecycle-only acceptance run produced `qualification.json`.
Its host/agent revisions are the tested FES commit. Its runtime revision is
the unchanged installed-image metadata, `8c4b690964ca2af06581e4cf11df22d33f48e1e0`:
the agent reports its compiled revision while reading runtime identity from
the installed build-input record. Candidate live runtime identity is established
by executable hashes before and after the lifecycle, not that metadata field.
No installed metadata was relabelled to make the check pass.

Initial operator attempts exposed local harness issues: SCP transport, lease
request bodies, disabled remote input, file-bind detection during cleanup, and
the expected health-identity distinction above. They were corrected and reviewed;
failed-attempt evidence remains separate. Original setup restoration was verified
before retrying. These are not hidden successful qualification runs.

Final restoration checked all original disk/live binaries, unchanged boot and
target configuration, ready/idle status and a free lease. Private containers,
configuration and target staging were removed. The authentication token was
left unchanged. Restored agent/runtime hashes are
`4c1863abe55afcd293b60ed25c8d5da989acf475e3caf74c2b5299873ebe48a7` and
`4c7f75680e3ee1be35120dcd42e0cb018b0ad9380577a4aab5c57302ae28958b`.

## Evidence and integration boundary

Local build/test/rehearsal evidence is retained under
`/home/deano/fes/out/dev/development-ease/`; hardware evidence is under
`/home/deano/fes/out/hardware/coleco-consolidated-20260920/`. The final status
report consumes the actual lifecycle and restoration observations: verified
built bytes, recorded package lifecycle pass, and restored binary identities.
Deployment image identity remains unknown where not observed; remote CI remains
unknown until publication. Neither is inferred from a successful build.

The latest pre-publication inventory found unchanged remote heads and no open
PRs in FES or its four former components. This cannot discover unpublished team
branches. The cutover checklist that this record pointed at has been removed.
Import ancestry is already on main. Publication,
cutover tags and repository policy changes are separate from this local record.
