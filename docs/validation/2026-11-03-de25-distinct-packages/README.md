# Two distinct Nano diagnostic packages and matched native boot files

The subsequent [board test](../2026-11-03-de25-distinct-hardware/README.md)
passes the three-session switch. This earlier record retains its original
software/artifact checkpoint scope.

The Linux-free native service now has two real sealed package choices for its
next bounded test: the previously tested ZX81 payload and a newly compiled ZX80
payload. This record is software, simulation and artifact evidence. Neither the
new ZX80 package nor the refreshed native boot chain has been loaded on a board.

The producer adds `--computer-model zx80`. It selects the captured machine's
ZX80 ROM decoder, NMI and tape-loader logic, and places the reviewed combined
image's ZX80 firmware in its 4 KiB ROM window. Both variants retain the existing
diagnostic's built-in 16 KiB RAM. The source image's two exact hashes are checked
before applying its known layout. The model enters the canonical functional
record before synthesis; actual project macros and both firmware copies are
checked afterward. The existing HPS, DDR, clocks and master inventory are
preserved.

| Choice | FPGA producer | Package ID prefix | Build ID | Core payload SHA-256 prefix |
| --- | --- | --- | --- | --- |
| ZX81 diagnostic | `c272cb2b3207` | `b2353f540cc8` | `dda236646bb868e63c72bd0e777f3dd6` | `e9255d3dc149` |
| ZX80 diagnostic | `afdabcae299a` | `79f27adbfea0` | `90f763a6040701d3c254567d1cc01511` | `d684d2e73f0e` |

Both core payloads are 1,646,592 bytes. Their bytes, complete package IDs and
build IDs differ. Their complete contained platform contracts match, including
HPS parameters, HPS I/O, generated handoff, PFG identity and master inventory.
The actual sealed archives pass native retention and preflight in the order
ZX81, ZX80, ZX81 against one compiled boot anchor, without firmware I/O.

Network delivery adds the fixed `FES/PKGALT.BIN` and `FES/PROGALT.JSN` slot beside
the existing package and metadata. Both archives have an 8 MiB bound; both
metadata files have a 16 KiB bound. The prepared 513-byte selection file refers
to the two independent slots. The upload model checks six writes in one owned
session and fresh reads of all six files after the final write, including
failure retirement and protected boot files. The hardware runner still needs
that six-file update before a board attempt.

Validation:

- All eight affected scopes before the broad FPGA scope passed on
  `afdabcae299ad698634972e3cb9514f2021b5a4a`, including 1,359 parent tests
  (1,357 passes and two explicit environment skips).
- The broad FPGA command reached its 300-second aggregate limit. Its exact
  1,577 test occurrences subsequently completed in bounded complete-module
  groups: 1,502 passes and 75 explicit environment skips. An old sealed-record
  fixture needed the newly required model field; the fix also checks model
  identity sensitivity. Partition zero retains its passing producer-commit
  result; the other groups ran on `bde52b56470ddcbd5934b3d208270d11d1011162`.
  The only FPGA-tree difference is that fixture. Original failures and logs
  remain retained.
- The real captured CPU/machine simulation distinguishes ZX81's ROM window and
  NMI from ZX80's. Native computer control passes against the actual captured
  ZX80 RTL, its installed ROM and matching build ID. The upload suite passes
  all 35 tests. The actual two-archive native preflight passes with race checking
  against both the previous and refreshed native SDK anchors.
- One complete Quartus Pro build on `fes-build-3090` held the existing
  `/tmp/lander-quartus-3090.lock` throughout generation, compilation and PFG
  conversion. Its inode remained 2949; it was never replaced or removed.
  Every compile leaf retained its 120-second limit and normal constraints.
  All 41 reported timing-summary rows have nonnegative slack (minimum 0.001).
  That observation does not qualify constraint completeness or hardware timing.
- All 1,813 regular artifact files were independently read back after transfer.
  A quoting error in the first export helper was corrected before the transfer;
  its failed log remains retained.

The refreshed SDK, entry, BL31, U-Boot and local PFG composition use source
`bde52b56470ddcbd5934b3d208270d11d1011162`. Native boot ID is
`13c81622014c34cb`. The 6,823,844-byte FIT has SHA-256
`fde8f5bd4d9dc07be97d8b0c313b87ec0afb8a83f49ddbecadc1a89b080d41bc`;
the matched SOF has SHA-256
`3ca36db5da87536f821a5a5b9a6892349a551007fe3d38f81c57415f64a7aea4`.
The native Go ELF is byte-identical to the previous service build. The boot
chain refresh binds current builder inputs; it adds no native execution feature.
All 122 selected Git blobs and 1,298 pinned module/SDK files match. Linked
request-loop, per-load firmware provider, CPU translation instructions, vendor
startup/relocations, contained console and whole-file boot seals pass their
actual artifact checks.

The [machine-readable summary](software-and-artifact-summary.json) records full
identities and report hashes. The prepared files and detailed logs are under
`out/nano-distinct-payloads-20261103/`; source and artifacts are retained under
`/home/deano/fes-retained/de25-distinct-packages-20261103/` with checksums and
explicit frozen-input dependencies.

No board, SD, recovery-loader or flash write occurred during this work. The
last independently verified TRWU8CBL state remains the earlier service boot
`916b895d55afba6e`, terminal Park at application generation 6, SD contained and
data held, with the continuing exclusive claim and spare card retained. This
record supplies no new hardware-state observation. Ordinary Nano admission,
distinct physical package switching, native tenfoot, USB/input, video and native
Ethernet remain open. The next step is the six-file runner update and its fault
checks, followed by one bounded three-session test on TRWU8CBL.
