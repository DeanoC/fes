# Coleco 32 KiB media integration

Validated 2026-09-19 on FES base `d66016d`. This integration selects the
merged Coleco streaming implementation and adds its shared golden fixture to
parent consistency checks. The legacy blob limit remains 16 KiB; stream-enabled
Coleco packages support up to 32 KiB through the existing application ABI.

## Integration handoff

Selected component revisions:

| Component | Merged revision | PR |
| --- | --- | --- |
| misteross | `10eaac1d24b5614f15530b7eb4e953dad7bff1f4` | DeanoC/misteross#77 |
| libmister-runtime | `079b4548d5ab84c094686e00df4fbbe658f4f613` | DeanoC/libmister-runtime#29 |
| FogCast | `e07aec6547caefdcb0c74bd0e1b61c892e9444c9` | DeanoC/FogCast#288 |
| mister-packages | `41f4d9406955bed7abf318e1a14fde44e500dc92` | DeanoC/mister-packages#13 |

FogCast's runtime lock already selects `079b4548`. Packages changes are
documentation only; there is no schema or ABI version change. This isolated
FES worktree also adds the shared application stream golden fixture to parent
consistency checks (20 generated consumers, 20 fixture copies, 4 source pins).
Parent consistency and doctor checks pass. A fresh `make test`
passes 403 parent tests (36 skipped), boot-selector tests and image-script
tests; the final log is `../parent-tests-final.log`.

Both complete Coleco simulation lanes, shared demos/audio, 85 producer tests,
14 diagnostic tests, runtime tests, host tests/race checks, host CI and 13
parent consistency tests passed. Explicit worker-map consistency passed.

Authenticated HIP signoff passed: system 54.66/52 MHz and pixel 81.26/74.25
MHz. Exact tested pre-squash misteross `8b141c3` produced package
`95a0f0fbb3c2b15b9a3cbfa3ac44e7dd1d7993ef49fd6904931e5ad0a7127f7a`,
RBF `a50b16859a2639ca00ca4df437eafa93cb59b3ae654935688bab63044dbac6b0`.
Matched host `3992f4f` and runtime `90fc85a` were temporarily installed.
32 KiB, 24 KiB, 32767-byte and legacy library launches, Stop and 32 KiB
relaunch passed on the designated kit, with 664 HDMI reference points per
frame. Same-generation stale-tail reload coverage comes from simulation;
hardware library launches each reprogrammed the FPGA.
Merged FogCast also includes separately developed tenfoot/rooms changes beyond
the tested commit; the affected host/agent/protocol/adapter sources match.

The installed binaries were restored and hashes checked; target ready/idle,
lease free, boot unchanged. Temporary host/config/target files were removed.
Evidence: `/home/deano/fes/out/hardware/coleco-stream-32k-20260919/README.md`.
This is package-level diagnostic evidence, not new integrated image acceptance.

## Selected-revision build

The parent producer built all three selected HIP packages. Coleco now has
package ID `e4c7a43361bb11258fdf94b28655bd72280589483df4de077eeb30cca5b902bd`
and RBF SHA256
`a5bf9097ca77d88247c542c8e450c7633f27a2f03ad461cdf72d1afdaf2304e0`.
The first placement missed system signoff; the next configured placement
passed at system 53.775/52 MHz and pixel 88.5504/74.25 MHz.

A separate exact-package hardware diagnostic using merged host/agent
`e07aec6` and runtime `079b4548` passed 32 KiB, 24 KiB, 32767-byte and legacy
cartridges, Stop and 32 KiB relaunch. Each settled HDMI capture passed 664
reference samples. The first immediate capture was black and failed; the
runner stopped cleanly. Subsequent checks used a fixed settling interval.
Evidence, including that failure and the successful captures, is retained at
`/home/deano/fes/out/hardware/coleco-stream-merged-20260919/README.md`.
Original kit binaries were restored and verified ready/idle/free, boot
unchanged. This validates the selected Coleco package with temporary binaries,
not the assembled image or the separately rebuilt Pong/ZX81 packages.

Plain `make dev` initially stopped at compiler-cache authentication: cache
slots prohibit symlinks and are non-relocatable. The external diagnostic
driver `../run-parent-dev.py` invokes the same parent `dev` entrypoint with
each recipe's explicit cache root pointing to the existing authenticated
`/home/deano/fes/out/cache/misteross-toolchains`. It changes no compiler checks,
recipe inputs or tracked builder code. The attempted copied cache was removed.
The image recipe's changed base key requires a fresh Buildroot base.
The first build driver received SIGTERM during filesystem finalization while
its container continued and exported the image. After that container exited,
the same incremental command resumed for parent validation and publication.
Logs: `../parent-dev.log`, `../parent-container.log`, `../parent-dev-resume.log`.

The resumed development build exited 0 and published
`out/native-integration-dev/development/linux.img` (67,108,864 bytes), SHA256
`17ff6e4d721f34c2f77ec12e852aece1593abf68e44f4625e9b07472a9e400a5`.
Its `development.json` receipt binds the image, manifests, package selections
and installed-file report. Structural and package validation passed. The
first export and resumed export match, but these are incremental assemblies;
the receipt correctly records cold two-pass reproducibility as not run.
`make host` also exited 0 and published the matching Linux amd64 CLI/API
binaries and `host.json`. All files named in the host and development receipts
were rehashed and matched their recorded digests.

## Remaining release validation

Cold `make build` / `make verify` and exact-image boot acceptance have not been
run for this integration. The rebuilt Pong and ZX81 packages have compiler and
structural evidence, not fresh hardware acceptance here. The selected Coleco
package has the bounded hardware evidence described above. No persistent kit
image was deployed.
