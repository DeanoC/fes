# DE25 managed image virtual boot

The actual managed-base rootfs and its matching owner kernel boot together
through Linux init on QEMU's AArch64 `virt` machine. The existing DE25 builder
now exposes this packaging check as `boot-smoke`, and the managed factory
producer requires it before publishing its factory receipt.

Tested producer source: `54440171df2ae454a87873099857c10de7e17263`.
Image producer: `cad04bd65d82c98f0e375de927051d215dd63af1`.
The earlier actual-byte preflight uses `c083e44e4`; it is retained separately.
Neither the kernel nor the base image is rebuilt or modified in this increment.

| Input or evidence | SHA-256 |
| --- | --- |
| Actual managed-base ext4 | `588421ad2ca56d9b4dae256772f46260068d680f0cdb5387faf3e1754206a9aa` |
| Actual matching owner kernel Image | `28201c24220c9ed11ac5943aa2583f647904d2f694796407d66a7728c1c1456b` |
| Original managed-base receipt | `9eb0231578a501c4e0c49818346dcf0454562dc436069dec594db010d5d34dff` |
| New actual virtual boot log | `945c8c053123464a4ae9f57b1f54855fa1b65ed139d326569e3a4617d187a45d` |
| New boot recipe | `07919e0082b49fde17e76d94c00af47f31e20bbc97831095339ae645ad93b7ea` |

The action requires an explicit managed-base or managed-factory selection. It
authenticates the image against its real Git producer using archived source as
data and current verification/kernel policy. The source and input image are
read-only mounts in the pinned unprivileged builder. QEMU uses a disposable disk
copy, Cortex-A53, 512 MiB RAM, a virtual block device and no network. It observes
the guest for 45 seconds, then terminates and reaps its own process. An
uncooperative process is killed and the check fails. Failed logs are retained;
existing evidence is never overwritten.

The shared init smoke must report a read-only root and writable tmpfs at `/run`,
`/tmp` and `/var/log`. Missing or conflicting markers, a kernel panic, a kernel
internal error, unsuccessful process exit, changed input or mismatched report
is refused. The report records image and boot producer identities separately,
exact image/kernel/input hashes, command, emulator version/binary hash, pinned
builder, bounded termination and the actual boot log hash.

For an existing authenticated managed base:

```sh
python3 image/de25-nano/build.py boot-smoke \
  --image-profile managed-base --output out/de25-managed-base \
  --boot-output out/de25-managed-base-boot
```

A successful future `managed-factory` build runs the same real boot check and
includes `boot.json` and `qemu-smoke.log` in its closed artifact hashes.
`verify-factory` checks that retained evidence against the actual factory,
kernel, producer recipe and init markers. It does not rerun unchanged evidence.
The ordinary package selector still has to admit sealed MENU before a factory
build starts. A base boot report cannot become factory evidence by selecting
another profile. The existing headless `smoke` remains a userspace-only probe.

## Executed evidence

- The actual preflight and the new public action each pass in 49.4 seconds.
  Both execute the real matching kernel and real rootfs. The new action also
  authenticates the old image producer before and after execution, and checks
  exact input hashes after its disposable disk is removed.
- All 24 focused boot/factory/base cases pass in 0.9 seconds. The new report
  fixtures use explicitly fake image/kernel bytes and an observer stand-in;
  they test refusal and binding, not boot. Separate real child-process tests
  exercise normal termination, unsuccessful exit and forced cleanup.
- Four actual CLI refusals pass in 0.5 seconds: foreign factory selection,
  existing proof output, missing profile and a headless profile. Existing
  evidence remains byte-identical; no refused output is created.
- All six affected commands pass in 214.0 seconds: 1,282 selected parent tests
  with six skips in 207.192 seconds, and 40 generated consumers/42 fixture
  copies. [checks.json](checks.json) records that affected-plan result.
  [source-scope.json](source-scope.json) confirms all 374 canonical MENU, 59
  combined-compiler and 58 runtime-test inputs are unchanged. No Quartus or
  combined RTL/CPU proof is rerun.
- [retention.json](retention.json) binds independently restored source, original
  jobs/logs, both virtual boot observations, CLI refusals and independent raw
  boot-log/input readback. The actual base/kernel remain in the verified
  prerequisite archive rather than being relabelled as newly built artifacts.

Source/archive restoration and independent readback pass in 2.1 seconds. The
restored tree is `2c736dbbe92a6de57ba4461429368ccdfbf6b7f4`. The 30-member
proof archive is 90,155 bytes, SHA-256
`60fb61405398d5614a679cb5f2889363383ef6987a9173feceff7e49c6d3646b`.
All 12 completed owned process/process-group identities and the two named
proof containers are checked absent.

[boot.json](boot.json), [cli-refusals.json](cli-refusals.json) and the retained
terminal job records keep the evidence boundaries explicit.

## Remaining acceptance

This is kernel/init/filesystem packaging evidence for a managed intermediate.
The log reports absent virtual Ethernet and persistent FAT, and a D-Bus
machine-ID write failure on the read-only root. Network/SSH, D-Bus/BlueZ and
managed runtime service readiness are not established. The D-Bus failure needs
a targeted rootfs repair and a freshly built/booted image; these smoke markers
do not erase that observed defect.

No complete admitted factory or normal card is built. High-level DE25
release/bootstrap/media receipt selection remains to be integrated, preserving
its real factory-boot requirement. Physical firmware, bootloader, CPU/reset,
SID/SMMU/coherent DDR, FPGA, HDMI, watchdog/update recovery and MENU/catalog ZX81
operation remain native acceptance gates. Output/whole-reset timing and an
eligible independent identical-RBF build still block sealed MENU. There are no
additional PCB files and no extra timing exceptions.

No board, UART, SSH, capture, SD, installed software/kernel or flash is accessed.
Bottom TRWU8J8G remains claimed at its last authenticated ABI2 baseline
(2026-11-01 14:48 UTC); it is not refreshed, restored, promoted or released.
Top TRWU8CBL is untouched. The full normal-development-kit goal remains open.
