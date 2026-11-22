# DE25-Nano native C64 PAL trial

TRWU8CBL booted the matched SD image with a volatile JTAG startup. Tenfoot
launched the same sealed C64 package twice in one boot. The open diagnostic
read and checked bytes from a 174,848-byte D64 through the emulated 1541 and
showed `PASS` on HDMI both times. Tenfoot returned visibly after both launches.
The final Park reached generation 5, the host handles closed, and the kit
lease was released. The 4 GB spare stayed installed.

| Launch | D64 upload | Execution startup after opening the computer |
| --- | ---: | ---: |
| First | 2.822 s | 3.588 s |
| Same-boot relaunch | 2.824 s | 3.590 s |

The runtime keeps the five-second opening and execution gate and the one-second
non-media control budget. It checks the enclosing deadline against a monotonic
clock during D64 upload. The host serial capture was reopened after the first
playback to repair confirmation polling. The resumed trace retains the same
owner and continues from the verified running state. The UART-input test leaves
USB keyboard qualification open. Factory restoration remains deferred under
#1867.

[Hardware result](hardware-result.json) pins the matched boot ID
`fa72bf1e707edbc7`, the package, the disk digest and the traces.
[Evidence archive](evidence.tar.gz) contains the sealed package and its build
record, timing results, the image and card receipts, both serial captures
and the HDMI confirmations. Archive SHA-256
`13cf2a4801c2c9ee4dc83ea38bc92eb86a4c90927f9a035cfdb7f6831febc16c`.

The two independent Quartus Pro 26.3 fits at `50157db7d` passed package
signoff with no unreviewed findings and produced identical FPGA payloads.
Both used 357 of 358 M20Ks, with +1.853 ns worst setup slack and 0.000 ns
reported worst hold slack. The sealed `.fcore` SHA-256 is
`5221360b261b768c1bd01d77098241299a9db66e34348a883e4e6023cc6a5ae3`.
The board run used TamaGo compiler inputs verified against `92763642c`.

The attached seal and board run predate the merge of main at `345c2f8be`.
That merge brings newer shared C64 RTL and native runtime changes. The
attached artifacts document the exact pre-merge test and leave integrated
hardware qualification open.
