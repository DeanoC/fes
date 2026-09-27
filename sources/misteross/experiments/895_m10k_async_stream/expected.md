# 895 74.25 MHz asynchronous M10K stream

This diagnostic fills one inferred 256x40 M10K, then checks 65,536 reads at
the pixel-clock cadence and 65,536 reads with each address held for an extra
cycle. The read address is `scan_address + 1`, matching the menu FIFO
look-ahead shape. Writes and reads occur in separate phases, so a
read-during-write collision cannot explain a mismatch. The GPI signature is
`0xD895`; bit 15 signals completion, bit 14 PLL lock, and bits 13:0 count
errors (saturating). GPO bit 3 selects the held-address error count on page
0; zero selects the at-speed count. The test does not exercise the full menu
reader or HDMI.
After completion, the gate can stop the RAM clock while GPO bits 15:8 change
the read address. With GPO bit 3 set, pages 1–3 report the live 40-bit read
data. A true combinational read must change from the word at address 1 to
the word at address 2 without another clock edge.

GPO bits 2:0 select readback pages: 0 is status; 1 is first bad address;
2/4/6 are the low/middle/high words of the first expected datum; 3/5/7
are the corresponding observed words. The probe prints those pages on error.

`make sim EXP=895_m10k_async_stream` checks the digital sweep.
`make oss EXP=895_m10k_async_stream` must infer exactly one asynchronous
256x40 M10K and close 74.25 MHz timing. The async address-to-data arc is an
estimate, so signoff does not establish physical correctness.

Claim the designated kit with `scripts/kit.py session`, load the exact RBF,
then run `hardware/probe.sh` on that kit under the held lease. Stop and
release the session afterward. No Quartus comparison is implemented.
