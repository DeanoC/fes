# Bounded native cycle with verified file reuse

This procedure retains the three consumed native-image attempts and allows one
new attempt, for a maximum total of four. The repair removes about 101 seconds
of repeated host derivation before the native collector starts. It preserves
the 450-second collector alarm, 480-second worker limit and every per-action
limit; it does not turn the previous failed trial into a pass.

The unchanged full validator runs in off-board preparation. Its file-only
result is bound into the [frozen operator](operator-source.json). Root preflight
and the actual worker load it only after fresh complete source/artifact hashes,
directory membership, frozen digest and claim checks. The measured worker prefix
took 0.77 seconds, with zero HPS workers, covering 5,488 files/eight trees.
The result carries no live hardware authority. Real boot CPU/memory/firmware
checks, independent one-use live JTAG scans, input ownership, FPGA identity,
ROM receipts, native drain and final Park checks remain active.

[image-readmission.json](image-readmission.json) binds the unchanged producer
`f8653867f` and boot identity `aa11ddf99761b275`. No firmware/FPGA rebuild or SD
image reburn is involved. The top board TRWU8CBL retains its spare card and
continuing claim; the user's factory-restore waiver remains. The bottom kit is
excluded. No physical controller action is required for this serial cycle.

The root preflight must bind current clean committed source, all cached and
additional inputs, exact tools/probe/helpers, unique top USB identity, free UART
and live claim. Complete prepared-chain retention and archive readback precede
any programming. The trial then runs one recovery, twelve fixed uploads and
readbacks, twelve fresh final reads, ZX81 → ZX80 → ZX81, four menu sessions,
seven activations and final owned Park at logical generation 7 / bridge 40.

Bounds remain: startup/launch/menu restore 100 seconds; configuration 20;
core close and final Park 5; programmer 90; native collector 450/process 480;
uploads 300/process 320; recovery process 270; outer 840/host 890. Live scans
remain bounded at 30 seconds inside those envelopes, traces at 8 MiB, console
at 1 MiB and passive failure tail at two seconds. First failure is terminal.
Consumed procedures and command phases never replay.

Host policy and actual-prefix mutation tests, staged programming/controller
tests, upload/receipt mutations and cleanup lifecycle checks pass. The full
parent suite passed 1,688 tests with three existing prerequisite skips on repair
source `973dba255`. These results qualify the software preparation only.
Native boot, the complete cycle, final Park, physical display/input, ordinary
package admission and DE10-equivalent readiness require their own evidence.

The [bounded hardware trial](hardware.md) now passes on its frozen operator
`2e6534540`: twelve uploads and fresh reads, three computer launch/return
cycles, four menu sessions, seven configurations and final Park at logical 7 /
bridge 40. The native collector took 428.67 seconds within its unchanged
450-second alarm. This is exact retained serial diagnostic evidence; ordinary
package admission and visible/input appliance acceptance remain open.
