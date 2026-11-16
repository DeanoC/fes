# #1581 S7: front-end identity fields

S7 of [`docs/design/rtl-frontend-lanes.md`](../../design/rtl-frontend-lanes.md) records which tool
elaborated a core's logic. `toolchain` keeps its meaning, the place-and-route tool; `front_end` is new.

## What a record now says

| Field | Value | Rule |
| --- | --- | --- |
| `parameters.front_end` | `yosys`, `quartus-lite` or `quartus-pro` | On a format-2 record with `toolchain = "quartus"`, and on no other record |
| `parameters.device_family` | `cyclonev` or `agilex5` | The family that front end targets |
| `parameters.quartus_qsf_sha256` | The canonical QSF's digest | On every Quartus record |
| `parameters.front_end_program_sha256` | The canonical Yosys program's digest | Exactly where Yosys elaborates the logic, the same condition as `tools.yosys` |

Format 1 has no validated Quartus discriminator, so a format-1 record carries none of these. The
pc386 and Z80 sealers gain them when they move to format 2 (S9b–S9f, S10).

The recipe registry carries the same pairing. A `toolchain = "quartus"` row names its `front_end`,
and its `[recipes.quartus]` table gains `edition` and `device_family`. An OSS row elaborates with
Yosys and says so by omission. The producer checks the recipe's pair against its `quartus.lock`,
which is what the installed Quartus is authenticated against, and records the lock's value.

Yosys on Pro/Agilex 5 is design decision D3 and is rejected until that decision is taken.

## The two canonical forms

The BUILD_ID derives from the record that holds these digests, so neither form can carry it. Each
also leaves out what changes between runs of one build:

- **The canonical Yosys program** is `elaboration()` with `$SOURCE`, `$WORK` and `$YOSYS_LIB` in
  place of the three absolute roots and a BUILD_ID of 32 zeros. The signoff and repeat runs
  elaborate under different work trees, so without the placeholders no single digest could cover
  both. `executed_elaboration()` renders the program to run and refuses it unless it equals the
  canonical form with the roots and the real ID substituted, so one renderer makes both.
- **The canonical QSF** is the package project's QSF without its `SEED` assignment, the only line
  that varies across a signoff's seeds. The board script refuses to compile a seed whose QSF does
  not reduce to it. PC/XT's QSF has no BUILD_ID line at all: the ID reaches the design through
  Yosys.

Both are written beside the seal summary, as `front-end-program.ys` and `quartus-project.qsf`, so a
reader can recompute either digest by hand. The seal summary itself gains `front_end`, `toolchain`,
`quartus_edition` and `quartus_version`.

The producer records the digests before any tool runs, because the BUILD_ID is derived from the
record. After the build, `require_timing` compares them with what the board script recomputed from
what it actually ran.

## Scope

PC/XT is the only producer in this slice. DE25's `fes.menu` and `fes.zx81` write format-2 Quartus
records through their own exporter, `boards/de25nano/package/export_core_package.py`, which this
slice does not change, so those records still validate unchanged. Their recipe rows carry
`front_end = "quartus-pro"` because the registry is shared. Giving the DE25 records the fields
belongs with S6b or S16.

## The D12 question this record was written to answer no longer arises

Main merged DeanoC/fes#1882 (`9a9b8cbb3`), which closes DeanoC/fes#1878 and implements all three
of its slices: `BUILD_ID` is now the **design** identity, and `scripts/export_core_package.py`
and `scripts/core_package.py` are classified `packaging` in
`scripts/input_identity.py`'s `PACKAGING_INPUTS` -- excluded from it.

Those two files are the only reason this branch ever touched twelve cores' identities. Measured
against the merge base `333900cbb`, with the roles the policy itself assigns:

| producer | closure hits | of which design inputs |
| --- | ---: | --- |
| all twelve | 1 each (`scripts/export_core_package.py`) | **none** |

So **no design identity moves**, for any of the twelve, and there is nothing to reseal. The
condition-1/2/3 campaign below is retained as the record of how that was established at the
time, not as a live claim -- including the two condition-3 failures, which were real and remain
worth reading for what they say about margin, but which no longer gate this branch because the
identities they were measured for do not change.

**What this branch does still move.** It adds four parameters to format-2 **Quartus** records
only, and the split puts them where they belong:

| parameter | projection | why |
| --- | --- | --- |
| `front_end` | toolchain | which compiler elaborates the logic; a compiler change is advisory |
| `front_end_program_sha256` | toolchain | that compiler's own program text |
| `quartus_qsf_sha256` | toolchain | computed over the recipe *including* `threads`, which this policy already classifies as toolchain; as a design input it would move the design id on a thread-count change, which `quartus_recipe_parameters()` exists to prevent |
| `device_family` | **design** | names what the design is compiled for, not what compiled it. The build record carries no other device field, so classifying this as toolchain would stop the design id distinguishing a Cyclone V build from an Agilex 5 one |

`device_family` is new, so **PC/XT's design identity moves once** -- one core, deliberately, and
the core whose front-end identity this branch exists to record. That is the reseal this branch
owes, and it replaces the twelve-core question entirely.

## D12

`scripts/export_core_package.py` is in every audited closure, so this change moves the BUILD_ID of
every format-2 core with no RTL change. PC/XT moves twice over: its producer and board script are in
its closure, and `quartus_recipe_sha256` hashes the whole recipe entry, which now has three more
keys.

The list is generated, not written by hand:
[`affected_cores.py`](affected_cores.py), output in
[`affected-cores.txt`](affected-cores.txt). Thirteen of thirteen closures hold a changed file:
twelve audited producers through `export_core_package.py`, and PC/XT through that plus its own two
scripts. `fes.pcxt` and `fes.pcxt-dos` share one producer and so one closure.

**The D12 evidence is in.** [#1752](https://github.com/DeanoC/fes/pull/1752) moved every
BUILD_ID as well, so evidence gathered before it landed would not have survived it; it merged as
`04bf27bb6` and this branch merged main forward, which is what unblocked the campaign.

### Result

**Ten of twelve closures discharge D12's three conditions. `coleco_v2` and `c64` do not, and
this record does not claim they do.** Conditions 1 and 2 hold for all twelve. Condition 3 -- the
PR's own unforced build passing the producer's normal timing and seal gates -- fails for both,
for two different reasons, and the control run below shows the cause is not this PR's changes
but the identity constant they move. The evidence is committed under
[`condition-3/`](condition-3/).

Conditions 1 and 2 were measured against merge base `1a4bb8d41`; `zx81` was re-measured against
`e9218eb5e` after main's DeanoC/fes#1791 changed `cores/fes-zx81/rtl/top.v`, `zx81_machine.sv` and
`zx81_machine_clock.v`. The other eleven carry forward under D12's merge-forward clause: their only
closure hit in that window is `scripts/core_package.py`, which is neither compiled source nor a
route-deciding input.

| core | base `BUILD_ID` | RBF sha256 (base = forced1 = forced2) | netlist sha256 | condition 3 `BUILD_ID` |
| --- | --- | --- | --- | --- |
| `sg1000` | `7934cd7de29e79d7fb035d0228f3a091` | `33c5bfb5d0825e81bbecde46885c0cc64880da723a18d4fdd001d063f00aaa7e` | `2391423461a89232d884276d069603666efe1c58f9c2efdb4b6190261bc2eaab` | `0eeeff46645d95ff8e24df2172adf63e` |
| `zx81` | `198cabcfee18c2607f738b7c23502e33` | `118c0032439e0fa5c8831aea6659ef1a8866cd895c580e1341b2296dd4dcc91a` | `82a85826e84fda84bdf695a6c7c394574c3dabb85d73639134fe3436f36cb247` | `2fb09f637972c70817b67001a7480911` |
| `sms` | `ae427058d7af3fc0d5b4d1bcdd931857` | `13bdd21e2bcf0a3dabe0bad46f3d963169c31cb94b63e5fb808272cf75cdfde4` | `96bd06d9be2499e648faeb6c9cc5d6a3ce409626b94cbd5da49a876f6794d282` | `f37aac4ab73a9fe637e7ab5cfedce6c9` |
| `coleco_v2` | `4c89ac89fa17748f98eb933d11a42448` | `b96654ed2215a8f5fde32c45ae4a458d7dc4d2dd31b67947431c17aad1e7d5fc` | `625f45a5de90868b8073d860a4ba98805dd93c1d1e5941e59b02bf822a47f316` | `3846446170600c0f492c5dc07319c7e7` |
| `apple2` | `8f2b0f5c44d80fd3cc859d2279df535b` | `f13e1a07375ff8c706b95082b6a87d9ea95abcd635b245eb7117c81f6b9ac345` | `7bcf92be4a48016282c30219d8c16207285c0151de169ab64beaacec17eaaee7` | `a064651a5629eb6aed9e59dc4a19fcc6` |
| `spectrum` | `48d10766ec24b2e78dc1d51617d5dfb7` | `dea25f242efbb936c2bbe6af2235021a3d67d02e852890befbb25bb4d4b93e7d` | `0bb6d9e18fc65beb4dab5d950b8149a4554b8697ae5facbedb8547e4f756ed21` | `58634e6beb2975b36164cc8cde00ae0d` |
| `atari_st` | `33b9e9dd654a7d81a08428145f176f07` | `ab47e3937b51bdbb295ee8db16c8d9cdf90182109b42fd0bf32c64364c5aebd4` | `d475f5413aa990bbda774f9da4dbdd05f4792fd307e48a7fb75a2255cf3a9257` | `4ce3baac4b206caba4f1265264fc8e44` |
| `c64` | `8016924c62c6148f47a59e405b61ab1f` | `b0eccc7fe829630294aaa7728e34df6ad405cb39b6cf869d84108bdc1c83ca65` | `08644e85311fa460b4e85b25f29c99240557a15f05e38bf8acedf590ede6e704` | `e5b26860f2d00eed8309eb2bdf063eeb` |
| `pong` | `1d32b88b34acf5c5e953c2e721bac236` | `b668bbea9891ac6d8528c44a66e1d4920aa0d8b47f1d32557c1b83d93e727ad0` | `951d41c8b2a2d2ac64251e7582e16f3853f49fe1d85602fa81aad98eb2869193` | `a3e19a6710ce45d1a15f9824f9d87daa` |
| `menu` | `177d3678bfc8e8b4187ed489066bef2f` | `f85ec56563d14ee7337464ec1608820721d8c0af3cecfd13b424294fd95e974e` | `fddf97b917a61b9f9527a2b9e430f8391727935955eb041baeaba8d4d8bd9257` | `918325bde7bb5366fd99f67e59ef5b30` |
| `ramtest` | `7a60de20fc0fb448849cd24eed50a253` | `d3774d8027b61a943e77edac3f6815c7e8f3a526da1d18dd688bcf91f64fd925` | `a5e0d01b501fb7721240a64b8a9b71065940e8d5b23af7c2b72ff76173543347` | `4d8eb4efff711f03f1b842ed2b056b9a` |
| `riscv` | `e0a17b8e0d06d1b5b5fae611bdedc308` | `a7224f83777d917af62b4b4a1d3db814e34f69028c1fb09254b8f09946303831` | `99f92fafc5b0c591a34968b535468610c479219066d579bfee046b7d42c33a50` | `89ce0e93c74934dc3ed7bfc1a131d990` |

### The control: main passes, by 27 kHz

`coleco_v2` was built unforced at **main's** head `e9218eb5e` to ask whether the miss is
pre-existing. It is not -- and the answer is worse than if it had been.

Main passes: `rc=0`, seed 5, `BUILD_ID aafe5d2738586e5238711651a99d9f35`, RBF
`347164e76223cb0a482c6669ad9339455c7d8393bb65a702be6d9bd893612cee`. But its binding clock
clears by **0.052%**:

| clock | achieved | constraint | margin |
| --- | ---: | ---: | ---: |
| `system_clock.clocks[0]` | 52.25206 | 52.22477 | **+0.052%** (27 kHz) |
| `pixel_clk` | 106.48493 | 74.25007 | +43.4% |
| `system_clock.clocks[1]` | 196.92792 | 12.28803 | +1502% |

So `coleco_v2` on main today closes timing by twenty-seven kilohertz. It is not a core this PR
broke; it is a core with no margin, on which any identity move is a coin toss. Main is one
closure-touching commit away from the same failure at any time, by whoever happens to push it.

**The repeat reproduces exactly.** A second unforced build at this branch's head returned the
same verdict and the same ten per-seed ratios to every recorded digit -- best seed 2 at
0.9795876837, down to seed 7 at 0.8594254767. The failure is deterministic, not seed luck, so
re-running it is not a route to a pass.

**What this corrects.** An earlier revision of this record implied `scripts/core_package.py`
caused the failure. That is too strong: main carries that same change, with its own moved
identity, and passes. What decides the outcome is the identity *value*, not which file moved it.
`core_package.py` is still the reason every identity moved at once -- it sits in nearly every
closure while compiling to no RTL -- but the failure belongs to the margin, not to the file.

### Condition 3 for `c64`

`c64` fails too, by a different mechanism, and the two should not be read as one finding.

```
build-fes-c64-oss: no placement met timing (best seed=9 weight=2000 worst_ratio=0.9894)
```

Of ten seeds, **three** produced a placement to time -- 0.9894, 0.9700, 0.9180 -- and **seven**
hit the recipe's 600-second router budget and were killed (`exit 124`), recording
`worst_ratio` 0.0. Those zeros are not near-misses; they are seeds that never finished routing.
[`condition-3/ratios.py c64`](condition-3/ratios.py) says which is which, and
[`condition-3/c64.router-timeouts.log`](condition-3/c64.router-timeouts.log) holds two of the
router tails: seed 1 was killed at `iter=139 overused=1`, one overused wire from a completed
route, and seed 6 reported a routed system-clock Fmax of 50.62 MHz against a 52.22 constraint,
so it would have missed even given more time.

So `c64`'s condition 3 is partly a routing-time problem and partly a timing one, and the
recipe's 600 s budget is binding on it. Raising that budget is a recipe change -- a
route-deciding input -- so it is not available inside this PR, and it is not attempted here.

### How close the passers came

"Eleven of twelve passed" is true and misleading on its own, because a
`BUILD_ID` move reshuffles placement and a core passes or fails on the headroom
it had on its **tightest** clock. The producers' own timing blocks are copied
verbatim into [`condition-3/clocks.json`](condition-3/clocks.json) and
[`condition-3/margins.py`](condition-3/margins.py) prints the ranking, so these
figures are derived rather than transcribed:

| core | tightest clock | achieved | constraint | margin |
| --- | --- | ---: | ---: | ---: |
| `coleco_v2` (this PR) | — | — | — | **FAILED, best seed 0.9796** |
| `c64` (this PR) | — | — | — | **FAILED, 3 of 10 seeds timed, best 0.9894** |
| `coleco_v2` (on **main**) | `system_clock.clocks[0]` | 52.252 | 52.225 | **+0.05%** |
| `sms` | `system_clock.clocks[0]` | 53.067 | 52.225 | **+1.61%** |
| `ramtest` (100) | `ram_clock.clocks[0]` | 103.670 | 100.000 | +3.67% |
| `atari_st` | `system_clock.clocks[0]` | 54.215 | 52.225 | +3.81% |
| `riscv` | `endpoint.clk` | 77.537 | 74.250 | +4.43% |
| `apple2` | `system_clock.clocks[0]` | 56.004 | 52.225 | +7.24% |
| `spectrum` | `display.control.clk` | 90.942 | 74.250 | +22.48% |
| `menu` | `endpoint.blit_control.clk` | 92.997 | 74.250 | +25.25% |
| `pong` | `core.game.clk` | 93.817 | 74.250 | +26.35% |
| `sg1000` | `system_clock.clocks[0]` | 71.659 | 52.225 | +37.21% |

`zx81` is absent because its condition 3 was taken at `574a73432` and is not
re-measured here.

**The swing is measurable, and it is bigger than some of these margins.** The
same `ramtest` 100 MHz package, `ram_clock.clocks[0]` against a 100.0
constraint, across four identities:

| identity | achieved | margin |
| --- | ---: | ---: |
| DeanoC/fes#1881 head | 100.97950 | +0.98% |
| (earlier) | 101.44046 | +1.44% |
| (earlier) | 102.37511 | +2.38% |
| this record's head | 103.67000 | +3.67% |

2.69 percentage points of spread on one package, from changes that compile to no
RTL. The figure drifts with identity rather than tracking anything functional.
The three lower readings are DeanoC/fes#1881's measurements, not re-derived here.

**Two ways to say which cores this bites, and they agree.** By the swing above,
anything inside about 2.7pp of its constraint can flip. The DeanoC/fes#1881 work
offers the blunter version, a binding clock sitting within roughly a megahertz of
its constraint, and on this table both pick out the same cores. `coleco_v2` **on main** clears
by 0.027 MHz, the thinnest reading anywhere in this record, and `sms` by
**0.843 MHz** (53.067 against 52.225); nothing else among these is close. The
corollary matters more than the rule: for a core in that band, **a condition-3
pass at a neighbouring commit is worth nothing**, because the identity it was
measured under is not the identity that ships.

So `sms`, at +1.61%, is **inside** that band: it passed this time and could fail
on the next identity move, exactly as `coleco_v2` did. That is the concrete case
for DeanoC/fes#1878. The cost of the current identity rule is not only wasted
rebuilds; it is that two cores' timing closure is decided by a constant with no
functional meaning, and one of them has already lost.

Credit for the observation to the DeanoC/fes#1881 work, which noticed its own
memory margin had thinned across identities and said so rather than recording a
bare pass.

### Does a merge invalidate these identities?

Only if it touches a closure. A `BUILD_ID` hashes its producer's transitive
source closure, so a merge that changes no file inside a closure moves no
identity, and condition 3 measured before it still describes what ships. The
fear that every merge forces a re-measurement is wrong, and acting on it is a
treadmill -- but the *converse* error is the one that cost this record a review
round, so the question is answered mechanically rather than by reading paths:

```
python3 docs/validation/2026-11-13-1581-s7-identity-fields/closure_touched.py <base> <head>
```

It prints every producer, its closure size and any hit, exits non-zero if any
core is affected, and refuses to answer for a producer whose closure it cannot
find rather than reporting a false clean.

For main's `333900cbb` (DeanoC/fes#1860, merged forward here): 9 files changed,
**0** under `sources/misteross/`, no closure hits across all twelve. So no
identity moved and the condition-3 results below stand at the merged head. This
is why `scripts/core_package.py` was different in kind -- it is not compiled and
decides no route, but it *is* in nearly every closure, so it moved every
identity and took `coleco_v2`'s timing with it.

### Condition 3 for `coleco_v2`

`coleco_v2`'s own unforced build at this branch's head misses timing. The producer's verdict, in
[`condition-3/coleco_v2.producer.log`](condition-3/coleco_v2.producer.log):

```
build-fes-coleco-socket-v2: no placement met timing (best seed=2 weight=2000 worst_ratio=0.9796)
```

All ten seeds in `fes.coleco`'s recipe ran and none met timing; the best is 2% short and the spread
runs to 0.859, so this is not a tie one more seed would break. The per-seed figures are not
transcribed here -- run [`condition-3/ratios.py`](condition-3/ratios.py), which derives them from
the producer's own committed `coleco_v2.qor-ranking.json`.

**What moved.** An earlier condition-3 run of this core, at `f07950e12`, passed on seed 1 in seven
minutes, the search stopping at the first seed to meet timing. At this head seed 1 scores 0.8720:
the whole distribution moved down, not one seed. Exactly one file inside `coleco_v2`'s 56-entry
source closure differs between those commits, `scripts/core_package.py`, and its diff is DE25 nano
format-3 package *validation* -- it emits no RTL and feeds no synthesis input. Its only effect on
this build is that it is in the hashed closure, so the `BUILD_ID` moves, and the producer injects
that as `chparam -set BUILD_ID 128'h<id> top`.

That the netlists are otherwise equal was checked rather than assumed: 61 modules on both sides with
identical names, one differing module (`top`, 5382 cells against 5387), the LUT mix shifted
(`MISTRAL_ALUT5` -5, `ALUT3` -4, `ALUT6` +3, `ALUT2` +2), and the renamed cones landing in the bus
readback and `audio.lrclk` logic -- which is where folding a different 128-bit constant through the
identity readback path would land. Synthesis reproduces on this host: the determinism run showed two
builds of one tree byte-identical in both `core.rbf` and `synth.json`.

So a 128-bit constant with no functional meaning decides whether this core closes timing. That is
the identity cost DeanoC/fes#1878 proposes to remove, appearing as a hard build failure rather than
a wasted rebuild.

**What this record does not do about it.** It does not add seeds, widen the weight list or touch the
recipe: those are route-deciding inputs, and changing them here would void the comparison this
record exists to make. It does not cite the `f07950e12` pass, which ran at the wrong base. It does
not treat a later passing repeat as a replacement, because the gate was precommitted. Moving the
core's nextpnr lock forward is the project's sanctioned remedy for a timing miss, but it changes a
route-deciding input, so it belongs in its own PR with a reseal and kit HIL -- not here. The
disposition is Deano's call.

Every digest is the full sha256 of the artefact named. The RBF column is one value because the base
build and **both** forced builds produced it byte for byte; the netlist column likewise. Condition
3's `BUILD_ID` differs from the base's because it is the PR's own unforced build, and its RBF
differs too -- a different `BUILD_ID` constant maps differently, which D12 anticipates. Condition 3's
claim is that the producer's own timing and seal gates passed with this commit's real identity, not
that its bytes match.

### The commands that produced them

Recorded because D12 asks for "both digests, and the command that produced them". The harness
scripts are committed beside this record in `harness/`; they carry absolute paths and host names, so
they are a record of what ran rather than a portable tool -- run them unchanged and they would write
into the same directories on the same two hosts.

Eight cores on `chip-30x0-ai-dev` (CUDA), from `/data/d12s7/same`, one checkout path per core with
the build directory cleared between runs:

```
git -C /data/d12s7/same checkout --detach <base|head>
cd /data/d12s7/same/sources/misteross
# base, and condition 3 at the head:
python3 scripts/<producer>.py --cache-root /data/fes-toolchain-cache --gpu-router CUDA
# forced, twice:
python3 /data/d12s7/same/docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py \
    scripts.<producer> <base build-summary.json> -- \
    --cache-root /data/fes-toolchain-cache --gpu-router CUDA
```

Four cores on Powerboat (HIP), from `/data/d12pb/same`, same shape without `--gpu-router` because
`pong`, `menu`, `ramtest` and `riscv` accept no router flag and `fes_build_common` pins
`FES_GPU_ROUTER = "HIP"` (DeanoC/fes#1862 addresses that), and with
`--cache-root /home/deano/fes/out/cache/misteross-toolchains`. `ramtest` additionally takes
`--memory-mhz 100`; its 130 MHz package is out of scope, see below. Where a toolchain had to be
built first, `FES_TOOLCHAIN_GPU_ROUTER=HIP` is required -- `scripts/bootstrap.sh` defaults it to
`OFF`, which builds CPU-router installs under different keys, reports success and then fails the
producer's verification.

Per-core logs, build records and artefacts are under `/data/d12s7/{confirm,c3,zx81new}` and
`/data/d12pb/{results,c3}` on those hosts; they are not committed, being several gigabytes of RBFs
and netlists.

### What this evidence does not cover

- **`fes.pcxt` and `fes.pcxt-dos` take the normal reseal and HIL path**, not D12's exemption.
  Neither condition is measured, and the reason is a toolchain-authentication boundary rather than
  cost: `cores/fes-pcxt/quartus.lock` pins the *binary* digest of the Yosys that may elaborate this
  core, and on Powerboat resolving `cores/fes-pcxt/toolchain.lock` through the per-tool cache yields
  a different build of that same commit, so the producer refuses before elaborating. The pinned
  build is present on the host under another key; the installed Quartus matches its own pin exactly.
  Satisfying it locally would mean editing the lock, which relaxes a seal, or forcing the cache to
  serve an install other than the one resolution selected, which defeats the digest pin. Filed as
  DeanoC/fes#1868. Deano's decision is that this PR merges with that reseal to follow rather
  than waiting for it; the obligation is DeanoC/fes#1869, so it does not rest on anyone
  remembering. Note that #1868 blocks sealing this core on Powerboat at all until it is
  settled, which bears on when #1869 can be done.

  What *is* measured for these two is supporting, not sufficient: their canonical QSF is
  byte-identical between the base and this branch for **all eight** combinations of
  `package_profile`, `video_mailbox_contract` and `audio_contract`, so Quartus's inputs are
  unchanged however the producer is invoked, and that QSF carries no `BUILD_ID` line, only `SEED`.
  But PC/XT elaborates its whole top with Yosys (`quartus_fes_pcxt.FRONT_END = 'yosys'`), so D12's
  condition 1 for it is the **Yosys netlist**, not the QSF — the QSF is what the rule asks of a
  *native* Quartus front end. An earlier revision of this record described the QSF comparison as
  condition 1, which was wrong.

- **The forced-build driver is unverified by a control.** Every forced build goes through
  `forced_build_id.py`, which patches `build_identity` and stubs the package export. No run here
  shows that an *unpatched* build reproduces what the driver produces, so a flaw in the patching
  would present as a passing comparison. The campaign for DeanoC/fes#1862 does run that control and
  found its adapters faithful; this one does not.
- **The harness has no clean-tree guard of its own.** The producers do: each calls
  `_require_clean_source` and refuses a modified tree, and both campaign worktrees held no modified
  tracked files. So the evidence is sound, but by the tooling's enforcement rather than this
  harness's.
- **`ramtest`'s 130 MHz package is out of scope by decision, not by oversight.** It *is* a
  separately sealed artefact -- `Makefile` has `build-fes-ramtest-130`, `--memory-mhz 130` seals
  into `build/fes-ramtest-130` with its own memory PLL, and `docs/architecture.md` says both rates
  seal distinct packages with their own timing gates -- so this change moves its `BUILD_ID` too, and
  the 100 MHz evidence cannot discharge D12 for it. Deano's decision is that the 130 rate was an
  original aspiration and 100 is the current target, so it is not measured here. If that package is
  ever sealed again it needs conditions 1, 2 and 3 of its own first.
- Condition 3 is **not** satisfied by the base and forced builds completing their producer's
  checks, which is what an earlier revision of this record claimed. A forced build carries the
  base's `BUILD_ID`, so it is not this commit's own build and cannot discharge a condition about
  this commit's identity (DeanoC/fes#1806 review). Condition 3 is a separate unforced run per core,
  and the `condition 3 BUILD_ID` column holds its result.

## Review fixes

A review of the first two commits found one real defect, fixed in `1a314bde3`.

**DE25 builds would have aborted after the fit.** DE25's producers encode their records with
`boards/de25nano/package/export_core_package.py`, but `boards/de25nano/scanout/async_inputs.py`
read them back through the shared exporter's `build_identity`. A DE25 record is format 2 with
`toolchain = "quartus"` and none of the new parameters, so the new rule rejected it, and both
`build_menu_package.py` and `build_zx81_package.py` call that review on every fit.
`tests.test_build_de25_menu_package` failed with two errors. DE25 now reads its records with the
exporter that wrote them. The two exporters derive a format-2 identity identically, so no DE25
value changes, and nothing else under `boards/de25nano` uses the shared exporter.

**A non-string `front_end` crashed the loader.** `front_end` is optional in the recipe schema, so
the generic non-empty-string check does not reach it, and a TOML array or table raised `TypeError`
from the membership test instead of the loader's own error. Both the loader and the support matrix
now check the type first, and the reject cases cover an array and a Boolean.

A third finding, that the matrix's new cross-check never reaches the two DE25 rows, was refuted:
`validate()` filters the registry to `de10-nano` by design.

## Tests

No core was built for this commit. The new tests:

- one QSF digest covers seeds 1, 2 and 3, and a QSF with no `SEED` line is refused;
- one program digest covers the signoff and repeat runs at two different BUILD_IDs, and a program
  that leaves the canonical form is refused;
- the record binds the front end, its device family and both digests, and the two PC/XT profiles
  differ in both digests;
- a compiled project that differs from the record is refused in each of the three fields;
- a Quartus record that misstates its front end, device family or digests is rejected, in six
  shapes, as is a `quartus-lite` record carrying a program digest and a `yosys` record without one;
- only a Quartus record carries the four fields: a format-1 record with any of them is rejected, and
  so is a format-2 record without `toolchain`;
- two records differing only in `front_end` get different identities;
- an OSS record encodes to the same bytes and the same identity as before;
- a recipe row's front end must fit its edition and device family, in six rejected shapes, and an
  OSS row cannot name one.

The whole repository suite passes on this tree: `python3 -m unittest discover -s tests` is 1,830
tests, 47 skipped, no failure, in 1,019 s, and `docs/support-matrix.py --check` reports ok. In
`sources/misteross`, `tests.test_build_fes_pcxt_quartus` (23, 1 skipped),
`tests.test_export_core_package` (23), `tests.test_build_de25_menu_package` (20),
`tests.test_pnr_recipes` and `tests.test_core_package` pass.
