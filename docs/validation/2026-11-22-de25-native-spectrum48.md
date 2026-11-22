# Native Spectrum 48K shared-shell trial

On 2026-11-22, TRWU8CBL booted the opt-in native Tenfoot image from the spare SD card with a matched volatile JTAG startup. In one boot, the operator launched both fitted Spectrum 48K builds with the keyboard, observed the diagnostic green border after tape playback on both, and returned to Tenfoot with Ctrl+Alt+Escape after each. Serial replay verified the same boot owner across the sequence and final Park at Tenfoot generation 5. Host hardware handles were closed.

| FPGA build ID | Package ID | Startup control | Tape delivery |
| --- | --- | ---: | ---: |
| `4e0c321de1822ab60f0af9004c0d5aaf` | `416b2638ce1c0b843161d59ce856a282f17cfd84f50a7b8adb8c88b85a3d52a1` | 673.455 ms | 1033.534 ms |
| `fad69cce3a737598d24e3b813be1c30b` | `fccb28ffdb2e23c340f4da734eec82af155a11dc0c0aeddc847029316c1a4271` | 673.642 ms | 1034.197 ms |

Startup control includes the 16 KiB ROM upload and fetch checks. The one-second limit applies to startup control. The 66-byte PZX files each converted to the same 104-byte cassette stream. The live observer initially expected the source length and later compared the USB lease generation with the Tenfoot session generation. Offline replay of the retained serial captures verified the converted tape receipt and the independent USB Stop and Tenfoot Park receipts.

The tested source was `4a9485803fd72fac705e9c7cdb9c6d2294b5fc4b` plus the firmware and TamaGo changes preserved in `4a8ba33e6`, before the main merge.

| Artifact | Identity |
| --- | --- |
| Native boot ID | `a4288659b9315f68` |
| SD image SHA-256 | `f9080a8d212520f9497c5997205176b7b3d7676d678d7e90d23d53413d476a25` |
| Boot FIT SHA-256 | `362a7f9ff0a4448f11b5c480a6af291e41515bdd56028149e87a8667ab434f0f` |
| Volatile phase-one RBF SHA-256 | `9a7f6d185036ddd9794a3f1d0d3cd17ab12af5a7f4571082b8b89228e3f40f12` |
| BL31 SHA-256 | `aa6346bb6b56b9fb89e530572609cb5b7e42e8c75cf79fa9a89d9147659d828f` |
| TamaGo ELF SHA-256 | `be08a7b54552d9f181f2dc0b9a45549e9d1226564fd6dcf1f6007d6552d67f98` |
| First serial capture SHA-256 | `90d36909cf5fd50cfa5b72f387890424a8bbc7a576f068212531e2910c56dd34` |
| Same-boot continuation capture SHA-256 | `d1ff7fd547a70e44817bcc72bba8a936c3d1453173238a1edd86ff2d2beec532` |

This was a diagnostic trial on TRWU8CBL. The integrated PR head requires its own qualification. Deliberate DMA faults and wedged-master recovery were exercised off-board. Factory restore remains deferred under #1867.
