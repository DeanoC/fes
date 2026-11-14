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
and the repository consistency check also pass. A fresh selected matched build
is still required; the earlier unselected image does not qualify these hooks.

The selected firmware has not been programmed. Physical cold reset, loader
redelivery, both SMMU banks, fresh device/boot-epoch admission and subsequent
repeated core loading remain unqualified. No current source sequence may reuse
memory or continue because a reset request was issued. TRWU8CBL is untouched;
its last verified hardware state remains the original native baseline at Park.
