# Host survey — 2026-08-26

Initial non-mutating survey on the development host:

| Capability | Result |
| --- | --- |
| CMake | present |
| Ninja | present |
| Python | present |
| Git | present |
| FPGA tools (Quartus, Yosys, nextpnr-mistral, Mistral, openFPGALoader) | not detected |
| USB-Blaster | not detected |

This log records exact tool identities, command syntax, device support, cable
visibility, artifact hashes, and hardware observations as they are established.
The initial host survey does not install packages or alter the machine.

## Evidence policy

Bootstrap and diagnostics are idempotent and non-mutating unless an explicit
build action is requested. Before wrappers rely on command syntax, the pinned
tools' version/help output will be captured here. A hardware result records the
experiment, tool versions, artifact hash, and observed LED cadence.

## CLI and device evidence — 2026-08-26

The following commands were run after `source scripts/env.sh` against the
repository-local install. Complete raw stdout/stderr is retained only under the
ignored `build/toolchain/evidence/` directory.

### Pinned tool identities and help behavior

| Exact command | Observed result |
| --- | --- |
| `nextpnr-mistral --version` | `"nextpnr-mistral" -- Next Generation Place and Route (Version nextpnr-0.11.1-14-g7d4f72c0)` |
| `openFPGALoader --version` | `openFPGALoader v1.1.1` |
| `mistral-cv --help` | Exit 1: `Unknown command --help`; the current CLI prints its command list and uses `models` for the device database. |
| `mistral-cv models` | Lists `5CSEBA6U23I7` as die `sx120f`, package `u23`/672, speed grade 7. |

The exact device acceptance probe was:

```text
nextpnr-mistral --device 5CSEBA6U23I7 --test
```

It exited 0 with `Info: Program finished normally.` after checking the
architecture database. This is a parse/database probe only; it does not ingest
a design, write a netlist, generate a bitstream, or access hardware.

### Confirmed build and programming flags

The pinned `nextpnr-mistral --help` accepts these exact inputs and outputs:

```text
--json <synth.json>                 JSON design file to ingest
--device 5CSEBA6U23I7               target Cyclone V device
--qsf <pins.qsf>                    QSF constraints input
--sdc <clocks.sdc>                  SDC timing input
--rbf <top.rbf>                     RBF bitstream output
--write <routed.json>               routed JSON design output
--report <timing.json>              timing/utilization report output
--detailed-timing-report            include detailed net timing data
```

The pinned `openFPGALoader --list-boards` contains `de10nano` with cable
`usb-blasterII`; `--list-cables` contains the `usb-blasterII` entry at VID/PID
`0x09fb:6810`. The board-aware, read-only discovery commands are:

```text
openFPGALoader --board de10nano --scan-usb
openFPGALoader --board de10nano --detect
```

Hardware readiness accepts exactly one `--scan-usb` row whose parsed numeric
VID/PID is `0x09fb:0x6810` (the normalized `09fb:6810` form is also accepted);
descriptor text is not used for cable identity, so USB-Blaster III, wrong-PID,
and suffixed descriptor rows are not ready. It then requires
`--board de10nano --detect` to return one JTAG-chain device matching the exact
`5CSEBA6U23I7` alias, model `5CSE*A6`, or the pinned loader IDCODE
`0x02d020dd` (Cyclone V SoC `5CSE*A6/5CSX*6`). A different IDCODE or multiple
chain rows/devices is not ready. These cable and JTAG results are measured
evidence only: JTAG identifies silicon family/IDCODE and does not establish
the PCB, package, or pin equivalence.

Because the cable scan is board-agnostic, strict hardware readiness also
requires an explicit operator attestation:

```text
python3 scripts/doctor.py --strict hardware --expected-board de10nano
```

`de10nano` is the only supported attestation value in this task. The doctor
reports this as **operator-attested board identity**, separately from measured
cable and silicon checks; it is not an automatic board/package proof and does
not establish clone pin compatibility.

Programming syntax is `openFPGALoader --board de10nano --write-sram <top.rbf>`
(or `--cable usb-blasterII --write-sram <top.rbf>` after an explicit cable is
identified). The help output states that `--write-sram` is volatile/default,
while `--write-flash` is a separate option defaulting to false; no flash,
HPS-storage, SD-card, or network deployment operation is part of this task.

### Current hardware result

`openFPGALoader --board de10nano --scan-usb` found no connected cable, and
`openFPGALoader --board de10nano --detect` exited 1 with `JTAG init failed`
(missing USB-Blaster II firmware/no cable). The host `lsusb` listing contained
only hubs and ordinary peripherals, with no Altera/Intel USB-Blaster VID `09fb`
device. The available MiSTer Pi and SuperStation One devices have no onboard
USB-Blaster, and no external USB-Blaster is connected. No operator board
attestation was supplied, so general diagnostics report hardware as **NOT
READY** while OSS tool/device readiness remains independently reportable; no
clone package/pin compatibility is inferred.

`QUARTUS_ROOTDIR` was unset for this evidence capture. Quartus was therefore
not inspected; it remains an optional oracle dependency and is only inspected
when that variable is explicitly provided.

## OSS synthesis, place-and-route, and RBF evidence — 2026-08-26

Task 7 was run entirely from the authenticated repository-local toolchain. No
hardware programming command was run. The initial TDD RED run was:

```text
$ python3 -m unittest tests.test_oss_purity -v
Ran 3 tests in 0.014s
FAILED (errors=3)
FileNotFoundError: .../scripts/build_oss.sh
```

After adding the pipeline, the focused tests were GREEN:

```text
$ python3 -m unittest tests.test_oss_purity -v
Ran 3 tests in 0.037s
OK
```

The required logical simulation also passed:

```text
$ make sim EXP=010_blinky
transition cycle=8 count=8 LED=1
transition cycle=16 count=0 LED=0
PASS: 24 post-edge counts verified (0-7 low, 8-15 high, wrap low)
```

### Authenticated tools and confirmed CLI

The exact local identities used by the build were:

| Command | Output or evidence |
| --- | --- |
| `build/toolchain/install/bin/yosys --version` | `Yosys 0.68+132 (git sha1 13b43f8c8, Release, GNU /usr/bin/c++ 14.2.0)` |
| `build/toolchain/install/bin/nextpnr-mistral --version` | `"nextpnr-mistral" -- Next Generation Place and Route (Version nextpnr-0.11.1-14-g7d4f72c0)` |
| `mistral-cv models` | Device database lists `5CSEBA6U23I7` as die `sx120f`, package `u23`/672, speed grade 7 |

The build captured the local help output before routing:

```text
scripts/run_logged.sh build/oss/010_blinky/nextpnr-help.log \
  build/toolchain/install/bin/nextpnr-mistral --help
```

That captured help explicitly provides `--json`, `--device`, `--qsf`, `--sdc`,
`--freq`, `--rbf`, `--write`, `--report`, and
`--detailed-timing-report`; no unsupported option was guessed.

### Exact OSS commands and result

The synthesis command is retained in `build/oss/010_blinky/yosys.log`:

```text
build/toolchain/install/bin/yosys -p 'read_verilog experiments/010_blinky/rtl/top.v; synth_intel_alm -nobram -nolutram -nodsp -top top; cd top; rename LED \LED[0]; stat; write_json build/oss/010_blinky/synth.json'
```

The post-synthesis port rename only changes the generated JSON key from the
one-bit Verilog port name `LED` to the indexed `LED[0]` name required by the
checked-in QSF. It does not modify the RTL or constraints. This keeps the
shared `boards/de10nano/pins.qsf` and `boards/de10nano/clocks.sdc` inputs
identical for the OSS lane.

The route command is retained in `build/oss/010_blinky/nextpnr.log`:

```text
build/toolchain/install/bin/nextpnr-mistral \
  --json build/oss/010_blinky/synth.json \
  --device 5CSEBA6U23I7 \
  --qsf boards/de10nano/pins.qsf \
  --sdc boards/de10nano/clocks.sdc \
  --freq 50 \
  --rbf build/oss/010_blinky/top.rbf \
  --write build/oss/010_blinky/routed.json \
  --report build/oss/010_blinky/timing.json \
  --detailed-timing-report
```

The route exited 0 with `Info: Program finished normally.` and constrained
both `FPGA_CLK1_50` and `LED[0]` to the shared V11/W15 assignments. A
case-insensitive search of the complete route log found no `unrouted` marker.

The reported utilization was:

| Resource | Used / available | Percent |
| --- | ---: | ---: |
| `MISTRAL_COMB` | 28 / 83820 | 0% |
| `MISTRAL_FF` | 25 / 167640 | 0% |
| `MISTRAL_IO` | 2 / 472 | 0% |
| `MISTRAL_CLKENA` | 1 / 2 | 50% |
| `MISTRAL_M10K` | 0 / 553 | 0% |

The checked-in SDC requests 50 MHz (`20.000 ns`), and the initial signoff
report was `234.69 MHz (PASS at 50.00 MHz)`. Earlier placement/timing passes
in the same log also passed at 361.01 MHz and 220.85 MHz.

### Historical initial artifacts, hashes, and rebuild stability

`make oss EXP=010_blinky` succeeded twice. The required RBF was nonempty at
7,007,204 bytes. Its SHA-256 was identical on both runs:

```text
6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632  build/oss/010_blinky/top.rbf
```

The initial artifact and log evidence from this pre-fix run was:

| Path | Bytes | SHA-256 |
| --- | ---: | --- |
| `build/oss/010_blinky/top.rbf` | 7007204 | `6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632` |
| `build/oss/010_blinky/synth.json` | 251174 | `e34cbb5ab34ffc1987031f523d6134acc81070b3ab6221c5a27b84a5e484096b` |
| `build/oss/010_blinky/routed.json` | 119035 | `66d06fc73fc94742fd5ad21dc28171f054eda93c174c0f8fba8944a77838455a` |
| `build/oss/010_blinky/timing.json` | 34131 | `935069090971210c96561a59fdac82481800bbed3682e246c3efb44c3fc68df7` |
| `build/oss/010_blinky/timing.txt` | 367 | `3399171a5d35db330b07c4da9b4d89eaf31eb4783a245db2981479302b883c6a` |
| `build/oss/010_blinky/build-summary.json` | 2479 | `8b8e4d871824fc0adbe185a8c2576b432ff52d40e79c38e63605313cce5090e0` |
| `build/oss/010_blinky/yosys.log` | 48930 | `15a9fbf175cc90f1c70da0e3b48747966dc56a496e5e117366d328cd700ca119` |
| `build/oss/010_blinky/nextpnr-help.log` | 6001 | `38888729e6dcbea9b91d43e2cb629e35fb31b521ee362175cfa001a322d79110` |
| `build/oss/010_blinky/nextpnr.log` | 40132 | `7f99cd8e4d78be31efa2ec5bc4956f112c4e3a424f58f4737fd1f07a70b271ac` |
| `build/oss/010_blinky/summary.log` | 1020 | `38480fa8c3643f80f40ad6d54fc68f3f3d0413f8f006532a31589bd6cfeda2e1` |

The historical manifest was generated by `collect_manifest.py` with explicit
RTL, QSF, SDC, script, lockfile, command-log, and artifact arguments. It
records target `5CSEBA6U23I7`, lane `oss`, all lock pins, source hashes, and
exact stage commands. This initial evidence was collected before the
implementation was committed, so its dirty working-tree state is the earlier
pre-commit measurement; it is not the current authoritative Git state.

## OSS review fix round 1 evidence — 2026-08-26

The first review identified four gaps, all addressed before this build was
accepted:

- output and generated-log paths are checked for symlink components and
  canonical containment under `$ROOT/build` before any directory creation,
  truncation, or logging;
- real execution accepts only the canonical repository-local
  `build/toolchain/install` and `build/toolchain/build` roots, while fixture
  overrides remain print-only;
- the final `timing.json` is parsed structurally for exactly one intended
  `FPGA_CLK1_50` entry constrained to 50 MHz with achieved frequency at least
  50 MHz, independent of earlier route-log PASS text;
- `build-summary.json` is supplied explicitly to `collect_manifest.py` and
  stored under the manifest `build` object with build/route status, timing,
  utilization, hard blocks, authenticated binary SHA-256 values, and measured
  RBF stability.

The regression suite covers an output symlink pointing at an external sentinel,
external and symlinked tool roots, missing/failing final timing entries after
an earlier PASS line, summary schema values, and collector propagation. The
external sentinel and tool invocation marker were unchanged by the rejected
path tests.

The final machine-readable build summary is:

```text
build.status = pass
build.route.status = pass
build.timing.clock = FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q
build.timing.requested_mhz = 50.0
build.timing.achieved_mhz = 234.6866912841797
build.reproducibility.rbf_size_bytes = 7007204
build.reproducibility.rbf_sha256 = 6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632
build.reproducibility.previous_rbf_sha256 = 6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632
build.reproducibility.rbf_stability_measured = true
build.reproducibility.rbf_stable = true
authenticated_tools.yosys.sha256 = 6efab7e5f944c8b835af470d082fc660791b97ccd9266271c5b793191f669d7d
authenticated_tools.nextpnr-mistral.sha256 = 47ad7d4be8cf23f455e8b0835cb108a98593f4ac05b309f5f82c5f6a05a14713
```

The route and RBF outputs remain the same as the original successful OSS
bring-up: no unrouted markers, 28 combinational cells, 25 flip-flops, 2 IOs,
0 M10Ks, and 234.69 MHz final signoff against the 50 MHz request.

## OSS review fix round 3 evidence — 2026-08-26

This is the authoritative post-Round-2 evidence. The two real OSS builds and
the artifact hashes below were measured immediately after the clean
implementation/test commit
`0c353892e797c3b2598dd57b1958e4aa37625c64`. At that measurement point the
manifest recorded `git.state = clean`, `git.dirty = false`, and no changes.
This documentation update is subsequent documentation work, so the ignored
manifest intentionally retains the measured implementation commit and clean
state; it does not claim to be self-referential to the later documentation
commit.

### TDD and verification

The Round-3 regressions were written before the implementation change. Against
the prior code they failed for the unclassified Mistral multiply resources and
the missing previous-experiment check:

```text
$ python3 -m unittest -v tests.test_oss_summary
Ran 11 tests in 0.288s
FAILED (failures=2)
```

After the minimal fix, the focused and full suites were green:

```text
$ python3 -m unittest -v tests.test_oss_purity tests.test_oss_summary tests.test_manifest
Ran 34 tests in 1.011s
OK

$ python3 -m unittest -v tests.test_blinky_sources tests.test_bootstrap_interface tests.test_doctor tests.test_lockfile tests.test_manifest tests.test_oss_purity tests.test_oss_summary tests.test_repository_contract
Ran 88 tests in 45.561s
OK
```

`MISTRAL_MUL9X9`, `MISTRAL_MUL18X18`, and `MISTRAL_MUL27X27` are now explicit
forbidden DSP resources. A previous manifest whose top-level `experiment`
does not equal the current experiment cannot establish RBF stability. Unknown
resources remain fail-closed. The real route's current utilization has no DSP
use; the final report classifies the forbidden `MISTRAL_M10K`, HPS, and
oscillator entries as zero-use hard resources and reports the ordinary Mistral
fabric resources separately.

The required real checks were:

```text
$ make sim EXP=010_blinky
transition cycle=8 count=8 LED=1
transition cycle=16 count=0 LED=0
PASS: 24 post-edge counts verified (0-7 low, 8-15 high, wrap low)

$ make oss EXP=010_blinky
exit 0
$ make oss EXP=010_blinky
exit 0
```

### Current machine-readable result

The final manifest is schema 2 for target `5CSEBA6U23I7`, lane `oss`, and
experiment `010_blinky`. It records the clean measured implementation commit
above and reports:

```text
build.status = pass
build.build_status = pass
build.route_status = pass
build.hard_block_status = pass
build.timing.clock = FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q
build.timing.requested_mhz = 50.0
build.timing.achieved_mhz = 234.6866912841797
build.resources.MISTRAL_BUF = 3/0 (ordinary)
build.resources.MISTRAL_CLKENA = 1/2 (ordinary)
build.resources.MISTRAL_COMB = 28/83820 (ordinary)
build.resources.MISTRAL_FF = 25/167640 (ordinary)
build.resources.MISTRAL_IO = 2/472 (ordinary)
build.hard_blocks.MISTRAL_M10K = 0/553 (forbidden, zero use)
build.hard_blocks.cyclonev_hps_interface_mpu_general_purpose = 0/1 (forbidden, zero use)
build.hard_blocks.cyclonev_oscillator = 0/1 (forbidden, zero use)
build.unknown_resources = {}
build.reproducibility.rbf_size_bytes = 7007204
build.reproducibility.rbf_sha256 = 6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632
build.reproducibility.previous_rbf_sha256 = 6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632
build.reproducibility.rbf_stability_measured = true
build.reproducibility.rbf_stable = true
build.reproducibility.rbf_stability_reason = previous successful manifest provenance matched
```

The authenticated binaries were unchanged and remain tied to the lock pins:

```text
yosys 13b43f8c85ec430a33ee55d058fb4c32b42b6910 sha256=6efab7e5f944c8b835af470d082fc660791b97ccd9266271c5b793191f669d7d
nextpnr-mistral 7d4f72c0aabc15da932748a54e82a6ff7b41921e sha256=47ad7d4be8cf23f455e8b0835cb108a98593f4ac05b309f5f82c5f6a05a14713
```

The complete final artifact and log inventory, including the manifest's
measured sizes and hashes, is:

| Path | Bytes | SHA-256 |
| --- | ---: | --- |
| `build/oss/010_blinky/top.rbf` | 7007204 | `6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632` |
| `build/oss/010_blinky/synth.json` | 251174 | `e34cbb5ab34ffc1987031f523d6134acc81070b3ab6221c5a27b84a5e484096b` |
| `build/oss/010_blinky/routed.json` | 119035 | `66d06fc73fc94742fd5ad21dc28171f054eda93c174c0f8fba8944a77838455a` |
| `build/oss/010_blinky/timing.json` | 34131 | `935069090971210c96561a59fdac82481800bbed3682e246c3efb44c3fc68df7` |
| `build/oss/010_blinky/timing.txt` | 391 | `b9842880dd3d066df02960c5661da3343d3e1a5986a148ab9acd4fcb251c0a00` |
| `build/oss/010_blinky/build-summary.json` | 3874 | `256f35fb18245060642a1820cbadb38bfa5256155fdab1fbe04988963d80a27d` |
| `build/oss/010_blinky/yosys.log` | 48930 | `f898fa597afc7030f92f5ea23fa5f0c01e4226a1707f10652bca7362cce5f2c1` |
| `build/oss/010_blinky/nextpnr-help.log` | 6001 | `38888729e6dcbea9b91d43e2cb629e35fb31b521ee362175cfa001a322d79110` |
| `build/oss/010_blinky/nextpnr.log` | 40132 | `d659c43dae726cd3228ffc418d2bc15d367f76fb773472272e1e02e8fd317ed8` |
| `build/oss/010_blinky/summary.log` | 2108 | `81ad337116ea149a671d638f0f87af1ff4a6ef6bc4b5c1e23a93a664e159cc3b` |
| `build/oss/010_blinky/manifest-collect.log` | 2287 | `ac3e3ba03d0d67dc5b029de18b0ecb7d1529ade3066166638c3dd7cfb2977369` |
| `build/oss/010_blinky/manifest.json` | 11520 | `424f685573848b270517d74e500d662742d35a4160d765d4111bd2d1fc8bca89` |

The final route log contains no case-insensitive `unrouted` marker. No
hardware was accessed and no Task 8 work was performed.
