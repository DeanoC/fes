# Coleco joystick/keypad controller diagnostic

This extends the [two-player keyboard diagnostic](2026-09-12-coleco-input.md)
with standard Coleco controller semantics: a shared joystick/keypad latch,
two fire buttons and twelve encoded keypad keys per player. The unchanged
40-bit FES keyboard transport supplies these controls; this is not physical
gamepad, spinner, Super Action, complete VDP, audio or retail-game acceptance.

## Scope and source

FES base: `b91a2496accee7e331c26bf4046fdd2afa17aaef`.
misteross base: `2a640cf93f21d4fc4db64ef1ded37119204e90dc`.
Result: `3d81c99eaf42249d782f0f25f8c946327800bd7a`, branch `feat/fes-coleco`.
Implementation is isolated in `out/dev/fes-coleco/misteross`; integration is
isolated in `out/dev/fes-coleco/fes-integration`. Root integration inputs and
unrelated work are preserved. Only the misteross gitlink changes; FogCast,
runtime, mister-packages and all shared wire contracts remain unchanged.

The controller guide in `sources/misteross/cores/fes-coleco/README.md` records
the exact matrix mapping and pinned MiSTer comparison reference. Reset selects
keypad mode; OUT 80..9F selects keypad, C0..DF joystick, ignoring data. IN E0..FF
selects player by A1. Standard stationary-controller bytes have bit 7=0,
bits 5/4=1, active-low selected fire in bit 6 and directions or encoded keypad
in the low nibble. Multiple keys use lowest-index priority, not an electrical
chording model. No proprietary BIOS or cartridge is embedded or downloaded.

The open controller diagnostic switches modes and renders four raw-byte banks
(player 1 joystick/keypad, player 2 joystick/keypad), eight panels each, bits
0..7 left to right. Green means zero, orange one. Preview-only `--matrix`
does not change cartridge bytes or deliver input.

| Cartridge | Bytes | SHA-256 |
| --- | --- | --- |
| `controller.rom` | 2299 | `ef9443c2787cd02b6d78d233d015b0bbf3fb21d53d1a5890497cdbb7897f053c` |
| `controller-16k.rom` | 16384 | `45972cf19ead11558d0b55e092cc2f1c9ed86717c0c5bf701814a84582e8e16d` |
| Updated `input.rom` | 1857 | `9eb70dd0d379de3ddb24f55eb27df1f458c7ce199ca26e4f4cc3ed66e27dd913` |
| Unchanged `graphics-i.rom` | 989 | `9f9fa280b141e0538a571bb66f1e2447f691eecb853ae05547720f2ccc20783c` |

The old five-panel interactive cartridge must be regenerated: it now selects
joystick mode and maps Fire 1 from bus bit 6 to panel bit 4. Prior five-bit
adapter packages are not reused as evidence for this functional RTL change.

## Focused verification and findings

The CPU regression initially failed on the old RTL (`ff` instead of neutral
`7f`). The current probe executes real CPU IN/OUT instructions and stores results
in CPU RAM: neutral, all 40 individual matrix bits, mixed and all-pressed
states, each through two resets. Every mode-select alias is exercised from
the opposite mode with data 00 and FF, every read alias is sampled, both players
are checked, and unrelated writes must preserve the mode. It leaves joystick
selected before HALT so the next reset must actually return to keypad mode.
Review identified and closed those alias/reset coverage gaps. The strengthened
probe passes; no CPU bus or RAM contents are forced by the test.

The focused Python suite passes 17 tests. Static ROM identity is unchanged;
controller previews check literal raw-byte fixtures, player/fire separation,
key priority and unused bits. Invalid options are rejected without output.

The new board test initially timed out because the observer counted all
qualified CPU OUT strobes as VDP writes, including controller mode selection.
All four CPU cache bytes were already 127. Restricting VDP write accounting to
selected BE/BF ports fixed the observer while retaining the duplicate-write
check. This was a simulation-monitor correction, not a hardware or compiler
workaround. Real CPU-generated VRAM contents and repeated polling establish
settled controller output; unchanged banks must not repaint.

Unmodified Quartus 17.0.2 vendor RAM/media probes pass, including immediate
release at 1, 3, 989, 16384 and 989 bytes. Model digest remains
`e7bc6f0200f8236986c4b255a4ce7937596946bdb646a93551057edc1e08ca69`.
The existing Icarus/vendor-model and registered-memory accommodations are
unchanged. No new Yosys/nextpnr/Mistral workaround has been introduced by the
controller implementation; the core guide retains the existing handoff table.

`OBJCACHE= make sim-fes-coleco VERILATOR=build/toolchain/install/bin/verilator`
passes both complete lanes with production `TV80_REFRESH=1`. Each lane checks
three static frames, the retained 81 interactive frames and 11 new controller
frames (95 total). The new event-driven CPU/VRAM checks cover press/release of
all 40 bits, adjacent-key priority pairs for each player, all-pressed and mixed
states, changed-bank-only repainting, reset-only and held reload/restore.
No cartridge, CPU execution state, VRAM or framebuffer is preloaded by the
oracle. The complete log is `evidence/controller-simulation.log` beneath the
task's ignored `out/dev/fes-coleco/` directory.

Independent review accepted the final CPU coverage and VDP observer fixes with
no remaining findings. The component was committed only after the full test
run passed; fresh FPGA builds consume that clean selected commit.

Parent `make test` passes: 209 top-level tests (36 skipped), including nested
31 real-image tests, followed by the native image recipe shell regressions.
These test-created images are not a new appliance release or deployment.
Log: `out/dev/fes-coleco/evidence/controller-parent-tests.log`.
Parent `make check` passes with the new gitlink staged: 14 generated consumers,
11 fixture copies and four copied source pins match. Other component pins,
runtime lock and shared contracts are unchanged.

## Hardware setup

Both clean recipes built and sealed new packages from `3d81c99`:

| Lane | Package ID | RBF SHA-256 | Build ID |
| --- | --- | --- | --- |
| OSS | `9cae548470224b343eb390358e063c74a7ac73da1f8167d1c9bf46f37f1a1c4c` | `7c0e350dc5736dfff73a375422e8052b65388c06b1e64945b14831d3d33b8b76` | `e3030b021cdf69bff0f8e9edb0684e66` |
| Quartus | `5388ade271963a3f8a91dbefd9c5e6cbdd9ac018956c80f9402a0629a1a5e12d` | `7371f49c7aee5e1ef908189fa3f083688451f3a2a923e055499bcc4e94ea8f24` | `56c6a4d164f859a6dbc12e5d52e83c57` |

OSS passes routing and timing at 59.2207 MHz system / 89.2379 MHz pixel
(requirements 52 / 74.25 MHz), using 133 M10K, 2722 combinational cells and
665 FFs. Its RBF is 2,477,901 bytes. The unchanged authenticated tools are
Yosys `da6373c0d7565f36036051efc7895fb0d9ac13c3`, nextpnr-mistral
`fd862a2c59db7f0406e32831f2e57b3cfe034251`, and Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`. No new flags, constraints, memory
wrappers or compiler patches were needed.

Quartus Prime Lite 17.0.2 completes with zero errors and 35 retained warnings.
All required TimeQuest checks pass: setup 3.082 ns, hold 0.133 ns, recovery
12.150 ns, removal 1.531 ns and minimum pulse width 0.961 ns, all TNS zero.
Its RBF is 2,327,964 bytes. Full logs are `controller-{oss,quartus}-build.log`;
the component's `build/fes-coleco-{oss,quartus}/build-summary.json` and sealed
manifests contain source/input/tool identities. Packages are under
`sources/misteross/build/packages/<package-id>.fcore` in the integration tree.

The existing leased updater confirmed diagnostic image
`77dccfd1f6ff2ea013ff186e31d6030bd5cdd566d1bc22985d05652fa3a2dd3e`
on boot `59928db4-e303-4b2c-af22-ac04b3536c96`, trial false, raw idle ready.
This reuses the previously documented dynamic-runtime derivative, not a new
cold reproducible appliance. FogCast remains
`c761cff0d9e7878d90eb3dee24ba96010acb46de`, runtime
`2629c6e1a896663b3e06688462624c3fac67ba67`. The temporary headless host uses the
previous owner-only configuration copy with remote input enabled; the original
configuration and normal host are preserved. Its SHA-256 is
`a1ace56e6feaf145bff7eaa98eded182ea671544764c7ae106e25c8e2c520013`.

Hardware operations use the existing host-owned target-agent lease and runtime
path, not direct GP, runtime socket, SSH programming or JTAG. The updater's
lease was released after confirmation. Local operator evidence lives under
`out/dev/fes-coleco/evidence/`; the private copied configuration must not be
published with the capture/log artifacts.

## Exact-artifact controller captures

`evidence/run_controller_hil.py` uses the temporary host's automatic input
attachment, submits keyboard events through its owning session, uploads only
the open cartridge bytes, captures uncompressed 1280×720 YUYV HDMI and compares
each frame to the expected matrix preview. No active hardware state is inferred
from a host request alone. Capture comparison excludes two pixels around
expected horizontal color transitions for YUYV chroma, without translation or
scaling; this is color/geometry evidence, not bit-exact RGB capture or gameplay.

Quartus package generation 1 passes all 88 captures: neutral; press and release
of every one of the 40 matrix bits; mixed controls; full-size then compact
reloads while keys remain held; all-pressed priority-zero; release of each
player's zero key to expose priority-one; detached neutral; reattached neutral.
All stable whole-frame and logical-region class comparisons are 1.0. Media
reloads retain the core generation and restore the held matrix. Final Stop
returns idle and releases the kit lease. The four unused bits leave the image
unchanged. Per-state API responses, events, PNGs and checks are in
`evidence/controller-quartus/`, with `controller-quartus-hil.log` alongside.

OSS package generation 2 passes the identical 88-capture sequence, with stable
whole-frame and logical-region comparisons also 1.0. Its final Stop returns
idle and releases ownership. A complete SHA-256 list comparison of all PNG
filenames finds **all 88 pairs byte-identical** between the compiler lanes.
OSS evidence is in `evidence/controller-oss/` and `controller-oss-hil.log`.

| Observed image | Paired PNG SHA-256 |
| --- | --- |
| Neutral, released or detached | `bc87be89d55727b254facb324923f0055c46662ce18973f2c7ac0b7170bea9eb` |
| Mixed / held full-size / held compact reload | `e48157192ea939cb90bb03e82c43a7317cc46c4196c9bed0b9f29763ac08f18a` |
| All pressed, priority-zero | `f6efb8fa32605b7547a22e5b5150b4280b6a1b383d29e16f1e0fc0f491ba7844` |
| Zero keys released, priority-one | `06467da08a0f8b3874ee6db09324e539ba6c381926464238952abac55944adee` |

This establishes keyboard-event-to-CPU-controller-byte-to-video behavior for
these exact new FPGA artifacts on the named diagnostic appliance. Reset-only
HOLD/RELEASE without media commit is covered by simulation, not claimed as a
separate host hardware operation. It does not establish a cold reproducible
appliance release, full Coleco compatibility or physical-gamepad support.

## Restoration and handoff

The leased rollback confirms the original image
`d1733d3fdcc97c4669f07576c3f449ba72f05e6ed77fbcf3b0a029830d98d760`
on boot `9b72aceb-b9eb-43f5-b474-1dc5c3d3bb9e`: raw idle ready, trial false,
pending empty and corrupt false. The kit lease is free and the normal host
reports ready on that boot with original runtime
`a729acc593ec772fa5ecd5f802e2dee9758bd4dc`. Only the temporary test host is
terminated; original configuration, normal host, diagnostic/recovery images,
factory, kernel and bootstrap are preserved. Evidence is
`controller-rollback.log`, `controller-final-lease.json` and
`controller-restored-host.json`.

The newly selected component's 17 focused tests and final parent `make check`
pass; whitespace checks are clean. The integration branch selects only
misteross `3d81c99`; no push or main-branch merge is performed. The base and
result component commits, artifact identities, tests and hardware scope above
are the handoff for this completed controller slice.

For the Yosys/nextpnr/Mistral agent: no new compiler workaround was needed.
The existing registered RAM, replicated VDP read ports, initialization,
constraint-subset and I²C placement accommodations remain documented in
`sources/misteross/cores/fes-coleco/README.md` under “Bring-up fixes and compiler
workarounds.” The mode latch and keypad encoder are functional emulation
changes; the BE/BF observer fix is test-only. Neither should be treated as a
toolchain defect.

Next implementation slice: VDP buffered reads and interrupt behavior, followed
by sprite support, with open CPU-driven diagnostics and fresh paired builds.
Those features are not included in this controller result.
