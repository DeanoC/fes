# DE25 owner-kernel Linux FPGA loading and ZX81 hardware proof

This physical diagnostic uses bottom board `TRWU8J8G`, software/client source
`d2012e3ac6df0d66a8525bd2558735f7ec0a565b`, kernel producer
`8e7479d48d87a8b1977cc4040438cf378d35746c` and installed root metadata producer
`8281d03a1a7c91004af34461bb3a1b300bccc7c8`. It extends the
[owner binding and reboot proof](../2026-10-31-de25-owner-binding/README.md).
No production source or FPGA build changed during this trial. Normal runtime
admission remains closed.

## Exact artifacts and operations

The previously tested non-DMA BRAM ZX81 RBF has SHA256
`7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a`, producer
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc` and build ID
`06ee047f143317df0fd1d98717b2b991`. It matches the installed HPS-first bootstrap.
The native static AArch64 `de25-computer` client was rebuilt from the named
software source using the locked SDK; its complete runner took 5.6 seconds.
The overlay was compiled with the retained pinned kernel's DTC. Build receipts,
payload hashes and the exact overlay source/blob are retained here.

Two real configfs/SDM firmware loads completed with overlay status `applied`
and FPGA manager state `operating`, in 2.54 and 2.31 seconds respectively.
Both ran within boot `1cdac9ba-52eb-4268-87c0-d6533ec445f6`, without JTAG or a
power cycle. The owner preconfiguration syscall check passed before and after
loads; this core did not admit a scanout allocation. Twenty-one native client
operations passed, reporting actual CPU MMIO and explicitly denying production
runtime admission. Control addresses were limited to the known core's
`0x20010300` resource and 32-byte span.

The ROM-saved 923-byte BASIC tape has SHA256
`b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
After reset, LOAD and RUN produced 7. Editing line 1 to PRINT 9 and explicitly
running it produced 9. Reloading the original tape and running it produced 7
again, without reset or FPGA reconfiguration. After the second FPGA load,
reset/LOAD/RUN again produced 7. Captures used only the shared
`/dev/v4l/by-id/fes-ms2109-hil` fan-out and were visually inspected.

| Observation | Capture |
| --- | --- |
| Initial ROM K prompt | [rom.png](rom.png) |
| First BASIC RUN: 7 | [run.png](run.png) |
| Edited program RUN: 9 | [edited-run-confirmed.png](edited-run-confirmed.png) |
| Original tape reloaded and RUN: 7 | [reloaded-run.png](reloaded-run.png) |
| RUN after second FPGA load: 7 | [second-run.png](second-run.png) |

The first RUN sent immediately after the edit still showed the PRINT 9 listing
and K prompt; [that capture](edited-run.png) is retained. A later explicit RUN
produced 9. A key-delivery acknowledgment is not proof of ROM/editor readiness,
and this record establishes no fixed-delay input guarantee.

## Cleanup, software reboot and retained limits

The core was held before the owned overlay was removed. The owned configfs
mount, uploaded firmware and temporary client/media tree were removed with
absence checks. A software reboot then reached new boot
`9191e320-2d97-41a2-8e28-735ab8695f7c`, DHCP `192.168.10.69` and pinned-key SSH.
The generic first-boot observation/offline verifier and actual owner syscall
check passed again. The owner node remained root:root mode 600. All owned
UART, SSH and capture clients closed. QSPI and the newly supplied reader card
were untouched. The bottom board remains under the existing candidate trial
claim; it is neither baseline-promoted nor released as restored.

Overlay application emits two known OF memory-lifetime warnings per load,
for `firmware-name` and `config-complete-timeout-us`; all four warnings are
retained. No warning waiver or clean-kernel-log claim is made. Successful loads
do not establish bounded failed-transfer recovery, cancellation or concurrency.
Removing an overlay or warm-rebooting does not prove FPGA SRAM was erased.

No DMA grant, frame publication, external DDR transfer, SID/SMMU/firewall
physical acceptance, global reset/drain proof or new timing signoff occurred.
The running SD remains the earlier verified file-level maintenance update,
not a whole-card attestation of the final offline image. The production
AArch64 factory guard stays closed: the existing DE10 programmer directly
accesses Cyclone V registers and cannot be enabled on DE25 by relaxing a guard.
Managed kernel-backed programming/control, agent/catalog/MENU and normal ZX81
lifecycle remain required for DE10-equivalent development-kit acceptance.

[results.json](results.json) indexes the operation receipts and observations.
Complete retained build/trial artifacts live at
`/home/deano/fes/out/artifacts/de25-owner-zx81/d2012e3ac`; their independent
[artifact inventory](artifact-inventory.json) records hashes. Operational helper
scripts containing local authentication plumbing are excluded from the public
record. This evidence documents the named producer bytes, not a new source
revision or managed FES HIL pass.
