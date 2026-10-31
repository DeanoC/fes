# DE25 standalone display builds — 2026-10-28

Both standalone FPGA-only diagnostics compile for `A5EB013BB23BE4SCS` with
Quartus Pro 26.3.0 Build 114. This is host-only evidence; the board is in
transit. No hardware, kit, block device, QSPI or harness was touched.

Base: `e300dcdcf6f20cdcf0f36013900eb750a4217ed7` (board parent #1176).
Both final artifacts select functional producer
`a00b0eebb362fd6594a28364e9b83263174b5db8`. Later evidence changes do not
relabel that producer. The worktree is
`/home/deano/.codex/worktrees/c134/fes/out/dev/de25-idle-menu`.

The [recipe](../../../boards/de25nano/display/README.md) builds `idle` from
`fes_splash_core`, and `menu-pattern` from the actual menu reader, synchronous
FIFO and video/sequence path with its local responder. There is no HPS,
LPDDR4 controller, GP ABI, host framebuffer upload, blitter or runtime menu
control. Default DE10 sources, producers, packages and image selections are
unchanged. These SOFs do not implement runtime-presentable `fes.menu`.

| Final mode | ALMs | Registers | Block RAM | DSP/HPS | Full compile |
| --- | ---: | ---: | --- | --- | --- |
| idle | 216 | 136 | none | 0 / 0 | 0 errors, 14 retained warnings |
| menu-pattern | 446 | 667 | 3 M20K, 24,576 optimized bits | 0 / 0 | 0 errors, 14 retained warnings |

Both use one PLL and all 36 selected physical pins match the pinned SCS
Golden_top locations and IO standards. The original reader declares a
128-bit by 256-word FIFO. For this target only, a separately retained derived
copy changes its guarded `M10K` RAM-style hint to `M20K`; no RTL logic changes.
Synthesis removes unused alpha bytes and maps a 256 by 96-bit simple dual-port
FIFO, with registered input/address behavior and no MLAB. The original Git
reader and compiled derivative have separate recorded hashes.

Artifacts remain under the worktree:

- Idle: `out/de25-display-idle-a00b0eebb3/project/output_files/de25nano_display.sof`,
  SHA-256 `66dcdb3bf831197f0e604c52cc2d30f2ef3072cb49f3bc680b828e942aeefa4a`.
- Menu pattern: `out/de25-display-menu-pattern-a00b0eebb3/project/output_files/de25nano_display.sof`,
  SHA-256 `f93b62820d8f62b74e4c328f67730666ed73e6253b48193a7e4c0c8916aba72b`.

[Idle receipts](idle/build.json) and [menu receipts](menu-pattern/build.json)
bind the exact source revision, tool/argv, SOF, input record and every retained
compiler report/generated SDC hash. Source-preservation records verify all ten
copied original FES inputs against that commit and separately verify the
adapted reader and unchanged vendor I2C bytes. The complete ZIP is locked and
extraction consumes the same captured bytes that were hashed. Only guarded
SCS device metadata corrections are applied to vendor PLL/Reset Release IP;
full original functional parameter maps, including additions, are checked
around generation and compilation.

All seven clock identities/types/periods match between final builds: the PLL
retains 50 MHz reference and 74.25 MHz pixel output. The initializer's exact
counter derives `adv7513_config` at 100,040 ns (divide by 5002), and neither
final design has an unconstrained internal clock. Each has 15 constrained STA
rows with zero TNS and nonnegative slack:

| Final mode | Worst setup ns | Hold ns | Recovery ns | Removal ns | Pulse width ns |
| --- | ---: | ---: | ---: | ---: | ---: |
| idle | 11.752 | 0.104 | 18.371 | 0.052 | 5.858 |
| menu-pattern | 10.231 | 0.090 | 18.254 | 0.009 | 5.858 |

These rows do not cover the external HDMI/I2C contract. Design Closure remains
**Fail**, while constrained Timing Closure passes. Both final designs retain
exactly two failed Design Assistant rules, with no waivers:

- TMC-20012: 29 missing output delays for RGB/sync/DE and I2C outputs.
- TMC-20011: two missing input delays for `HDMI_I2C_SDA` and `HDMI_TX_INT`.

Coverage reports contain two unconstrained input ports and 30 output ports;
the forwarded clock output adds one beyond the 29 Design Assistant outputs.
CLK/CDC/RES/RDC rules have zero failures in these fully clocked final models.
The two detected reset synchronizer chains each have two registers, but both
are excluded from MTBF analysis: design MTBF is **Not Calculated**.

The SDC cuts only the four asynchronous clear ingress pins. A separate
completed-database check verifies that both stage-0 to stage-1 data paths
remain present and timed. Each query returns one path in one slow model:

| Final mode | 50 MHz config setup/hold ns | 74.25 MHz pixel setup/hold ns |
| --- | --- | --- |
| idle | 19.578 / 0.108 | 12.998 / 0.110 |
| menu-pattern | 19.572 / 0.112 | 12.803 / 0.196 |

The [idle query receipt](idle/reset-path-audit.json) and
[menu query receipt](menu-pattern/reset-path-audit.json) bind query/report
hashes and confirm that all original compiled reports remain unchanged.
These are narrow mapped-path diagnostics, not physical reset/MTBF acceptance.

The final committed-source affected run passes 23 and 31 planner tests,
870 parent tests (one skipped), 1,409 FPGA software tests (35 skipped), and
consistency. `make check` confirms 23 generated consumers, 37 fixture copies
and zero source pins. The 14 focused display tests include the 13 preparation
and guard tests plus both actual wrapper/video/reset simulations. The latter
check 720p dimensions, logo/motion, 123-frame pattern slot switching without
underflow, silent audio and asynchronous configuration/PLL blanking/relock.
The menu simulation uses the same derived reader as the compiler.
[Software execution record](software-checks.json) identifies final producer;
[retained log hashes](software-log-sha256.json) bind complete local logs.

Two earlier candidates are preserved separately:

- [Initial idle 53a44708e2](initial-idle-53/build.json) compiled with an
  unconstrained vendor I2C clock. Its SOF is
  `bb4a9cd9cde89368ff6a586fad82bd8946533d72be15948b5ea63e65284eb06c`.
  The final recipe adds the source-derived 5002-divider clock definition.
- [Menu c436951365 failure](failed-menu-c436/failure.json) stops at synthesis
  because Agilex rejects the original M10K hint. It produces no SOF; its
  compiler log and inputs remain under the named private output.

HDMI outputs terminate at the clocked ADV7513 receiver. The pinned reference
has no complete receiver/board flight-time/setup/hold budget, so the recipe
adds neither output-delay guesses nor output false paths. Internal positive
STA does not qualify the combinational RGB output paths or the physical link.
There is also a vendor documentation discrepancy: all checked QSFs use
`1.1-V` for HDMI clock DJ24 while revision-B manual table 3-13 says 3.3 V.
No board schematic/netlist in the package establishes the rail/translation
contract; the recipe faithfully retains the SCS QSF pending electrical proof.

Physical acceptance requires an identified compatible SCS board, confirmed
clock/pin electrical behavior, actual ADV7513 I2C configuration and an HDMI
capture of the exact idle/menu-pattern artifacts. PLL lock, reset/relock,
reconfiguration and sustained scanout still need board checks. These FPGA-only
SOFs do not preserve the BSP's HPS/LPDDR4 design and are not a validated pair
with factory QSPI, bridge-enable U-Boot, DTB, Linux framebuffer or `/idle.rbf`.
Runtime menu work next needs the Agilex HPS/LPDDR4 allocation/transport contract,
then the real DDR reader, menu control/blitter and runtime admission path.
