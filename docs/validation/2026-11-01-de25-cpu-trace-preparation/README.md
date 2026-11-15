# Nano CPU trace candidate preparation

This is **off-board preparation only** for the next bounded TRWU8CBL test.
No new lease, UART session, board shutdown, JTAG operation, card write or flash
programming was performed. The previous physical attempt remains
[inconclusive](../2026-11-01-de25-cpu-state/README.md); its authenticated factory
restoration and release are unchanged.

## Corrected startup attribution

The earlier inspection cited the generic `lowlevel_init_soc64.S`. The pinned
Nano Makefile actually selects `lowlevel_init_agilex5.o`. Comparing the previous
failed candidate against the native seed shows that the emitted `.text*`
sections of `start.o`, `lowlevel_init_agilex5.o` and `crt0_64.o` already match.
Thus the prior claim that a changed generic startup path might explain the
silence is withdrawn. The failure location and CPU/reset semantics remain open.

The new producer checks the exact startup sources, macro header and SPL linker
script against the seed, then requires the three emitted startup text sections
to match. It retains `CONFIG_SPL_ATF=y` and its required `CONFIG_SPL_LOAD_FIT=y`
to avoid changing those vendor selections. The later ATF invocation is replaced
by a stop independently, with no linked `spl_invoke_atf` or cold-handoff symbol.
There is no firmware/native payload. Restoring that selection is not claimed
to fix the previous failure.

## Earlier checkpoints and containment

The private `board_init_f` retains the vendor order through console setup, with
checkpoints bracketing each major operation. Its existing cold sample is still
taken before device initialization. The first C checkpoint records MPIDR and
current EL without consulting the device model, timers or UART.

| Hex stage | Completed/starting operation |
| --- | --- |
| `01`, `02` | First C entry, cold sample completed |
| `03`, `04` | Device-model setup begin/done |
| `05`, `06` | Manager addresses and pin setup completed |
| `07`, `08` | Timer begin/done |
| `09`, `0a` | Mailbox begin/done |
| `0b`, `0c` | SDM stage notification begin/done |
| `0d`, `0e` | Clock driver begin/done |
| `0f`, `10` | Normal console begin/done |
| `11`, `12` | CPU/reset report begins, unconditional park |
| `e0`, `e1` | Device-model or clock setup returned an error |

The 304-byte trace is initialized loaded OCRAM data at `0xefc8`, with 32 event
slots and an overflow indicator. Before clock setup succeeds, recording touches
only OCRAM and CPU system registers. It does not use DDR BSS, allocation or
normal console state. After clocks, it can replay checkpoints over raw UART1
before normal console initialization if UART1 is already out of reset and in
8N1 with DLAB clear. Otherwise it tries after the normal console setup. The
trace never changes UART reset, clock, divisor, framing or interrupts.

Each complete line includes the diagnostic ID, hex stage/value, first-C MPIDR
and EL. A line has a shared 262,144-poll transmit budget; an exhausted budget
disables further trace transmission while retaining events in OCRAM. Existing
normal console operations and individual stalled MMIO accesses are not made
preemptible by this limit.

The report unconditionally parks with `yield` for both successful and rejected
admission states. All subsequent board DDR/device initialization is removed.
SPL MMC, FAT, EXT4, RAM-device, watchdog and SDRAM options are disabled; the
linked image excludes their initialization, ATF invocation and native handoff.
Native admission predicates and firmware ownership/master-drain contracts are
unchanged. A complete trace is diagnostic capture, never native admission.

If the board stops before clocks, serial can remain silent. Earlier events
remain in OCRAM, but their retrieval through JTAG is not qualified. The next
test must retain that limitation instead of inferring a failure PC from silence.

## Prepared artifacts and checks

Final diagnostic ID: `15a3809e5598e76c`. The completed build used the pinned
Terasic U-Boot `d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`, exact v10 seed,
recorded tools and known reference SOF. Its receipt records base `43f7a9287`
plus exact producer/input and transformed-source hashes. The artifact predates
the implementation commit; those hashes bind its actual producer contents.

| Artifact | SHA-256 |
| --- | --- |
| CPU trace SOF | `41e0693a304474f7cb747de6564e69f7440ebe3600d7fc1c43265c80858bd8f5` |
| SPL hex | `d3bfc1823d4e5726566c2efc5ce575c4578c082bd407c6abc56e08a95545513f` |
| Recovery reference SOF | `e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09` |

Four focused tests cover 12 actual transformed C board/report paths (both
admission outcomes and primaries, plus device/clock failures), actual trace C
with ready/reset/misframed/stalled UART and overflow, changed source contexts,
and partial/mixed/reordered/changed-affinity capture decoding. Strict warnings
and UBSan are used. The affected suite covers parent regressions and generated
consumer consistency. Target disassembly/symbols, actual startup text comparison,
OCRAM placement, PFG device/order and frozen input hashes are checked separately.
These checks do not establish physical console, CPU reset, DDR, native boot or
ownership/drain acceptance.

The first off-board attempt correctly stopped because disabling FIT loading
also removed ATF through Kconfig. A second build succeeded before capture
decoding was added; its exact producer is retained. The final build includes
that decoding in the producer identity. None was programmed.

## Next bounded test recipe

The fresh runnable templates and frozen plan are under
`out/nano-cpu-trace-trial-20261101/`. They have not been executed. The plan binds
the implementation commit, exact candidate, helper files and passing checks.
At the next authorized live step:

1. Recheck the authoritative top-kit status and claim it; authenticate current
   factory kernel/root, card CID/capacity/model and capture its boot UUID.
2. Cleanly halt Linux, keeping the original factory card in place.
3. Run one exact-cable volatile CPU trace. Require FPGA ID `4362C0DD`, exact
   diagnostic banner, ordered trace, matching affinity/EL, unchanged `BEGIN`
   and `SCOPE` observations and unconditional park for a complete capture.
   Save partial checkpoints on every failure or deadline. Never send `fesboot`.
4. Restore the exact reference SOF, authenticate the factory system with a fresh
   boot UUID, verify JTAG identity, close own clients and restore USB ownership
   before releasing. If reference recovery fails, keep the claim and request
   the user-provided power-cycle route; do not retry the candidate.

Bounds remain 30 seconds for the read-only query, 90 for programming, 40 for
CPU serial observation and 240 for the entire phase; the recovery observation
gets 50 seconds. No SD/flash writing or card swap is part of this test.

Preparation receipts and logs are under `out/nano-cpu-trace-prep-20261101/`;
the compact record is [evidence.json](evidence.json). Source, artifacts and
templates are retained under
`/home/deano/fes-retained/de25-nano-cpu-trace-preparation-20261101/`.
