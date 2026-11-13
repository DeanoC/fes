# First shared USB/Tenfoot hardware attempt: configuration timeout

TRWU8CBL, native/firmware `adcb08ee40a7ec223622e8e01a19e1a388a85eb8`, operator checkout `4095b0fd1`. One attempt, no retry.

Recovery boot and all seven fixed uploads/readbacks plus fresh hashes passed. The matched native image reached Tenfoot Ready with ZX81/ZX80, all 14 CPU checkpoints, complete sealed HPS context/pin observations, and the expected 98-byte gamepad descriptor. Seven physical reports were delivered before the first launch paused USB polling at its existing generation 2.

The first ZX81 configuration exceeded the existing 20-second native configuration deadline. The service published `reboot_required`, generation 1, and retained resources. No core cycle, USB resume, return or quit completed. The operator stopped, saved its receive-only tail, and sent no follow-up command. The board is parked with the test card retained; factory card remains untouched. Independent cleanup verified original15M, exact USB permissions, private JTAG gone and UART free.

At the first write observation, 10.101 seconds had elapsed since arm, of which 2.482 seconds was measured hashing. The earlier successful serial-only load reached that observation in 3.172 seconds, including 2.487 seconds hashing. The extra cost is outside hashing; this does not prove a specific instruction's physical latency.

A separate injected-register run of the actual cold, board, USB and private master C paths found 47,434 physical register accesses and 496 ULPI PHY read transactions in one `fes_nano_bridges_observe`. A direct cold ownership check added 1,530 accesses and 16 PHY transactions. Counts repeated across all three model cycles. Configuration guards repeat these checks. This identifies a substantial repeated observation cost to investigate; it grants no timing or electrical acceptance to an optimization.

The original deadline and ownership failure behavior remain unchanged. Normal package/ROM/media admission, physical controls/video and DE10-normal-kit readiness remain incomplete. `retention.json` binds the exact failed trace, procedure and cost model, with the matched candidate retained separately.
