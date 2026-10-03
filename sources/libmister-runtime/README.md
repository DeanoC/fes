# libmister-runtime

Development lives in the FES repository under `sources/libmister-runtime`.
The former standalone repository is archived.

The C++14 library and local `mister-runtime` daemon own FPGA programming,
physical lifecycle, media/input delivery, and recovery. FES described packages are the only product launch path. The installed driver supports
`fes.simple-game`, `fes.simple-computer`, `fes.application`, and
`fes.computer` through `fes-gp-v1`. Protocol 2 is the only local socket protocol.

Raw RBF loading is an explicit, idle-only hardware diagnostic using
`development-contained-v1`; it does not infer a game, ABI, media, or persistence.
Startup, Stop, and bounded failure recovery load the sealed splash through the
contained programming profile and ADV7513 video path. A `fes-gp-v1` package
load releases the HPS bridges after the FPGA enters user mode. The SDR FPGA
ports stay in reset unless the package requires `fes.memory.hps-ddr` 1.0,
the boot core latched the shared layout, and the package's verified identity
and live SDR mirrors match that layout. The
runtime contains no Main launcher, conventional game profiles, MiSTer SPI
driver, or general framebuffer service.

The Atari ST home-computer contract uses required `fes.media.atari-st-floppy`
1.0 for exact 737,280-byte `.st` disks on media unit 0. Insert and eject keep
execution running. Optional `fes.expansion.atari-st-bus` 1.0 composes one card
in physical socket 1 through the existing slot-composition path. These paths
have host test coverage; Atari ST hardware acceptance is pending.

Menu-display GP, immutable staging and reserved-memory primitives have host
test coverage. Explicit idle-menu activation and generation-bound presentation
use the existing local socket with immutable descriptor transfer.
Required simple-computer session-display and HPS DDR capabilities reuse that
frame path with a separate display generation. Opening/closing controls the
plane without resetting the machine; a session frame fault disables the plane
and retains the active core instead of loading idle. This has host coverage;
combined ZX81 exact-artifact hardware acceptance remains pending.
Default startup remains splash unless the image passes an explicitly selected
menu package to the daemon. That menu returns after Stop with a fresh
presentation generation.

Package admission retains validated artifacts before mutation. Activation
verifies live ABI/build identity before input or media controls. Initialized
loads admit that sealed package, and a recomputed cart when one is selected,
then program a separate bitstream whose digest matches the host receipt. The
sealed package id stays the admitted package. Library loads
explicitly bind durable core data; development loads remain volatile. Failed
save publication retains session ownership and captured data for retry.

Developer Coleco parts admission distinguishes raster RGB and native indexed
video sockets by their sealed marker, map and layout. The native 1.0 socket
fixes a 256 by 192 four-bit TMS9918 palette source and requires a video part;
inspection alone does not authorize an uncomposed package load. These admission
and lifecycle guards have host test coverage; native exact-artifact hardware
acceptance remains pending.

Hardware-supported package paths: 0.
Exact-artifact hardware acceptance is pending for this cleanup. The historical
[Mega Drive baseline](../FogCast/docs/hardware/native-megadrive-baseline.md)
records a retired path and does not validate the current package artifacts.

Build and validate with `make all` and `make test`. See
[architecture](ARCHITECTURE.md), [support matrix](docs/support-matrix.md),
[menu presentation](docs/menu-display.md),
[development](DEVELOPMENT.md), [application I/O](docs/application-io.md),
[home-computer I/O](docs/computer-io.md), [stream media](docs/media-stream.md), and
[core persistence](docs/core-persistence.md).

The default build uses `-O2`, including appliance builds. Package admission
hashes every sealed member; unoptimized hashing can exceed the target's bounded
activation deadline for large ROM maps. An explicit `CXXFLAGS` still overrides
the default for diagnostic builds.

Format-3 package inspection validates the closed manifest/RBF/ROM-map file set,
retains the map descriptor, and binds all three files to the package identity.
The required ROM slot metadata is exposed in inspection. Map semantics and CRAM
linking belong to the target agent; C++ does not implement another linker.
Format-3 activation uses `load_rom_core`, `load_rom_library_core`, or
`load_rom_composed_core`. Each carries `programmed_path` and a closed `rom_link`
receipt: `rom_id`, `map_sha256`, `source_sha256`, `source_size`,
`programmed_sha256`, and `programmed_size`. The runtime binds the receipt to the
sealed ROM descriptor, retains the programmed FD, and rechecks its bytes and
size before retiring input or quiescing hardware. The local agent owns linking
and source-ROM validation. Ordinary and initialized operations reject format 3;
ROM operations reject format 2. Native capabilities advertise `rom_linking: 1`.
Active status retains the receipt until retirement; library ROM loads preserve
core-scoped persistence. This path has host software coverage only and adds no
hardware acceptance claim.
