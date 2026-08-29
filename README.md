# Open MiSTer OSS Cyclone V Toolchain

This private, local repository tests whether a fully open toolchain can build a
useful bitstream for the Terasic DE10-Nano/MiSTer FPGA (`5CSEBA6U23I7`). The
first experiment, `010_blinky`, drives one user LED from a 50 MHz fabric
counter. The software-tested `020_linux_mailbox` experiment instead exposes a
small HPS GPI/GPO mailbox with no external FPGA output and returns the exact
payload `OSS FPGA OK\n` to a private FogCast development loader.

The three lanes are deliberately separate:

- `sim` uses Verilator as the logical oracle.
- `oss` uses only pinned Yosys, nextpnr-mistral, Mistral, and openFPGALoader.
- `oracle` uses Quartus Prime Lite 17.0.2 only when explicitly requested.

Quartus is never an OSS or simulation dependency. FPGA sources are built from
pinned commits into `build/toolchain/`; host prerequisites are reported, not
silently installed. No build target programs hardware automatically. M1
programming is volatile only: it does not write flash, HPS storage, or an SD
card.

Bootstrap is repository-local and idempotent. Check ordinary host prerequisites
without changing the machine, inspect the lock-derived plan, then build the
five pinned tools:

```text
make toolchain-check
scripts/bootstrap.sh --print-plan
make toolchain
source scripts/env.sh
```

`build/toolchain/src/` contains detached exact-commit checkouts,
`build/toolchain/build/` contains build logs, identity output, and per-commit
SHA-256 digest stamps, and
`build/toolchain/install/` contains the shared local prefix. A dirty or
mismatched checkout stops with instructions for a manual, reviewable fix; the
bootstrap never resets or deletes source trees. `scripts/env.sh` prepends only
the local `install/bin` directory and adds local library/pkg-config paths. It
does not search for Quartus.

The public interface is:

```text
make toolchain
make toolchain-check
make doctor
make sim EXP=010_blinky
make oss EXP=010_blinky
make oracle EXP=010_blinky
make compare EXP=010_blinky
make program EXP=010_blinky BUILD=oss

make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
make dev-bundle EXP=020_linux_mailbox BUILD=oss RUN_ID=<32-lower-hex>
make dev-preflight EXP=020_linux_mailbox BUILD=oss RUN_ID=<32-lower-hex>
make dev-load EXP=020_linux_mailbox BUILD=oss RUN_ID=<32-lower-hex>
make dev-fault-inject EXP=020_linux_mailbox BUILD=oss RUN_ID=<32-lower-hex>
```

The `dev-*` targets are a separate FogCast transport; they do not change or
wrap the existing `program` path. They default to dry-run and require explicit
target attestations before live use. See
[Linux mailbox development](docs/linux-mailbox-development.md) for the exact
workflow, evidence limits, recovery behavior, and rollback boundary.

The repository remains private while the open flow is being reduced,
simulated, compiled, and validated. The M2 handoff is **Software-tested** only:
no mailbox RBF has been loaded on hardware by this repository gate. Video,
SDRAM, audio, and persistent-storage changes remain outside this project
cycle.
