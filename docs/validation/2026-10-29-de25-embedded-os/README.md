# DE25-Nano Linux-free feasibility evidence

29 October 2026. Scope: isolated portable FES GP probe plus Zephyr and pure
bare-metal AArch64 execution. FES base is
`eea51e94e014e9603055e8a0309d7fefb7d5009a`; the result files bind the actual
experimental source bytes independently of a later documentation commit.

**Classification: host-only software evidence.** No DE25, kit, boot flash or
production runtime was accessed or modified. The FPGA endpoint is a C++ model
in all three runs. No FPGA RTL simulation, physical MMIO, DMA, reconfiguration,
USB, network or standalone FES qualification is claimed.

| Run | Result | Meaning |
| --- | --- | --- |
| Native host C++14 | 22/22 passed | Deterministic protocol and injected failure behavior. |
| Bare-metal AArch64, QEMU `virt`, EL1 | 22/22 passed | Same logic executes without an OS, libc, unresolved symbols or ELF interpreter. |
| Zephyr 4.2.0 AArch64, QEMU `virt` | 22/22 passed | Same logic executes inside a real RTOS with kernel startup, console and hardware-cycle clock. |
| `make check` | Passed | 21 generated consumers and 37 fixture copies match; no shared definitions changed. |
| Affected-planner unit tests | Passed | 21 affected-selection tests and 31 changed-test-runner tests. |

The 22 cases cover identity-before-release, input/neutralize/hold, exact request
encoding, all identity words, late ACK, timeout, complete rejection, invalid
signature, reverted ACK, unstable payload, I/O failure, expired deadlines,
invalid requests, the 100 ms cap, invalid initial state, and stalled/backwards
clocks. Callbacks must themselves return; iteration bounds are not hardware
real-time guarantees.

The final bare-metal ELF has 9,301 bytes of text/constants, no initialized
data and 16,400 bytes of BSS including a 16 KiB stack. Its 77,200-byte ELF
file contains metadata and alignment as well. The Zephyr linker reports
368 KiB of RAM allocation for this configuration. These are diagnostic-build
sizes, not estimates for a complete FES appliance or performance comparisons.

The two emulator runs were stopped by the harness after ten seconds, after
their success markers; they do not provide clean-shutdown evidence. QEMU uses
Cortex-A53 and a generic `virt` machine, not DE25-Nano or Agilex 5 hardware.

Each `*-result.json` retains commands, exact source hashes, compiler identity,
ELF hash and outcome. Console logs, ELF headers, bare-metal size and undefined
symbol checks are adjacent. SDK 0.17.4 archive hashes and the pinned Zephyr
commit are in the [experiment lock](../../../examples/de25-embedded-os/toolchain.lock.toml).
The runner records the tools actually supplied; the lock is the tested recipe,
not an authenticated FES core-producer identity. Built ELF images remain in the
task worktree's ignored `out/de25-embedded-os/*-final/` directories.

`make test-changed ... --plan-only` selected every software/RTL lane because
the new `examples/` code is an unknown top-level input. The plan summary is
retained. That full unrelated regression campaign was not run; focused probe
execution, consistency and planner checks were used for this isolated study.
No production integration or CI completion is implied.

The first configure attempt selected system Python instead of the isolated
environment and failed to import `pykwalify`. The runner now supplies CMake's
`Python3_EXECUTABLE`; subsequent clean configuration/build and the final run
passed. Earlier local runs are not the source-bound final evidence above.

Independent code review found a possible infinite wait on a stalled clock and
an unchecked initial ACK state. Both were repaired and the final runs include
their regressions. Review approved the bounded exploratory scope after those
fixes. The next useful evidence is a separate, explicitly authorized Nano
RAM-resident serial/timer diagnostic with an identified firmware/DDR handoff.
