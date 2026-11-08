# DE25 development-workflow acceptance audit

Audit date: 2026-11-08. The objective remains the normal DE10-style development
workflow, with Deano's explicit exception for physical partial-persona loading
only. This is a requirements audit, not a new qualification policy. The eight
requirements come from [the development-kit design](../../design/de25-linux-platform.md).

Current hardware: bottom TRWU8J8G, kernel `9cec7e77` / notes `b0c2f6b1`, root
`0858eef0`, stock Terasic FIT `bb6a48c7`, normal default-off software `61a4b0aa`
generation7 after the bounded-submission candidate trial and normal restoration;
generation5 retains the corrected observed-prompt rollback/restore sequence;
earlier passing controls retain generation1 and the original failure retains
its generation2/3 evidence. Ordinary package `3da0f1df` / build `490c1ae5`. Kernel/software
producer is `ecaefd4af`; core producer is `8af614f4`. Source identities remain
separate from subsequent documentation commits.

| Requirement | Current evidence and boundary |
| --- | --- |
| 1. FES Linux baseline | Native stock boot and one idle/active OS reboot each pass exact boot files, Ethernet/pinned SSH, selected software, data identity and ordinary relaunch. [Stock result](stock-firmware-native.json), [reboots](stock-reboot-complete.json). These do not qualify another rebuilt image. |
| 2. Linux FPGA programming | Normal full-image Play/Stop/relaunch and same-boot runtime-loss recovery pass the shared runtime/programmer. [Ordinary control](stock-ordinary-control.json), [runtime-loss proof](stock-runtime-recovery.json). Recovery after an actual failed full-image upload/secure call remains unverified; process loss is a different fault. Physical persona failure remains unsupported/default-off under #1568 and its scoped waiver. |
| 3. Board/package identity | The sealed ordinary package retains source/independent-fit qualification and exact package/build validation during the native session. [Standard package record](../2026-11-05-de25-static-expansion-offboard/README.md), [ordinary result](stock-ordinary-control.json). Host/model rejection checks do not establish physical support for other packages. |
| 4. Production runtime | The authenticated AArch64 runtime uses normal catalog launch, media/HID and serialized Stop; same-boot idle/active process-loss replacement verifies inherited-owner retirement and fresh MENU. [Recovery record](stock-runtime-recovery.json). Dirty SAVE, uncertain failed uploads and guest resumption are not qualified. |
| 5. Managed software | Supported install/activation of the source-built default-off bundle and native executable identity pass. [Normal update record](README.md#development-policy). The retained normal setup is user-approved. A later [current-tuple rollback trial](stock-software-rollback.json) accepts one generation-CAS factory selection and one normal restore, ending at61a4 generation3 with stable MENU, exact process/file identities and retained data. Its factory BASIC control fails with0/0 and no7; that original gate stays failed. A [corrected observed-prompt sequence](stock-software-rollback-prompt-v2.json) then passes one factory generation4 and one restored normal generation5 LOAD/RUN7/Stop control, with exact accepted operations, process/file identities and retained data. This completes the scoped functional qualification in [#1572](https://github.com/DeanoC/fes/issues/1572), without proving the earlier failure's cause. |
| 6. MENU display | Native MENU/ZX81 presentation, exact allocation/configuration replacement, zero owner errors/underflows and ten seconds of stable post-recovery readiness pass. [Recovery proof](stock-runtime-recovery.json). Physical F12/Escape proof belongs to the earlier e649 tuple; broader current-tuple chooser stress and arbitrary-fault display containment remain unverified. |
| 7. ZX81 normal launch and persistence | Current tuple passes Play, RAMTOP128, LOAD/RUN/reload7/9/7 and Stop/relaunch. New ROM SAVE emits923 bytes, persists/acks through Stop, survives one owned host restart, then fresh LOAD9/edit7/reload9 passes; original media is restored. [SAVE proof](stock-save-persistence.json). Wired physical P/F12/Escape is separately retained on the earlier firmware tuple; no new physical-keyboard/chooser test is claimed here. |
| 8. Shared kit operations | Status-file claims, authenticated own-host/session identity, normal execution leases, no foreign takeover, idle/free release and same-card operation are recorded for the named trials. Current private host remains PID1823444, binary `0de3179f`, same configuration/catalog. No main HIL baseline has been promoted; the retained development setup is distinct from main's shared-kit baseline. |

The SAVE trial's first immediate process-identity read failed during the one
host restart. The same new process was subsequently authenticated and its
retained catalog/bytes checked before relaunch. The original failure is retained;
there was no second restart. This is not a measured restart-latency result.

The source and admission gates stay intact. The known failed persona upload is
not converted to a pass. Current UART evidence also retains overlay-property
warnings and Bluetooth HCI errors; it is not Bluetooth or warning-free acceptance.

The current PR stack is still open: #1297 is the parent, #1312 its sole child.
Both are ready and Deano-owned. The two #1297 inline findings were resolved
against its already-committed headless native boot/autoload and corrected
bootstrap evidence. Resolving those findings is not a clean review of the entire
current stack. A clean current-head owner review and current-head CI remain
required. Two full automated review rounds are already used; this audit requests
no third round, handoff or merge.

Remaining integration work includes ordinary failed-upload containment, the applicable current-head review/CI
and owner-authorized landing/main baseline work. Untested peripherals retain
their status; no all-core or all-peripheral support is inferred from ZX81. The
full objective remains open rather than being reduced to the passing control.

The later [stock-watchdog diagnostic](stock-watchdog-reset.json) adds one actual
countdown/disarm/survival control and one deliberate expiry with all SD-backed
mounts read-only. It returns through stock firmware to fresh authenticated boot
`27d28fac`, the same normal61a4 generation7, writable data and stable MENU. A
fresh ordinary BASIC7/9/7/Stop control and catalog/SAVE retention pass before
release. This strengthens requirements1 and8 for the tested healthy-system
reset. It does not complete requirement2's failed-upload/secure-call recovery
gate, qualify dirty-data safety or add automatic reset policy. The exact code
head `cdf0fa609` has green CI; any subsequent head still needs its own check.

The [submission-deadline repair](submission-deadline.md) reproduces a separate
off-board caller-blocking defect and tests an owned worker with retained lease
and uncertainty. Its changed runtime `a495366c`, producer `644ed8017`, additionally
passes native supported activation, one fresh ordinary BASIC7/9/7 control and
Stop/MENU before restoring original61a4 generation7 and repeating that control.
This ordinary acceptance does not replace the physical failed-upload containment
gate or transfer the original runtime's reboot/runtime-loss evidence to new bytes.
