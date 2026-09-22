# libmister-runtime

Development lives in the FES repository under `sources/libmister-runtime`.
The former standalone repository is archived.

The C++14 library and local `mister-runtime` daemon own FPGA programming,
physical lifecycle, media/input delivery, and recovery. FES format-2 described
packages are the only product launch path. The installed driver supports
`fes.simple-game`, `fes.simple-computer`, and `fes.application` through
`fes-gp-v1`. Protocol 2 is the only local socket protocol.

Raw RBF loading is an explicit, idle-only hardware diagnostic using
`development-contained-v1`; it does not infer a game, ABI, media, or persistence.
Startup, Stop, and bounded failure recovery load the sealed splash through the
same contained programming and ADV7513 video path. The runtime contains no
Main launcher, conventional game profiles, MiSTer SPI driver, or framebuffer.

Package admission retains validated artifacts before mutation. Activation
verifies live ABI/build identity before input or media controls. Library loads
explicitly bind durable core data; development loads remain volatile. Failed
save publication retains session ownership and captured data for retry.

Hardware-supported package paths: 0.
Exact-artifact hardware acceptance is pending for this cleanup. The historical
[Mega Drive baseline](../FogCast/docs/hardware/native-megadrive-baseline.md)
records a retired path and does not validate the current package artifacts.

Build and validate with `make all` and `make test`. See
[architecture](ARCHITECTURE.md), [support matrix](docs/support-matrix.md),
[development](DEVELOPMENT.md), [application I/O](docs/application-io.md),
[stream media](docs/media-stream.md), and
[core persistence](docs/core-persistence.md).
