# Current build architecture

misteross turns small experiment RTL into local MiSTer RBF artifacts. Network
deployment and target lifecycle are outside this repository.

## Build lanes

```text
experiment RTL + constraints
  |-- sim ----> Verilator result
  |-- oss ----> Yosys -> nextpnr-mistral/Mistral -> top.rbf
  `-- oracle -> Quartus Prime Lite 17.0.2 -------> top.rbf

oss manifest + oracle manifest -> compare report
```

`sim` checks the experiment's logical behavior with Verilator. Simulation-only
models never enter either synthesis lane.

`oss` uses only the pinned repository-local tools described by
`toolchain.lock`. Generated sources and tools live under `build/toolchain/`.
Build output lives under `build/oss/<experiment>/`.

`oracle` uses an explicitly configured Quartus Prime Lite 17.0.2 installation.
It uses the same production RTL and timing intent as the OSS lane. Output lives
under `build/oracle/<experiment>/`. Quartus is not an OSS or simulation
dependency.

`compare` reads the two lane manifests and writes its result under
`build/compare/<experiment>/`. Differences between compiler-produced RBF bytes
are expected; the comparison focuses on target, sources, resources, timing,
and successful artifact production.

## Experiments

`010_blinky` is a 50 MHz counter driving one LED. It is the smallest physical
output test.

`020_linux_mailbox` uses one Cyclone V HPS general-purpose interface to return
the constant message `OSS FPGA OK\n`. It has no external FPGA output. The
simulation substitutes a small HPS model; both synthesis lanes use the real
HPS primitive boundary.

## Artifact boundary

The integration outputs are:

```text
build/oss/<experiment>/top.rbf
build/oracle/<experiment>/top.rbf
```

FogCast will select, upload, and load one of these ordinary files. No bundle,
attestation record, run ID, recovery journal, or fault-injection result is
required.

`make program` is an optional direct diagnostic. It is deliberately separate
from `sim`, `oss`, `oracle`, and `compare`, so building an RBF never touches
hardware.

## Pinned core trees

`cores.lock` records third-party FPGA core git identity (repo, commit,
in-tree release RBF path, sha256, size, Quartus project). `make fetch-core`
checks out that exact commit under `build/cores/<name>/` and hashes the
locked RBF. It does not clone `HEAD`, does not reset dirty trees, and does
not run Quartus. The hashed release remains the oracle until a later
Quartus rebuild bit-matches.
