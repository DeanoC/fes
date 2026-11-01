# Nano automated upload and first cold serial trial

Tested source: `6253f834202005f91a3c89d6d8f6c146b0e27d9f`. This follows the
[off-board preparation](../2026-10-31-de25-cold-upload/README.md) on TRWU8CBL.
The Ethernet upload/write/readback passed. Native SPL rejected its initial
ownership proof before DDR/FIT loading, EL3 handoff or native execution.
No `fesboot` was sent and the rejection was not bypassed or retried.

## Physical upload

The original factory kernel, root filesystem, card CID/capacity, model and
boot UUID `8700edbb-81df-4107-9c04-c8d679cf0a60` were authenticated over UART.
The identified spare USB card was 3,980,394,496 bytes, unmounted, at its expected
reader port. Its protected 16 MiB prefix matched the previous recovery card.
The overwritten region was privately backed up, the combined image written,
and raw readback matched the prepared image. Factory Linux was cleanly halted
before the user inserted the spare. The original card and flash were preserved.

The exact top JTAG cable reported FPGA ID `4362C0DD`. The known recovery SOF
booted from the combined card and reported the expected loader and 1 GiB DDR.
The production uploader's `perform` and actual serial/TFTP transport ran under
the encompassing trial claim. Its standalone CLI claim acquisition/release
was not exercised on hardware.

DHCP returned `192.168.10.211` for volatile MAC `02:fe:25:08:cb:01`; the host was
`192.168.10.202`. TFTP delivered **4,711,676 bytes**, the exact native FIT SHA-256
`9926773577fcb139b4a379cc293646d1e178619e3a4c8190f843e17d172bbf26`.
RAM hash, the single FAT overwrite of `FES/UPLOAD.BIN`, separate-buffer SD
readback hash and byte comparison all passed. The protected raw prefix and
root recovery FIT remained exact before/after. Own TFTP/UART clients closed
and the loader remained at its prompt. No automatic activation, raw SD update,
failure replay, power-loss durability or native network driver was tested.

## Native initial rejection

The matched native SOF programmed successfully and emitted the expected
`ae9d1d6ba30a7e5f` SPL build. It then parked with this retained line:

```text
FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=4 valid=0
```

Against that source's predicates:

| Observation | Result |
| --- | --- |
| SDM JTAG boot kind 4 | Accepted kind |
| Unused-master reset mask `0xff213f3f` | All required bits held |
| Four data-bridge reset bits | All held |
| CPU release request low four bits | Clear |
| Reset cause `0x4` versus old allowed mask `0x1` | Rejected |
| CPU reset value `0xf` versus primary-0 `0xe` or primary-2 `0xb` | Rejected for either accepted primary |

The first line did not record MPIDR or primary core. This is a mismatch between
the model's initial-state assumptions and the board observation; it does not
establish that the board is unsafe or that broadening acceptance is justified.
FIT seal verification, SD PIO drain/reset, BL31 consumption, private ownership
snapshots, native Go and physical launch/return were not reached.

## Register investigation and contained follow-up

The official [CS/R1 HPS register map, RC28](https://docs.altera.com/v/u/resources/864455/agilex-5-cs-and-r1-suffix-opn-hps-register-map)
was downloaded through the link in Altera's
[reset-manager register chapter](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/reset-manager-address-map-and-register-definitions?contentId=Plcc9UDIuxCVG50y8BxIUg).
ZIP SHA-256: `c4720c039263d5423662994fc41cd1c9a324f9fcf3bc1187a3a742f532537925`.
Its `Reset_Mgr.stat` at `0x10d11000` has reset value `0x4`: bit 2 records the last
POR, bit 0 records SDM cold reset, and bit 1 records SDM warm reset. The POR
flag persists through warm/cold reset; it alone cannot prove fresh boot history.

The follow-up permits only the documented POR/cold flags (`0x5`) alongside the
existing independent SDM boot-kind proof. It continues rejecting warm,
watchdog/debug and unknown bits. It retains the CPU gate and adds a diagnostic
after console/clock initialization: full MPIDR, primary index, fresh cause,
CPU reset/release registers and reset-handshake timeout status. That later
sample cannot replace a failed first sample or grant admission.

Host tests exercise all four valid cause combinations for both primaries,
every other cause bit, and the exact physical tuple. The physical tuple still
rejects for both primaries after the POR correction, with no register writes.
The CPU register's meaning/timing and a qualified primary/secondary inventory
remain open. Altera's [reset sequencing](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/reset-de-assertion-sequence?contentId=Z8zFZqWjknEOqRcvl2Xvbw)
describes CPU release separately; this run does not prove why all four CPU bits
were set. The follow-up candidate is off-board and has no physical acceptance.

All three focused cold tests pass with strict warnings/UBSan: 75 producer/
consumer cases, 12 actual platform/SiP cases and 11 SPL seal cases. All ten
affected checks pass against base `6253f8342`, including parent regressions,
generated consistency, native/tenfoot lifecycle, producer identity and RTL
integration. BL31 and U-Boot/SPL cross-build as matched `v10`; actual PFG and
mtools prepare `kit-v5`. The audit verifies strong firmware symbols, the final
FIT seal in SPL, zero cold configuration features, the 168-byte OCRAM record
and every final vendor parser read range. None of these checks reruns the new
candidate on hardware.

| Off-board follow-up | SHA-256 |
| --- | --- |
| Chain identity | `67030ba9047443528faaf5bd92139145527860cedad54528819b40856a98b39c` |
| BL31 | `20c4a7b6c0509b0f8f6a77522c714e67a6c2400bbad489203ab9b7634dd34cb6` |
| Native FIT | `12e6e7843b5c9f3a949a2d45856c32e9eb8e314d8dac617a21da37b870b55592` |
| SPL hex | `52977d56fe01c71a60c49008bcd8001af24e0a6580128e601da011863728412a` |
| Native SOF | `ebf1b052e953a674145f362f6790e792e8cae9b0b71b8e1e8d0063781ab2915b` |
| Combined SD image | `c371189bfa7b88c9e31ea2ee163abb5c03f931c885fb1c09604690e1277020d9` |

## Restoration and evidence

The user reinserted the original factory card. Recovery stopped before
programming because the exact cable reported a broken JTAG chain. One
lower-clock check also failed; the original 15 MHz setting and USB permissions
were restored. No shared JTAG service was restarted and no other board was used.
The cause of the broken chain is not established. A later bounded serial
observation initially did not find the factory login.

On 2026-11-01 (Europe/Sofia), the user reported the requested reboot completed.
Authenticated UART verification matched the factory kernel
`6.12.11-gd7d192a9ddd9`, root `/dev/mmcblk0p2 ext3`, original card CID/capacity
and board model, with fresh boot UUID `3dc3f7ca-7b12-467a-9291-eca23caf6190`.
The read-only exact-cable query recovered JTAG ID `4362C0DD`. No further FPGA
programming was needed. The physical reset method was user-reported; independent
electrical power-on or exact QSPI artifact qualification is not claimed.

The UART session logged out and closed, programmer/TFTP clients are closed,
no UDP 69 listener remains, and the exact top USB node is restored to
root:root mode `0664`. TRWU8CBL is idle and **released** in its authoritative
status file. The factory card and flash were not programmed or reimaged;
normal login accounting may change the existing root filesystem.

The [evidence record](evidence.json) contains the partial physical result,
verified restoration, completed software validation and exact artifact/
log identities. Retention is
`/home/deano/fes-retained/de25-nano-upload-cold-trial-20261031`.
The earlier physically tested pair and image remain in the preceding
preparation's retention; the new `v10`/`kit-v5` artifacts are separate. Private
credentials and the card-region backup are excluded from published evidence.
