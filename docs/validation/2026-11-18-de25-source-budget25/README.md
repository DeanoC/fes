# DE25 enclosing source-operation budget: 25 seconds

**FAILED final USB shutdown; configuration/return sequence completed.** All five configurations, proofs, retirements and continuations pass, with four successful source releases and both core CPU-fetch receipts. Quit reaches closeProgramUSB(), which panics on a DWC2 packet timeout with uncertain USB ownership. Final native Park/quiescence is not verified. The source deadline is no longer the blocker.

The source-operation budget is deliberately raised from 20 to 25 seconds.
It keeps the original pre-hash clock origin through transfer, final validation,
retirement and continuation. This is a policy change, not a latency optimisation.
The separate command, preparation, retirement, source-release, control, poll and
host limits remain unchanged. Every ownership, mapping, integrity, retained-source
and no-retry predicate remains. Linux defaults and both core payloads are unchanged.

The [preceding hardware trial](../2026-11-18-de25-final-cost-physical/README.md)
remains failed at 20,142,473 us under its original 20-second contract, and its
procedure remains consumed. The new result does not turn that earlier failure
into a pass.

## Software contract and validation

Native artifact source: `a0d2c7aead849a8e0287774b9cd18d662770a33b`; host-decoder source `505ee1c60e9caa50462c17553c608e632391d9ae`;
main comparison base `e2b8130b5a18b652095a8d5e23124fd0a994e9c0` (#1938).
The firmware constant, sealed profile and native computer/MENU services agree
on 25 seconds. The trace decoder reads the profile's budget. Regression tests
reject cross-language drift and the old 20-second sealed contract.

All 11 affected scopes pass on native implementation a0d2c7aea: 2,902 parent tests (21 skips), actual native/C/RTL/
Tenfoot/producer integrations and 2,512 FPGA software tests (90 skips). Counts
overlap. The terminal wrapper exited before collecting the suite; independent
collection verifies its complete final framework report, all 11 subprocess
return codes, source identity and cleanup within the original 4,000-second cap.
That wrapper interruption is retained separately. The 48 focused supplier/clock/trace tests and native Go race checks
pass. The subsequent host-only validator fix passes 134 observer/client tests
on clean source `505ee1c60`. [Source split and fresh full file admission](host-decoder-proof.json)
verify that the compiled artifact input maps contain none of the changed host
files. Native payloads are not rebuilt or relabelled as the newer host source.
Thirteen source-clock cases cover the former physical rejection,
just-before/at/after expiry, invalid context/frequency/backward time and sticky
failure without retry I/O. Both native services accept the former 20.142473s
boundary; late completion at the new limit retains staging and rejects control.
[Commands, limits and log digests](software-results.json).

[Retained 20-versus-25 policy control](policy-proof.json) runs the same actual
suppliers and synthetic workload. The 20-second executable expires; the new
policy completes final proof, retirement and continuation at 21,217,676 us.
Completed zero-cost paths retain identical device-access, table-read and barrier
counts. Already-expired and higher-cost 25-second cases still reject without
retirement. Cost coefficients are synthetic, not hardware timing attribution.

The [linked target audit](target-proof.json) verifies a 24,999,999 comparison
threshold with rejection above it, unchanged 128-byte OCRAM margin and one
counter/frequency observation in the existing clock helper. Ownership and phase
engine source files are byte-identical to the parent. No clock restart is added.

## Matched artifacts and fresh physical procedure

Boot ID `ef11c39154352b0c`; FIT 8,571,584 bytes,
SHA-256 `964e52676f8659257f26e73191da8d54b4c9f08589adcb4057af0cbc54fd66cb`; full SOF
`1f52ee6fb193a9a420a8bd24072b4fd1d6101bac6728927d0a7e32c63fa472c8`. The complete SDK/entry/BL31/SPL-U-Boot/full-SOF chain from native source
a0d2c7aea passes [matched source and checked native file admission](matched-chain.json).
Composition uses the shared Quartus Pro lock; both core payloads are unchanged.
[Build stages](build-stages.json).

Fresh TRWU8CBL procedure:
`9558bfc119612fd736f27cb91402d0420033d4b478a2d42b9ef8f7771fadd46b`. Host source
`7119ffa96b153149499f67e797a7ae18fe2413db` is unchanged. Source/capture admission,
five image-selection rejection checks, seven programmer wrapper tests, a fresh
60-second USB window and independent free-handle checks precede dispatch.
The candidate and restoration use different one-use recovery nonces and each
establishes fresh reset/loader/card/NS-SMMU gates before writing. No failed
procedure is retried. All three programmers and their bounded owned-server
traces pass; the older JTAG rescan fault's cause remains unproven.

### Native serial evidence

| Source generation | Input status | Proof | Recorded elapsed us |
| --- | ---: | ---: | ---: |
| 4 | 0 | 1 | 11337813 |
| 10 | 0 | 1 | 12115151 |
| 15 | 0 | 1 | 20034341 |
| 21 | 0 | 1 | 12132202 |
| 26 | 0 | 1 | 20034736 |

Successful input elapsed values are accepted final-proof clock samples.
For a failed proof, elapsed is the saved last accepted transfer sample; any
rejected clock sample is preserved separately in [raw audit records](physical-audit.json).
These are operation observations, not isolated function timings.

| Source release | Elapsed us; unchanged 1,000,000 us bound |
| --- | ---: |
| 4→10 | 894825 |
| 10→15 | 882002 |
| 15→21 | 896574 |
| 21→26 | 882016 |

CPU-fetch deltas are 11,658 for ZX81 and 11,646 for ZX80;
ZX81 receives the 917-byte initial tape. [Native terminal receipts](native-terminal.json).
Quit is issued, but USB close panics at `cmd/fpgaload/tenfoot.go:191` with
`usbcontrol: uncertain or closed USB ownership; retain resources` and a
DWC2 packet timeout at device 1 / endpoint 1. There is no completed Quit
reply or final Park/quiescence proof. The failed verdict remains failed.
The [independent raw-wire replay](physical-audit.json) verifies the native verdict,
all 17 before/after file checks per transaction, 16 preserved files, unique
command/session/reset identities, 19 NS reset words per recovery and exactly two
FAT writes. All owned handles/processes, trace detach, original USB permissions
and JTAG 15 MHz / auto-adjust 1 restoration are independently verified.

| Host role | Seconds |
| --- | ---: |
| forward | 460.795 |
| native | 264.235 |
| rollback | 304.156 |

Host role limits stay 1,200/480/1,200 seconds, capture 450 seconds and controller
3,400 seconds; total controller time is 1030.527 seconds.
Exact original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
is restored **without power cycling**. The top kit is parked at its verified
recovery prompt, spare installed, continuing claim held. The ledger governs
later operations. No QSPI/raw-SD/factory/bottom/J8G or old-native baseline action.

The [initial 25-second attempt](initial-host-stop.json) remains failed: firmware
returned to MENU after generation-15 proof at 20,034,392 us and retirement in
1,576,788 us, then the host paired validator stopped at an overlooked 20-second
literal. Its exact original FIT was independently restored, and that procedure
remains consumed. The shared configuration validators now derive their upper
bound from the sealed source profile, with expiry at 25 seconds. This fresh
trial has new nonces, admission and retention; the initial attempt is not retried.

## Evidence boundary and retention

[Software/build retention](software-retention.json) independently verifies the
before/native Git sources, model executable/request and complete matched boot
chain under `/home/deano/fes-retained/de25-source-budget25-20261118-r1`. [Prepared retention](prepared-retention.json) binds the fresh
trial inputs and the newer host-decoder Git source; the native terminal is archived before restoration completes.
[Physical retention](physical-retention.json) verifies 335 entries,
SHA-256 `23b9d3c1fc1ea42106a9fc4006f41d2fe38c4f1af15248ad3c2f4ee6b198c4f5`, under `/home/deano/fes-retained/de25-budget25-aligned-trial-20261118-r1`.
Raw wire, ROMs, binaries and programmer traces remain outside Git.

Next: reproduce the final USB-close timeout through suspend/resume and Park
callbacks, repair it without weakening ownership or packet deadlines, then
qualify a fresh complete cycle. The two stopped procedures remain consumed.

This is experimental serial/control evidence. Physical display/input,
repeated-run variation, complete failure coverage, vendor reset/DMA guarantees,
exact-head review and ordinary Kit D25 HIL/restore remain open. A successful
serial cycle alone does not establish those guarantees or Linux feature parity.
[#1867](https://github.com/DeanoC/fes/issues/1867) stays open;
[#1952](https://github.com/DeanoC/fes/pull/1952) stays draft, `lane:deano`.
