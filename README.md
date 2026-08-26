# Open MiSTer OSS Cyclone V Toolchain

This private, local repository tests whether a fully open toolchain can build a
useful bitstream for the Terasic DE10-Nano/MiSTer FPGA (`5CSEBA6U23I7`) and
blink a physical board LED. The first experiment is `010_blinky`: a 50 MHz
input, a fabric counter, and one user LED.

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
```

The repository remains private and local while the open flow is being reduced,
simulated, compiled, and validated on hardware. Video, HPS integration, SDRAM,
audio, and persistent-storage changes are outside this project cycle.
