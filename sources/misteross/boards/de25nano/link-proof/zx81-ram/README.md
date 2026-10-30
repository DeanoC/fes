# DE25 static ZX81 and reconfigurable RAM-pack proof

This opt-in profile keeps the actual ZX81 machine/NMOS CPU, stock firmware
and 1 KiB RAM in the preserved GHRD's static 100 MHz region. One Quartus PR
partition implements either an empty expansion slot or the actual
`zx81_ram_pack` module with 16 KiB writable memory. A guarded board-local
derivation selects that module's portable dual-port implementation and
retargets `zx81_dpram` to Agilex 5/M20K; shared Cyclone V RTL stays unchanged.
Quartus reads the MIF firmware; Verilator reads the matching HEX. Both derived
initialization files preserve all 12,288 original bytes and explicitly pad the
unreachable 4 KiB tail with zero to match the shared 16 KiB ROM declaration.
The ZX81 address wiring only reaches the original first 8 KiB; that guard is
checked before preparation.

The existing counter diagnostic remains the default. Its 64-byte control map,
four-cycle diagnostic latency, source guards and memory checks are unchanged.
This profile has its own interface/source/control/base/root identity and
persona names `zx81_empty` and `zx81_ram16k`. A/B hardware IDs retain their
numeric values only within that new base identity.

The static machine schedules 6.5 MHz ULA/3.25 MHz CPU enables at 13/200.
The 46-bit request and 20-bit response follow `zx81_bus_pack.vh`: static
request FF, persona request FF, synchronous RAM, persona response FF, static
response FF span five sampled edges/four elapsed cycles, within the existing
six-cycle bus bound. Request bits 38..43 are explicitly zero because diagnostic
peek addresses are 0x40xx. Response flags16..18 are zero, bit19 is RAM presence;
empty read/peek bytes are zero. Preparation checks these exact captured drivers.
All 20 persona response FF variables have preserve/dont_merge/noprune attributes:
without them Quartus folds empty responses and fails the PR boundary DRC.
A same-part constant-register probe confirms the preservation semantics, and
actual profile synthesis must report all 20 protected FFs without boundary
inversions before fitting starts.
Both the 46 static request launch FFs and the 20 persona response FFs use
preserve/dont_merge/noprune. Without those attributes Quartus folds constants
and either violates the registered boundary or cannot route an imported PR
input. Synthesis preflight proves all 46 static launch FFs in the base and all
20 response FFs in each revision before fitting. Report-only limits expose
all protected/inverted registers. The completed base must pass its full
all-corner boundary timing audit before implementation builds begin.
Every bit, including the source-constant fields, then has real registered
setup/hold coverage; there is no constant-cone timing exception.

Deasserting RUNNING resets the CPU and captures a complete idle CPU memory
bus at the static request FF: address/data zero and inactive active-low controls.
Peek/clock fields retain their separate contract. This fences read responses
and writes independently of the CPU's enable-scheduled reset latency. The simulation bounds the remaining writes to three edges after that
transition. Diagnostic drain and the existing four-cycle reset settle finish
before Frozen; no writes, retirements or response changes occur after Frozen.
Selection resets/reboots BASIC and makes no promise of program preservation.

Normal diagnostic retirement takes five cycles in this profile. LAST_DATA's
low byte samples the real machine's peek at 0x4000 plus the held diagnostic
address; identity probing retains the full persona ID. The stock ROM stores
RAMTOP as an exclusive bound: 0x4400 with 1 KiB and 0x8000 with the RAM pack.

Run from this directory's parent:

```sh
python3 zx81-ram/sim.py --output /absolute/new-simulation
python3 -m unittest discover -s zx81-ram -p '*_test.py' -v
python3 build.py build --profile zx81-ram --output /absolute/new-build \
  --quartus-root /data/altera_pro/26.3 --vendor-archive /absolute/vendor.zip \
  --placement 'X2 Y7 X26 Y20' --routing 'X1 Y6 X27 Y21'
python3 proof.py --artifacts /absolute/new-build/hps/project/artifacts.json \
  --output /absolute/new-composition --qemu /absolute/qemu-aarch64-static
```

The builder freezes all selected production sources and executes captured
profile bytes. Real base/A/B fitting, internal five-category timing, physical
boundary setup/hold, M20K/fitter-selected firmware initialization inspection, imported-root preservation and
vendor POF0/integrity checks precede artifact composition. The standard native
and emulated AArch64 composer retains exact opaque Quartus payload bytes.

This RAM-only profile adds no computer mailbox, keyboard/tape transport,
video output, physical address, SDM executor or production package. Simulation
models persona selection with a mux and memory behavior rather than physical
PR or M20K primitives. No kit, image, flash or hardware acceptance is implied.
