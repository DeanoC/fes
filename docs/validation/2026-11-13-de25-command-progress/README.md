# Retirement queue-progress observations

The format-9 input profile replaces four separately guarded completion-poll
reads with a single bounded read-only observation. Its 24 registers cover
fixed capabilities, error/event state, queue bases, abort, both control values,
the published producer and a freshly sampled consumer. Ownership/time checks
bracket both the batch and caller. Callback substitution, invalid or backward
consumer progress and the original command/poll bounds still retire the attempt.
Slot stores, the producer write, final queue snapshots and all five table scans
keep their existing gates. Arm-only and fault profiles leave this callback
unselected and retain their individual queue reads.

New mutation tests expose a late phase change during the final ownership
observation that could previously reach the next disable write. Command ownership
now validates phase and binding after its register batch as well as before it.
The new regression stops before that fifth write. The initially failed model
runs are retained separately; their failures are not relabelled as passes.

All 23 focused retirement/profile/observation tests pass. The C models run
under ASan/UBSan. Callback tests cover failed suppliers, owner loss, changed callback
or context, expired/backward clocks, stalled and invalid/wrapped/backward
consumer pointers, and failure after returning a completion-looking value.
Portable retirement mutations cover every read position after publication,
including the new progress batch and final ownership views. Failed attempts
retain memory, forbid retry and grant neither reuse nor ordinary activation.
The explicit profile rejects actual format-7 and format-8 contract records.

The [joined cost model](model-result.json) compares against a separately captured
format-8 operation baseline. Command-phase supplier reads fall from 5,514 to
3,006 (45.48%), counter calls from 293 to 149. Counts in the other six phases
remain identical, including all 25,600 table-word checks and the same five
control writes. Injected 32 us read cost completes where format 8 failed; 64 us
reads and a slow counter still fail. The 100 ms command, one-second retirement
and 20-second source bounds and their origins are unchanged. These injected
costs do not predict physical Nano latency or establish consumer completion.

The [AArch64 compilation](portable-arm64.json) checks six portable units,
including both setup profiles, with the pinned compiler and strict firmware
flags. The largest individual frame is 384 bytes; this is not a linked BL31,
whole EL3 stack proof, matched boot image or hardware acceptance.

The prior [format-8 physical trial and restoration](../2026-11-13-de25-command-guard/README.md)
remain separate: full source transfer passes, command retirement fails after
publication and recovery programming reports a broken JTAG chain. A later
physical reset permits exact original-FIT rollback and an independently verified
51-command baseline cycle/final Park. The complete format-9 matched chain is
described below; its later bounded
physical failure and successful baseline restoration are recorded separately
in the final section.
No ordinary activation or source-memory reuse is enabled.

The [affected checks](checks.json) pass the full selected software campaign:
2,651 parent tests (15 reported skips), the native runtime/firmware and producer
integration checks, and 2,181 FPGA software tests (88 reported skips). Generated
consistency verifies all 69 consumers and 45 fixture copies; documentation links
also pass. These remain software and model checks, not a physical retirement
qualification.

## Complete matched candidate

The [matched result](matched-result.json) binds production source
`f5142047077851c98465886f79a386debffa62cf`. The selected-program native SDK,
compressed CPU entry, complete BL31, SPL/U-Boot and FPGA composition all build
and pass the full fresh native file admission. The [commands](matched-checks.json)
retain their terminal results. Composition holds the shared Quartus Pro lock;
it performs no synthesis, fitting, card write or device access and preserves
the exact sealed FPGA payloads.

The new chain is
`b1c328df40d1a1497b61bd8c780c611a5bbf3121069c72fd72096d8b95b6794e`,
boot ID `8cf056aa789ab65f`, FIT SHA-256
`4e6a484f2a7c5da3666190380dbbfc7d61492409a339e25a854cb5e8e4c66a53`.
The FIT is 8,514,256 bytes within the unchanged 16 MiB contract. The new
[archive](retention-result.json) retains 1,139 files with independent readback,
SHA-256 `55e45c804dcbbaf58c3a6cd254ef4f666e01acd0f28e29bf38c69263611d0b95`,
including the software proof sources, cross-build objects, raw checks, prior
failed prototype logs and source ROM. The two complete unchanged sealed FPGA
project trees remain in the [separately verified archive](reused-project-archive.json)
`9587828cdddfd72ab618154a1bb73bb1ccaaa7b3bbcfff8ebfb369991f4247eb`;
complete membership and hashes match. Both archives are required.

These build results are off-board evidence. The later bounded physical trial
below does not qualify retirement, independent interconnect drain or source
reuse; it separately restores the original native baseline Park.

At candidate publication, the complete native file/tree snapshot matched after
the final documentation updates and archive retention. Subsequent publication
commits change documentation only; the built production source remains `f5142047077851c98465886f79a386debffa62cf`.

## Bounded physical trial and restoration

The one-use format-9 procedure
`d5d57f29d941185977324394646b07e15a099ce069b29e6a78233fad23c026b4`
was consumed at source `8b631999f0c52af866f345bb9af937ad884c814b`
under the user's standing trial approval. The production source and matched
artifacts above were unchanged. Seventy-two operator cases, eight actual
role/root admission boundaries, both complete native admissions and actual
user/root default checks passed before the frozen 30,688-file procedure ran.
The prerequisite replay verifies the latest actual three-role restoration from
its retained archive. Earlier failed fixture checks remain in the preparation
records; no hardware attempt was retried.

The [actual physical result](physical-result.json) is **stopped**, with a failed
retirement diagnostic and a separately successful original-baseline restoration.
The complete 1,708,032-byte source transfers in two posts/two acknowledgements,
with matching SHA-384 and 11,641,652 us elapsed. Retirement then stops in phase 4,
error -3, after four control writes and producer publication. The observed
command phase spans 107,369 us against the unchanged 100,000 us bound;
the observed retirement interval is 286,439 us. Source memory, tables and queues
remain retained; retirement, reuse and ordinary activation are unqualified.

The failure receipt's zero producer/consumer/control fields are incomplete result
fields, not measured hardware values. Existing timing does not reveal the last
sampled command consumer or which queue operation consumed the bound. The lower
model read count does not identify the physical cause. The next diagnostic
should preserve already sampled queue progress and failure stage, with no extra
MMIO or changed deadlines, before another matched build and bounded test.

Restoration-only recovery programming succeeds without another physical reset.
Exact original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
is restored/read back; all 17 protected-file checks before and after and both
read-only SMMU reset gates pass. A fresh original baseline completes all 51
ZX81/ZX80 launch/return commands and final Park at generation 5. Independent
raw-wire/program-witness replay verifies both native boot boundaries and the
baseline cycle. This is a bounded baseline cycle, not a new persistence proof.
All owned workers stop, UART handles are closed with a root process-FD scan,
JTAG auto/15 MHz and exact USB identity/inode/permissions are restored.
TRWU8CBL is left at original native baseline final Park with the test card
installed; the top-only claim remains held. QSPI, factory card and bottom kit
are untouched.

The [terminal checks](physical-checks.json) preserve the nonzero diagnostic
terminal alongside passing independent replay and archival checks. The
[physical archive](physical-retention.json) is independently verified by full
byte/hash/mode readback: SHA-256
`b6b860c21f983556b619c96a4a316208f078e167bb1b7e6f43666eea763044c0`.
Its explicitly recorded preparation archives are dependencies. This failure is
not acceptance of mapping retirement, independent interconnect drain, memory
reuse or general core loading.

GitHub Actions run [43442713493](https://github.com/DeanoC/fes/actions/runs/43442713493)
completed successfully at the exact tested source
`8b631999f0c52af866f345bb9af937ad884c814b`. This documentation update has its own
publication identity and inherits no new hardware qualification.
