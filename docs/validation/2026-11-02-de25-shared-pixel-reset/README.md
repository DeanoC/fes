# DE25 ZX81 shared pixel reset

Producer: `1937f9ac76b7c383ab3bc9e8f948a3ba67fdab3d`.
Software and complete combined-model fixture:
`5e56245d4a7444f9352812e365085d093aed9f42`.

The ZX81 video block exposes its existing final pixel-reset release stage to
the transport readiness synchronizer. This removes the duplicate two-stage
transport release and its obsolete ingress constraints. The renderer remains
byte-identical after removing the new output/alias and their comment. Frame
ownership, HOLD, epochs, GP/mailbox behavior and external interfaces retain
their existing contracts. The independent board PLL/configuration reset still
controls pad readiness; this change does not settle its coordination with the
peripheral reset.

## Fresh production-path builds

The normal Quartus Pro 26.3 recipes complete generation, fitting and Linux phase
conversion in 629.7 s for ZX81 and 629.6 s for MENU. The ordinary parent
registry/materializer/tool API reproduces both complete producer input records,
covering 577 ZX81 and 377 MENU files. MENU's source closure includes ZX81 files,
so its source identity is rebuilt too.

| Candidate | BUILD_ID | RBF SHA-256 |
| --- | --- | --- |
| ZX81 | `0276dccace12a773359f7184ef71d476` | `e3929f8c1d695fa93affaadb0511917e2b51854832c191b56cb80b19174d7fac` |
| MENU | `26508631f95d180199d674313193a242` | `df3d55344f49844b190d4405f6bce4d0c27c61464b095422caa48eb02a3a0245` |

All 41/69 reported timing checks pass, with 85 signoff rules and zero waivers
per core. Both retain 30 HDMI/output-delay findings, the two previously approved
asynchronous-input findings and unresolved reset-domain findings. ZX81's
remaining reset row names the board pixel release and renderer pixel release;
the duplicate transport head is gone. Package admission is **false**. These
candidates still need eligible signoff and independent identical-RBF
reproduction before export.

## Functional and fitted checks

[Validation](validation.json) binds the focused checks, formal projection,
fitted audit, clients and complete producer identities.

- All 26 board-source tests and four package tests pass. The unchanged frame
  bench checks 21,048,339 pixels at three clock ratios. Endpoint tests cover
  MMIO, stopped-clock reset, retained epochs, late tape and frame boundaries.
- Fresh native and Cortex-A76 QEMU client/RTL proofs pass all 20 internal-RAM
  and 14 combined RAM-pack cases in 1,538.4/1,785.0 s. Internal clients select
  the actual canonical BUILD_ID. The combined selection/persona model declares
  its synthetic identity. Both architectures SAVE/load/run/edit/reload/run,
  print 7 twice and remove the edit. All four SAVE images contain the same
  923 bytes, SHA-256
  `b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
- Initialized two-state induction proves the actual five-bit projected release,
  readiness and frame-mask logic with arbitrary pixel-edge inputs and
  `async2sync` reset modeling. Changing only the shared output to the first
  stage produces an initialized eight-step counterexample. This projection is
  neither whole-renderer equivalence nor metastability/physical qualification.
- A private fitted query checks all 24 system recovery/removal paths plus 136
  shared-release paths across four corners: eight interstage checks, 40 checks
  at five asynchronous-reset consumers and 88 checks at eleven data consumers.
  Minimum slacks are 0.110 ns, 0.228 ns and 0.132 ns respectively. Interstage
  and downstream paths remain timed. The original fitted project is unchanged.

The first query used traversal switches that returned no fanout. Direct fitted
fanout and actual timing reports establish one interstage connection and the
16 final consumers. The corrected query binds that complete named inventory;
the original query, failure and probe remain retained. The initial local formal
attempt lacked Yosys; the remote source-bound proof passes with stock Yosys
0.33. The first package runner used the wrong Python import directory; the 26
successful board tests are retained and the remaining checks run from the
correct component directory. No warning or timing setting changes.

## DE25 software scope

[Software coverage](software.json) records the completed actual affected plan.
The parent suite passes 1,154 cases with five skips; the planner suites pass
62 cases. The generic FPGA aggregate runs all 1,787 cases and records 1,760
passes, 19 skips and eight failures. Its original failed result is preserved.

At Deano's direction, exactly seven Cyclone V/M10K mapper checks and one PC/XT
elaborator fixed to Cyclone V are not applicable to DE25. The scope audit
requires the complete discovered inventory and exactly this failure set;
**all 1,760 executed DE25-applicable cases pass**, with 19 existing skips.
This classification changes no global test selection, timing or seal gate.
Generated consistency separately passes in 7.3 s, with 40 consumers, 42 fixture
copies and zero copied pins. The generic affected-plan failure prevents its
wrapper from reaching its later `make check`; the separate check supplies that
evidence. The private, hash-checked Verilator 5.032 selection preserves warning
settings, test bounds and clocks; its 110 files verify before and after use.

## Retention and remaining acceptance

[Retention](retention.json) records independent extraction and verification of
all 6,862 compiled/model files and 124 local-focused/software files. The former
includes complete canonical candidates, all clients and captured source,
formal inputs/logs, fitted paths, producer records and failed remote wrappers.
The latter includes frame/endpoint objects and complete software logs,
inventory, tool manifest, scope audit and failed local attempts. Source Git
objects restore independently without alternates; actual public source,
timing, topology, constraint, client and ELF readers pass against the restored
artifacts. The first software archive check compared original group-write
permissions with safe extraction's filtered permissions. Its failed script,
archive and extraction are preserved; the final reader verifies every byte,
the archive's original modes and the expected safely filtered restored modes.

These are off-board builds and functional models. They provide no physical PR,
SDM, MMIO, coherent DDR or native acceptance. HDMI output timing, board/peripheral
reset coordination, firmware pairing, an admitted factory/card and native ABI3
MENU/catalog/ZX81 lifecycle, persistence and reboot acceptance remain open.
Terasic's working pin/IO baseline and the two approved asynchronous-input
exceptions are unchanged. No board, UART, kit network, capture, allocation,
FPGA, SD, kernel, userspace, QSPI or power operation occurs. Claims and baselines
remain unchanged. Delivery stays in the existing ready `lane:deano` PR #1312.
