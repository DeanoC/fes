# 286 IMUL diagnostic timing experiment

This corrects a narrowly qualified part of the existing default-off timing
diagnostic. It is not a production timing model or an Ultima VI pitch fix.
The 12.5 MHz reference is an 86Box Award 286 with one wait state, not a
measurement on physical hardware. These reference runs do not establish the
identity of the historical game machine's ROM and CMOS. No evidence establishes
that the measured Ultima VI calibration or delay executes these IMUL forms,
or that this correction changes its pitch.

Eight initially measured 69h/6Bh register and RAM loops included both short
loops and 24-ADD queue-pressure loops. A second, separately precommitted set
used 13 independently encoded forms with varied signs, immediates, destination
and source registers, effective addresses, alignment and segment prefixes.
The guest checked its arithmetic and completion; the captured second run had
464 speed samples at 100%, with maximum 0.5041 s between samples. The reference
executable SHA-256 was
`ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969`.
The eligible register and aligned RAM forms agreed with the experimental
model within approximately 0.05 clocks per iteration. Odd/ea3 and prefixed
forms remained outside this experiment's correction and showed remaining
differences. These results are model evidence, not production acceptance.

The real-CPU controls compare current-main baseline and candidate with the
diagnostic both off and on. They cover short and queue-pressure loops, the
13 forms, operand/address prefixes, protected and unreal modes, selected SS,
RAM/MMIO boundaries, segment reloads and a real paging fault and retry. The
measured off-path marker, cycle, charge and architectural-state signatures
matched in those controls; their scope is finite and does not prove all
possible issue, retirement or reset sequences. The separate Unreal-mode test
found a counterexample to a real-mode-only guard, which is why the candidate
checks cached descriptor properties at issue and retirement.

On measurement base `e2b8130b5a18b652095a8d5e23124fd0a994e9c0`, the disabled
Yosys CPU netlists were **not byte-identical**: baseline SHA-256
`d4dd1288c4fa01c5c08d7359a4e4722360777b65c4b8ba8884d187eb2b70892c`,
candidate `22827273d7baf8050eb7d8dde45a3a72a58a55dcda1f762f8560b465e73e9b7f`.
Bounded formal equivalence attempts did not complete; there is no formal or
byte-identity claim. The retained exact-main inputs, commands, outputs and
hash manifest are separate from this source change; the referenced candidate
CPU closure was `c934c1ddeb2129c88ac875011222829c52ceb3b4d91318067511e5f228bb1e0f`.

The remaining gates include full lockstep, changed-file and parent checks,
independent review, and a normal timing and seal run on the exact committed
RTL. The tracker’s game and pitch acceptance remain open.

## Retained local evidence and commands

These directories are local research artifacts, not files committed with this
change or a promise of external availability. On the machine holding them,
set `P=/home/deano/.codex/worktrees/d31c/fes` and verify:

```sh
(cd "$P/.1902-imul-variant-v7-3090-20261117" && sha256sum -c EVIDENCE-SHA256SUMS)
(cd "$P/.1902-imul-exact-main-20261117" && sha256sum -c SHA256SUMS)
(cd "$P/.1902-imul-tracked-regression-retained-v1" && sha256sum -c SHA256SUMS)
```

The corresponding manifest SHA-256 values are respectively
`e702ef0be166c71ea0fc631a50cfda66b90c0737ea0943d34809e147a7711b11`,
`091b655f54b360b7f99f48051d8ea97864ea6c47421ceb0d2be2ddeb95717dd3`, and
`e6df69fa1172136147fc61f844a246734fb8d7477b063259f4320fdc9f3419ac`.
The reference directory retains `PRECOMMIT.md`, `ES-PRECOMMIT.md`,
`REMOTE-PRECOMMIT.md`, the acquisition script, raw guest result, speed frames,
frame hashes, OCR checks and acquisition service log. The original remote
service launch command itself was not retained; the script and logs do not
by themselves prove its invocation arguments. Earlier invalid captures remain
in their separate v4, v5 and v6 evidence directories.

The exact-main directory retains precommits, source snapshots, four CPU
binaries, compiler argument files, build/run scripts and logs. The tracked
regression was run with the following command from the task worktree; the
recorded output directory already exists and must not be overwritten:

```sh
W="$P/out/dev/1902-imul-diagnostic"
TMPDIR="$W/out/tmp" python3 "$P/.1902-imul-canonical-v2-20261117/supervise.py" \
  python3 "$W/sources/misteross/tests/test_i386_imul_guard.py" \
  --baseline-off "$W/out/imul-check/baseline-on0/Vfes_i386_cpu" \
  --baseline-on "$W/out/imul-check/baseline-on1/Vfes_i386_cpu" \
  --candidate-off "$W/out/imul-check/candidate-on0/Vfes_i386_cpu" \
  --candidate-on "$W/out/imul-check/candidate-on1/Vfes_i386_cpu" \
  --nasm /home/deano/fes-scratch/u6ref/tools/nasm/nasm \
  --output "$W/out/imul-tracked-regression-v1"
```

All nine cases passed; the retained regression includes each log and its
report. Binaries are source-bound by the exact-main `matrix-identity.json` and
recorded compiler commands. The test itself does not attest binary provenance.

## Other factory video-part identities

The normal registered core shell source-root audit found no changed-path hit
for Coleco or Atari ST shell BUILD_ID inputs. Their independently resolved
factory video parts use broad `cores/fes-common` source roots, which include
the changed i386 sources. The measured source-input design projections changed
for Coleco raster, Coleco native and Atari ST; these are part-cache key changes,
not measured shell BUILD_ID or RBF changes. On normal selection they cause
three cache misses. This experiment did not build or compare those parts,
their bitstreams or their hardware behavior. No artifact-equivalence, D12 or
HIL exemption is claimed. Coleco's support-matrix check level is reduced and
Atari ST's is full.

The local audit is `.1902-imul-closure-audit-v1`, whose `SHA256SUMS` SHA-256
is `f4eed78409ac269e018fdceb28d89c7550569a5632c3b26f2d13be02979ae299`.
