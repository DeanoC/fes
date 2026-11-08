# First initial-tape hardware attempt stops at the operation deadline

One bounded TRWU8CBL attempt used operator
`cd342603adf7ad3e12af12b4e8c96a052e6fd88d`, compiled source
`ba2f1ef5e08cbfb42d94f5fcf42c3e807b3534bc` and boot identity
`71ae1d6bc14ee11d`. Its original result is FAIL. The procedure and its one allowed
attempt on this new image are consumed; there was no replay or later launch.
The preceding image's four attempts remain separate historical evidence.

Recovery and native programming each passed a separate fresh one-use vendor
JTAG scan. All thirteen uploads/write-readbacks and thirteen fresh reads passed,
including the exact 917-byte tape. The matched secure/native boot and initial
FPGA menu passed. First ZX81 launch configured its selected core/ROM and reached
the held tape transfer, which hit the existing one-second operation deadline.
No computer launch completed and no final Park was observed.

The original native error records `load-tape`, `after-read`, opcode 8, index 35,
770 reads and 48 writes. Elapsed time was 1.000934915 seconds. The last guarded
read measured 486.585 microseconds before the register operation, 284.618 for
the operation and 495.382 afterwards. Those are last-access observations, not a
complete timing profile; they identify substantial repeated per-access cost.
The next repair must measure and reduce this overhead while preserving live
ownership/mapping checks, protocol validation and the existing deadline.

The runtime reported `reboot_required`, logical generation 1 and Held true;
it published no successful tape receipt or fetch-progress receipt. The compiled
launch path calls Run only after successful tape completion and another held
fetch sample. Thus no Run on this tape failure is a source-bound inference,
rather than an independent trace of each GP write. Firmware master-stop output
was captured during failure containment. This is not final Park or a verified
running application. The total procedure stopped after 264.622 seconds; the
outer wrapper did not time out.

After the failed launch the operator sent no further native command or image
programming request. Cleanup restored and read back 15M, restored exact original
USB permissions, stopped the owned private server and freed UART. These cleanup
clock operations are distinct from native application commands. An independent
host audit and saved-evidence join passed, including frozen source/assets and
both programming grants/client traces. Original JSON, console and error records
remain unchanged.

The continuing top-board claim remains held. Its spare card stays inserted and
the factory-restore waiver applies. The last reported application state is
RebootRequired, not Parked. The bottom kit was untouched. No physical tape
delivery/consumption, display/input appliance acceptance, ordinary admission or
full DE10-equivalent readiness is claimed.

See [hardware-result.json](hardware-result.json). Raw evidence is under
`out/nano-game-tape-trial-20261108/`; the already verified prepared-chain archive
is `/home/deano/fes-retained/de25-game-tape-prepared-20261108-r1`.
