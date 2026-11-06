# Guarded OCRAM firmware chain

The selected Nano cold chain now places BL31's stack and BSS in private on-chip
RAM. Both firmware builders require `--nano-firmware-ocram`, the matched
shared-ROM profile and the native CPU exclusion contract. The selected profile
participates in the cold-chain identity. The default ELF gate remains DDR-only.
This is an off-board milestone; the new chain has not booted on TRWU8CBL.

The preceding physical ROM trial stopped at 7,824 of 8,192 bytes under the
unchanged one-second limit. This change tests whether moving firmware's working
memory off DDR can reduce that cost. It does not increase the 16-byte transport
frame, one-second deadline, 64-poll bounds or grant cached ownership permission.

SPL's existing single-use guard checks the current stack, global data, early
allocator, loaded image, cold record, DDR allocator and ATF parameters before
its final branch. Only a successful guard supplies the private fifth argument
to the matched BL31. The compiled success path has no subsequent stack stores
or calls before the branch. This cookie is a private boot-chain contract, not
a nonsecure service or a cryptographic secure-boot claim.

At BL31's first instruction, before vendor initialization, a stackless guard
checks EL3 state, the one-shot initialized DDR latch, cookies, parameter and
stack bounds, the full cold-record CRC, exact chain, owner/epoch and linker
boundaries. It also reads the live master/bridge resets, spare CPU resets,
CPU power requests/acknowledgements and boot-core selection. It never reads or
writes the candidate RAM. Rejection parks permanently. Success retains the
vendor reset handler, CPU-data setup, cache initialization, BSS clear and stack
selection. The existing C provider still checks the complete saved history and
live ownership before granting any service.

The lifetime allocation audit binds the actual linked SPL and BL31, rather than
assuming all 512 KiB of OCRAM are spare:

| User | Range or boundary | Lifetime and exclusion |
| --- | --- | --- |
| Actual SPL loaded image | `[0,0x25440)` | Pinned maximum is `0x40000`; cold record is inside its initialized data. |
| Actual cold record | `[0x22020,0x22100)` | Read and checked before firmware initialization. |
| Selected firmware stacks | `[0x50000,0x58000)` | Acquired only after both guarded entries succeed. |
| Selected firmware BSS | `[0x58000,0x5dec0)` | Zeroed by vendor initialization after the EL3 guard. |
| Complete reserved range | `[0x50000,0x60000)` | Native EL1 removes all sixteen pages before its first checkpoint and continuously checks their exclusion. |
| SPL permitted stack interval | `[0x61000,0x71000)` | Live SP and allocation checks retain a 4 KiB gap above firmware RAM. |
| SPL early allocator | `[0x6f000,0x71000)` | Checked with current global data and stack. |
| Vendor warm scratch / handoff | `[0x7eff0,0x80000)` | Above the reservation; not repurposed. |
| Vendor BL2 MMC scratch | `[0x7d000,0x7f000)` | Above the reservation; BL2 is not the selected SPL boot path. |
| SPL main allocator / BSS | `[0xbfa00000,0xc0000000)` | Pinned DDR ranges; actual BSS ends at `0xbff002b8`. |
| Full U-Boot initial stack | `0x80300000` | DDR; this chain pins the matching SPL configuration and handoff. |
| Native Go RAM | `[0x84000000,0x94000000)` | DDR, separate from firmware OCRAM. |

The early entry requires all bridges closed, HPS DMA masters reset and spare
CPUs contained. Later SD/USB operations use their existing bounded PIO paths;
FPGA transfers retain the fixed DDR arenas and existing ownership policy.
These checks do not qualify other loaders, arbitrary firmware, unknown OCRAM
users or whole-board memory isolation.

Validation on firmware/bootloader implementation
`b642fc3c9cc1c8d2b7d7f7af24507110932ef01b`:

- All eleven required changed-input integration steps passed, including 1,504
  parent and 1,581 FPGA producer tests, generated-consumer checks and the
  native firmware/FPGA launch integrations. Five focused selection tests passed
  after adding the test-only assertion of every assembly/C record offset.
- 251 fresh QEMU EL3 boots executed the real guard: a valid record and 224
  individual byte corruptions; a second valid control and 25 live-state,
  replay, cookie, stack, owner, chain and layout failures. Nano registers are
  modeled in RAM and rejection has a test observer. This does not emulate the
  board or execute the complete vendor startup.
- The real selected BL31 and SPL/U-Boot cross-builds passed. BL31 has one
  zero-file-byte RW segment `[0x50000,0x5dec0)` and its code/data in DDR. The
  actual ELF admission checks stack/BSS boundaries and initialized DDR latch
  and chain bytes. The ordinary gate rejects this ELF.
- The real BL31 disassembly confirms an 848-byte guard before vendor setup,
  no guard stack frame or calls, only the initialized DDR latch store,
  canonical reset/power addresses and a permanent rejection loop. The actual
  SPL disassembly confirms the leaf exclusion guard and both full-width
  cookies at the final branch.
- The 8,097,792-byte FIT fits the unchanged 8 MiB limit. File-only Quartus
  composition ran under the shared lock and verified that the 1,658,880-byte
  FPGA payload remains unchanged from sealed producer `505993c08155d71794bacd1260155ace736fe19a`.

The native application and 7,516,160-byte entry are reused byte for byte from
implementation `31d716957759c3dede3330877dac2ce7bcd2d643`. Their receipts,
selected source hashes and retained archive inventory were checked against the
current tree. They were not rebuilt or attributed to the newer firmware
commit. The [receipt](result.json) binds these checks, allocation ranges and
evidence under `out/nano-firmware-ocram-20261106`.

Development failures remain retained: the first source-anchor fixture,
assembly linker-script fixture, simulated-register injection fixture and
disassembly observer. Corrected fixtures passed without changing runtime
bounds. No board programming, SD write, upload, UART or factory restoration
occurred in this milestone. The claim and spare card remain retained after the
earlier O2 ROM stop; the factory card and other kit are untouched.

Next prepare a fresh bounded operator using this exact composed chain and the
byte-exact native entry. Keep implementation, reused-native and operator source
identities separate. Revalidate the live kit and stop at the first failed gate.
Physical memory ownership, boot, ROM completion/Run/return, ordinary package
admission and DE10-equivalent core loading remain open.
