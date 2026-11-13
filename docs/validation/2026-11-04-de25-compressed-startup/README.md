# Stopped compressed native startup on TRWU8CBL

One bounded attempt on `178d3737be00a2a16e5638570eda94eaf63ac4f9` reaches the entry loader's EL1
handoff, then stops at the unchanged 100-second SD-package stage deadline.
There is no native package-retention, frame, master-stop or final closure
receipt. This attempt is **failed**. The preceding twelve-frame pass on
`26dbec9ce` stays separately retained.

The full Tenfoot command handler is linked and exercised by the selected
search/text fixture. The complete ELF is 14,921,903 bytes, its encoded
payload is 7,169,112 bytes, its entry image is
7,196,449 bytes, and its matched FIT is
7,741,204 bytes, below the unchanged 8 MiB limit. Go runtime
metadata is preserved. Decoding uses the existing owned RAM at
`0x91fe0000` with a 32 MiB cap. The conservative entry-stack bound
is 5,376 bytes against the existing 16 KiB stack.

All seven affected software checks pass. The actual freestanding decoder
passes differential, truncation, checksum, bound and overlap tests, plus 1,024
address/undefined-sanitizer mutation cases. Nine QEMU entry guests pass, and the
full model launch/return/failure suite passes through the compressed loader.
These results do not establish Nano boot or ordinary physical admission.
The first software-only prototype exposed an unaligned trailer read. Its exact
source, image and failure are retained separately; strict alignment and local
EL2 parking vectors are present in this tested source.

The reused FPGA producer is `df234b524d0e1340b343df411c841bd1dc553817`. Recovery, both Ethernet
uploads and fresh SD readbacks pass before native programming. The
[raw capture](native-console.log) independently reproduces the same incomplete
`el1` checkpoint: [replay](replay.json). The loader checks decoding and ELF
bounds before that handoff. The log has no early CPU timings or Go main/SD
checkpoints, so it does not establish whether time was spent before Go startup,
in Go initialization or in SD retention. The complete native receipt spans
approximately 125 seconds, including programming and boot observation.

A later bounded, read-only [serial follow-up](passive-tail.json) receives zero
bytes and sends no commands. Final bridge state remains **unknown**. There is
one failure on this source, no retry and three prior failed window procedures.
Native240, encompassing phase270, SD-stage100 and transfer/read/return45-second
limits remain unchanged.

Independent host cleanup verifies clock `15M` readback, exact USB permissions,
stopped private JTAG and closed UART. The continuing exclusive claim and spare
card remain; the factory card stays safe outside the board. There is no factory
restoration. The [result](result.json) keeps software, startup and physical
acceptance separate. Ordinary core admission, the native UI loop, HDMI, hub
input and native networking remain open.

The matched stopped chain, all codec software prototypes, protected recovery
and selected SDK sources are retained in `/home/deano/fes-retained/de25-full-command-entry-20261104-stopped/stopped-native-chain.tar.gz`
(SHA-256 `98f3732a60c0da7c948a9eaa3f73740b870877b54dfddef34a8e3efcc3a3b1d7`). Every 45,984 member
reads back against its manifest. The standalone source bundle restores without
alternates and passes strict fsck. Expanded working files are preserved.

The next change will add CPU-only decode/copy timings and Go/SD checkpoints,
and evaluate instruction caching for the trusted loader while preserving its
clean-entry, data-cache, memory and time constraints. No same-source hardware
retry follows this stop.
