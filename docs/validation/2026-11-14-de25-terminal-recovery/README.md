# DE25 selected terminal recovery supplier

The [held-lock reset entry](../2026-11-14-de25-held-reset/README.md) now has a
trusted completed-source terminal supplier. Explicit
`--nano-sdm-source-recovery` in both boot producers selects input format 18;
it requires source-rearm selection and its compiled CPU binding. Formats 16/17
keep recovery disabled. The selected contract, header, installed source
inventory and final ELF checks bind the supplier, held-lock controller and
fixed cold provider. Ordinary runtime activation and source reuse remain false.

After a full accepted/posted/acknowledged/completed source and failed retirement
or continuation, the terminal path retains its existing Park record, consumes
one reset attempt and revokes ordinary boot/bridge authority. A separate cached
recovery-binding check validates the same private USB/master callback identities
only after both ordinary authorities have failed. It grants no ordinary device
operation or live containment. Ordinary binding still rejects either failure.

The supplier then checks the actual completed transfer, exact source/header
binding, current configuration/retirement, retained reservations, closed data
bridges, CPU/MMU scope and inactive SD/MAC. Paired live cold observations check
CPU/reset state and USB clocks, DMA/IRQ-disabled PIO and master idle. Its fresh
one-second observation budget never renews the source or retirement deadline.
No SMMU read/write, command store, error acknowledgement or buffer release is
performed by this observation. Closed data bridges do not establish independent
fabric/interconnect drain. A matching previous-source release lineage is
required after warm rearm; its old engine remains retained unchanged.

Loss of this independent scope refuses the reset request. A returning cold
request remains uncertain and parks with no resume, source release or retry.
The repeated-load trace decoder accepts the explicitly selected profile but
permanently rejects a source sequence containing a recovery attempt/return;
request publication cannot qualify a fresh boot or continuation.

The new joined sanitizer fixture runs the real transfer/retirement, terminal
supplier, cold/USB/master checks and held-lock controller. Its 33 cases include
ordinary successful continuation, returning/nonreturning reset requests,
ownership/CPU/device/callback/record faults, invalid slots, source retention,
deadline expiry and replay refusal. Two additional actual warm rearm cases
cover MENU and computer sources and preserve the preceding private engine.
Peripheral MMIO, CPU instructions, source data/hash fixtures and reset transport
remain injected. Initial fixture compile and invalid-slot assertion failures
are retained separately; they do not qualify physical behavior.

All 33 source/retirement sanitizer tests pass on the completed supplier,
including the cold and warm terminal fixtures. Seven input-profile, five
rearm-profile, three recovery-profile and four rearm-trace tests pass. USB
master/composition, four coherent-snapshot tests, the held-lock reset fixture
and the repository consistency check also pass. Earlier unselected images do
not qualify these hooks.

Source `8394acfa52b1cbd898010e33d8e70aa380dd632d` passes a fresh normal
SDK, CPU-entry, selected BL31, SPL/U-Boot, HPS-first composition and complete
native file admission in frozen `de25-terminal-recovery-proof`. Its format-18
chain is `52191427e95cfc88ec79f251518a6141ff6b96f716c8ed9252f9a50fe5137903`,
boot tag `c5556b2812fa937c`, FIT
`3a70eb273cbdd3d2f01882f03e8b12c64ef2851d5ba56b4a4d7136e21d259a0c`
(8,547,024 bytes). [matched.json](matched.json) binds the producer checks.
The canonical core payloads remain unchanged; composition holds the shared
Quartus Pro lock without synthesis, fitting or programming.

Unlike the preceding image, this final ELF retains the real terminal supplier,
held-lock controller, fixed provider and private live recovery scope. Their
installed bodies/object symbols are independently checked. The private NOBITS
end remains `0x5ff40`, leaving 192 bytes with four unchanged 8 KiB stacks.
Compiler frames are 288 bytes for cached recovery/source validation, 96 for
the live scope, 16 for the recovery entry and 1,040 for source finish. These
individual frames do not qualify the complete EL3 call chain. See
[integration.json](integration.json).

The source bundle, selected matched outputs, completed checks, initial fixture
failures and the unchanged provider's earlier model are retained at
`/home/deano/fes-retained/de25-terminal-recovery-20261114-r1/matched.tar.gz`,
SHA-256 `3ad30477f6575fe6cbf541f7c4841bed0a82af45cdeb9680e215e4736cff91e7`.
Independent readback verifies all 46,759 entries for bytes, hashes, modes and
link targets. [retention-proof.json](retention-proof.json) records this result.
The earlier vendor primitive model remains bound to its original source; only
its unchanged fixed cold function/provider are reused here.

The host's `terminal_recovery_trace.observe()` records an exact selected-build
attempt/return only after terminal Park. Four tests cover partial, foreign,
duplicated, malformed and reordered records and a subsequent SPL banner.
Every result leaves reset completion, fresh boot admission, recovery safety,
source reuse and ordinary resume false. This is UART telemetry, not permission
to bypass the host's existing physical-reset stop after a native failure.

The [subsequent bounded trial](../2026-11-14-de25-native-programming-stop/README.md)
installed the selected FIT, then stopped with a JTAG programming error before
any native boot output. No successful selected-firmware programming/boot receipt
exists; partial configuration is uncertain. Physical cold reset, loader
redelivery, both SMMU banks, fresh device/boot-epoch admission and subsequent
repeated core loading remain unqualified. No current source sequence may reuse
memory or continue because a reset request was issued. The linked trial record
tracks restoration separately from this source-bound build evidence.
