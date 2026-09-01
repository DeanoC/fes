# Development

## Host build and checks

The repository requires a C++14 compiler, GNU Make, `ar`, `nm`, and POSIX
build utilities. The canonical host path is Linux with GNU `ar`/`ld`. Apple
clang and Apple `ld` also build the library and daemon on macOS by using BSD
`ar` (`rcs` plus `ZERO_AR_DATE=1`) and `-force_load` for archive link closure.
Run the ordinary host path with:

```sh
make clean
make all
make test
```

The [support matrix](docs/support-matrix.md) is the canonical record for
software and physical-hardware support.

Run the memory/undefined-behavior and thread sanitizer paths separately:

```sh
make sanitize
make tsan
```

The full repository checks also include:

```sh
make archive-audit
scripts/check-active-tree.sh
scripts/check-history.sh
```

`check-active-tree.sh` rebuilds the canonical host products while checking
dependency invalidation and deterministic archive output.
`make test` also exercises incremental test-header invalidation and verifies
that changing an overridable version input updates `mister-runtime` without a
clean build.

## Arm cross-build

Use the pinned GNU Arm 10.2-2020.11 A-profile toolchain from host setup; it is
not vendored or downloaded by this repository.

```sh
runtime_target_bin=/home/deano/.cache/toolchains/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf/bin
make target \
  TARGET_CXX="$runtime_target_bin/arm-none-linux-gnueabihf-g++" \
  TARGET_AR="$runtime_target_bin/arm-none-linux-gnueabihf-ar"
file build/target/libmister-runtime.a build/target/mister-runtime
```

The expected executable is a 32-bit Arm EABI5 hard-float Linux artifact. The
archive contains objects produced by the same target compiler, with 64-bit
`off_t` enabled for production MMIO offsets.

## Native-image installation — not yet available

There is no authorized native-image build, service ordering, packaged idle
RBF, installation procedure, production profile, or physical-Pi acceptance at
this milestone. Do not install the host or cross-built artifacts onto a Pi and
do not replace the working legacy image. Image integration needs a later,
separately reviewed plan.

## Physical evidence

Hardware evidence is concise engineering evidence, not an attestation system.
Record each result in this format:

```text
Date: YYYY-MM-DD
FogCast commit: <full SHA>
Runtime commit: <full SHA>
Image identity: <reproducible image name or digest>
System: <canonical support-matrix ID or development RBF>
Result: pass | fail
Evidence: <launch/core/video/input/save/stop/relaunch observations and useful failure detail>
```

Only a test performed on the physical Pi can change a hardware-support claim.

## Rollback principles

- Keep the conventional legacy image unchanged and independently bootable.
- Treat image selection or reflashing as the explicit switch between legacy
  and native ownership; do not add runtime auto-detection or fallback.
- Do not switch the disposable kit's everyday image until every required
  hardware acceptance row passes.
- Record the exact runtime, agent, and image revisions before any later switch.
- Preserve the last known-good legacy source and image as the direct rollback.

## Extraction provenance

This repository was seeded by filtering the useful runtime history from Main_MiSTer.

- Source repository: https://github.com/DeanoC/Main_MiSTer.git
- Source branch: codex/task9-main-eol-canonicalization-20260816
- Original source SHA: `346ba9f6c6d00fee446b52659229cded9bf16d8c`
- Imported rewritten tip: `3fe6698f007e8ae91bd138a0ecc30de28f9891f1`

The filtered commit SHAs differ from the original source SHAs because irrelevant paths and empty commits were removed. Authorship, commit dates, messages, and the per-file evolution of the imported paths are retained.

### Exact extraction allowlist

```text
runtime/mister_runtime.h
runtime/mister_runtime_internal.hpp
runtime/mister_runtime_v2.cpp
runtime/native/Makefile
runtime/native/hardware_broker.cpp
runtime/native/hardware_broker.hpp
runtime/native/linux/native_audio_adapter.cpp
runtime/native/linux/native_audio_adapter.hpp
runtime/native/linux/native_av_io_adapter.cpp
runtime/native/linux/native_av_io_adapter.hpp
runtime/native/linux/native_content_adapter.cpp
runtime/native/linux/native_content_adapter.hpp
runtime/native/linux/native_core_artifact_adapter.cpp
runtime/native/linux/native_core_artifact_adapter.hpp
runtime/native/linux/native_core_protocol_io_adapter.cpp
runtime/native/linux/native_core_protocol_io_adapter.hpp
runtime/native/linux/native_fpga_programmer.cpp
runtime/native/linux/native_fpga_programmer.hpp
runtime/native/linux/native_input_adapter.cpp
runtime/native/linux/native_input_adapter.hpp
runtime/native/linux/native_linux_v2_context.cpp
runtime/native/linux/native_linux_v2_context.hpp
runtime/native/linux/native_mmio_adapter.cpp
runtime/native/linux/native_mmio_adapter.hpp
runtime/native/linux/native_offload_adapter.cpp
runtime/native/linux/native_offload_adapter.hpp
runtime/native/linux/native_save_adapter.cpp
runtime/native/linux/native_save_adapter.hpp
runtime/native/linux/native_scheduler_adapter.cpp
runtime/native/linux/native_scheduler_adapter.hpp
runtime/native/linux/native_video_adapter.cpp
runtime/native/linux/native_video_adapter.hpp
runtime/native/native_clock.hpp
runtime/native/native_containment.cpp
runtime/native/native_containment.hpp
runtime/native/native_core_profile.cpp
runtime/native/native_core_profile.hpp
runtime/native/native_core_protocol.cpp
runtime/native/native_core_protocol.hpp
runtime/native/native_core_protocol_session_state.hpp
runtime/native/native_hardware_io.hpp
runtime/native/native_input.cpp
runtime/native/native_input.hpp
runtime/native/native_lifecycle.cpp
runtime/native/native_lifecycle.hpp
runtime/native/native_peripheral_session.cpp
runtime/native/native_peripheral_session.hpp
runtime/native/native_recovery.cpp
runtime/native/native_recovery.hpp
runtime/native/native_resources.hpp
runtime/native/native_save_key.cpp
runtime/native/native_save_key.hpp
runtime/native/native_snes_content.cpp
runtime/native/native_snes_content.hpp
runtime/native/native_spi_bus.cpp
runtime/native/native_spi_bus.hpp
tests/include/linux/input.h
tests/include/sched.h
tests/include/unistd.h
tests/mister_runtime_v2_c99_smoke.c
tests/mister_runtime_v2_cpp14_smoke.cpp
tests/mister_runtime_v2_test.cpp
tests/native_audio_video_adapter_test.cpp
tests/native_containment_test.cpp
tests/native_core_profile_test.cpp
tests/native_core_protocol_authority_test_peer.hpp
tests/native_core_protocol_capability_reachability_test.cpp
tests/native_core_protocol_reachability_test.cpp
tests/native_core_protocol_test.cpp
tests/native_hardware_broker_test.cpp
tests/native_input_test.cpp
tests/native_lifecycle_test.cpp
tests/native_link_closure_test.sh
tests/native_linux_artifact_programmer_test.cpp
tests/native_linux_core_protocol_io_test.cpp
tests/native_linux_input_execution_test.cpp
tests/native_linux_mmio_containment_test.cpp
tests/native_linux_v2_context_test.cpp
tests/native_peripheral_authority_test_peer.hpp
tests/native_recovery_test.cpp
tests/native_save_adapter_test.cpp
tests/native_snes_content_test.cpp
tests/native_spi_bus_test.cpp
```
