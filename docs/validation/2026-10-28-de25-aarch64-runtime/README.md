# DE25 AArch64 runtime and agent software checks, 2026-10-28

The C++-enabled AArch64 SDK/rootfs built and passed image verification and
userspace smoke checks. The exported SDK then built the runtime archive and
daemon; six C++ unit binaries, the agent's early-exit command and five Go client
tests passed under QEMU user emulation. This is `userspace-only` evidence. The
production daemon was not started and no hardware was accessed.

## Selected sources and artifacts

- C++ SDK/image producer: `3ede3890895aad82be69dfbe3e4c4cbbb1c02c6c`.
- Runtime/software-check producer: `8c35e844a330bf93791fd8fa78709c3ec049bdd3`.
- Runtime source tree: `ea6aecf99cbbe179e6e3c5ddee6683cb47c5c631`.
- Builder: `sha256:ea200655b92ab1211bc8b326c6e799815f1d648b2d5e5a6c982f597cb2b9650d`.
- C++ compiler: Buildroot 2021.02.4 GCC 9.4.0.
- Go compiler: `go1.26.5 linux/amd64`; target
  `linux/arm64 GOARM64=v8.0 CGO_ENABLED=0`.
- QEMU user CPU: `cortex-a53`.

[runtime.json](runtime.json) and [checks.json](checks.json) are exact copies of
the completed software probe's receipts. [image.json](image.json),
[verify.json](verify.json) and [smoke.json](smoke.json) are exact copies for its
selected C++ SDK/rootfs. The runtime receipt binds that image producer and SDK
digest; the image reports bind the same recipe, rootfs and SD-image digests.

| Artifact | SHA-256 |
| --- | --- |
| C++ SDK | `e4c09c2ebe908a4fa1077b98122a9b561712345435ae1b2fe7d4c563d1ac12c7` |
| SD image | `e8836b89667899efe1350d94668644fda19ebf397a3c18d770f6e93dd2d3e854` |
| `mister-runtime` | `7d368817277a1ce6e24ca7fa747e9114be363ecd6c47c485fa9526a85c3d848f` |
| `mister-agent-linux-arm64` | `c92aa941d3b75fb5487461c28a36551fc226c0c9196acb99b94447627634a6bc` |

The retained artifacts are under `out/de25-nano-linux-cxx/` and
`out/de25-runtime/` in
`/home/deano/.codex/worktrees/c134/fes/out/dev/de25-linux-assessment/`.
Binaries and compiler caches are not copied into this evidence directory.

The completed runtime command was:

```sh
make smoke-de25-runtime DE25_RUNTIME_ARGS='--image-output out/de25-nano-linux-cxx --output out/de25-runtime --jobs 2'
```

## Completed software checks

The selected image passed `verify-de25-linux` and `smoke-de25-linux`. Its QEMU
smoke reports AArch64 BusyBox and `SSH-2.0-dropbear_2020.81` with a temporary
host key. This is not an authenticated SSH login, kernel boot or Ethernet test.
The C++-enabled rootfs still contains no installed runtime or target agent.

The runtime probe relocated the authenticated SDK and used the existing runtime
Makefile to build the canonical archive, daemon and tests. Its archive audit
passed. The audit's exact raw-I/O owner list was corrected to include the
existing `native/hardware.o` tape-save implementation; no tape behavior or
other audit condition was changed.

| QEMU C++ test | Result |
| --- | --- |
| [runtime_test](runtime_test.log) | reported `56 passed` |
| [protocol_test](protocol_test.log) | 27 tests passed |
| [mmio_test](mmio_test.log) | 3 passed, using injected MMIO operations |
| [fpga_manager_test](fpga_manager_test.log) | 28 passed, using fake hardware |
| [off_t_test](off_t_test.log) | 1 passed |
| [production_hardware_test](production_hardware_test.log) | identity, ordering and unavailable-adapter checks passed |

The guard accepts only the identified ARMv7 DE10 production target. AArch64,
DE25, unknown or malformed identity is refused before driver loading or
construction of MMIO/I2C adapters. The existing unavailable-hardware adapter
retains failure-only behavior and advertises no programming capabilities.

An additional [rootfs loader probe](rootfs-probe.sh) reran the same guard-test
binary under Cortex-A53 QEMU with the extracted image rootfs as its loader
prefix, rather than the SDK sysroot. It passed ([output](rootfs-probe.log));
the production daemon was not started.

The AArch64 [agent command](agent-cli.log) returned `fes-kit-maintenance-v1`.
The five selected [Go tests](agent-runtime-tests.log) passed: invalid development
paths before dialing, wrong protocol/unknown fields, unavailable runtime
socket, health/readiness states, and reboot-required reconciliation. The
production runtime daemon was compiled but not started by the probe.

## Local aggregate and CI evidence

The local `make test-changed` run at
`ddc5c25af7c55df81d416167cad07dcfaf61dc29` passed 1,046 parent tests with two
skips and all selected host checks, including browser checks. It then failed in
`native_hardware_test` at `TestWritableDiskInputFaultSavesBeforeIdleOrRetainsRAM`:
the `InsertLibraryMedia(...).ok()` assertion at line 3982 failed. Its cause is
not established. The same built binary passed an isolated retry
([output](native-hardware-isolated.log), reported `34 passed`). The aggregate run
remains failed in `out/de25-offboard-test-changed.log`; the retry is separate
evidence. Contract and FPGA software checks were not started by that local run.

[Software CI for the tested producer](https://github.com/DeanoC/fes/actions/runs/40681546650)
passed every selected job, including runtime, contracts and FPGA software.
The `8c35e844a3` change after the local aggregate run only selects the Go tests'
package working directory; runtime, FogCast, shared-contract and FPGA source
bytes are unchanged between those revisions. The successful QEMU receipts
identify the later producer explicitly. CI coverage is not physical acceptance.

## Remaining board gate

DE25 kernel boot, serial/DHCP/SSH login, persistent SSH host identity and MAC
behavior still require the mailed board. This work adds no DE25 FPGA programming
backend, hardware MMIO map, DDR/cache behavior, display, media or managed-update
acceptance. The matching Terasic factory HPS QSPI and MSEL requirements remain
the image's boot contract. No physical support claim changes.
