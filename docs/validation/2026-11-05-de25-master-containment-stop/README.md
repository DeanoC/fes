# Fresh master containment: identity deadline remains

Compiled source `faf1b9f3ca58f16a660af8dcd190958a6f9a96a9`, operator publication `18521b9da`. One bounded attempt on TRWU8CBL, with no retry or subsequent native command.

Qualified recovery, seven fixed uploads/fresh SD readbacks, matched native Ready and all 14 CPU checkpoints passed. The expected 98-byte gamepad descriptor and seven reports preceded owned USB generation-2 Pause. The first ZX81 transfer reached STATUS/COMPLETE with 1,650,688 bytes retired and data held, spanning 14.610788678 seconds from ARM to COMPLETE. Native advanced beyond the unchanged configuration guard into computer identity.

The one-second guard then stopped the identity operation:

```text
reason=deadline operation=open phase=after-read
elapsed=1.034687528s step=51.754508ms limit=1s
offset=0x8 opcode=1 index=0 polls=1/64 reads=19 writes=1
```

One identity command was issued, but its first stable acknowledgement observation did not complete before the deadline. No identity or Run was accepted. The final checked read fell from 66.579352 ms to 51.754508 ms, but remains too expensive for the complete identity. These are whole checked-read timings, including before/after firmware ownership observations; no breakdown isolates raw MMIO, barriers or callback cost.

The source preserves full fresh clock/pin/owner checks and before/after containment for every computer access. Its selected master and firmware-gate paths issue no PHY transactions, as the source-bound C tests and compiled hook proof establish; explicit USB readiness queries retain them. The three-cycle fixture recorded 2,196 board-supplier accesses per bridge observation and 366 per gate, with zero PHY requests. These software work counts do not establish the physical duration of individual steps.

The service entered `reboot_required` at generation 1 and retained uncertain resources. No accepted launch/return, USB resume, quit or ordinary package activation occurred. Master-stop reported complete with data held. The full panic tail was retained; CPU halt was not independently proved. Independent cleanup verified unchanged 15 MHz, restored exact USB permissions, private JTAG exit and a free UART. The exclusive claim and test card remain; the factory card and other kit were untouched, with no factory restore under the user's standing instruction.

Evidence is retained at `/home/deano/fes-retained/de25-master-containment-stop-20261105-r1`, with candidate/source prerequisites at `/home/deano/fes-retained/de25-master-containment-20261105-r1`. Next: measure and reduce read-only policy-sampling overhead while preserving every clock/pin value and fresh containment check, callback failure retirement and the existing limits. Normal FES package/ROM/media admission, physical controls/video, native Ethernet and DE10-normal-kit readiness remain unproved.
