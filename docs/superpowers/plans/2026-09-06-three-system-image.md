# Three-system image implementation plan

**Goal:** Assemble Mega Drive, Pong and basic SNES through the normal selected image recipe, with verified artifact identities.

**Design:** Extend misteross's existing format-1 RBF bundle to its existing Pong and SNES build lanes. FES selects all three and FogCast's existing native image recipe installs and verifies the selected set. Keep the Mega Drive-only standalone and historical paths usable. Do not migrate image assembly or extend runtime support.

**Constraints:** Preserve frozen diagnostic artifacts and worker trees. Keep sources clean and build selected commits. Use the installed Quartus 17.0.2; SNES seed 3 is an explicit recipe choice and exports require passing timing. Pong provenance identifies the committed local source tree and validates framework/local input hashes. Do not relabel a diagnostic override as a normal build.

## Component work

- [x] misteross: add tests for Pong/SNES export and timing rejection, implement strict export validation, explicit SNES seed, build the selected recipes, and document the artifact contract.
- [x] FogCast: add selector/packaging tests for optional additional cores, install complete selected sets with per-core records, verify image bytes and reproducibility records, preserve default Mega Drive-only operation.
- [x] FES: extend bundle validation and orchestration to the profile's core list, reject wrong source/recipe or incomplete sets, publish all bundle/selection artifacts, sanitize core-selection environment variables.
- [x] Select reviewed local component commits; run parent and affected component checks. Build the assembled image using the authoritative child recipe and record structural/reproducibility results.
- [x] Claim the designated kit before hardware use; verify the exact assembled artifact through Pong/SNES/Mega Drive launch, input, Stop and switching. Preserve limitations and any unavailable checks in the handoff.

The parent integrator owns FES and kit operations. Independent workers own misteross and FogCast image packaging. No worker deploys or changes parent pins.

## Progress

- Parent tests: 29 passed. Independent parent/producer/consumer reviews found no blocking issue.
- FogCast full `make test`: passed at `02378114c25a4da26e424630b22146a88249cd4e`; Chrome integration fixture skipped because Chrome is unavailable.
- Fixed the container cache key's accidental dependence on absolute checkout paths, with a regression fixture proving identical inputs share identity across two locations.
- SNES normal seed-3 build passes timing and exactly reproduces the prior diagnostic RBF hash `fdd6d3c51cf3662cb59c5250eee8d4aa48fdab14a272c756fb892677d5ff1226`.
- Waited for the other task to release its lease, then claimed the free kit for deployment. No takeover was used.
- Mega Drive rebuild also reproduces its prior hardware-tested artifact (`195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e`). Its existing negative timing slack remains: setup -3.114 ns/TNS -480.181 and another -1.214 ns/TNS -69. The new strict timing gate covers Pong/SNES; Mega Drive retains its existing artifact qualification and must not be described as timing-clean.

- Normal image build and verification passed: both cold passes produced `9af1a0140acb36a689e9de9101806d1ad6f879a7cbc8e307aca9df7b6b3bea03`; structural and QEMU packaging checks passed. The deployed first-pass bytes match the final verified image exactly.
- Warm `make dev` reused the verified Buildroot tree and compiled only the runtime package before assembly; the subsequent unchanged invocation reused all three checked FPGA bundles and reported nothing to rebuild.
- Exact-image hardware acceptance is recorded in `out/three-system-image-evidence/acceptance.json`; switching/audio passed on one boot, extended gameplay was visually confirmed after a separate reboot with the same installed image hash. Kit released and ready.
