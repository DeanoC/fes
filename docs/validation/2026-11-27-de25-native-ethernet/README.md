# Native Ethernet startup on TRWU8CBL

On 27 November 2026 a volatile SPL sampled MAC0's clock, reset and stream
routing before DDR or storage initialization. Both samples agreed. The MAC
and its ECC interface were held in reset, so the SPL skipped the MAC version
register and parked. The installed 4 GB C64 card stayed in the board.

| Register | Both samples | Interpretation |
| --- | --- | --- |
| Clock manager status | `0x02030300` | Both PLLs were locked and their transitions had completed. |
| Peripheral reset 0 | `0xff7fbfff` | The TSN0 functional and ECC reset bits were set. |
| TSN0 stream control | `0x00000030` | Both secure selection bits were set and both stream-valid bits were clear. |
| TSN0 read/write IDs | `0x00000000` | The later BL31 assignment of nonsecure SID 1 had yet to occur. |

The diagnostic SOF was `5473592561f281ade6761e0434836f2e15a57415ad2404ae246aa5929718e440`,
with SPL ID `d885bcf2aa670022`. The capture contains the initial CPU trace,
both register samples, the reason for skipping the MAC register and the
terminal Park marker. [Exact UART bytes](startup-uart.gz) · [Run and restore receipt](startup.json)

The operator then loaded the matched C64 image into volatile memory. Its UART
reported boot ID `af4407e48b21634e` and RAM preferences. C64 reached Ready and
accepted the single Quit command. The launcher parked at generation 1, and
the operator verified its board handles were closed. Both operations left SD
and QSPI untouched. This measurement covers startup before DMA. On-board
packet transfer and discovery have separate acceptance criteria. [CBL Ethernet issue](https://github.com/DeanoC/fes/issues/2357)
