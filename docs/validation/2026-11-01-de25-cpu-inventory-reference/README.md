# Nano CPU inventory reference and recovery

A bounded factory comparison shows that `CPUINRESET` is not a complete live
CPU-running/reset inventory. With all four CPUs online it reports `0x1`; after
CPU1 was logically offlined it still reports `0x1`. CPU1 power status changes.
The subsequent CPU1 online request and restore guard both failed, so hotplug
testing stopped. A clean shutdown and exact reference-SOF reload restored a
fresh authenticated factory boot with all four CPUs online. No native policy
was relaxed. See [evidence](evidence.json).

## Read-only reference

The complete previous [EL3 initial trace](../2026-11-01-de25-initial-trace/README.md)
was retained before the necessary factory comparison. Original kernel, root,
card CID/capacity/model and a fresh boot UUID were authenticated through the
exclusive TRWU8CBL UART. Factory Linux reported CPUs `0-3` online, possible and
present: two A55 and two A76 cores.

The first helper paused at an administrator prompt and was resumed without
replaying its pending command. BusyBox was absent, so that attempt obtained
no register values. Early host-helper initialization errors also occurred
before their target read commands were sent; their sources/receipts are kept.
A generic exact-offset four-byte `pread` then returned EFAULT at the first
reset-status read. Its precise fault cause was not established.

A fixed-address AArch64 reader succeeded. It opens `/dev/mem` with `O_RDONLY`,
creates only `PROT_READ` mappings and uses a disassembly-checked single
aligned `LDR W` with barriers for each register. It has no register-write
operation or dynamic runtime dependency. Source and ELF hashes are in the
build receipt. It was transferred to a fresh directory on verified `/run`
tmpfs, SHA-256 checked before execution, and removed afterwards. Raw nonce
frames, exact read order, reader ABI/hash, before/after CPU masks and actual
cleanup lines were independently checked.

The [Linux I/O access documentation](https://docs.kernel.org/6.12/driver-api/device-io.html)
requires access widths and mapping appropriate to device memory; success of
this mapped reader does not prove why the earlier generic read failed.

| Register | All four online | CPU1 logically offline |
| --- | --- | --- |
| Reset cause `0x10d11000` | `0x00000004` | `0x00000004` |
| CPUINRESET `0x10d11090` | `0x00000001` | `0x00000001` |
| CPURSTRELEASE `0x10d11094` | `0x00000000` | `0x00000000` |
| CPU0 PWRSTAT `0x10d1401c` | `0x90000021` | `0x90000021` |
| CPU1 PWRSTAT `0x10d14024` | `0x90000021` | `0x80000021` |
| CPU2 PWRSTAT `0x10d1402c` | `0x90000021` | `0x90000021` |
| CPU3 PWRSTAT `0x10d14034` | `0x90000021` | `0x90000021` |
| MPU PCHCTLR `0x10d14050` | `0x0000003f` | `0x0000003f` |
| MPU PCHSTAT `0x10d14054` | `0x0000003f` | `0x0000003f` |
| MPU BOOTCONFIG `0x10d14058` | `0x0070021e` | `0x0070021e` |

## Secondary transition and stopped restoration

One controlled CPU1 experiment used the documented
[Linux CPU hotplug interface](https://docs.kernel.org/6.12/core-api/cpu_hotplug.html),
with CPU0 kept online and an EXIT restore guard. CPU1 became offline and the
`0,2-3` mask plus all ten read-only observations above were captured. Bringing
CPU1 back failed (`failed to come online`); the guard's restore request also
failed (`psci: failed to boot CPU1 (-22)`). Four-CPU restoration and automatic
tool cleanup were therefore **not** claimed. No further hotplug experiments
or direct register writes were attempted.

The inspected TF-A source pin
`580dcb5c4e10aa7daff2bf69d493c076a6d3e665` has a GIC-only
`socfpga_pwr_domain_off`, while `bl31_plat_set_secondary_cpu_off` is declared
and defined without a call in that tree. This is a lead, not a proved cause:
the factory BL31 bytes were not independently extracted and matched to that
source pin. Neither logical hotplug nor this failed restore qualifies a native
CPU-stop callback.

## Verified recovery

The remaining hash-verified private tmpfs reader was removed explicitly. The
same original boot/card were checked, Linux shut down cleanly, and the exact
reference SOF `e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09`
was loaded once by volatile exact-cable JTAG. Fresh UART authentication matches
the original kernel/root/card/model and verifies CPUs `0-3` online. New boot
UUID: `0eb2188f-ddb1-4f0f-a1ad-b91ad1ed25e6`. UART was logged out and closed.
No physical power cycle, card swap, card/flash reimage, boot configuration or
shared JTAG service change occurred. Normal Linux login/shutdown accounting
can update the existing filesystem. The exclusive claim remains held for the
continuing Linux-free bring-up.

The [official FPGA-first boot flow](https://docs.altera.com/r/docs/813762/25.3/hard-processor-system-booting-user-guide-agilextm-3-and-agilextm-5-socs/boot-flow-overview-for-fpga-configuration-first-mode)
describes non-boot cores starting in WFI. The ownership contract must therefore
qualify secure initial parking and prevent later release; it cannot infer a
complete live inventory from the reset word. The next contained EL3 trace
adds first-C and pre-park power/request/boot-configuration observations. These
remain diagnostic values, not an admission grant. Native DDR/handoff, verified
core loading and real launch/return are still unqualified.

Full receipts, unsuccessful helpers, fixed-width reader source/ELF/disassembly
and recovery evidence are retained with checksums under
`/home/deano/fes-retained/de25-nano-cpu-inventory-reference-20261101/`.
