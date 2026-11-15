# Computer identity guard: deadline before the first command

Compiled source `b5bf86ca0d05727b1b41d997b12ddae517a64faf`, operator publication `e2f2cec26`. One bounded diagnostic attempt on TRWU8CBL. No second attempt, command replay or same-image retry.

Qualified recovery and all seven fixed network uploads with fresh SD readbacks passed. Matched native startup reached Ready with all 14 CPU checkpoints, the exact sealed pin/clock contract, the expected 98-byte gamepad descriptor and seven reports before owned USB generation-2 Pause. The first ZX81 transfer again reached STATUS/COMPLETE with 1,650,688 bytes retired and data held. The hardware ARM-to-COMPLETE span was 15.145826073 seconds, and native execution advanced beyond the unchanged configuration guard.

The new diagnostic identifies the next failure exactly:

```text
reason=deadline operation=open phase=after-read
elapsed=1.065466487s step=66.579352ms limit=1s
offset=0xc opcode=1 index=0 polls=0/64 reads=16 writes=0
```

The driver expired during the first identity command's preliminary stable-state reads. It had issued no command write and performed no acknowledgement poll. The final checked read, including its fresh secure ownership brackets, consumed 66.579352 ms. This excludes ACK-poll exhaustion and backwards clock for this failure. The trace measures the entire checked read; it does not separately time the raw Avalon read and each firmware observation.

The previously retained actual Go measurement shows 266 reads and 16 writes for a healthy complete identity, bracketed by 564 full ownership observations. Together with the actual C fixture's work counts, this makes repeated full USB/board readiness work a concrete optimization target. The per-CSR live ownership/containment obligation and one-second/64-poll limits must remain; cached readiness cannot grant ownership. Separate device readiness qualification from narrowly scoped fresh master-containment checks only after tracing and testing that contract.

The service entered `reboot_required` at generation 1 and retained uncertain resources. No accepted live computer identity, Run, USB resume, launch/return, quit or ordinary package activation was observed. Master-stop reported complete with data held. The complete panic tail was retained without a secondary error; CPU halt was not independently proved.

Independent cleanup passed: original 15 MHz unchanged, exact USB permissions restored, private JTAG server gone, UART free. The exclusive claim and test card remain, the original factory card is untouched, and factory restore remains waived under the user's instruction. The other kit was untouched.

Raw traces, source receipts, upload/readbacks, frozen helpers, bounds, work evidence and cleanup checks are retained in `/home/deano/fes-retained/de25-computer-guard-stop-20261105-r1`; the restored candidate/source prerequisites remain in `/home/deano/fes-retained/de25-computer-guard-20261105-r1`. Normal package/ROM/media admission, physical USB controls, HDMI, native Ethernet and DE10-normal-kit readiness remain open.
