# DE25 repeated-frame native diagnostic: stopped attempt

TRWU8CBL boots the matched native image, retains the verified SD package,
configures the unchanged qualified FPGA and reads/drains its first updated
frame. The host's 270-second native phase bound then expires before a complete
twelve-frame return. This attempt is **failed**; it proves no final master stop,
bridge closure, repeated-frame acceptance or ordinary core admission.

Native Go, entry and firmware use `51b6939c83649cdb33cc4caea03f3b9443c0b0bf`. The
unchanged FPGA producer is `df234b524d0e1340b343df411c841bd1dc553817`, package
`a2388c4d7d8640d6e70d51c4d164e3c803157e3fb21e91451d6febd74faf6017` / build `7fa7e3c27ec2fb055e148514b8dac7a2`. Native boot ID is
`fc1a87eed51e468f`. The complete FIT is 7,964,660 bytes, within the unchanged
8 MiB limit. All ten affected software checks and the matched build pass on the
exact native source. The fixture supplies changing view states to the real
renderer; it does not run the complete UI's network or launch command handlers.

Both fixed Ethernet uploads and fresh SD readbacks pass. Native package
retention takes 62.498 seconds; the configuration
counter interval is 13.593 seconds.
Both full-window permission gates and data-open generation 6/reset `0x5` are
observed. Frame 1 delivers all 3,686,400 bytes, CRC `0xf7429b0e`, reader generation
2/frame cookie 1 and matching local drain generation 2/cookie 2. Its publication
SHA-256 is
`a58b682bcf4749139141601be2b1f655c1019c627befc941a511c27b388a856d`.

The native helper receipt spans 109.119
seconds, including programming/boot and SD retention, before the controller
terminates it. Approximately 160.881
seconds of the encompassing 270-second phase preceded that receipt, including
repeated validation. The helper invokes complete candidate validation twice.
That budget consumption is concrete; it does not establish why no second frame
appeared. A separate 45-second read-only UART observation after the stop receives
zero bytes. It sends no command, requests no reboot and makes no retry.

The [raw capture](native-console.log) independently replays to the same
incomplete result: [replay](replay.json). No native reader timeout, firmware
master-stop failure or final closure is observed. The final bridge state is
**unknown** and the reservation remains pinned until reboot. Further tracing
must distinguish rendering, CPU publication and reader rearming before another
hardware attempt. Hardware bounds and seal checks remain strict.

Independent host cleanup passes: clock `15M` is restored/read back, exact USB
permissions restored, private JTAG stopped, UART closed and the exclusive claim
retained. The spare stays in TRWU8CBL; its factory card remains safe outside it.
There is one failure on this native head, two cumulative window-procedure
failures including the earlier narrow-width producer. No same-head retry or
factory restoration follows this stop.

The stopped matched chain, recovery files, selected external SDK sources,
frozen helpers and original traces are retained in
`/home/deano/fes-retained/de25-repeated-window-20261104/stopped-native-chain.tar.gz`
(SHA-256 `23875332bf2c61e6ffecd98b7b01f5ab350b5b0870792e0385c09f60e3fe0925`). Every archive member reads back
against its manifest. The standalone source bundle independently restores
without alternates and passes strict fsck. [Result and retention details](result.json)
include hashes and the private evidence root. Expanded working files remain in
`/home/deano/fes/out/dev/de25-embedded-os/out/nano-present-sized-20261104`.
