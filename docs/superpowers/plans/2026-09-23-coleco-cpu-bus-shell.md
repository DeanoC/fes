# Coleco CPU Bus Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a timed, sealed Coleco shell with a vacant CPU expansion socket and an independently routed diagnostic module whose CRAM changes stay inside that socket.

**Architecture:** Add a registered Z80 CPU edge to the current Coleco machine without changing its vacant behavior. Route the ordinary shell with a reserved CRAM rectangle, then route a tiny diagnostic module against the frozen shell. This is the FPGA feasibility gate for the larger, approved library-composition design; it does not advertise or install the socket in a product package yet.

**Tech Stack:** SystemVerilog/Verilog, Verilator, authenticated Yosys + HIP/nextpnr-mistral + Mistral, existing Python package producer and CRAM audit helpers.

**Spec:** [Coleco expansion-bus design](../specs/2026-09-23-coleco-expansion-bus-design.md).

**Feasibility update (2026-09-23):** Task 1's vacant CPU edge and full Coleco simulation pass. The registered-memory nextpnr commit `0fad53a7` ignores `FES_RESERVED_RECT`, so the original region was rejected. A combined development lock uses the Coleco registered-memory Yosys pin with the ZX81 socket-aware Mistral and nextpnr pins. A smaller `24 1 28 11` region and separate development producer seal a vacant, timed shell. The first boundary layout sealed at seed 4 but left `plug_request[23]` unroutable in the cart. Moving that bit to the vacant third-row FF allows the independent module to route. The exact-source development snapshot `snapshot-stxnqljk` sealed at seed 1 with all three clocks passing; the diagnostic route passed the same gates and changed 2,763 CRAM bits inside the socket, zero outside. The routed JSON omits the system PLL's second `outclk[1]` connection, so the builder reconstructs it from exact frozen net metadata. The production package recipe remains unchanged and does not advertise an expansion slot.

**Implementation ruling:** Keep `build_fes_coleco_oss.py` and `toolchains/registered-memory.lock` unchanged so factory identities remain stable. The development-only `build_fes_coleco_socket_dev.py` selects `toolchains/coleco-expansion.lock`, pins the 42 request/response boundary FFs, canonicalizes their netlist names for the existing frozen-scaffold linker, and rejects any other shell cell in the reserved rectangle. The CRAM window is `(1769, 32, 2806, 1034)` with exclusive upper bounds; the independent module route must prove all changed CRAM bits fall inside it.

**Integration next step:** The cart's derived scaffold reconstructs the second PLL driver from the producer's routed net and removes the obsolete `outclk[0]` pin-map alias; its placement QSF reserves only the empty interior `25 1 27 11` to avoid re-binding the 42 frozen boundary FFs. The host-only feasibility gate has passed. Follow the [dependent integration plan](2026-09-23-coleco-expansion-integration.md) for Go linker policy, FogCast/runtime admission and exact-artifact kit acceptance before registering a product expansion asset.

## Global Constraints

- Work only in the FES worktree `out/dev/coleco-expansion-bus/fes`, based on `dbfb6e332221c0df23aee2b89527c1d4593a7cd6`; leave the root checkout and unrelated work alone.
- Preserve `fes.coleco` format 2, its media/firmware mailboxes, and the factory package until end-to-end integration is separately accepted.
- The socket's CPU request is A[15:0], write data[7:0], `/MREQ`, `/IORQ`, `/RD`, `/WR`, `/M1`, `/RFSH`, and reset. Its response is read data[7:0], claim, WAIT, and maskable INT; zero means vacant/inactive.
- The shell permits expansion read claims only for memory `0x2000–0x5fff` and unclaimed I/O ports. Console-owned BIOS, RAM, VDP/controller and cartridge reads always win.
- No bus-master, NMI, video/audio takeover, or ROM/BIOS migration in this plan. No Python, compiler or routing operation runs on the kit.
- Return an uncommitted diff. Do not commit, push or open a PR without user authorization, per root `AGENTS.md`.

## Review Focus

- An unselected socket must return the same bytes, frames, input and sound as the current Coleco package. Task 1 exercises the vacant path with existing board simulations.
- A module trying to claim BIOS, RAM, cartridge or VDP/controller reads must not alter CPU data. Task 1 adds claim-mask assertions.
- WAIT on a held CPU transaction must not duplicate a VDP/PSG write. Task 1 adds a stretched-cycle regression.
- A module built for another shell or changing CRAM outside its rectangle must fail before an archive is published. Task 3 tests both conditions.
- A package build that misses any existing system, pixel or audio clock gate must fail sealing. Task 2 runs the authenticated timing checks.

---

### Task 1: Model and simulate the vacant Coleco CPU edge

**Files:**
- Create: `sources/misteross/cores/fes-coleco/rtl/coleco_bus_pack.vh`, `sources/misteross/cores/fes-coleco/rtl/coleco_expansion_socket.v`.
- Create: `sources/misteross/cores/fes-coleco/sim/expansion_machine.v`, `sources/misteross/cores/fes-coleco/sim/expansion_tb.cpp`.
- Modify: `sources/misteross/cores/fes-coleco/rtl/coleco_machine.sv`, `sources/misteross/cores/fes-coleco/rtl/top.v`, `sources/misteross/Makefile`, `sources/misteross/cores/fes-coleco/README.md`, `sources/misteross/docs/architecture.md`.

**Interfaces:** Produce `COLECO_BUS_REQ=31` bits in order `{reset, /RFSH, /M1, /WR, /RD, /IORQ, /MREQ, Dwr[7:0], A[15:0]}` and `COLECO_BUS_RSP=11` bits in order `{INT, WAIT, CLAIM, Drd[7:0]}`. The physical socket module has `clk_sys`, those packed request/response buses, and an open, active-low CPU edge wired through the machine. Future module RTL consumes exactly this packing.

- [ ] **Step 1: Add the failing behavioral simulation.** In the new harness, instantiate the real `coleco_machine` twice: once with `11'b0` response and once with a small behavioral responder. Use the existing original diagnostic cartridge as both machines' startup media. In `expansion_tb.cpp`, require the vacant machine's graphics frame and CPU progress to match the existing board case; require responder reads at `0x2000` and an unclaimed I/O port to return known bytes; drive an illegal claim at `0x0000`, `0x6000`, `0x8000`, `0xbe` and controller ports and require the console values; assert WAIT for two CPU enables and require one memory/VDP write; assert INT and observe the TV80 interrupt acknowledgement. Start the tests with a failing claim/WAIT/INT assertion against the current hardwired `WAIT_n(1'b1)` / `INT_n(1'b1)` machine.
- [ ] **Step 2: Run the focused failing case.** Run `make -C sources/misteross sim-fes-coleco-expansion`; expected failure is the missing Make target or the new responder assertions, before any RTL change is treated as passed.
- [ ] **Step 3: Add the minimal edge.** Define the packed widths above. Give the shell response register active-high outputs so a vacant socket's zero bits mean data unclaimed, no WAIT and no INT. Wire TV80 as `.WAIT_n(~bus_wait)` and `.INT_n(~bus_int)`. Mask `bus_claim` with `cpu_mem_read && cpu_addr >= 16'h2000 && cpu_addr <= 16'h5fff`, or with `cpu_io_read && !console_io_select`; use that masked claim only for `cpu_din`. Keep current console decode and NMI behavior. Feed request bits from actual TV80 signals. Register request and response at the socket boundary as the ZX81 pattern does; use a clock-enable/timing proof in the simulation so one sys-clock boundary latency fits within a CPU phase.

```systemverilog
wire bus_claim_allowed =
    (cpu_mem_read && cpu_addr >= 16'h2000 && cpu_addr <= 16'h5fff) ||
    (cpu_io_read && !console_io_select);
wire bus_read_selected = bus_claim && bus_claim_allowed;
// In the existing cpu_din mux, select bus_rdata only when bus_read_selected.
// Keep .NMI_n(vdp_irq_n); set .WAIT_n(~bus_wait), .INT_n(~bus_int).
```
- [ ] **Step 4: Wire the simulation target.** Add `sim-fes-coleco-expansion` beside the existing Coleco targets, compiling the new harness, socket, real machine, TV80, VDP and RAM sources with `FES_COLECO_OSS=1`. Add it to the `sim-fes-coleco` aggregate only after the focused test passes. Ensure existing board harnesses tie the new response to zero when they instantiate the machine directly.
- [ ] **Step 5: Verify both modes.** Run `make -C sources/misteross sim-fes-coleco-expansion` and `make -C sources/misteross sim-fes-coleco-oss`. Expected: all checks pass, including no behavioral change with zero response. Update the Coleco README and architecture to describe the proposed CPU socket as a development lane, not a shipped package.

### Task 2: Route the empty shell and prove its timing

**Files:**
- Create: `sources/misteross/scripts/coleco_expansion.py`, `sources/misteross/tests/test_coleco_expansion.py`.
- Modify: `sources/misteross/scripts/build_fes_coleco_oss.py`, `sources/misteross/cores/fes-coleco/constraints-oss.qsf`, `sources/misteross/cores/fes-coleco/README.md`, `sources/misteross/docs/architecture.md`.

**Interfaces:** The build helper produces a deterministic socket QSF, checks every retained boundary FF's type/BEL/clock and packed order in `synth.json`, and exposes one fixed, half-open CRAM rectangle to the later module build. A passing route can seal a format-2 development candidate declaring optional `fes.expansion.coleco-bus` 1.0; it does not update the selected factory package.

- [ ] **Step 1: Write failing producer tests.** Unit tests must reject a missing request FF, changed BEL, wrong system clock, changed request width, wrong socket rectangle and stale `synth.json`. Test that the generated QSF contains exactly one `FES_RESERVED_RECT` assignment and the producer's pinned functional input list includes the socket RTL, packing header and helper.
- [ ] **Step 2: Run those tests to failure.** Run `python3 -m unittest discover -s sources/misteross/tests -p 'test_coleco_expansion.py' -v`; expected: failure because the helper or socket input closure is absent.
- [ ] **Step 3: Implement the authenticated shell route.** Follow `scripts/zx81_expansion.py` and `build_fes_zx81_oss.py`: add a Coleco-specific helper that checks named FFs and one `clk_sys` bit, writes `socket.qsf` with the reserved rectangle, and canonicalizes FF names before routing. Extend `RTL_SOURCES` and `PINNED_INPUTS` in `build_fes_coleco_oss.py`; keep `toolchains/registered-memory.lock`, the HIP requirement, and the existing 52.224/74.25/12.288 MHz timing gates. Include the socket contract and rectangle in the functional build record so a geometry change produces a new BUILD_ID. Declare optional `fes.expansion.coleco-bus` 1.0 only in the newly sealed development package manifest; do not alter the old selected package. A failed route must publish no package.

```python
def shell_qsf(base: str) -> str:
    return base.rstrip() + f'\nset_global_assignment -name FES_RESERVED_RECT "{SOCKET_RECT}"\n'
```
- [ ] **Step 4: Run the actual feasibility gate.** Run `make dev-snapshot` from the feature worktree, enter its `sources/misteross` directory, and run `python3 scripts/build_fes_coleco_oss.py --root . --cache-root /home/deano/fes/out/cache/misteross-toolchains`. The FES `core-dev prepare` entrypoint rejects development snapshots, so invoke the authenticated component recipe directly for this development-only feasibility check. A pass requires a complete authenticated route, no unrouted nets, all three existing clock gates, all boundary FFs at their specified BELs, and a reserved rectangle containing no shell-owned cells except its boundary. Retain the snapshot and record compiler identities, rectangle, development package ID and shell RBF digest in ignored `out/` evidence. If those gates cannot be met, stop this plan and report the measured blocker; do not loosen timing or publish an unqualified shell.
- [ ] **Step 5: Repeat unit and source checks.** Run the focused Python tests, `make -C sources/misteross sim-fes-coleco-oss`, and `git diff --check`. Expected: pass on the final exact source. A full FES image is not evidence for uncommitted bytes.

### Task 3: Build a diagnostic module and prove the frozen rectangle

**Files:**
- Create: `sources/misteross/cores/fes-coleco/expansions/diagnostic.v`, `sources/misteross/scripts/build_coleco_bus_diagnostic.py`, `sources/misteross/tests/test_coleco_bus_diagnostic.py`.
- Modify: `sources/misteross/Makefile`, `sources/misteross/cores/fes-coleco/README.md`, `sources/misteross/docs/architecture.md`.

**Interfaces:** The module input is the exact packed 31-bit request; output is the exact packed 11-bit response. The builder takes `--shell` pointing at the Task 2 frozen shell and emits an archive of canonical `manifest.json` plus `cart.rbf`, bound to `fes.expansion.coleco-bus` 1.0 and its exact shell package/BUILD_ID/RBF digest. The rectangle and map name come from the Task 2 shell contract, never from user input.

- [ ] **Step 1: Write failing module and builder tests.** The module must expose a deterministic read/write register in `0x2000–0x5fff`, one unclaimed I/O status port, reset behavior and a controllable WAIT/INT diagnostic. The builder tests must reject a wrong shell hash, wrong slot/map/version, cart cells outside the reserved rectangle, and any non-CRC RBF bit change outside that rectangle. A valid module must produce byte-identical archive bytes on repeated builds with the same source/tool inputs.
- [ ] **Step 2: Run tests to failure.** Run `python3 -m unittest discover -s sources/misteross/tests -p 'test_coleco_bus_diagnostic.py' -v` and `make -C sources/misteross sim-fes-coleco-expansion`; expected: diagnostic module/builder cases fail before implementation.
- [ ] **Step 3: Implement the independent module recipe.** Follow `scripts/build_zx81_bus_validation_cart.py` for exact-shell authentication, frozen placement, CRAM-diff proof and canonical archive. Use Coleco's registered-memory lock and the Task 2 shell QSF/SDC, but a distinct `fes.coleco-bus.socket/1` map and `fes.expansion.coleco-bus` slot. Route no shell logic in the module. Its manifest binds exact shell identities, source revision, recipe digest, device, map, and module digest.

```python
manifest = {
    'format': 1,
    'slot': 'fes.expansion.coleco-bus',
    'slot_major': 1,
    'slot_minor': 0,
    'map': 'fes.coleco-bus.socket/1',
    'device': '5CSEBA6U23I7',
    'shell_package_id': shell_package_id,
    'shell_build_id': shell_build_id,
    'shell_sha256': shell_sha256,
    'cart_sha256': cart_sha256,
    'cart_size': cart_size,
    'recipe_sha256': recipe_sha256,
    'revision': revision,
}
```
- [ ] **Step 4: Run functional and physical build checks.** Pass the focused simulation and unit suite, then build the diagnostic module twice against the exact Task 2 shell. Require matching archive/module digests, placement/timing pass, and a CRAM diff confined to the socket. Run `git diff --check` and document the resulting development-only artifact identities.

## Handoff gate

Stop after Task 3 with a reviewable uncommitted diff and exact simulated/compiled evidence. This plan does not change FogCast, runtime, mister-packages, the library API or the factory image. Once the empty shell and module route are proven, write the dependent integration plan using the measured Coleco socket geometry and exact package/archive identities: generalize the Go linker policy, extend FogCast and runtime admission, then run the private-host and designated-kit sequence in the approved spec. Do not claim end-to-end expansion support from this feasibility result.
