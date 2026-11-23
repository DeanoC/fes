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

## Open at the PR head

The attached artifacts record the exact pre-merge test. The seal and the
TRWU8CBL run bind to `50157db7d` (both fits) and `92763642c` (TamaGo
inputs). The branch has since merged main twice, and no recorded fit or kit
run covers either merge:

- `3c5a4d70a` merges main at `345c2f8be`. It brings #2072, which adds a
  6567R8 NTSC parameter to the C64 VIC RTL (`c64_pal_timing.vh` becomes
  `c64_timing.vh`, and the VIC, its fetch and its sprite fetch change). It
  also brings native runtime changes (#2099, #2102, #2103, #2105, #2107).
- `817f72db5` merges main at `eb4a855a5`. It brings #2090: canonical Qsys
  and qip generation in the HPS builder, and one compile per producer with
  no repeat fit. It also brings TamaGo DMA, interrupt, storage, power, RTC
  and watchdog changes (#2116, #2131 to #2134, #2136 to #2138, #2140 to
  #2143). Its conflict resolution in `cmd/fpgaload/tenfoot.go` arms the USB
  interrupt inside the UART-only guard.

`git log --oneline --no-merges f8363b991..3c5a4d70a` and
`git log --oneline --no-merges 3506030fa..817f72db5` list each merge's
commits.

At `817f72db5` the SDK still required a repeat fit, so no package from the
single-compile producer could be staged for a kit. After `817f72db5`, the
SDK stages the signoff fit, and `c64-clocks.sdc` escapes the bracketed
readiness patterns that the Linux `pal` fit reads. The `nativepal` fit
takes the fetch-snapshot branch instead. Integrated hardware validation
needs a new `nativepal` seal at the PR head and a kit run. It is open.
