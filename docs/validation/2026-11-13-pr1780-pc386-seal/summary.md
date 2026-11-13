# pc386 Quartus seal for PR #1780, 13 November 2026

This is a normal pc386 Quartus seal of commit
`9ba4f85d8092e27d40e66a27ff1a22c98768055b`, the PR head after it merged
main. It replaces the earlier seal of
`7a6dcb1ec8583983db6502c1c91cf734e0cafac3` (build `a939617c…`). Six of that
seal's 116 recorded inputs changed in the merge:

- `pc386_at_core.sv`, `pc386_machine.sv` and `pc386_system.sv`, from #1740
  (the floppy controller) and #1802 (the HID keyboard's key state);
- `pc386_hid_keyboard.sv` and `pc386_port2.sv`, from #1802;
- `scripts/build_fes_pc386_quartus.py`, from #1782 (the lockstep gate).

So the earlier seal no longer matched the head. Since #1782 a seal also needs
a passing lockstep report, which this one includes.

## Result

**PASS.** It was sealed on chip-30x0-ai-dev from a clean worktree of
exactly `9ba4f85d8` (`git status` empty). The sequence:

1. `scripts/build_pc386_firmware.py --download`;
2. the pc386 lock's toolchain slot, checked with
   `FES_TOOLCHAIN_GPU_ROUTER=CUDA scripts/bootstrap.sh`;
3. `scripts/test_fes_i386_lockstep.py --download --netlist --gpu-router CUDA --cache-root /data/fes-toolchain-cache --jobs 14`
   (16:45–16:56 UTC), kept here as [`lockstep-report.json`](lockstep-report.json);
4. `scripts/build_fes_pc386_quartus.py --cache-root /data/fes-toolchain-cache --seed 1 --gpu-router CUDA --lockstep-report <that report>`
   (16:56–17:07 UTC).

Quartus Prime Lite 17.0.2 Build 602 did the fit and timing analysis. Yosys
came from the authenticated pc386 CUDA slot, as for the earlier seal.

- Build ID: `3e9d57407dad8e3e94f314a927ba9ba2`
- Seed: `1`
- BIOS SHA-256: `d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a`, matching `firmware.lock`
- VGA BIOS SHA-256: `8522568e32a33b2f5649109260fec70b0b2cd0d80a6216ac7af6a1041c102e02`
- RBF SHA-256: `ef51fcc06b33c810acb2b1442363a4aa95f6d3928efca1dabb8111781e4cb39a` (3,890,512 bytes)
- Timing: every analysis has 0.0 ns TNS.
  - setup +0.542 ns
  - hold +0.093 ns
  - recovery +7.219 ns
  - removal +0.388 ns
  - minimum pulse width +1.122 ns
- Clocks: system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.
- Resources: 36,334 of 41,910 ALMs, 274 of 553 M10K, 40 of 112 DSP blocks,
  32,708 registers.

### Lockstep

The report's `run` is `{"passed": true, "default_suite": true,
"complete_corpus": true}`, with every suite argument at its default. The
summary's `lockstep` record names it (`report_sha256` `e0ea09c4…`, the
sha256 of `lockstep-report.json`) and the CPU netlist the sealer regenerated
(`netlist_sha256` `a9a12253…`, CPU source closure `51903bb2…`, which no
commit in this PR changes). Every lockstep section passed with no failures:
8,000 protected-mode programs, 4,000 task-switch, 4,000 debug and 4,000
undefined-encoding programs, 84 directed, 7 unreal-mode, 726 protected-mode,
148 paging, 1,933 no-387 and 27 memory cases.

## Verification

The full build summary and the input receipt are kept here as
[`seal-summary.json`](seal-summary.json) and
[`build-inputs.json`](build-inputs.json). From the repository root:

```sh
python3 sources/misteross/scripts/verify_pc386_quartus_seal.py \
    --summary docs/validation/2026-11-13-pr1780-pc386-seal/seal-summary.json \
    --inputs docs/validation/2026-11-13-pr1780-pc386-seal/build-inputs.json \
    --head 9ba4f85d8092e27d40e66a27ff1a22c98768055b
```

prints:

```
status pass
build_id 3e9d57407dad8e3e94f314a927ba9ba2
seed 1
firmware.bios d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a
timing.status pass
rbf {'sha256': 'ef51fcc06b33c810acb2b1442363a4aa95f6d3928efca1dabb8111781e4cb39a', 'size': 3890512}
inputs.revision 9ba4f85d8092e27d40e66a27ff1a22c98768055b
inputs.bound 119 files
SEAL_OK
```

The seal now records 119 source inputs. Main added three: `pc386_fdc.sv`
(#1740), and the i386 `toolchain.lock` and `scripts/lockfile.py` (#1782). The checker requires a passing status and
timing, and the locked BIOS digest. It also needs the receipt to name the
revision and the receipt's tools, recipe and ABI definition to be the
summary's. Every one of the summary's 119 recorded source inputs must hash
the same at `9ba4f85d8` in git.

This record is Quartus build and timing evidence only. No kit or hardware
test was run.
