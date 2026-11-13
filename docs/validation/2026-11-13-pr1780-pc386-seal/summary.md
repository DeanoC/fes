# pc386 Quartus seal for PR #1780, 13 November 2026

This is a normal pc386 Quartus seal of commit
`7a6dcb1ec8583983db6502c1c91cf734e0cafac3`, the PR head after it merged
main and bound the seal checker to the source. It replaces the earlier seal
of `3bc8449a590d9ee8841eb79f65963c6e348f0ed3`. Merging main changed
`cores/fes-common/generated/fes_application.vh`, one of that seal's 116
recorded inputs: #1714 removed firmware-mailbox defines that no sealed
source uses. So `3bc8449a5`'s seal no longer matched the head.

## Result

**PASS.** It was sealed on chip-30x0-ai-dev from a clean worktree of
exactly `7a6dcb1ec` (`git status` empty). The sequence:

1. `scripts/build_pc386_firmware.py --download`;
2. the pc386 lock's toolchain slot, checked with
   `FES_TOOLCHAIN_GPU_ROUTER=CUDA scripts/bootstrap.sh`;
3. `scripts/build_fes_pc386_quartus.py --cache-root /data/fes-toolchain-cache --seed 1 --gpu-router CUDA`.

Quartus Prime Lite 17.0.2 Build 602 did the fit and timing analysis. Yosys
came from the authenticated pc386 CUDA slot:
`commit=e240a4653bb6578e0b267f730128f8a5dac28483`, binary SHA-256
`470eb6f6…`. The run took 47 minutes (11:16–12:03 UTC).

- Build ID: `a939617cb48be3efdf5eb7ac7eb38537`
- Seed: `1`
- BIOS SHA-256: `d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a`, matching `firmware.lock`
- VGA BIOS SHA-256: `8522568e32a33b2f5649109260fec70b0b2cd0d80a6216ac7af6a1041c102e02`
- RBF SHA-256: `d3ef2c5dc3e4cacb8b76e3737b553130d7ccf9c28decabb8b733e4d9f0aa8c8d` (3,884,556 bytes)
- Timing: every analysis has 0.0 ns TNS.
  - setup +0.446 ns
  - hold +0.022 ns
  - recovery +6.463 ns
  - removal +0.396 ns
  - minimum pulse width +1.122 ns
- Clocks: system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.
- Resources: 36,940 of 41,910 ALMs, 269 of 553 M10K, 38 of 112 DSP blocks,
  32,835 registers.

## Verification

The full build summary and the input receipt are kept here as
[`seal-summary.json`](seal-summary.json) and
[`build-inputs.json`](build-inputs.json). From the repository root:

```sh
python3 sources/misteross/scripts/verify_pc386_quartus_seal.py \
    --summary docs/validation/2026-11-13-pr1780-pc386-seal/seal-summary.json \
    --inputs docs/validation/2026-11-13-pr1780-pc386-seal/build-inputs.json \
    --head 7a6dcb1ec8583983db6502c1c91cf734e0cafac3
```

prints:

```
status pass
build_id a939617cb48be3efdf5eb7ac7eb38537
seed 1
firmware.bios d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a
timing.status pass
rbf {'sha256': 'd3ef2c5dc3e4cacb8b76e3737b553130d7ccf9c28decabb8b733e4d9f0aa8c8d', 'size': 3884556}
inputs.revision 7a6dcb1ec8583983db6502c1c91cf734e0cafac3
inputs.bound 116 files
SEAL_OK
```

The checker requires a passing status and timing, and the locked BIOS
digest. It also needs the receipt to name the revision and the receipt's
tools, recipe and ABI definition to be the summary's. Every one of the
summary's 116 recorded source inputs must hash the same at
`7a6dcb1ec` in git.

This record is Quartus build and timing evidence only. No kit or hardware
test was run.
