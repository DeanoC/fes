# Native lifecycle ownership of network transitions

`ProgramService.AttachNetwork` now transfers one active network controller
before frontend ownership. The controller claim is shared across copies and
cannot be transferred during a control operation, attached twice or reused by
a foreign service. Attachment performs no firmware or device IO. Initial menu
construction still precedes network startup; a qualified platform must supply
the exact boot/device/DMA ownership and coexistence policy.

After request validation, launch, return, installation, running Park and final
menu shutdown pause networking before storage/configuration/closure. Successful
Ready/Running completion restarts it before releasing the service operation
lock. Terminal Park never restarts it. Stale requests reject before callbacks.
A pure RAM package preparation performs no new device work.

The common native failure path also drains networking before closing a known
computer or menu, covering presentation errors and frontend retirement. If drain
is uncertain, the service retires in software without further firmware IO. If
restart is uncertain after an otherwise completed operation, that error returns
through the original API and the backend does not publish a successful session
or installation. No speculative cleanup follows an unknown restart. Cached
Held/identity observations remain as last observed; software retirement supplies
no new hardware containment receipt. Input remains paused when the service has
retired. Controllers, plans, menu/core sessions and DMA memory remain retained.

Focused tests use the real shared traffic gate and native service/loader/store,
with injected platform callbacks and CPU registers. They cover repeated launch/
return, running Park, installation/menu shutdown, stale requests, exclusive and
fabricated attachment, copied ownership, drain failures before each lifecycle
operation, restart errors after launch/return/install, configuration/return
failures, frontend retirement and presentation-failure cleanup. Firmware and
physical CPU calls are checked behind the modeled drain; uncertain boundary
tests assert no further native calls and no invented Held/identity updates.
The existing XGMAC descriptor/IP-stack HTTP test still passes separately.
These model callbacks are not firmware DMA proof or physical Ethernet evidence.

The first focused attempt found a test-wrapper interface mismatch; the second
found that the HTTP client's rejected launch is a result error, rather than
necessarily a Go error. Both logs remain retained. Corrected tests also check
the direct service API, so deferred restart errors cannot silently disappear.
Evidence and source hashes are recorded under
`out/nano-runtime-network-20261110/`, indexed by `validation.json`.
All seven selected checks pass, including parent regressions, generated consumer
consistency and the native lifecycle/firmware/FPGA integration. That integration
uses real patched firmware C and Verilated endpoint RTL with injected physical
boundaries; it is not hardware acceptance. The selected source snapshots remain
unchanged throughout verification.
The full Nano ARM64 launcher cross-build passes from committed source
`218f928ac9e52945e360b6d897acf913cb63263d`, based on
`3e4d635f8f6a89146b4d0b3df4412644e8ed604b`. All 2120 compiler input hashes
match the current source. ELF SHA-256:
`f966435fd1324f16af811361e800aa7ad6e329811937a750f0682980474ee81a`.
The generated network profile remains disabled. No matched entry/BL31/FIT/SOF
was rebuilt or deployed; cross-compilation supplies no boot or DMA qualification.

This completes software guarding after an explicit attachment. It does not
implement the secure MAC lease or independently qualify stop/drain/restart.
The existing SD lease requires closed bridges and cannot substitute for a MAC
ownership policy. It also advances the shared bridge generation on grant/stop;
a MAC lease must preserve an existing menu/core session and track its own
peripheral generation. This is a requirement for the future firmware supplier,
not a capability established by these model hooks.
The accepted boot producer inventory still includes EMAC0..2
among stopped HPS producers; no reset/memory/boot check was weakened. Ordinary
frame/computer control while a MAC is active also needs a qualified firmware
policy. Nano Ethernet remains disabled, SD remains read-only and preferences
remain volatile. No kit/card/programmer was touched, and physical testing stays
paused until the operator returns. Full DE10-equivalent core loading remains
unproven.
