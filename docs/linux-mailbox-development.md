# Linux mailbox experiment

`020_linux_mailbox` is a small development core for checking that an HPS/FPGA
boundary built by either toolchain can communicate with Linux. It returns the
constant payload:

```text
OSS FPGA OK
```

The core has no video or external FPGA output. A blank display is therefore
not by itself a failure.

## Build

Build and simulate the open-source version:

```sh
source scripts/env.sh
make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
```

Output:

```text
build/oss/020_linux_mailbox/top.rbf
```

With Quartus Prime Lite 17.0.2 configured, build the reference version:

```sh
make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
```

Outputs:

```text
build/oracle/020_linux_mailbox/top.rbf
build/compare/020_linux_mailbox/comparison.json
```

## Run on the MiSTer Pi

The intended development flow is to select either local `top.rbf` in the
FogCast host UI. FogCast transfers the file and loads it through the resident
Main-compatible command path.

Until that UI action exists, `make program` is available as an optional direct
diagnostic. It is volatile and a reboot restores the normal menu. The mailbox
core may not implement enough of the normal MiSTer framework for Main to stay
healthy; rebooting the disposable target is an acceptable recovery.

## Current status

Both toolchain lanes produce RBF artifacts and the logical simulation is
available. Loading and reading the mailbox on the dedicated hardware is the
next relevant physical check; it should be performed through the simple
FogCast development-RBF path rather than recreating the removed bundle and
recovery transport.
