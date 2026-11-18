# DE25 final input proof: clock rejection and bounded table reads

Guard 31 now records its first rejected member and the already-observed clock
data. The actual production final input proof is reproduced off-board: with
the modeled counter anchored at the physical receipt's 19,626,142 us and a
synthetic 12 us barrier cost, the before-fix code rejects the enclosing
20-second clock after complete DATA, before retirement. Reason 7 identifies
that rejection as elapsed-time expiry.

The repair brackets each existing 256-byte Normal-NC table read block with
full-system barriers. It retains every volatile word read/comparison, original
ownership check before and after each block, CSR barrier and first-mismatch
stop. Every operation deadline and clock origin is unchanged. No data, mapping,
ownership or hash result is cached; no callback or integrity condition is removed.

This is an off-board checkpoint for [#1867](https://github.com/DeanoC/fes/issues/1867)
in draft [#1952](https://github.com/DeanoC/fes/pull/1952), `lane:deano`. The
[prior physical trial](../2026-11-18-de25-comparison-physical/README.md) remains
failed. It did not save the rejected sample or member, so **its exact physical
guard-31 member remains unproven**. The modeled expiry and improved work count
do not retroactively establish hardware latency attribution. No kit action
occurs in this checkpoint; the rebuilt image is unprogrammed.

## Failure-only clock diagnostics

The original short-circuit condition and frequency/counter observation order
are preserved. A 40-byte private record captures only the first rejection;
later calls cannot overwrite it. Reporting uses the retained values without
new probe/MMIO observations, clock samples or ownership callbacks; the existing
logger emits them. The complete 64-bit frequency is
checked and retained, including unexpected upper bits.

| Reason | Rejected condition | Counter sampled |
| --- | --- | --- |
| 1 | Already failed source | No |
| 2 | Source clock not started | No |
| 3 | Null caller output | No |
| 4 | Wrong pinned probe context | No |
| 5 | Frequency differs from 400,000,000 Hz | No |
| 6 | Sample before last accepted clock | Yes |
| 7 | Elapsed time at least 20,000,000 us | Yes |

`FES_NANO_SOURCE_CLOCK` reports reason, sampled flag, frequency, start, last
accepted and rejected sample, and unchanged bound, with `retained=1 grant=0`.
For final-proof failure it is emitted before INPUT_RESULT so a host that stops
on that result can retain it. A later retirement/continuation clock failure
gets the same snapshot before the firmware failure park. Successful wire and
existing result fields are unchanged. The record confers no recovery, reuse
or activation authority.

## Reproduction and repair

The before-fix control is `34781f8af3530d6944e7647f99f71513a775337e`;
repaired source is `379c6aa7d40d1283d80f353e36a7ddce43ae4f1c`, above main
comparison base `e2b8130b5a18b652095a8d5e23124fd0a994e9c0` and the retained
physical evidence. The control adds diagnostics/tests but keeps the old table
barriers. Its executable, source archive and expected failing regression are
retained independently.

The joined fixture already compiles the production finish function, vendor
transfer/parser, final proof, retirement/continuation, activation, master-stop,
release and rearm callbacks. The new meter selects generation 15 only after
ISDONE/configured and before retirement starts. It advances the counter once
under the original source-clock origin, then charges barrier operations. It
does not reset or restart the clock. MMIO/CPU state, source hashing, ROM work
and counter costs remain injected.

The 19,626,142 us anchor is a physical transfer-service receipt, not a recorded
final-proof entry time. The 12 us coefficient is **synthetic**, selected to
exercise the observed boundary, not a measured DSB latency. The model cannot
identify the old physical rejection retrospectively.

| Same anchored model | Outcome |
| --- | --- |
| Old barriers, 12 us each | Guard 31 / reason 7; rejected sample 20,042,687 us after start; retained, no retirement |
| Repaired barriers, 12 us each | Final proof, retirement and continuation pass; last accepted source elapsed 19,925,685 us |
| Repaired barriers, higher 16 us cost | Guard 31 / reason 7; no retirement |
| Repaired barriers, counter already at 20 seconds | Rejects at the same unchanged bound; no retry I/O, stores or DATA |

[Independent work audit](work-audit.json) compares completed zero-cost paths,
preserving all 5,120 word reads
(40,960 compared bytes), 30,556 MMIO reads and one measured counter sample.
Table barriers fall from 10,240 to 320: two per 32-word block rather than two
per word. All 24,624 other measured barriers are unchanged. Total measured
barriers fall from 34,864 to 24,944. There is no hardware speedup claim.

The source tables are Normal-NC memory, with writers controlled by the same
ownership policy checked around every existing 256-byte block. No MMIO barrier
is moved. The closing barrier also executes on the first-mismatch exit.
Arm defines DSB completion for preceding accesses; using that completion at
the unchanged block boundary is the ordering argument here, under the existing
controlled-writer contract. [Arm DSB semantics](https://support.arm.com/documentation/102336/0101/Data-Synchronization-Barrier?lang=en).

The fault regression rejects early table corruption and changes to owner,
USB health, callbacks, configuration generation, mapping, digest and queue
producer at the last word. None reaches retirement or continuation. Clock
tests cover all seven rejection members, the just-below/exact bound,
unexpected frequency upper bits, sticky first cause and reporting without
new observations.

## Validation and preservation

All 42 sanitized supplier/retirement tests and all 11 affected scopes pass: 2,899 parent tests (21 skips), native/RTL/Tenfoot/producer integration and 2,512 FPGA software tests (90 skips). Counts overlap. The [software record](software-results.json) binds the exact source, executed commands, expected before-fix failure and remaining coverage limits.

[Target proof](target-proof.json) confirms a barrier-free word loop and full
barriers at block entry, normal exit and first mismatch. Its validation frame
stays 352 bytes. BSS ends at `0x5ff80`, retaining the 128-byte OCRAM margin and
unchanged stack reservation; the 40-byte clock record stays in private OCRAM.

A fresh SDK/entry/BL31/SPL-U-Boot/full-SOF chain passes [matched source and native file admission](matched-boot-chain.json), with all [six stages](build-stages.json) passed. Boot ID `1a8c1e6016feca1c`; FIT 8,571,584 bytes, SHA-256 `1ca80b57e7ff7a6ebbdb089fb41821706df698bbe92fc8f293f0da377a0b572a`; full SOF SHA-256 `27073c9b07896da38a54535646c19dad42a0453a337edf26bffc3f6f281a6e06`. Both core payloads are unchanged. Composition used the shared Quartus Pro lock. This is an unprogrammed file-admitted artifact, not runtime authority or hardware acceptance.

[Retention receipts](retention.json) independently verify before/fixed Git archives against every blob and mode, plus complete boot and software evidence archives against hashes, modes and symlink targets. Sources, preserved executable/control and raw target/model outputs are under `/home/deano/fes-retained/de25-guard31-offboard-20261118-r1`. ROMs and complete raw binary/log evidence stay outside Git.

The next physical test needs fresh admission and a new one-use bounded
procedure. It must check complete repeated launch/return/Park and retain the
first clock snapshot on any failure. Historical procedures remain consumed.
Exact-head review, complete hardware/failure coverage, vendor retirement/reset
guarantees and ordinary Kit D25 HIL/restore remain open. No HIL waiver is
recorded; #1867 remains open. Linux defaults and both core payloads are unchanged.
