# Linux mailbox experiment

`020_linux_mailbox` is a small development core for checking that an HPS/FPGA
boundary built by the OSS toolchain can communicate with Linux. It returns the
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

## Run on the MiSTer Pi

FogCast loads the local RBF through the host development endpoint. There is no
browser file picker and no Main command FIFO on the designated native kit:

```sh
curl --fail -H 'Content-Type: application/octet-stream' \
  --data-binary @build/oss/020_linux_mailbox/top.rbf \
  http://127.0.0.1:8787/api/v1/session/development-rbf
```

The agent stages `/tmp/fogcast-development/core.rbf` and the native runtime
calls `load_development_rbf`. The mailbox uses the FPGA-manager GPO/GPI pair
(`0xFF706010` / `0xFF706014`, `h2f_gp` / `f2h_gp`). Native development load
probes MiSTer SPI identity on those same wires after programming, so this
core does not stay in `running_development`. Stop restores idle through the
development reboot handshake. A blank display is not a failure.

`make program` is a separate Main-FIFO or JTAG diagnostic, not the native kit
path.
