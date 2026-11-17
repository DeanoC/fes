# Volatile JTAG recovery without a physical power cycle

One bounded TRWU8CBL test passed on source
`23113b14496b89d673efe0c29a196970317bf3a6`. The operator reloaded a complete
volatile recovery SOF, observed its fresh SPL entry and reached the existing
SD recovery loader. It then verified the protected 16 MiB card prefix,
`u-boot.itb`, `FES/RECOVERY.JSON` and the unchanged experimental FIT using
24 read-only admission/readback commands. The entire operation took 53.68 s.
No physical reset was requested or performed by this operator. No SD or flash
write, normal native restart, bottom-kit or J8G operation occurred.

The prior fe4 native return failure remains failed and its procedure consumed.
This is a new recovery qualification, not a retry or revision of that verdict.
The new witness reports JTAG boot kind 4, cold reset cause 4, CPU0 at EL3 with
MMU/data cache off, secondary CPUs parked and HPS masters/bridges held in
reset at both observations before DDR/SD initialization. The recorded nonce
proves that the newly selected SPL ran; programmer success alone is insufficient.

| Input or observation | SHA-256 |
| --- | --- |
| Recovery SOF | `a4186213c3c62e5975f1b3a5615f9861fb566a762f45e81cc5d056903cf01142` |
| Prior failed native wire | `2bb22ae6c869d8693e5ea9887d3dcb9d138d3d64d4fa77640417e78113681cc3` |
| New recovery boot wire | `28f4cf4bec4354a0710143cfac6d8fe42f8f1104f202d6830af245a7ed7ed08f` |
| Protected recovery FIT | `3722e24c551a794dd970e665cc4932b96cb62aefa353f39079285458d67d48e5` |
| Unchanged experimental FIT | `df0cabe09eb4dbc6453e55766f5e1dfecd3166750e165b554c81d3435ef15520` |
| One-use procedure | `d4b36301dabb313f2dd547dc7abdad98e081aa1f5c2f181231c8adae7debfb7f` |

[Raw boot wire](recovery-boot.log), the [complete readback wire](protected-readback.log)
and the [independent audit](hardware-audit.json) are committed. The auditor matched the
exact boot-wire prefix to the controller's digest, rejected five damaged
physical witnesses, checked the read-only command family and observed command
outputs, and independently verified that the private server/programmer were
absent, top UART/adapter handles were free and original USB node permissions
were restored. The transport verified restoration of its original JTAG clock
and auto-adjust settings before stopping its owned server.

The SPL was built using the pinned Terasic U-Boot source. Recovery configuration,
full device tree and SPL device tree were byte-identical to the loader already
on the spare card. The compiled console resolves to UART1 at `0x10c02100`.
The probe SPL is 257,251 bytes, within the unchanged 256 KiB OCRAM limit.
The package independently decoded the new SPL from the composed SOF and checked
preservation of the reference's other FPGA/configuration sections. Composition
held `/tmp/lander-quartus-powerboat-pro.lock` under Quartus Pro 26.3.

Before the physical test, 82 focused tests passed, including execution of the
actual generated C witness, per-register read faults, CPU/reset/cache ownership
failures, stale/partial/spliced UART data, fresh-scan failure, uncertain
programming, changed ownership and protected-card readback failure. The target
SPL/FIT build and separate diagnostic package admission passed. Earlier failed
off-board builds are retained; no failed physical attempt was retried.

This establishes a tested HPS/FPGA rebootstrap route to a read-only recovery
loader for this retained failure context. It does not establish SDM reset,
fabric drain, POR behavior, recovery from every hang, card-write/rollback
acceptance, or a normal native launch/return cycle. Altera's
[reset response table](https://docs.altera.com/r/docs/813773/25.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/device-response-to-configuration-and-reset-events?contentId=b~SI8o4QrB3o1F5em~BvFQ)
distinguishes HPS/configuration reset from the SDM reset caused by power cycling.
A genuinely wedged SDM or inaccessible JTAG transport can still require power.

The board is left at the verified recovery prompt with the candidate FIT still
on its spare card and our claim held. Future restoration needs a fresh bounded boot transaction and its own live
`UploadSession`; the closed verification session and its volatile marker are
not adopted. This one-use receipt grants no old source reuse or ordinary
runtime resume. The whole secure-system investigation remains open in
[issue #1867](https://github.com/DeanoC/fes/issues/1867).

Exact artifacts, generated/vendor sources, private transport/operator files,
prior failed wire and completed test/build logs are retained at
`/home/deano/fes-retained/de25-reset-recovery-20261117-r1`.
[Retention receipt](retention.json) records the tested-source archive and
artifact archive digests; all 44,244 regular artifact members were read back
and verified. The source archive was also checked against all 17,105 Git blobs
at the tested commit. The wider software check was still running during artifact
retention; its complete final log/result are retained in the `software/`
supplement. [Software results](software-checks.json) record all 11 selected
checks passing: 2,887 parent tests (21 skips), native/firmware/RTL integration
and 2,380 FPGA tests (90 skips), in addition to the focused recovery checks.
