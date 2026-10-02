# Coleco video-parts seal and software diagnostic

This records host-only evidence for the first `fes.coleco-video.parts/1`
layout on 2026-10-02. No kit was claimed or programmed. It does not accept
an appliance image, HDMI output, audio, or a later rebuilt shell.

## Sources and artifacts

The task starts from FES `8e99fbc6978bbd27fd9854058e583bbbfbec54e5`.
The clean shell seal uses `9c6e0f973171efb0fb11e2144c4787a4377decba`;
independent part builds use `c2ae7ebfa` and bind that original shell.
The contained developer transport and lifecycle are committed at `c893e6408`.
All first-party changes are in FES. No compiler pin or factory selection changed.

The authenticated `toolchains/coleco-sgm.lock` selects Yosys
`e2d425dee148cc60c50f4e9b354a10d90eab15f4`, nextpnr
`a93fe013af841214ecb4f7be3af0de65f3de3a0f` and Mistral
`7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`. Routes use the live HIP
backend. Outputs are under the task worktree's `sources/misteross/build/`;
the compiler cache is `/home/deano/fes/out/cache/misteross-toolchains`.

| Artifact identity | Value |
| --- | --- |
| Base package ID | `3a7f36570018f0be35a99a47b68912637849275c8ca5ad8ba07384f7a8f3af28` |
| Base BUILD_ID | `0fff6c5fc1fed9054db6451398182d4b` |
| Base RBF SHA256 | `ee1d46ff0c1c0e23bb65d4e2583b23e9bf565fdd42f96418a1cc198455adcaf7` |
| Direct part ID | `de0260958ef24dce7d3a17bedbf1ca86d6b2179b7ba9b701fe1e917679d022fb` |
| Direct cart RBF SHA256 | `63adf8a0f171d373b83994ad369dd33328ae55a772c047c00bb205afbbc9f1aa` |
| Scanline part ID | `5d16683c8bf5f902045f910a3937ad5f20f6468842a728992b7a81f5567a245f` |
| Scanline cart RBF SHA256 | `fb19f9cb182b314b535033acba8ba8b49c42bf27e06495309870467f81c45456` |
| SGM expansion ID | `842be3ba81d5106391ddf6515b530b663686cf296722a0776f5f59334d417e22` |
| SGM cart RBF SHA256 | `4bf0a4abbc15d5ccceefe103b43189c9432455cf8b2dfdec4a8f56a2cc8a1e52` |

## Physical build evidence

`make build-fes-coleco-video CACHE_ROOT=...` selected seed 5 after seeds 3
and 4 failed routing. The sealed shell meets every required clock and passes
both pinned-boundary and vacant-region checks. Its achieved frequencies are
80.7689 MHz pixel, 54.0745 MHz system and 196.2323 MHz audio, against
74.25, 52.224 and 12.288 MHz requirements.

`make build-fes-video-part VIDEO_VARIANT=direct|scanlines VIDEO_SHELL=...
VIDEO_PACKAGE=... CACHE_ROOT=...` produced contained archives against this
exact shell. `scripts/build_coleco_sgm.py` built the existing SGM against the
same shell. All three routes completed normally and met the three clock gates:
80.7689, 54.7046 and 196.2323 MHz respectively. The direct part is
combinational; the scanline part retains one state clock pin on `pixel_clk`.

| Selected part | Half-open CRAM rectangle | Changed bits inside | Outside |
| --- | --- | ---: | ---: |
| Direct | `(1769,1800,2806,3442)` | 4,396 | 0 |
| Scanlines | `(1769,1800,2806,3442)` | 6,091 | 0 |
| SGM | `(1769,32,2806,1800)` | 37,631 | 0 |

Each part preserves the original ORAM/PRAM header. The host Go
`fes-parts-link` output matches the Python region-overlay bytes for direct,
scanlines, direct+SGM and scanlines+SGM. FogCast's offline `fes-parts` CLI
produces matching composition receipts for these selected combinations.
Published transport files have mode 0600. An additional host diagnostic read,
staged, independently adopted and cleaned all four actual transport archives;
their composition tuples matched and no companion files remained after cleanup.
The video-only composition IDs are
`bb0ef695a80ade98e76170ffb8bc3f457d01f072e71e30e119913b21841efdd8`
and `5f1c6d9df0f14085a2c6db51eb068a3a752395e5b228522c43ae530a4f55f375`;
the corresponding SGM combinations are
`74417e1a3246818d8fd992a7ef5dbabbc252af4c6273f7462cd861f5d081273e`
and `9243b55b8f283942efc70a824cf94ef7758a78ddd1100decf6026ebf93b346e5`.

## Software validation

- `mister-packages`: `go test ./...`, `go vet ./...` and generated header comparison passed.
- Video RTL: `make sim-fes-video-parts` passed 2,475,201 cycles through the
  actual socket, including two full 720p frames, sync/blanking, CE gaps,
  HOLD, invalid requests and SOF/EOL recovery. Existing Coleco video timing
  and SG-1000 OSS machine simulations passed. The Quartus board simulation
  was unavailable because its Quartus root was not configured.
- FPGA producers: 25 focused Python tests passed, including publication
  fault injection, boundary alias loss, pinned clocks and disjoint regions.
- Expansion linker: `go test ./...` passed, including encoded configuration
  frames, out-of-region/header rejection, deterministic ordering,
  cancellation and ROM exclusion from both reserved sockets.
- FogCast: `go test ./corepackage ./internal/misterruntime ./internal/agent
  ./internal/httpapi ./cmd/fes-parts` passed. Coverage includes independent
  recomposition, private staging/restart adoption, lease admission,
  legacy-envelope rejection and exact-selection recovery after lost responses.
- Native host tests passed: composition admission, protocol (26), package
  admission (13 groups), hardware fakes (30), runtime lifecycle (55) and
  daemon integration (36). Retained-file tampering is rejected before the
  fake programmer is called. Production-serialized parts status fixtures
  match their Go consumer copies.
- Parent: 18 focused generation/consistency tests, `make check-generated`
  and committed-source `make check` passed: 16 generated consumers and 33
  fixture copies.

Independent reviews covered software admission/lifecycle and the FPGA
producer/socket. Their legacy-wrapper and premature-publication findings
were fixed and regression tested; no findings remained.

The implemented lane is volatile, fixed-720p developer loading. Its fabric
marker adds no operational GP bit; base package identity and BUILD_ID remain
the observed hardware identity. Native raster capture, DDR/CRT/overlay parts,
audio parts and user/library profile selection need subsequent work. The next
integration step is a designated-kit diagnostic using the exact selected
shell, parts and updated runtime/agent artifacts, then profile admission.
