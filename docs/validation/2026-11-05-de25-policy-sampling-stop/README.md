# Grouped sampling improves control cost, but identity remains late

Compiled source `1423c2ee4a0d440a137d7301c614df73ea269f3d`, operator publication `fc9180d24`. One bounded attempt on TRWU8CBL; no retry or subsequent native command.

Recovery, seven fixed uploads/fresh readbacks, matched native Ready, all 14 CPU checkpoints, the expected gamepad descriptor and seven input reports before owned USB generation-2 Pause passed. The first ZX81 transfer reached STATUS/COMPLETE with all 1,650,688 bytes retired and data held. ARM-to-COMPLETE was 13.74495831 seconds. Native advanced past its unchanged configuration guard into computer identity.

The one-second identity guard then stopped preparation for the second word:

```text
reason=deadline operation=open phase=after-read
elapsed=1.025109997s step=30.145792ms limit=1s
offset=0x10 opcode=1 index=1 polls=0/64 reads=33 writes=1
```

The first identity exchange completed; the next word had not been submitted. The complete live identity, Run and any launch/return were not accepted. The final checked read fell from 51.754508 ms to 30.145792 ms, but the complete identity remains too expensive. This timing includes the raw read and its two fresh ownership observations; the saved trace does not isolate their individual durations.

The paired C fixture and shipping-source proof preserve every clock/pin observation while reducing ordering callbacks. The hardware result confirms an improvement, not deadline qualification. Full 164-pin qualification is still repeated inside every register's ownership path. The next contract should distinguish external device configuration/readiness from internal no-DMA master containment: keep fresh owner, epoch, reset, bus-clock, DMA/IRQ and idle proof on the master path, with full pin/PHY qualification still required for device readiness. Any such change needs explicit trusted hooks and failure tests; cached readiness cannot grant memory access.

The service retired at generation 1, retained uncertain resources and issued no USB resume, return or quit. Master-stop reported complete with data held; CPU halt was not independently proved. The complete panic tail was saved. Independent cleanup verified unchanged 15 MHz, exact USB permissions, private JTAG exit and a free UART. The claim/test card remain, and the factory card and other kit were untouched. No factory restore was performed under the user's standing instruction.

Raw evidence and frozen procedure are retained at `/home/deano/fes-retained/de25-policy-sampling-stop-20261105-r1`; candidate/source prerequisites remain at `/home/deano/fes-retained/de25-policy-sampling-20261105-r1`. Ordinary FES package/ROM/media admission, physical controls/video, native Ethernet and DE10-normal-kit readiness remain open.
