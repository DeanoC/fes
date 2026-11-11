# DE25 proofs on both physical AArch64 boards, 2026-10-30

Owner: FES integration evidence, using the retained misteross and runtime proof
artifacts. Base: `725532742eaf4e8c51ff42f9875c543a3268a364` (#1283).
Normal merge `c707a41970` incorporates the later parent
`b21d90a5940ba93b4265335aa463e6b65d710649`. The proof remains bound to its
original selected executable/model/artifact snapshots and script producer
`8554a92b4`. Later parent RTL, descriptor validation and refreshed Quartus
artifacts are not physically qualified by these older binary results.
The two boards are identified by USB UART serials `TRWU8J8G` and `TRWU8CBL`.
Both run the original Ubuntu 22.04.3 image, AArch64 kernel
`6.12.11-gd7d192a9ddd9`, with four online CPUs. Executables run directly on those
CPUs, without QEMU. Temporary test files do not replace the factory image.

## Results

[All 48 client/RTL cases](clients.json), [24 exact bundles and 36 rejected
compositions](composition.json), and [both software suites](software.json) pass.
The four retained SAVE images are identical 923-byte files, SHA-256
`b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
The accepted isolated combined session on `TRWU8CBL` takes 1507.506 host seconds,
inside the unchanged 1800-second bound. Elapsed times include host simulation
and SSH; they are not board CPU or FPGA performance measurements.

[Board metadata](boards.json), the [published-reference audit](reference-audit.json),
[model initialization inputs](runtime-inputs.json), [execution-script identities](replay-source.json)
and the [manifest](manifest.json) bind the evidence. Replay scripts were frozen
at `8554a92b47290475d02099b15c5d4b77fa118ebf`. [Remote cleanup](cleanup.json)
removes owned proof files and failed-fixture partial files; [release metadata](released.json)
records completion of both serial-specific claims. Factory images and boot
settings remain as found; no task UART or SSH session remains.

## What this establishes

Three format-2 composition profiles run on each board: counter personas,
ZX81 empty/16-KiB RAM-pack personas, and the combined ZX81 computer profile.
Each profile selects both personas twice. All **24 bundles** match the retained
native references exactly, including deterministic archive metadata, ordered
deployment plans, composition IDs and unchanged vendor payloads. All **36
negative controls** reject without publishing output: wrong shell, wrong base,
foreign root, replaced descriptor, damaged persona and overlapping selections.

The actual AArch64 C++ runtime clients also execute on the boards. OpenSSH
forwards a private Unix socket from each board to its own frozen Verilator
process on Powerboat. No server emits the clients' commands. The controller
matrix has seven cases per board. The internal-RAM ZX81 matrix has ten cases
per board; the combined RAM-pack matrix has seven. Every result must equal its
retained native reference, rather than merely report success.

The full BASIC sessions type `1 PRINT 7`, capture unmodified ROM SAVE output,
LOAD/RUN, store a `2 REM` edit after RUN, reload without reset and RUN again.
Each requires output 7 twice, 24 editor returns and removal of the intervening
edit. SAVE bytes must match the retained native image. Combined sessions also
retain empty/RAM/empty selection, interrupted third LOAD/freeze and final
1-KiB BASIC boot. Full BASIC sessions retain #1283's 1800-second outer host
watchdog. Guest phase bounds and API/socket timeouts remain unchanged in the
retained executables.

Seven retained C++ unit binaries per board pass: runtime, protocol, injected
MMIO, fake FPGA manager, 64-bit offsets, production-adapter refusal and scripted
DE25 computer registers. Both boards also pass the five selected Go runtime
client tests and the agent's maintenance-contract early exit. The runtime and
agent production daemons are never started. These tests retain their original
producer `fe624de4f160c9bd50667b7934b4979431d9ced6`; this is CPU execution evidence
for those exact binaries, not a rebuild or acceptance of a later runtime tree.

## Provenance and replay

The reference audit checks retained receipts against their published copies.
The replay validates executable hashes before upload, after upload and after
execution. Composer inputs come from the retained native bundles, with the
producer descriptor and all payload lengths/hashes checked independently.
Downloaded outputs are inspected using the existing composition inspector.
Runtime fixtures are the complete original fixture tree from the runtime
producer, not newly generated replacements.

The three scripts in this directory reproduce the CPU runs:

```sh
python3 replay.py prepare --config /private/boards.json \
  --retained /absolute/fes/out/dev --output /new/private/run
python3 replay.py clients --config /private/boards.json \
  --retained /absolute/fes/out/dev --output /new/private/run
python3 composition.py --config /private/boards.json \
  --retained /absolute/fes/out/dev --output /new/private/run --root /absolute/frozen-proof-source
python3 software.py --config /private/boards.json \
  --inputs /absolute/verified-runtime-inputs --output /new/private/run
```

For composition replay, `--root` must point to a checkout of `8554a92b4`
(or its unchanged composer source at `725532742`), not a later parent checkout.
The composer source hashes are checked before execution; later descriptor
validation changes correctly fail that check. Reference-audit entries pin the
published revision as well as the path and bytes, since some parent validation
files have subsequently been replaced.

The private config contains a `known_hosts` path and a `boards` array. Each
board supplies `serial`, `ip`, `mac`, `boot_id`, `user` and `password`, established
through its physical UART. It is deliberately not committed. Every SSH command
checks the observed MAC and boot UUID before executing. SSH uses the public
host key obtained through the corresponding UART. Both factory cards share
that key, so the key alone is not a unique board identity. Credentials, private
network addresses and the raw network inventory stay outside this record.

The runtime inputs comprise the exact executables named by the original
runtime receipt, the two agent executables, `runtime-reference.json`, and
`fixtures.json` containing producer/relative-path SHA-256 entries for the full
`tests/fixtures/` tree. The scripts do not rebuild any executable or invoke
Quartus. Boards must be claimed in their serial-specific Powerboat status
files before replay. Temporary remote directories and SSH forwards belong to
this task; cleanup must name those exact directories after all runs finish.

## Retained setup failures and remaining boundaries

The preliminary client replay selected an older controller model whose command
line rejected the random-initialization arguments. That run is retained as a
rejected harness setup. The passing replay uses the model in the published
seed-41 receipt, producer `9663da282a`. No controller or RTL patch was needed.

Initial protocol-unit invocations lacked fixture files, then lacked the nested
package fixtures. Both fail at the original file-open assertions. Their logs
remain separate from passing results. The complete producer fixture tree and
the original working directory layout fix the setup; no binary or assertion
changes. Core dumps are disabled for the complete unit run.

With four host machine models running together, the first `TRWU8CBL` combined
session hits the unchanged 1800-second outer watchdog after both BASIC RUNs
have printed 7, before the full final-return observation. It is a rejected
complete-session candidate. The other board completes the combined matrix.
Only the unfinished board/model is selected for a separate replay using
`select_suite.py prepare` followed by `select_suite.py clients`, both with
`--board TRWU8CBL --model combined` and the same output directory. `prepare`
creates that directory and `clients` reads its `prepared.json`; use one fresh
path for the pair. The exact executables, seed, guest bounds, API timeouts and
1800-second outer watchdog stay unchanged. Passing controller, internal-RAM,
composition and unit results are retained, rather than repeated.

The current `replay.py` puts `wrong-build` first and the long `session` last so
cheap setup/refusal checks finish before another BASIC run. The retained replay
receipts record the original script hash in `replay-source.json`; this ordering
change applies to future retries and does not relabel the completed runs.

This establishes physical **CPU** execution and deterministic composition.
The ZX81, keyboard, loader, RAM personas, mailbox and renderer remain in host
RTL simulation. The persona selector is a mux model, not SDM programming.
The software tests use injected/fake hardware and retain the production
adapter's refusal of physical DE25 access. Kernel DMA/KUnit tests are not
rebooted on these factory images.

No FPGA is programmed, no physical MMIO or destructive DDR test is performed,
and no boot configuration, SD image or persistent service is changed. Physical
FPGA loading, firmware pairing, exact package/revision verification, DDR/IO/
reset timing, real persona swaps and FES-image boot remain separate checks.
No compile/timing result or older SOF/RBF is promoted by these CPU results.
