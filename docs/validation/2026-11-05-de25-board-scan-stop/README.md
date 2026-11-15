# Configuration completes in firmware; native deadline rejects the late return

TRWU8CBL, compiled source `db6729fe2560fa3cc33dafe06967268ccc32a712`, operator checkout `e6f8384b5`. One attempt, no retry.

Qualified recovery, all seven fixed uploads and fresh readbacks passed. The new native image reached Tenfoot Ready with the complete CPU/source/pin proofs and expected gamepad descriptor. Seven reports arrived before USB paused at its existing generation 2.

Secure firmware reached both writes, both completion polls, STATUS and COMPLETE. It reported the complete 1,650,688-byte payload and `FES_NANO_FABRIC_ACTIVATION complete=1 data_held=1`. The native guard then rejected the late result:

```
native program service: configuration deadline: elapsed=20.170364147s step=3.383110447s limit=20s
```

This proves elapsed-time expiry, rather than a backwards software clock. The late successful firmware return remains a failed native launch: no computer session opened, USB did not resume, and no launch/return/quit cycle completed. The service retired at native generation 1 with resources retained. Firmware completion with data held is not ordinary runtime activation or core acceptance.

The repaired receive-only tail collector captured the panic stack through the native main frame, without a secondary collector error or command writes. CPU halt is not independently proved. No further key, reset, retry or recovery command was sent.

Independent audit verifies startup, pause, the firmware trace, exact guard measurements and the first failed launch. Cleanup verified unchanged15M, exact USB permissions, private JTAG gone and UART free. The test card and exclusive claim remain; the original factory card is untouched. The board is parked with resources retained.

The next repair should remove remaining overlapping live-readiness/ownership work while retaining checks around actual operations and the full 20-second limit. The vendor entry macro already enables EL3 instruction caching; it is not evidence for an instruction-cache workaround. Ordinary package/ROM/media admission, physical controls/video, native Ethernet and DE10-normal-kit readiness remain incomplete.

`retention.json` binds the immutable trace, operator, timing/read-count evidence and separately retained matched candidate. The earlier shared-image failures remain retained independently.
