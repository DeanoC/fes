# 286 reference REP chunk experiment

This is a default-off 86Box timing surrogate, not physical 286 or package
acceptance evidence. Before the first reference execution, the candidate's
fixed whole-loop predictions were recorded in `predictions.json` and sent to
the task transcript. Source, program and candidate executable identities:

- Reference source `reference-fixed.asm`: SHA-256
  `3413fbe610d3057427dfbc8d6f1a769dd26fb5923ec6e7f457b1a1368d6fdc9f`;
  NASM 2.16.03 output SHA-256
  `0bca14dab1b9d01c8f7faf180147b32b9b6fbedfad6a9b61a8ca70271d4a5fdc`.
- CPU source `cpu.asm` NASM output SHA-256
  `fb7d6d834fcfa9405c71ffe8c344ee8c2841b62d72cc2cda33f4097bf1b7311f`.
  The separately frozen candidate CPU executable SHA-256 was
  `c950c2e6ed611a60216b916c5d28401dd5daa74341232c4a3962bf93714d34d4`;
  it used `GOV_N=5`, `GOV_D=3072`, and `GOV_REFERENCE_DIAGNOSTIC=1`.
  Its governor source SHA-256 was
  `6ff8e312135b7cfb7471dfd4600a79f0c752dd709f3d75f2aa47e5acedd1bfdd`.
- `predictions.json` SHA-256 was
  `6a30a7df0818daf5755d3107bc978e73cd8f9d2fc8d424e83a4edf7ffa99d646`.
  Reference source/control fixes were recorded before the successful run; the
  transcript is not an independent timestamp authority.

The reference was 86Box 6.0, Award 286 at 12.5 MHz, one wait state and no
Dynarec, as described in the [parent fixture README](../README.md). The program
counts 1,000-iteration blocks over 64 BIOS ticks; conversion and one-block
quantization are described there. `cases.json` names the 16 byte-string/count
cases and `cpu.log` contains raw CPU charge markers. The CPU source uses
relocated SI/DI/scratch addresses, not an identical complete binary. The
reference program initializes source to nonzero and destination to zero,
verifies DS=ES, checks that the first MOVSB case changed exactly 28 bytes,
clears the destination, and checks that the first STOSB case changed its first
byte before later tests. The CPU program initializes scratch memory but does
not independently read back the copied bytes.

`M1.TXT`–`M3.TXT` are three outputs from the first successful boot and
`M4.TXT`–`M5.TXT` are two outputs from a separately started second boot; all
are byte-identical, SHA-256
`901886976d1efff75aaaa65c00ae0c61b1f117395bb20993b5af64b36bf4f487`.
All contain the segment and first-write controls. The two pre-run private disks
had the same SHA-256
`bc4aec5a9e16392cd7f424b57511fe67cee339065bab09d8decb5da6980af90d`;
configuration hashes were
`2714759a456721d412a662028f366344ee95a6a4d5d3e16c2ccf83ffd0b31974` and
`47b6b86e5504a68837ea2b479b31e2b47e4ebafe96cb4c5da1f3aa9cf7263288`.
The second boot's pre-run NVR bytes hashed to
`d6e14e94a2f899311b0d9963789058ff9d3e1934572d3312c6403e45ff014372`.
The exact input/capture command sequences are retained in the JSONL files;
they use the private driver vocabulary, and do not ship the reference disk,
ROMs or emulator. `comparison-fixed.json` records block counts, predictions,
reference clocks, quantization and errors. All 16 errors fit within the
previously proposed diagnostic bound of the larger of 0.25 clock and twice
one-block quantization; this is not a tier threshold. SCASB count 1 differs
by 0.042 clock, slightly more than its 0.033-clock one-block quantization.

The first attempted matrix prefilled destination with the same byte later
written, so its inherited final readback could not prove a write. That run was
stopped, preserved locally and is invalid for write-verification evidence. The
successful reference above was rebuilt with explicit first-write controls and
run against separately copied, verified absolute disk paths. No common private
or shared disk was changed by either run.

The direct governor test covers MOVS/STOS/LODS/SCAS chunk boundaries, odd
refill carry, interruption/refetch and internal replay. The candidate-enabled
CPU passed 148 directed paging cases including REP page faults and 50 seed-42
random programs including 12 REP interruptions. These are simulated
architecture checks, not 86Box measurements of interrupted or faulted timing.
Word/dword alignment, trap flag, MMIO, device delays and additional workload
classes remain unmeasured; the experimental model stays disabled by default.
