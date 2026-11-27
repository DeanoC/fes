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

## MAC startup after DDR calibration

A second observer on CBL reported two identical snapshots after DDR calibration.
MAC0 remained in reset, both SMMUs were disabled, their queues were empty,
and the six-word SDM response indicated completed FPGA initialization.
Two transmit prerequisites failed on this boot. The SPL read DAIF as
`0x2c0`, which leaves the asynchronous-error mask clear. The vendor clock
driver returned 250 MHz for EMAC0, while the original diagnostic expected
125 MHz. The peripheral register clock was 100 MHz. The observer parked
and the original C64 launcher returned to Ready and Park. The release check
found all board handles closed. [UART capture](admission05-uart.gz) ·
[C64 return and board release](admission05-release.json)

For gigabit RGMII the controller takes a 250 MHz reference and divides it by
two for the 125 MHz transmit pin clock. The fitted carrier routes EMAC0
directly to the dedicated HPS pins. A corrected transmit test can check the
reference, RGMII selector and negotiated gigabit speed while keeping the
existing PLL configuration. [Agilex 5 XGMAC clock table](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/xgmac-clocks?contentId=ZVIw1NaUa2BuEkJDgdUxAA)

## MAC release and DMA revocation

The corrected trial reached the controller identification registers after
installing a restricted DMA mapping. MAC0 returned `0xcafd5b3f` for
`MAC_HW_Feature0`. The diagnostic had compared that word to its reset
default of `0xca7d5b3f`. The difference is the live PHY interface
selection in bits 24–23, which matched the RGMII mode just programmed.
The diagnostic stopped before posting a transmit descriptor. It revoked
the mapping, read back the invalid stream-table entry, and verified MAC0
back in reset. The host observer recorded zero frames and zero drops.
C64 reached Ready and Park before CBL was released. [UART capture](tx06-uart.gz) ·
[Trial and C64 return receipt](tx06-release.json)

## Gigabit PHY observation

The next run read all three MAC capability registers and confirmed the
dedicated RGMII interface, FIFO sizes and channel layout. The KSZ9031
reported `0x0f49` in register 31. Its speed and duplex fields indicated
gigabit full duplex while its link status check fail bit was set. This
single sample stopped transmission before descriptor dispatch. The
target verified mapping revocation and MAC reset, and C64 returned to
Ready and Park. [UART capture](tx07-uart.gz) ·
[Trial and C64 return receipt](tx07-release.json) ·
[KSZ9031RNX register descriptions, table 4-4](https://ww1.microchip.com/downloads/aemDocuments/documents/UNG/ProductDocuments/DataSheets/KSZ9031RNX-Data-Sheet-DS00002117.pdf)

A subsequent trial retained both PHY snapshots at the end of its four-second
negotiation window. Both reported basic status `0x7949`, zero partner
advertisement, zero gigabit partner status and zero PCS link. The local
transceiver still advertised gigabit. The test withheld transmission and
returned CBL to the original C64 baseline. [PHY snapshots](tx08-uart.gz) ·
[C64 return receipt](tx08-release.json)

After the Ethernet cable was connected, the PHY reported a partner
advertisement of `0xcde1` and resolved gigabit full duplex. Basic status
rose from `0x7949` to `0x796d` over 100 ms. The remote receiver had yet
to report ready and the idle-error count reached `0xff`. The trial stopped
at that sample, revoked the mapping and reset the MAC. The original C64
launcher reached Ready and Park before release.
[PHY transition](tx09-uart.gz) · [C64 return receipt](tx09-release.json)

The next trial let negotiation continue after the transient errors.
At 8.378 and 8.478 seconds after boot, both samples matched the earlier
factory gigabit observations: basic status `0x796d`, 1000BASE-T status
`0x3c00` (both receivers ready and zero idle errors), and resolved status
`0x0348`. The PHY also set both PCS indicators in register 19 (`0x0806`).
The diagnostic had required the 100BASE-TX indicator to clear and stopped
before dispatch. The gigabit indicator and resolved speed identify the
operating link. C64 returned to Ready and Park.
[PHY samples](tx10-uart.gz) · [C64 return receipt](tx10-release.json)
