# DE25 retained source and sealed Secure reader handoff

Firmware source `ab9e10c00594a60cb8930323aa45e7e51202d568` adds an explicit
format-14 handoff for the first MENU reader after programming-source retirement.
Host auditor source `539df63a3` connects that handoff to the actual paired
Tenfoot/installer session checker. Both changes are experimental. Neither has
been deployed or physically qualified.

The installed Quartus Pro 26.3 `parameters_data.tcl` maps AxUSER[8] to
AxMMUSECSID and AxUSER[24:9] to AxMMUSID. Complete, hash-bound HPS IP and generated
MPFE wrapper files from both sealed projects tie the upper user bits to
`17'h3`, selecting Secure stream 1. The complete vendor adapter supplies the
low-byte `ARUSER=0xE0`, `ARPROT=3`, `ARCACHE=2`. The transaction's Non-secure
AxPROT attribute and the SMMU's Secure stream namespace are separate fields.
The source identities and selected policy are recorded in
[the matched build](matched-build.json).

Arm SMMUv3 section 3.11 selects the Secure bypass-control bank for this stream
when both SMMUEN controls are clear. This motivates the new policy: retain
Non-secure `GBPA=0x100000` abort, require fresh zero Secure-bank controls,
preserve the completed source and admit only the original sealed MENU memory.
This is a source-based interpretation requiring a new hardware reader test;
it is not physical route qualification. See the
[Arm architecture](https://documentation-service.arm.com/static/66c5c097882fec713ef4a8ff)
and [Agilex untranslated interface](https://docs.altera.com/r/docs/814346/26.1/hard-processor-system-technical-reference-manual-agilex-5-socs/tbu-with-untranslated-interface).

The strong platform hook checks the actual completed private transfer,
retired/continued engine, source owner, callbacks, full unchanged table bytes
and fresh register observations around both prepare/release gates. It writes
no SMMU register or table and grants no source reuse. The paired launcher
auditor binds both reader records to all compiled boot/MENU roles, source
generation 4 and control generation 5. Another core configuration rejects
before sending input. Capability/remapper observations remain informational.

The [combined focused run](focused-proof.json) passes 233 tests with zero
failures or skips, including 36 sanitizer-backed joined reader cases, the
actual vendor mailbox parser, paired launcher/installer lifecycle models,
legacy gate rejection and cold capability/remapper failure checks. The matched
SDK, entry, BL31, SPL/U-Boot, locked composition and native file admission pass.
Chain `eb154268e7db9d882d3d429090289fc6b654f896e302fd990b3cbc217e9b2146`, boot
`35c6e069de206975`, FIT
`23f2bc37a3b9b81250095f46cfee378624874fa9ce74f1865d0fa3e8b37a9878`
(8,522,448 bytes). A [fresh admission](live-auditor-admission.json) verifies that
all recorded producer inputs remain byte-identical after the host-only update.
The compiled source and later auditor source are recorded separately.

The first current affected run was deliberately stopped when tracing found the
missing paired-auditor connection. It is incomplete, not a passing regression.
Full affected validation on the completed integration remains pending. The
preceding format-13 full affected pass applies to its own source only.
[Retention](retention.json) records independently read-back artifacts, source,
recipes and terminal results, including the retained failed fixture run and
stopped campaign.

TRWU8CBL's last verified state remains the exact original native baseline with
the full ZX81/ZX80 launch/return cycle and final Park, test card installed and
host handles closed. No kit action occurred for this checkpoint. The next
bounded trial must prepare a fresh frozen operator using these common auditors,
verify the first MENU reader and return/Park, then restore the original FIT and
baseline. Source reuse, independent interconnect drain, repeated core loading,
warm USB ownership recovery and DE10-equivalent appliance readiness remain open.
PR #1791 remains `lane:deano`; no merge or Lander handoff is requested.
