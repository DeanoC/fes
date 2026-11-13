# Shared USB core load reaches computer control, then stops

Compiled source `91ae1bc0ccb585249c210362564373b5e77577db`, operator publication `27e2eae7dff85f6d078d13aa640a3467fd2d2be7`. One bounded attempt on TRWU8CBL; no retry or additional native command.

Qualified recovery, seven fixed network uploads with fresh SD readbacks, matched native Ready, all 14 CPU checkpoints, the sealed HPS clock/pin checks, and the exact 98-byte gamepad descriptor passed. Seven input reports preceded the owned USB generation-2 Pause.

The first ZX81 load reached both configuration writes, completion polls, STATUS and COMPLETE with all 1,650,688 payload bytes retired and the data path held. The hardware counter spans 15.2033254975 seconds from ARM to COMPLETE. Native execution advanced beyond its unchanged 20-second configuration guard and opened the control-only path. This repairs the previously observed late configuration return for this matched diagnostic; it is not general timing qualification.

The native service then reported `computer operation exceeded its time or poll limit`, entered `reboot_required` at generation 1 and retained uncertain resources. No successful live computer identity, Run, USB resume, launch/return, or quit cycle was accepted. The error combines elapsed timeout, backwards time and exhausted acknowledgement polls; the saved trace cannot distinguish them. Per-register computer accesses currently bracket every read/write with full secure bridge observations. Their physical cost is a hypothesis to measure, not a proven cause.

Master-stop reported completion with the data path held. The complete panic tail was received without a secondary collection error. CPU halt was not independently proved. The procedure stopped without retry. Independent cleanup verified unchanged 15 MHz, exact USB permissions, private JTAG server exit and a free UART. The exclusive claim and spare card remain; the original factory card is untouched, and no factory restore was performed under the user's standing instruction.

All raw traces, frozen helpers, source receipts, upload/readback receipts, bounds, model work counts and cleanup evidence are retained in `/home/deano/fes-retained/de25-prepared-ready-control-stop-20261105-r1`. The independently restored candidate and source prerequisites remain in `/home/deano/fes-retained/de25-prepared-ready-20261105-r1`.

Next: separate computer guard failure reasons and record operation/offset/elapsed evidence, reproduce the cost through the real injected native/firmware path, and repair the confirmed bottleneck without widening the time or poll limits. Ordinary package/ROM/media admission, physical controls/video, native Ethernet and DE10-normal-kit readiness remain unproved.
