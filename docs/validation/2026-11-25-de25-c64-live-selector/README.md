# Native C64 in-play disk selector on TRWU8CBL

TRWU8CBL booted the matched prototype from SD into native Tenfoot. A USB keyboard
launched C64 PAL and its diagnostic displayed `PASS` on HDMI. The initial D64
transfer took 3.027 seconds and instruction fetch was verified in 3.941 seconds,
within the 5-second launch limit.

Ctrl+Alt+M opened the selector over the running C64 picture. USB Enter mounted
`SIDE2.D64`. The selector closed and the C64 picture reappeared. Opening the
selector again and selecting `C64.D64` restored the original disk. All four
observations carried the same owner, `77128746162639967`, and native generation 1.

| UART line | Selector | Mounted disk | Instruction fetch count |
| --- | --- | --- | ---: |
| 963 | Open | C64.D64 | 1,199,592 |
| 970 | Closed | SIDE2.D64 | 8,643,949 |
| 973 | Open | SIDE2.D64 | 21,182,443 |
| 979 | Closed | C64.D64 | 24,082,076 |

Ctrl+Alt+Escape returned to Tenfoot on HDMI. The keyboard Q press produced no
Quit event. The bounded UART Park completed at generation 3, the UART was
closed, and the transport cleanup passed.

The diagnostic prints `PASS` after the initial guest disk read and leaves that
text on screen. The swap receipts verify the mounted bytes, ownership and
continuing execution. A guest read after replacement remains unverified.

## Tested artifact

- Prototype runtime source: `e2c4d5a1a`
- Fitted FPGA source: `078f90c20`
- Boot ID: `75f1f40c06688c9e`
- Sealed C64 package ID: `3629447299b0ae29419572049b3f61f1d3aace9e8dd355b4f3657e7ac5745a10`
- C64 build ID: `6728c08f5deef5951aa982ae9051be39`
- SD image SHA-256: `a7417cb606af49fe4a1b23fc38d589f06fc5611f80f224bd5c1e1784369c89c2`
- Phase 1 SHA-256: `fab6a9d8042ec2fb0d8239dd57666d730b7788589d800ae0d04abf1b1feb8129`
- C64.D64 SHA-256: `2ba3d30f6384121156d2b429ab8d464f437b6524fd0e459135e458dda41756a0`
- SIDE2.D64 SHA-256: `087f05cb483249c74026092fd7d7281efd1646716359f1bb72fd6a1afda07374`

The board trial used the earlier BRAM-backed package. This branch combines the
selector with the current SDRAM-backed C64 disk store. That combination still
needs a matched fit, seal, and board trial.
