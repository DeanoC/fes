# Integration validation, 2026-09-05

This record covers the integration-entrypoint working tree based on FES
`5f3f6c5`, branch `feat/integration-entrypoint`. No component implementation was
changed; the parent selects already-merged component commits. Subsequent
incremental-build work is recorded in
[incremental build validation](incremental-build-validation.md).

| Component | Selected revision |
| --- | --- |
| FogCast | `1adc7c33da9355e4dc884fcb9f63a6b3e4361924` |
| libmister-runtime | `4398f41bf504329e5c9b21f916cb37952bfb4cc7` |
| misteross | `7912a3e7ee82a24c9aed82dfbb30a8974b7eec07` |
| mister-packages | `a29f63158079ab54856ac731308088b40d75904c` |

## Software checks

- All 18 parent tests pass, including historical revision selection, generated
  drift, source-pin drift, recipe mismatch, ambient Go settings and repeated
  publication of read-only selection records.
- `make check` validates the real package inputs and regenerates all three
  consumer files byte-for-byte. The two copied upstream source pins agree.
- `GOOS=windows GOFLAGS=-invalid GOWORK=/wrong make check` passes after shared
  environment normalization; before the fix, the emitter failed to execute.
- `make doctor` passes for all three profiles. Historical profiles select the
  original FogCast/runtime pair despite the newer current gitlinks.
- FogCast tests pass for `internal/misterruntime`, `internal/agent`,
  `internal/targetimage` and `internal/systems`. All mister-packages Go tests pass.
- The runtime `make test` and package `make test` (including package oracle
  comparisons) pass. Two concurrent FogCast worktrees were created using the
  documented workflow, leaving the integration checkout clean, then removed.
- Independent review identified environment leakage and read-only publication;
  both were reproduced, fixed and re-reviewed with no remaining blocker.

The CI workflow is present but has not run on GitHub. Its component job requires
repository secret `FES_COMPONENTS_TOKEN` with read access to the parent and four
private components. No credential has been installed or copied into CI here.

## Build validation

`make build`, `make verify`, and a subsequent unchanged `make build` passed.
Both clean native image passes produced SHA-256
`670ed8d9707728afcbbab1724aef019f6a137fa2470718c8f1677377f4f7b998`.
Structural validation and QEMU packaging smoke passed. The final repeat reused
both host and image outputs without compiling anything.

- Runtime SHA-256: `72dd916858b82590b7126238dd714565cbd6130ee09d79fc9accfb8cd9e4ea64`.
- Agent SHA-256: `13d35957f6844b50d7af5dc8d03b23f84f6ef1a59991fabd869e27d1e74bfad7`.
- Source RBF SHA-256: `195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e`.
- Recipe SHA-256: `3407571e63834158c71cbdcb891cde885b249bbc52a418f55f940ac95b4f6064`.

The source bundle was reused from the same pinned misteross revision after
validating its payload and recipe digest. This run does not claim a fresh
Quartus compilation. Existing dated FPGA compilation evidence is in
[source build validation](source-build-validation.md).

The earlier exploratory image run was deliberately stopped after review changed
the parent recipe; it is not counted as final build evidence. The final cold
build used the reviewed recipe. An attempt to reuse the previous QEMU kernel
cache missed its full host-toolchain key, so QEMU's test kernel was rebuilt.
The separate deployed MiSTer kernel is not built by this root-filesystem recipe.

Logs in this checkout: `out/parent-tests.log`, `out/component-tests.log`,
`out/runtime-tests.log`, `out/package-tests.log`, `out/integration-final-build.log`,
`out/integration-verify.log`, and `out/integration-reuse.log`. Published receipts,
normalized selection, manifests and QEMU log are in `out/native-integration-dev/`.

## Bounded hardware validation

Deployed this exact image to the designated kit at `192.168.10.239`. Installed
image, runtime, agent and RBF hashes matched the published outputs. The target
reported ready, with FPGA manager operating and without Main or its FIFO.

On boot `a4e22e63-0921-4953-8d11-426627ff25d2`, the public host API completed:

1. Sonic 2 launch, Start, movement and jump through the production remote-input
   path. Inspected HDMI captures show Emerald Hill and airborne Sonic.
2. Game Stop to idle, upload of the exact published RBF, and an active
   `fpga_development` session without game identity.
3. Development Stop to idle and a subsequent normal game launch on the same
   boot. Start, movement and jump worked again; inspected captures show gameplay.
4. Final game Stop to idle. The inspected settled capture shows the black/white
   static raster described by the component's native idle baseline.

Input helper metrics reported no sequence gaps or resyncs. The component's
`native-runtime-smoke.sh` passed against the published selection record, including
reboot from the above boot to `35dc0815-d412-44dc-8d6b-592b0ad29872` and return to
ready idle. The temporary validation host API on port 8789 was stopped afterward;
the pre-existing host service was left running.

Evidence is under `out/hardware-evidence/`: `installed.txt`, `new-health.json`,
`before-play.log`, `after-play.log`, gameplay/movement PNGs,
`development-load.json`, `development-stop.json`, `after-development-health.json`,
`final-game-stop.json`, `final-idle-005.png`, and `native-runtime-smoke.log`.
The initial control launch exceeded a ten-second client timeout but completed;
state was checked before proceeding. A single immediate capture after development
Stop was black and does not establish visible intermediate idle; API state,
subsequent gameplay and the settled final idle capture are the evidence above.

This is bounded lifecycle and input evidence, not audio, soak, every ABI, or
complete hardware acceptance. The complete bootable-media assembly migration
remains a separate next task.
