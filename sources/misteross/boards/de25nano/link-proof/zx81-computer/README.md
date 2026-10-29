# Combined DE25 ZX81 computer and PR RAM-pack diagnostic

This profile combines the HPS computer mailbox, real NMOS ZX81/BASIC, HID,
CRC-checked tape delivery and coherent 720p display with the registered
empty/16 KiB RAM personas. Both endpoints share the static base's build ID.
CPU execution requires transport health, PR RUNNING and mailbox RUN. HOLD or
PR freeze captures an idle CPU bus independently of enable-scheduled reset;
the existing drain and four-cycle settle complete before Frozen. Selection
resets/reboots BASIC and does not preserve a running program.

The PR bus retains its protected 46-bit request and 20-bit response, five
sampled edges/four elapsed cycles and six-cycle contract. The empty persona
uses the machine's internal 1 KiB; the RAM persona uses the actual
`zx81_ram_pack` in M20K. The static mailbox/tape/display and HPS/DDR remain
outside the PR region. Its separate interface identity is
`fes.de25-zx81-computer-empty-ram16k.v1`.

Run from `link-proof/`:

```sh
python3 build.py build --profile zx81-computer --output /absolute/new-build \
  --quartus-root /data/altera_pro/26.3 --vendor-archive /absolute/vendor.zip \
  --placement 'X2 Y7 X26 Y20' --routing 'X1 Y6 X27 Y21'
python3 zx81-computer/proof.py --native-only --output /absolute/new-proof
python3 proof.py --artifacts /absolute/new-build/hps/project/artifacts.json \
  --output /absolute/new-composition --qemu /absolute/qemu-aarch64-static
```

`zx81-computer/proof.py --help` lists the explicit SDK image, read-only volume,
compiler and QEMU arguments for static AArch64 execution. Both actual C++
clients operate separate resources in one actual RTL model. The session
selects empty/RAM/fresh RAM/empty, observes CPU-written RAMTOP 0x4400/0x8000/0x4400,
delivers a real BASIC LOAD through the RAM persona, and freezes a later load
in flight after a fresh RAM boot. The loaded NEW snapshot is not required to
return to the keyboard editor. Callback failure, wrong persona/build, uncertain writes, missing
responses and actual later reset must leave execution held and prevent retries.
Pixel-clock loss must not block selection, control, tape upload or CPU progress.
Simulation-only observations advance clocks and inspect state; they never
construct the clients' mailbox or PR commands or inject RAMTOP.

Persona selection is a mux model, not SDM execution. RAM is behavioral in
simulation. Production constructors, physical MMIO/device binding, global
reset fencing and exact-board HDMI/DDR/PR acceptance remain open. Builds and
proofs never program an FPGA, modify boot media or install software.
