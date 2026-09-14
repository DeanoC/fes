# FES package-set verification — 2026-09-14

## Scope

This validation covers the FES-first package-set change on
feat/fes-package-set in /home/deano/fes/out/dev/fes-package-set/fes.
The default native-integration-dev profile selects this ordered set:

~~~text
fes.pong
fes.zx81
fes.coleco
~~~

Each FES format-2 producer is routed through HIP with
gfx1100;gfx1201 and the shared out/cache/misteross-toolchains cache root.
The package-only image uses the exact selected package set and its receipt
fingerprint. Quartus was not run; its role remains bring-up/check-oracle
coverage where nextpnr does not yet support a system. No physical hardware
acceptance is claimed here.

## Verification results

| Check | Result |
|---|---|
| make check | PASS — package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match |
| PYTHONDONTWRITEBYTECODE=1 python3 -m py_compile scripts/*.py | PASS |
| PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tests.test_media | PASS — 62 tests |
| PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p 'test_*.py' | PASS — 289 tests, 36 skipped, 49.282s |
| GOCACHE=$(mktemp -d /tmp/fes-task4-go.XXXXXX) FOGCAST_DIR=$PWD/sources/FogCast make -C image test | PASS — target-image package-only, input, source, development-container, rootfs and kernel checks |
| git diff --check | PASS |

The full Python suite was first run after the profile change and correctly
found a stale one-package tests/test_media.py fixture. The fixture copied
the three-package repository profile but created only Pong, so strict image
receipt validation rejected it. The fixture now creates and fingerprints the
ordered Pong/ZX81/Coleco set, and retains dedicated single- and two-package
cases for compatibility and ordering tests.

## Cache evidence

An isolated resolver harness exercised one miss followed by one exact hit for
each package descriptor. The producer call is allowed once for the miss and
is prohibited on the second resolution; the second resolution therefore
proves reuse of the validated package record rather than merely returning a
successful result.

The harness was fixture-only. It created no producer checkout, so the
producer and mister-packages revisions below are explicit synthetic fixture
revisions, and the package IDs are explicit synthetic 64-hex identities. The
paths are the exact isolated package stores and shared toolchain cache root
used by that run:

~~~text
lane=fes.pong
producer_commit=cccccccccccccccccccccccccccccccccccccccc
mister_packages_commit=dddddddddddddddddddddddddddddddddddddddd
package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
package_store=/tmp/fes-task4-cache-evidence-8e3c427-v2/fes.pong/build/packages
toolchain_cache_root=/home/deano/fes/out/dev/fes-package-set/fes/out/cache/misteross-toolchains
selection=/tmp/fes-task4-cache-evidence-8e3c427-v2/fes.pong/fes-pong.package-selection.toml
manifest_sha256=30cdd2c5342fe74b520dd7a80623f7fd39a642689d05ac784997986b4c762509
core_rbf_sha256=9244e404a1b6c35eb95693af37b234c1ddfe4134e7eb011fc198c580ee3e0976
router=HIP miss_builds=1 hit_builds=0
yosys_nextpnr_hip=not-run quartus=not-run qemu=not-run
image_receipt_sha256=NOT-GENERATED

lane=fes.zx81
producer_commit=cccccccccccccccccccccccccccccccccccccccc
mister_packages_commit=dddddddddddddddddddddddddddddddddddddddd
package_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
package_store=/tmp/fes-task4-cache-evidence-8e3c427-v2/fes.zx81/build/packages
toolchain_cache_root=/home/deano/fes/out/dev/fes-package-set/fes/out/cache/misteross-toolchains
selection=/tmp/fes-task4-cache-evidence-8e3c427-v2/fes.zx81/fes-zx81.package-selection.toml
manifest_sha256=dd84b31950eff507cc9e6718c63c7eea824206a025208080ae3c6089e49ef6ac
core_rbf_sha256=a34fafa320a23efe75bfb2205367c5fafb2a5b9cd70c95ccec0b103bb4bcc7ba
router=HIP miss_builds=1 hit_builds=0
yosys_nextpnr_hip=not-run quartus=not-run qemu=not-run
image_receipt_sha256=NOT-GENERATED

lane=fes.coleco
producer_commit=cccccccccccccccccccccccccccccccccccccccc
mister_packages_commit=dddddddddddddddddddddddddddddddddddddddd
package_id=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
package_store=/tmp/fes-task4-cache-evidence-8e3c427-v2/fes.coleco/build/packages
toolchain_cache_root=/home/deano/fes/out/dev/fes-package-set/fes/out/cache/misteross-toolchains
selection=/tmp/fes-task4-cache-evidence-8e3c427-v2/fes.coleco/fes-coleco.package-selection.toml
manifest_sha256=7f1db8a0210df9933af1efde81278790744e856d57269ff9075befc4493feccf
core_rbf_sha256=65e74c0494e9ec58bbad3d922afdce576952cf52ea57c875533f8267103a4e39
router=HIP miss_builds=1 hit_builds=0
yosys_nextpnr_hip=not-run quartus=not-run qemu=not-run
image_receipt_sha256=NOT-GENERATED
~~~

For this cache-only harness, Yosys/nextpnr HIP execution was not run; HIP is
the asserted recipe identity only. Quartus was not run. QEMU was not run by
the resolver harness. No parent image was assembled, so the exact image
receipt SHA-256 for each lane is explicitly not generated, rather than being
inferred from a cache hit. The separate image shell suite passed as recorded
above, but supplies no physical-target acceptance.

This is resolver/fixture evidence for cache selection and reuse. It is not a
claim that a cold misteross build, Quartus compile, full release image, or
hardware run was performed.

## Acceptance boundary

The parent contract, target-image package-only contract, documentation and
tests are green. Real cold package builds, a release image, nextpnr hardware
timing closure, Quartus oracle checks and physical-target acceptance remain
separate validation lanes.
