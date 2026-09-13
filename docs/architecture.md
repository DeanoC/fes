# Current build architecture

misteross turns small experiment RTL into local MiSTer RBF artifacts. Network
deployment and target lifecycle are outside this repository.

## Build lanes

```text
experiment RTL + constraints
  |-- sim ----> Verilator result
  |-- oss ----> Yosys -> nextpnr-mistral/Mistral -> top.rbf
  `-- oracle -> Quartus Prime Lite 17.0.2 -------> top.rbf

oss manifest + oracle manifest -> compare report
```

`sim` checks the experiment's logical behavior with Verilator. Simulation jobs,
production source lists, and OSS synthesis flags come from the closed experiment
policy. Simulation-only models never enter either synthesis lane.

`oss` uses only the pinned repository-local tools described by
`toolchain.lock` (core recipes may select a tracked core-local compatibility
lock and isolated toolchain root). nextpnr writes a compressed Cyclone V RBF
(`--compress-rbf`) so the FPGA manager can reach CONF_DONE. Generated
sources and tools live under `build/toolchain/`. Build output lives under
`build/oss/<experiment>/`.

`oracle` uses an explicitly configured Quartus Prime Lite 17.0.2 installation.
It uses the same production RTL and timing intent as the OSS lane. Output lives
under `build/oracle/<experiment>/`. Quartus is not an OSS or simulation
dependency.

`compare` reads the two lane manifests and writes its result under
`build/compare/<experiment>/`. Differences between compiler-produced RBF bytes
are expected; the comparison focuses on target, sources, resources, timing,
and successful artifact production.

## Experiments

`010_blinky` is a 50 MHz counter driving one LED. It is the smallest physical
output test.

`020_linux_mailbox` uses one Cyclone V HPS general-purpose interface to return
the constant message `OSS FPGA OK\n`. It has no external FPGA output. The
simulation substitutes a small HPS model; both synthesis lanes use the real
HPS primitive boundary.

`030_m10k_rom` walks an initialized 256-byte table on the 50 MHz clock and
drives one LED from stored bit 0. OSS synthesis maps the table to exactly one
M10K. PLL, DSP, MLAB, and HPS remain forbidden.

`040_mlab_ram` is a 32-by-8 writeable table on the HPS general-purpose
interface. Linux peeks and pokes GPO/GPI; there is no LED. The table is
marked `ramstyle = "mlab"`. Yosys maps it to eight `MISTRAL_MLAB` cells.
nextpnr packs those into LABs and does not report an MLAB utilization key,
so the closed policy counts the Yosys cells and requires the HPS primitive
in the route report. Quartus maps the same table to 256 MLAB bits and zero
M10K. PLL, DSP, and M10K remain forbidden.

`050_lut_mul` is an eight-by-eight unsigned product on the HPS
general-purpose interface. Linux peeks and pokes GPO/GPI; there is no LED.
The product is marked `multstyle = "logic"` so both lanes keep it in ALMs.
OSS synthesis keeps `-nodsp`; Yosys must not emit `MISTRAL_MUL*` cells.
Quartus must measure zero DSP blocks. PLL, M10K, and MLAB remain forbidden.

`060_dsp_mul` is an eight-by-eight unsigned product on the HPS
general-purpose interface. Linux peeks and pokes GPO/GPI; there is no LED.
The product is marked `multstyle = "dsp"`. OSS synthesis drops `-nodsp` and
emits one `MISTRAL_MUL9X9`. nextpnr-mistral places that cell on one DSP BEL.
Quartus maps the same product to one DSP block. PLL, M10K, and MLAB remain
forbidden.

`070_mixed_mem` is a 32-by-8 lab table and a 256-by-8 block table on the HPS
general-purpose interface. Linux peeks and pokes GPO/GPI; there is no LED.
The lab table is marked `ramstyle = "mlab"` and the block table
`ramstyle = "M10K"`. Yosys maps those to eight `MISTRAL_MLAB` cells and one
`MISTRAL_M10K`. Quartus must measure 256 MLAB bits and one RAM block. PLL
and DSP remain forbidden.

`080_dsp_mem` is an eight-by-eight unsigned DSP product plus those same lab
and block tables on the HPS general-purpose interface. Linux peeks and pokes
GPO/GPI; there is no LED. Yosys maps one `MISTRAL_MUL9X9`, eight `MISTRAL_MLAB`
cells, and one `MISTRAL_M10K`. Quartus must measure one DSP block, 256 MLAB
bits, and one RAM block. PLL remains forbidden.

`090_pll_clock` uses one `altera_pll` and one HPS GP interface. The input is
50 MHz on PIN_V11 and the single output is 25 MHz, direct mode, zero phase,
50% duty, integer mode, with reset tied low. The closed OSS policy requires
both `meter.refclk` at 50 MHz and `clk25` at 25 MHz to meet timing. Memory and
DSP usage are forbidden. The nextpnr fork baseline
`9d9a027d401e98a5b611ccc8a0c17a5e6b04e8cc` merged the supported fixed PLL
profile on top of the existing DSP implementation.

The PLL experiment divides the output by 256 and counts synchronized rising
edges over 2^20 reference cycles. GPI signature `0xD711` identifies the
measurement protocol; the expected result is 2048 ±1. A request toggle starts
one measurement, and its result remains stable for two byte reads. Simulation
covers the HPS protocol and separately checks stopped/wrong clocks and sampled
lock loss. The simulation PLL models only the fixed digital ratio, not analog
lock acquisition. See `experiments/090_pll_clock/expected.md` for the register
layout and manual probe. The experiment currently has simulation and OSS
lanes; it has no Quartus comparison lane. This profile does not establish
reset/relock, other frequencies, phase shifts, or jitter behavior.

The OSS `090_pll_clock` artifact has SHA-256
`2d5be08a315dd7e5e9f620954e588e40340dbcfff7c735da707c3f68b8eb0bcb`
and size 1,955,735 bytes. Its reported reference/output Fmax values are
218.866/341.064 MHz against 50/25 MHz constraints. Exact-artifact kit diagnostics on 2026-09-06
returned 2048 on three successive measurements with lock asserted and no
sampled lock loss. The current `kit.py stop` completed development reboot
recovery and returned a free lease. This is hardware diagnostic acceptance of
the fixed profile, not native game acceptance.

`100_dsp_rom` is an eight-by-eight unsigned DSP product of an HPS operand and
one byte from an initialized 256-by-8 block table. Linux peeks and pokes
GPO/GPI; there is no LED. The table is marked `ramstyle = "M10K"` and holds
`index XOR 8'hA5`. Yosys maps one `MISTRAL_MUL9X9` and one `MISTRAL_M10K`.
Quartus must measure one DSP block and one RAM block. PLL and MLAB remain
forbidden.


`110_pll_reset` extends the fixed PLL measurement with active-high fabric reset.
The reset baseline nextpnr revision `6abe1e9a0ef7673f5f840d0b1168f005a268fcbd` introduced the
existing Mistral `NRESET0` endpoint. Its routed reset uses inverter bit0;
folded-low reset retains bit1, matching the Quartus17 oracle. Mistral tables
and pin stay unchanged. The experiment still requires one PLL, one HPS GP,
no DSP or memory, and passing 50/25 MHz timing.

GPI signature `0xD712` identifies this protocol. GPO bit2 requests reset through
two reference-domain registers, and GPI bit11 echoes the applied reset. The
reference clock and measurement handshake continue during reset. The hardware
probe performs ten cycles, requiring reset echo=1, lock=0 and count=0 while held,
then echo=0, lock=1 and count=2048 ±1 after release. Simulation checks the same
sequence using a digital PLL model with a nominal acquisition delay, plus the
existing meter fault tests. Neither simulation nor synchronous Fmax specifies
analog lock time, minimum reset pulse width, recovery/removal, or jitter.
See `experiments/110_pll_reset/expected.md`. Simulation and OSS are supported;
Quartus comparison is not implemented for this experiment.

On 2026-09-06 the integrated `110_pll_reset` OSS RBF (1,955,689 bytes) has
SHA-256 `5e48f9643c85a94bafeaaec2076c002710b15a8eb2b811dd42f6809da34880a4`.
It is byte-identical to the nextpnr diagnostic artifact tested on the designated
kit: all ten cycles returned 0 while reset and 2048 after relock. The reference
and output Fmax values are 214.684/331.126 MHz against 50/25 MHz constraints.
The current `kit.py stop` completed development reboot recovery and left the
lease free. This is exact-artifact functional diagnostic acceptance; it does
not establish native game acceptance.

The current nextpnr pin `9cbbf7353dd2b818ab73031fcf30d9993578c783` is
merged PR #68 (`6535e945377283260431cc14d3f9700536588833` onto
`2d3c216afb7051d2e2070cbf678a50f274b3f786`). It borrows a live design
clock for read-only asynchronous M10Ks whose write clock is folded,
fans that clock to both `CLKIN` sinks, routes `ENABLE[0]`, and programs
`TOP_CE0_SEL` for native 1024x10 cells without `CFG_BYTE_ENABLE`. Pair
it with Yosys `ec34fcf3`. Mistral stays `b28e30a`.

That pin sits on `2d3c216afb7051d2e2070cbf678a50f274b3f786`,
merged PR #67 (`edabecce3a8759b641351aadbe1526d56c53f05a` onto
`9c75153384c57d913ec6c8720f941f13ca4ca904`). It packs equal-width
8192x1, 4096x2 and 2048x5 true-dual-port M10Ks, including scalar
one-bit data ports and the thirteenth address bit. Mixed-width TDP
stays at the characterized 10/20-bit modes. Default-router 870 RBF
bytes match the kit-tested feature tree. Pair it with Yosys
`ec34fcf3`. Mistral stays `b28e30a`.

That pin sits on `9c75153384c57d913ec6c8720f941f13ca4ca904`,
merged PR #66 (`bf37618f5ef232525989c3b967f9ce83673c914f` onto
`8bd4875400b49c97a668013106b481c3b6d7e17b`). It adds the GPU connection
router `--router gpu` that the FES ZX81 OSS recipe now uses; the 870 ladder
uses the default router.

That pin sits on `8bd4875400b49c97a668013106b481c3b6d7e17b`,
merged PR #65 (`7ec1fa57bdf00dc14a3d4606170727a45b34e070` onto
`a3e9b19a00c6e49b9dc286610521bea24ffd6f18`). It audits every packed
M10K clock selector at design scale: live `CLK1`/`CLK2` to `CLKIN.0`/
`.1`, independent-clock `BOT_*` settings, unique sites, and rejection
of packed constant clocks. Pair it with Yosys `ec34fcf3`. Mistral stays
`b28e30a`.

That pin sits on `a3e9b19a00c6e49b9dc286610521bea24ffd6f18`,
merged PR #64 (`ecdaa4acb75407bf30fc0db4afcffd273443ebb3` onto
`47c4251acc89eb9bf6742e32204af744a23446e0`). It translates QSF
`HPS_LOCATION` assignments for `cyclonev_hps_interface_peripheral_i2c`
into BEL constraints, including
`HPSINTERFACEPERIPHERALI2C_X52_Y60_N111` to
`cyclonev_hps_interface_peripheral_i2c.52.60.0`. Pair it with Yosys
`ec34fcf3`. Mistral stays `b28e30a`.

That pin sits on `47c4251acc89eb9bf6742e32204af744a23446e0`,
merged PR #63 (`f9be23d3d95ce207a197e28455fd1f66b6df8460` onto
`914200556be0d83ebc0f74efde400ff00d98cc70`). It makes TDP
read-during-write contracts explicit: `CFG_RDW_MODE_A`/`CFG_RDW_MODE_B`
accept `NEW_DATA_NO_NBE_READ` or `DONT_CARE`, `CFG_RDW_MODE_MIXED`
accepts only `DONT_CARE`, and unsupported modes are rejected. Physical
settings remain `TRUE_DUAL_PORT` with A/B flow-through. Pair it with
Yosys `ec34fcf3`. Mistral stays `b28e30a`.

That pin sits on `914200556be0d83ebc0f74efde400ff00d98cc70`,
merged PR #62 (`23421df80037a522b9315729c9328e0e89a935d4` onto
`5909feb560da457c55374eec226d1040c4dc8dba`). It calculates bounded
fractional-N PLL profiles from a 50 MHz reference when the reported VCO
is 400–500 MHz, including generic 27 MHz, 99 MHz, and 27/13.5 MHz dual
outputs, while retaining the hardware-checked 11.2896, 12.288, 74.25,
and 12.288/24.576 MHz compatibility words. Pair it with Yosys
`ec34fcf3`. Mistral stays `b28e30a`.

The current native asynchronous-M10K toolchain uses Yosys pin
`ec34fcf38986217af9b5558936044b7197d968a7` (merged DeanoC/yosys PR #13,
feature `540998e36adcb0ddecdbdf39eed6df8e7551732d`) and nextpnr pin
`5909feb560da457c55374eec226d1040c4dc8dba` (merged DeanoC/nextpnr
PR #61, feature `5f6ba158c7f45e689b60e5590f5323573cb59f2f`), with
Mistral unchanged at `b28e30a36b5139aaed5a5d361a30b542e6b7c758`. Yosys
infers simple-dual flow-through M10Ks at the native 10/20/40-bit
geometries and true-dual flow-through M10Ks for two-write/two-read
memories. nextpnr routes the constant-high read enable, accepts
read-only cells whose write clock folds to a constant, and models
address-to-data timing for both TDP outputs. The host regressions and
the ZX81 native netlist use this pair. Exact-artifact kit diagnostics
on 2026-09-12 loaded native-Yosys `820_m10k_async_enable` (GPI
`0xD42F00A6`, INIT/write/neighbour) and `770_m10k_async_read` (GPI
`0xD42B00A6`, INIT/write/neighbour) at `MISTRAL_M10K.26.1.0` with
routed `ENABLE[0]`. Load JSON timed out; GPI and probe still passed.
`stop` recovered and left the lease free.

The earlier Yosys pin `da6373c0d7565f36036051efc7895fb0d9ac13c3` is
merged PR #12 (`11df3d330c0eb4c312bfc7659b05dd4211ffaaa2` onto
`758968907c116f685f586e0ce8186bae0f8b448c`). It infers a zero-valued
asynchronous read-output reset on `(* ramstyle = "M10K" *)` SDP onto
`ACLR1`, with `ACLR0` tied low. Nonzero reset values remain fabric.
Pair it with nextpnr `88cda8ae`.

That pin sits on `758968907c116f685f586e0ce8186bae0f8b448c`,
merged PR #11 (`74d285754bfbe78005cc4a65c7f849d3b8cf403f` onto
`ae5db2a9cd2c0bd1ebc73fada70d6a76f1b908d1`). That pin exposes active-high
`ACLR0`/`ACLR1` on `MISTRAL_M10K` and `MISTRAL_M10K_TDP` so an explicit
primitive can drive the Mistral clear inputs. Pair that ACLR-port baseline
with nextpnr `88cda8ae`.

That pin sits on `fca8ca0a5354e52ce0e158bc6e1eed481e590ed8`, which adds
the HPS peripheral I2C primitive on `10891a9e0256a0eac70c329aa64c633902fc6bc6`.
That baseline includes merged PR #7: Intel ALM infers mixed-width Cyclone V M10K
true dual-port RAM through `ram_style="m10k_tdp_mixed"` as `MISTRAL_M10K_TDP`
with `CFG_MIXED_WIDTH=1` (physical 20/10 and 10/20, padded 16/8 and 8/16),
including merged PR #6 byte-masked TDP, merged PR #5 unmasked TDP, merged PR #4
mixed-width SDP, merged PR #3 20-bit byte enables, merged PR #2 independent
CLK1/CLK2 and merged PR #1 initialized MLAB. The CMake base is
YosysHQ `13b43f8c85ec430a33ee55d058fb4c32b42b6910`. Pair that I2C baseline
with nextpnr `88cda8ae`.

The current nextpnr pin `517eb7c6b838dee5b0072b1551f9c8e914331102` is
merged PR #60 (`3d0c25aeb7d41706acb6193d4ce4fe9c3ed25d16` onto
`0523e0c68e4a6ee8cfc7f324f3ed641f67fe52af`). It materializes an omitted
async `B1EN` as a constant-high `ENABLE[0]` route and selects the bottom
core/input clock path for that enable. Pair it with Yosys `da6373c0`.
Locked Yosys still emits `clocks 1 1`, so OSS sets `CFG_ASYNC_READ` and
drops `B1EN`/`CLK2` after synthesis.

That pin sits on `0523e0c68e4a6ee8cfc7f324f3ed641f67fe52af`,
merged PR #59 (`2324c164789777bc629664a3bddc96a306d78a48` onto
`fd862a2c59db7f0406e32831f2e57b3cfe034251`). It matches initialized
async M10K defaults: omitted `B1EN` is preserved, `ENABLE[0]` is not
materialized, `BOT_CORECLK_SEL`/`BOT_INCLK_SEL` stay at their async
defaults, and constant-high `A1BE` omits `BYTEENABLEA` routes. Pair it
with Yosys `da6373c0`. Locked Yosys still emits `clocks 1 1`, so OSS
sets `CFG_ASYNC_READ` and drops `B1EN`/`CLK2` after synthesis.

That pin sits on `fd862a2c59db7f0406e32831f2e57b3cfe034251`,
merged PR #58 (`39e307889ff77bbea6115bba5c36db953e565649` onto
`74aab451fc767996e1c6195531a86d36c14b82c9`). It maps `CFG_OUT_REG_A`
and `CFG_OUT_REG_B` onto M10K `A_OUTPUT_SEL`/`B_OUTPUT_SEL`. Pair it
with Yosys `da6373c0`. Locked Yosys has no output-register parameter, so
OSS sets `CFG_OUT_REG_B` after synthesis.

That pin sits on `74aab451fc767996e1c6195531a86d36c14b82c9`,
merged PR #57 (`83450455be223249683699be0f200aade4622d1f` onto
`1e1745dcb40f9722d5b74389c1b411c56a27c8a8`). It maps connected
`ADDRSTALLA`/`ADDRSTALLB` ports to the existing M10K GOUT BEL pins.
Pair it with Yosys `da6373c0`. Locked Yosys has no stall ports, so OSS
attaches GPO[29] to `ADDRSTALLA` after synthesis.

That pin sits on `1e1745dcb40f9722d5b74389c1b411c56a27c8a8`,
merged PR #56 (`f854e966d690a6d54409425ee20ed55cdee046ca` onto
`c528c2389b2d4381ed2e3d24332bc6c9d8e2daaa`). It accepts the Quartus
SDC/QSF subset used by ZX81: `get_clocks`, `derive_pll_clocks`,
`derive_clock_uncertainty`, multiline `set_clock_groups`, and `-entity`
on `set_instance_assignment`. PLL clocks still come from packed
`altera_pll` cells. Pair it with Yosys `da6373c0`.

That pin sits on `c528c2389b2d4381ed2e3d24332bc6c9d8e2daaa`,
merged PR #55 (`adc288243ad9139403e53ae96d9783e90515b998` onto
`4d055daef276840c58fafc723bf189882b9e5d21`). It adds asynchronous M10K
read ports: `CFG_ASYNC_READ` and an absent `B1EN` keep `B1ADDR`/`B1DATA`
combinational and reject active ACLR. The packer ties the omitted read
enable to `$PACKER_VCC_NET` on `ENABLE[0]`, fans `CLK1` to both `CLKIN`
sinks, and uses Quartus-compatible bottom clock selectors. Pair it with
Yosys `da6373c0`. Locked Yosys still emits `clocks 1 1`, so OSS rewrites
the primitive JSON after synthesis.

That pin sits on `4d055daef276840c58fafc723bf189882b9e5d21`,
merged PR #54 (`8bbd94146826b32decb9df516243d3471bfc12a7` onto
`d22eaef1a858e2d81bcbe971b580f69265e59bde`). It adds a generic 520 MHz
PLL feedback profile so 52 MHz and the other exact 520 MHz divisors are
C counters on a Quartus-checked analog tuple. Pair it with Yosys
`da6373c0`.

That pin sits on `d22eaef1a858e2d81bcbe971b580f69265e59bde`,
merged PR #53 (`c43136fe6688ff36e2e104327f37faa09450766e` onto
`9cdc03cc8521317b729f986c65448733e4aa0422`). It adds native 18x19 DSP
views: `MISTRAL_MUL18X19` dual products and `MISTRAL_MUL18X19_COMBINED`
38-bit add/sub. Pair it with Yosys `da6373c0`. Locked Yosys has no 18x19
cell, so OSS maps a keep blackbox after synthesis.

That pin sits on `9cdc03cc8521317b729f986c65448733e4aa0422`,
merged PR #52 (`55f17b34dfa1ca6f87f1b70ed9442ee2e4ead7c7` onto
`d8a96b581e608736ea346c34a2a0ce8161d2e1ab`). That pin's default router2 retries
ordinary nets with router1 when a design has two `altera_pll` cells, one
`MISTRAL_M10K`, and less than 10% timing margin. Pair it with Yosys
`da6373c0`.

That pin sits on `d8a96b581e608736ea346c34a2a0ce8161d2e1ab`,
merged PR #51 (`9684edd8238f4538d77be2cae5391183c87e6131` stacked onto
`88cda8aeedf1d6cd48406844a5e8dced415c6ae5`). That pin folds a constant unused
M10K clock off the `CLKIN[1]` TCLK sink, maps only live clocks, and
preserves ACLR0/ACLR1 pin styles. Pair that TCLK-fold baseline with Yosys
`da6373c0`.

That pin sits on `88cda8aeedf1d6cd48406844a5e8dced415c6ae5`,
merged PR #49 (`71426e88e0c76de41f3cf06dd40b032dd8d1d467` onto
`d990fb2d92931e3ec1fc5d35e5ca1342558fa248`). That pin adds Cyclone V M10K
asynchronous clear: logical `ACLR0`/`ACLR1` map to physical `ACLR[0:1]`.
Omitted ports materialise as inactive `PIN_0`. Fabric or inverted
controls enable the matching output-clear register. Address-clear
enables stay disabled. No Mistral database tables change. It sits on
`d990fb2d92931e3ec1fc5d35e5ca1342558fa248`,
merged PR #46 (`fe411b3953d364a31493f7e6d457b0f7094695b1` stacked onto
`9020c5f43f085461bc2710da74e59c8a7bc32564`). That pin adds Cyclone V DDR
bidirectional I/O registers: a width-one `altddio_bidir` with connected
`datain_h`/`datain_l`, `dataout_h`/`dataout_l`, `combout`, shared clock
and OE packs into `MISTRAL_DDRBIDIR` on the existing GPIO/DQS16 site.
Missing data, inverted clocks, extra controls and used `oe_out` fail
closed. No Mistral database tables change. It sits on
`9020c5f43f085461bc2710da74e59c8a7bc32564`,
merged PR #48 (`d19c6aeec9b18216e611f1e3895441714a3884e0` onto
`10a8e8907f18e312b431e8303c4216f4501e6b3a`). That pin adds mixed-width Cyclone V
M10K byte enables: a 512×20 write port with two connected `A1BE` bits packs
onto existing `BYTEENABLEA` lanes together with a 1024×10 or 256×40 read
port. Missing or extra mask bits, non-20-bit writes, and mixed-width true
dual-port byte enables fail closed. Equal-width 512×20 byte enables keep
the ordinary SDP path. No Mistral database tables change. It sits on
`10a8e8907f18e312b431e8303c4216f4501e6b3a`,
merged PR #47 (`e64b6c33521c92c9460c07b43c1d396944895756` onto
`3e314db00a620caf3cbe09ecac93a4910df5f01a`). That pin adds
direct Cyclone V `altiobuf_in`, `altiobuf_out` and `altiobuf_bidir`
packing: each width-one primitive folds into the already constrained
`MISTRAL_IB`, `MISTRAL_OB` or `MISTRAL_IO` BEL. Bus hold and differential
mode stay disabled; the output profile requires `use_oe=FALSE`.
Bidirectional `dataio` must be the direct pad net of one `MISTRAL_IO`,
and `MISTRAL_IO.O` keeps the primitive's `dataout` users. Wider channels,
unsupported parameters and malformed pad topology fail closed. No Mistral
database tables change. It sits on `3e314db00a620caf3cbe09ecac93a4910df5f01a`,
merged PR #45 (`3eb10febf42cef0df8cdc62d21dd633211daad9d` onto
`9144784db9b4b85c1257be4d1943b9ac11ceb3e8`). That pin adds
fabric-data Cyclone V DDR output registers: a width-one `altddio_out`
with two connected nonconstant `datain_h`/`datain_l` nets packs into
`MISTRAL_DDROUT`. High/low data use GPIO `DATAOUT.1`/`DATAOUT.0`
(`D_H`/`D_L`) and the clock uses GPIO `CLKOUT.0`. Complementary constant
data still takes the existing clock-forwarding path. Mixed or missing
data, widths above one, dynamic controls, inverted output and used
`oe_out` fail closed. There is no characterized GPIO output-register
setup/hold or clock-to-pad model, so those arcs stay outside timing
analysis. It sits on `9144784db9b4b85c1257be4d1943b9ac11ceb3e8`,
merged PR #44 (`7a0447cb2c24d4d09ed4d96d1f692420b714441c` onto
`a1f8dbb5434fd88bc014ce059dd6b7ca74512693`). That pin adds
dedicated Cyclone V DDR input registers: a width-one `altddio_in` with
inactive controls packs into `MISTRAL_DDRIN`. High/low captures use GPIO
`DATAIN.3`/`DATAIN.2` and the clock uses GPIO `CLKIN.0`. Unsupported
widths, parameters, dynamic controls, inverted clocks, indirect data and
aliased outputs fail closed. There is no characterized GPIO input-register
setup/hold or clock-to-Q model, so those arcs stay outside timing analysis.
It sits on `a1f8dbb5434fd88bc014ce059dd6b7ca74512693`,
merged PR #43 (`298e7588ebaecf76245bf60d174048b0d23a0756` onto
`2c9f9c5cf06615affdefc8c346bbd7d26a47f6b6`). That pin adds
dedicated Cyclone V SDR input registers: `FAST_INPUT_REGISTER ON` absorbs
a directly connected `MISTRAL_FF` driven by a unidirectional `MISTRAL_IB`
into `MISTRAL_SDRIN`. The captured result is GPIO `DATAIN.3` and the clock
is GPIO `CLKIN.0`. The supported flop has constant `ENA=1`, inactive
`ACLR=1`, `SCLR=0`, `SLOAD=0`, exactly one consumer of the input-buffer
output and a non-inverted clock; unsupported controls, inverted clocks,
data fanout and parameters fail closed. `FAST_INPUT_REGISTER OFF` keeps
the fabric flop. There is no characterized GPIO input-register setup/hold
or clock-to-Q model, so those arcs stay outside timing analysis. It sits
on `2c9f9c5cf06615affdefc8c346bbd7d26a47f6b6`, merged PR #42
(`8c0c8e7db1a9b0ba101ae9fb26a38a450209e091` onto
`eefa26d3ec3151630ccd556824fd8ebfa9df4976`). That pin adds
dedicated Cyclone V SDR output registers: `FAST_OUTPUT_REGISTER ON` absorbs
a directly connected `MISTRAL_FF` into the constrained `MISTRAL_OB` as
`MISTRAL_SDROUT`. The supported flop has constant `ENA=1`, inactive `ACLR=1`,
`SCLR=0`, `SLOAD=0`, one Q consumer and a non-inverted clock; unsupported
controls, inverted clocks, fanout and parameters fail closed.
`FAST_OUTPUT_REGISTER OFF` keeps the fabric flop. There is no characterized
GPIO-register setup/hold or clock-to-pad model, so the packed data pin is an
unclocked timing endpoint. The DDR/Pong baseline `eefa26d3` merges
dedicated DDR clock forwarding from merged PR #40 with the four existing HPS
peripheral I2C sites, HDMI routing checks, and a GPIO input-buffer fix that
preserves external input on bidirectional pads. Width-one `altddio_out` with
complementary constant `datain_h`/`datain_l` packs into `MISTRAL_DDROUT` on
the existing GPIO BEL; fabric DDR data is rejected. It also routes explicit
constant-zero and constant-one `MISTRAL_FF.DATAIN` values through real fabric
sources: Cyclone V flip-flops have no hard constant data selector, so folding
a zero away could otherwise select unrelated co-packed logic after reset. The
merge parents are `9180a91a8c9412cc96238527c20f8e6d30ce0052` (PR #40) and
`5e31bf41f47c0b2403f77c6fb679ca306e9b2cd0`. It builds on
`ef294430c57b1d64c52f15129adcc6236ecbce01`. That baseline includes merged PR #39: a single 50→74.25 MHz fractional-N
output (`fractional_vco_multiplier="true"`, direct mode, 0 phase, 50% duty,
M=8 N=1 C6=6, K=`0xe8f5c239`, calculated 74,249,999.83243954 Hz). Integer
mode still rejects 74.25 MHz. That sits on merged PR #38 mixed-width true dual-port M10K packing
(`CFG_MIXED_WIDTH=1`, per-port 1024×10 or 512×20, `CFG_RD_ABITS`/`CFG_RD_DBITS`)
on merged PR #37 true dual-port M10K byte enables
(`CFG_BYTE_ENABLE=1`, `A1BE`/`B1BE` onto `BYTEENABLEA`/`BYTEENABLEB`) on merged PR
#36 unmasked TDP packing (`MISTRAL_M10K_TDP` into the existing M10K BEL with `CFG_TDP=1`) on merged PR
#35 `expandBoundingBox` / default router2, merged PR #34 mixed-width M10K packing on merged PR #33 20-bit M10K byte-enable packing, merged PR #32 dual-clock M10K, merged PR #31 MLAB INIT and merged PR #30 DSP modes: three-lane 9×9 packing (336 logical `MISTRAL_MUL9X9` BELs on 112
physical DSP blocks, RESULT `0:17` / `18:35` / `37:54` with a one-bit gap at
`RESULT.36`), `M18X18P36`, `M27X27`, M9 preadder subtract, M18 36-bit C addend
mapped on BX groups `{8,9,6,7}`, and DSP input/output registers. Each
`MISTRAL_MLAB` accepts a 32-bit numeric `INIT`; omitted bits stay zero. Omitted
NEGATE/SUB/ACCUMULATE/LOADCONST encode low; unused fabric ACLR stays low.
It retains PR #28 double-register clock enables, quarter-phase
100 MHz outputs (`0`/`2500`/`5000`/`7500` ps), 45° steps on
equal 50 MHz outputs, two independent `altera_pll` cells sharing V11, and
`cyclonev_clkena` packing into `MISTRAL_CLKENA` with low startup, running/gated
branches, `enaout` status, and `REG2_ENOUT`. 50 MHz 90°/270° shifts use
`CNT_PH_MUX_PRESET` as well as the counter preset. Compatible exact decimal
frequencies from 1 to 100 MHz still share one 300/320/400 MHz configuration
for the selected reference. The closed 50 MHz 25/50/100 MHz triple uses
C6=12, C7=6 and C5=3; the 25/50/100/75 MHz quad adds C8=4. Mixed 25/50/25
duties on 25/50/100 MHz force the 400 MHz configuration. A 25 MHz reference
with the same 25/50/100 MHz 50% triple uses M=24 N=2; a 100 MHz reference uses
M=6 N=2. Shifted 50 MHz and 100 MHz outputs still require a 50 MHz reference.
The second PLL site also requires 50 MHz. It also requires Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`, which corrects LAB/MLAB
`CLKx_INV` versus `CLKx_SEL` addresses so folded falling-edge FF clocks
actually toggle. An older Mistral library now fails before inverted-clock
RBF output. Fractional-N profiles still require 50% duty. The closed
`090_pll_clock` and `110_pll_reset` experiments retain their 25 MHz 50% output.
The 74.25 MHz single-output profile is accepted only with
`fractional_vco_multiplier="true"`.

Compiler fixtures under `mistral/tests/pll` in the nextpnr fork cover divider
selection, malformed frequencies, emitted configuration, meter simulation and
reset/relock. Quartus 17.0.2 oracles cover 20/40/80/100 MHz. On 2026-09-06 the
20/40/100 MHz OSS diagnostic artifacts each passed ten hardware cycles: zero
while reset, then 1638–1639 / 3276–3277 / 8192 after relock. Their reference and
output Fmax values were 216.732 / 326.584 MHz against 50 MHz and the selected
output constraint. Host pair regressions cover 25/40, 40/25, 20/100, 40/64,
80/80 and 1/1 MHz. Artifact hashes and reproduction commands are in the
[nextpnr PLL test documentation](https://github.com/DeanoC/nextpnr/blob/131f880a856ee7f4b9b6379aa9b2c3e7fb001fe5/mistral/tests/pll/README.md).

`120_pll_dsp` runs the eight-by-eight unsigned DSP product on the proven
50-to-25 MHz PLL output. Linux peeks and pokes GPO/GPI on the 50 MHz
reference domain; there is no LED. Operands and the selected product byte
cross with two-flop synchronizers. Yosys maps one `altera_pll` and one
`MISTRAL_MUL9X9`. Memory remains forbidden. The closed OSS policy requires
`host_port.FPGA_CLK1_50` at 50 MHz and `clk25` at 25 MHz. Reset stays tied
low; this experiment does not re-prove frequency ratio, reset/relock, or
analog lock. See `experiments/120_pll_dsp/expected.md`. Simulation and OSS
are supported; Quartus comparison is not implemented.

The OSS `120_pll_dsp` artifact has SHA-256
`f3253cd9bd4660e0094c249120f44e22b960e929b653a5c9a62f21481b17879b`
and size 1,956,437 bytes. Its reported reference/output Fmax values are
419.815/150.625 MHz against 50/25 MHz constraints. Utilization is one
`altera_pll`, one `MISTRAL_MUL9X9`, and one HPS GP. Exact-artifact kit
diagnostics on 2026-09-06 returned lock and the expected products
(`0x0A*0x0C=0x0078`, `0x12*0x34=0x03A8`, `0xFF*0xFF=0xFE01`) with GPI
signature `0xDC10`. The current `kit.py` close completed development reboot
recovery and left the lease free. This is exact-artifact functional
diagnostic acceptance; it does not establish native game acceptance.

`130_pll_dsp_40` is the same eight-by-eight unsigned DSP product on a 50-to-40
MHz integer PLL output from the current configurable pin. Linux peeks and pokes
GPO/GPI on the 50 MHz reference domain; there is no LED. Yosys maps one
`altera_pll` and one `MISTRAL_MUL9X9`. Memory remains forbidden. The closed OSS
policy requires `host_port.FPGA_CLK1_50` at 50 MHz and `clk40` at 40 MHz. Reset
stays tied low. See `experiments/130_pll_dsp_40/expected.md`. Simulation and OSS
are supported; Quartus comparison is not implemented.

The OSS `130_pll_dsp_40` artifact has SHA-256
`30eb112761e7ee5ea284a3087cf66057e1b4f559d48a6eed698f30a1aeae6edb`
and size 1,956,439 bytes. nextpnr selected 50→40 MHz with M=32 N=5 C6=8.
Its reported reference/output Fmax values are 419.815/150.625 MHz against
50/40 MHz constraints. Utilization is one `altera_pll`, one `MISTRAL_MUL9X9`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-06 returned lock
and the expected products (`0x0A*0x0C=0x0078`, `0x12*0x34=0x03A8`,
`0xFF*0xFF=0xFE01`) with GPI signature `0xDC40`. The current `kit.py` close
completed development reboot recovery and left the lease free. This is
exact-artifact functional diagnostic acceptance; it does not establish native
game acceptance.

`140_pll_dsp_20`, `150_pll_dsp_80`, and `160_pll_dsp_100` are the same DSP
product on the other integer PLL outputs that matter for cores: 20 MHz (Pong
game/video), 80 MHz, and 100 MHz (the maximum checked output). Each uses one
`altera_pll` and one `MISTRAL_MUL9X9`, HPS peek/poke on 50 MHz, and closed
timing for `host_port.FPGA_CLK1_50` plus `clk20` / `clk80` / `clk100`. GPI
signatures are `0xDC20`, `0xDC80`, and `0xDC64`. Simulation and OSS are
supported; Quartus comparison is not implemented.

The OSS artifacts are:

| Experiment | PLL | Size | SHA-256 | Fmax 50/out MHz |
| --- | --- | --- | --- | --- |
| `140_pll_dsp_20` | M=12 N=2 C6=15 | 1,956,437 | `77caf629c39a77c2532a418b7fffdcbdb712893f1781fd6c9cfb544aae843867` | 419.815/150.625 vs 50/20 |
| `150_pll_dsp_80` | M=32 N=5 C6=4 | 1,956,439 | `b37a1b186efa516d20addabc564a7739b6c8783bf7478eab9c6685aa587d4592` | 419.815/150.625 vs 50/80 |
| `160_pll_dsp_100` | M=12 N=2 C6=3 | 1,956,025 | `27be8deeb08466d6e591a0b3980afb410323b2544d83a294444a48408f2723d8` | 432.900/144.592 vs 50/100 |

Each uses one `altera_pll`, one `MISTRAL_MUL9X9`, and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-06 returned lock and the expected
products for all three signatures. The current `kit.py` close completed
development reboot recovery after each load and left the lease free.

`170_pll_dual` measures both outputs of the original 25/40 MHz pair through
HPS GP. GPI signature `0xD714` identifies the protocol; GPO bit3 selects the
40 MHz meter. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/170_pll_dual/expected.md`.

The OSS `170_pll_dual` artifact has SHA-256
`9db9ed4083e5dd5f81e590e2ed1d0d5cb03bbcca6a986482c71d575281887ee8`
and size 1,958,858 bytes. nextpnr selected 50→25/40 MHz with M=16 N=2 C6=16
C7=10. Its reported reference/25/40 Fmax values are 197.902/250.815/340.368 MHz
against 50/25/40 MHz constraints. Utilization is one `altera_pll`, three clock
buffers, and one HPS GP. Exact-artifact kit diagnostics on 2026-09-06 returned
zero while reset and 2048 / 3277 after relock for three cycles on each output.
The current `kit.py` close completed development reboot recovery and left the
lease free. This is exact-artifact functional diagnostic acceptance; it does
not establish native game acceptance.

`180_pll_frac` measures the checked 50→12.288 MHz fractional-N profile through
HPS GP. GPI signature `0xD715` identifies the protocol. Simulation and OSS are
supported; Quartus comparison is not implemented. See
`experiments/180_pll_frac/expected.md`.

The OSS `180_pll_frac` artifact has SHA-256
`55b099c8f0230e7938c6444d11a767b675ef5b93d89e35194c1612a752bea891`
and size 1,955,888 bytes. nextpnr selected 50→12.288 MHz fractional-N with M=8
N=1 C6=33 and 32-bit K=472790000 (achieved 12288000.000019869 Hz). Its reported
reference/output Fmax values are 195.274/340.716 MHz against 50/12.28803158 MHz
constraints. Utilization is one `altera_pll`, two clock buffers, and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-06 returned zero while reset and
1006–1007 after relock for ten cycles with GPI signature `0xD715`. The current
`kit.py` close completed development reboot recovery and left the lease free.
This is exact-artifact functional diagnostic acceptance; it does not establish
native game acceptance.

`190_pll_frac_441` measures the checked 50→11.2896 MHz fractional-N profile
through HPS GP. GPI signature `0xD716` identifies the protocol. Simulation and
OSS are supported; Quartus comparison is not implemented. See
`experiments/190_pll_frac_441/expected.md`.

The OSS `190_pll_frac_441` artifact has SHA-256
`0d95fe235f42e2adc5e8d66771c136b1a608ce325e907da7ad945ae0535bfc02`
and size 1,955,865 bytes. nextpnr selected 50→11.2896 MHz fractional-N with M=8
N=1 C6=36 and 32-bit K=551954751 (achieved 11289599.972143253 Hz). Its reported
reference/output Fmax values are 195.274/340.716 MHz against 50/11.28961182 MHz
constraints. Utilization is one `altera_pll`, two clock buffers, and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-06 returned zero while reset and
924–925 after relock for ten cycles with GPI signature `0xD716`. The current
`kit.py` close completed development reboot recovery and left the lease free.
This is exact-artifact functional diagnostic acceptance; it does not establish
native game acceptance.

`200_pll_frac_dual` measures both outputs of the checked 12.288/24.576 MHz
fractional pair through HPS GP. GPI signature `0xD717` identifies the protocol;
GPO bit3 selects the 24.576 MHz meter. Simulation and OSS are supported;
Quartus comparison is not implemented. See
`experiments/200_pll_frac_dual/expected.md`.

The OSS `200_pll_frac_dual` artifact has SHA-256
`4714712c158a24e3a7f7b52af22f039e1f62bb2f42063d7b3b630613d85f46ea`
and size 1,958,269 bytes. nextpnr selected 50→12.288/24.576 MHz fractional-N with
M=8 N=1 C6=34 C7=17 and 32-bit K=`0x5b18548b`. Its reported reference/12.288/24.576
Fmax values are 197.824/288.600/325.521 MHz against 50/12.28803158/24.57606316 MHz
constraints. Utilization is one `altera_pll`, three clock buffers, and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-06 returned zero while reset and
1006–1007 / 2013–2014 after relock for three cycles on each output with GPI
signature `0xD717`. The current `kit.py` close completed development reboot
recovery and left the lease free. This is exact-artifact functional diagnostic
acceptance; it does not establish native game acceptance.

`210_pll_duty` measures a 50→25 MHz integer PLL at 25% duty through HPS GP and
a mixed-edge capture of GPO bit0. GPI signature `0xD718` identifies the
protocol. The frequency meter cannot observe pulse width. Simulation and OSS are
supported; Quartus comparison is not implemented. See
`experiments/210_pll_duty/expected.md`.

The OSS `210_pll_duty` artifact has SHA-256
`6c3add1f53fed4698b924ddbc6c0c9a93e4c34fb585e08dece0569c47c6a6f60`
and size 1,956,173 bytes. nextpnr selected 50→25 MHz with M=12 N=2 C6=12 and a
posedge→negedge `max_delay` of 10 ns on `duty_clock`. Its reported
reference/output Fmax values are 225.581/233.209 MHz against 50/25 MHz
constraints. Utilization is one `altera_pll`, two clock buffers, and one HPS GP.
The previous Mistral pin produced SHA-256
`e301c748d303769f3b603e736390e42913cb229ceae62aae3c47cc5b98ecbeb1` (1,956,174
bytes) whose falling-edge capture stayed 0. Exact-artifact kit diagnostics of
the corrected bitstream on 2026-09-06 returned zero while reset and 2048 after
relock for ten cycles; launch and falling-edge capture both followed GPO bit0
(`0xD7182308` / `0xD7182008`). This is exact-artifact functional diagnostic
acceptance; it does not measure pulse width or establish native game
acceptance. [misteross#9](https://github.com/DeanoC/misteross/issues/9) is
closed on this evidence.

`220_pll_phase` measures two 25 MHz outputs at 0° and +90° through HPS GP. GPI
signature `0xD719` identifies the protocol. The 0° meter cannot observe analog
phase. Simulation and OSS are supported; Quartus comparison is not implemented.
See `experiments/220_pll_phase/expected.md`.

The OSS `220_pll_phase` artifact has SHA-256
`57cda8e448d9fd0fc9082eb4d32d2d9b439e11b8cb178e4669cc6d8c4cd57b48`
and size 1,956,210 bytes. nextpnr selected 50→25/25 MHz with M=12 N=2 C6=12
C7=12 and a 0°→90° related-clock `max_delay` of 10 ns. Its reported
reference/0°/90° Fmax values are 224.517/233.427/668.003 MHz against 50/25/25 MHz
constraints. Utilization is one `altera_pll`, three clock buffers, and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-06 returned zero while reset and 2048
after relock for ten cycles; 0° launch and 90° capture both followed GPO bit0
(`0xD7192308` / `0xD7192008`). The current `kit.py` close completed development
reboot recovery and left the lease free. This is exact-artifact functional
diagnostic acceptance; it does not measure analog phase or establish native
game acceptance.

`230_pll_phase_180` measures two 25 MHz outputs at 0° and +180° through HPS GP.
GPI signature `0xD71A` identifies the protocol. The 0° meter cannot observe
analog phase. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/230_pll_phase_180/expected.md`.

The OSS `230_pll_phase_180` artifact has SHA-256
`89ac5cb017517497fcda92c67b24149352a8164741fbd04b660be8377bc21f2a`
and size 1,956,250 bytes. nextpnr selected 50→25/25 MHz with M=12 N=2 C6=12
C7=12. Its reported reference/0°/180° Fmax values are 196.078/297.442/668.003
MHz against 50/25/25 MHz constraints. Utilization is one `altera_pll`, three
clock buffers, and one HPS GP. Exact-artifact kit diagnostics on 2026-09-06
returned zero while reset and 2048 after relock for ten cycles; 0° launch and
180° capture both followed GPO bit0 (`0xD71A2308` / `0xD71A2008`).

`240_pll_phase_270` measures two 25 MHz outputs at 0° and +270° through HPS GP.
GPI signature `0xD71B` identifies the protocol. The 0° meter cannot observe
analog phase. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/240_pll_phase_270/expected.md`.

The OSS `240_pll_phase_270` artifact has SHA-256
`4c61e94a3ddaf21a8edbaa120087493efd1211f95a2112cd3d72fc37f001d449`
and size 1,956,348 bytes. nextpnr selected 50→25/25 MHz with M=12 N=2 C6=12
C7=12. Its reported reference/0°/270° Fmax values are 197.746/334.560/942.507
MHz against 50/25/25 MHz constraints. Utilization is one `altera_pll`, three
clock buffers, and one HPS GP. Exact-artifact kit diagnostics on 2026-09-06
returned zero while reset and 2048 after relock for ten cycles; 0° launch and
270° capture both followed GPO bit0 (`0xD71B2308` / `0xD71B2008`). The current
`kit.py` close completed development reboot recovery and left the lease free.
This is exact-artifact functional diagnostic acceptance; it does not measure
analog phase or establish native game acceptance.

`250_pll_triple` measures 25, 50 and 100 MHz outputs of one integer PLL through
HPS GP. GPI signature `0xD71C` identifies the protocol; GPO bits 4:3 select
the meter. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/250_pll_triple/expected.md`.

The OSS `250_pll_triple` artifact has SHA-256
`e028ddd025162f4ec80b94625162aa8386dc03828e52ea26c6fd53bb7ffa85c9`
and size 1,961,364 bytes. nextpnr selected 50→25/50/100 MHz with M=12 N=2
C6=12 C7=6. Its reported reference/25/50/100 Fmax values are
181.884/322.477/340.832/361.141 MHz against 50/25/50/100 MHz constraints.
Utilization is one `altera_pll`, four clock buffers, and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-06 returned zero while reset and
2048/4096/8192 after relock for three cycles on each output.

`260_pll_quad` measures 25, 50, 100 and 75 MHz outputs of one integer PLL
through HPS GP. GPI signature `0xD71D` identifies the protocol; GPO bits 4:3
select the meter. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/260_pll_quad/expected.md`.

The OSS `260_pll_quad` artifact has SHA-256
`01b57cc1c257d70e5e77f94280d821726464808c36095123926c8ca592e06a98`
and size 1,961,927 bytes. nextpnr selected 50→25/50/100/75 MHz with M=12 N=2
C6=12 C7=6. Its reported reference/25/50/100/75 Fmax values are
148.214/268.745/331.895/335.458/331.455 MHz against 50/25/50/100/75 MHz
constraints. Utilization is one `altera_pll`, five clock buffers, and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-06 returned zero while reset and
2048/4096/8192/6144 after relock for three cycles on each output. The current
`kit.py` close completed development reboot recovery and left the lease free.
This is exact-artifact functional diagnostic acceptance; it does not establish
native game acceptance.

`270_pll_multi_duty` measures the 25/50/100 MHz triple with independent
25/50/25 duties through HPS GP. GPI signature `0xD71E` identifies the
protocol; GPO bits 4:3 select the meter. Simulation and OSS are supported;
Quartus comparison is not implemented. The meters cannot observe pulse width.
See `experiments/270_pll_multi_duty/expected.md`.

The OSS `270_pll_multi_duty` artifact has SHA-256
`475d62f76c87e3b934200b45ecdb36215e1c1f93a44a2c179dcd7ad4e1ceaae9`
and size 1,961,408 bytes. nextpnr selected the 400 MHz configuration
50→25/50/100 MHz with M=16 N=2 C6=16 C7=8. Its reported reference/25/50/100
Fmax values are 189.215/322.477/340.832/361.141 MHz against 50/25/50/100 MHz
constraints. Utilization is one `altera_pll`, four clock buffers, and one
HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero while
reset and 2048/4096/8192 after relock for three cycles on each output.

`280_pll_quadrature` measures four 25 MHz outputs at 0°/90°/180°/270° through
HPS GP. GPI signature `0xD71F` identifies the protocol; GPO bits 4:3 select
the meter. Simulation and OSS are supported; Quartus comparison is not
implemented. The meters cannot observe analog phase. See
`experiments/280_pll_quadrature/expected.md`.

The OSS `280_pll_quadrature` artifact has SHA-256
`281761251e3f6d973d9a9b53b9c5f2003746c1ca97dbbba75ba8a74c2c4b5a23`
and size 1,962,620 bytes. nextpnr selected 50→25/25/25/25 MHz with M=12 N=2
C6=12 C7=12. Its reported reference/0°/90°/180°/270° Fmax values are
145.773/347.584/268.962/319.693/338.983 MHz against 50/25/25/25/25 MHz
constraints. Utilization is one `altera_pll`, five clock buffers, and one
HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero while
reset and 2048 after relock for three cycles on each output.

`290_pll_phase_select` measures four 25 MHz outputs at 0°/180°/180°/0° through
HPS GP. GPI signature `0xD720` identifies the protocol; GPO bits 4:3 select
the meter. Simulation and OSS are supported; Quartus comparison is not
implemented. The meters cannot observe analog phase. See
`experiments/290_pll_phase_select/expected.md`.

The OSS `290_pll_phase_select` artifact has SHA-256
`94d25ccf007accbb8ab466f7060f209586fcaf2127b5bd0f7025f8070983bc07`
and size 1,962,812 bytes. nextpnr selected 50→25/25/25/25 MHz with M=12 N=2
C6=12 C7=12. Its reported reference/0°/180°/180°/0° Fmax values are
146.606/255.428/242.189/347.584/341.413 MHz against 50/25/25/25/25 MHz
constraints. Utilization is one `altera_pll`, five clock buffers, and one
HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero while
reset and 2048 after relock for three cycles on each output. The current
`kit.py` close completed development reboot recovery and left the lease free.
This is exact-artifact functional diagnostic acceptance; it does not measure
analog phase or pulse width, or establish native game acceptance.

`300_pll_ref25` measures 25/50/100 MHz outputs of one integer PLL whose
reference parameter is 25 MHz. GPI signature `0xD721` identifies the protocol;
GPO bits 4:3 select the meter. Simulation and OSS are supported; Quartus
comparison is not implemented. Analog kit measurement needs a physical 25 MHz
V11 clock. See `experiments/300_pll_ref25/expected.md`.

The OSS `300_pll_ref25` artifact has SHA-256
`f8b65f631864982a62e42fa59b58b4e2c5dcdcf1fa9e3ad08d6a8f00cbfba342`
and size 1,961,334 bytes. nextpnr selected 25→25/50/100 MHz with M=24 N=2
C6=12 C7=6. Its reported reference/25/50/100 Fmax values are
177.809/322.477/340.832/361.141 MHz against 25/25/50/100 MHz constraints.
Utilization is one `altera_pll`, four clock buffers, and one HPS GP.

`310_pll_ref100` measures 25/50/100 MHz outputs of one integer PLL whose
reference parameter is 100 MHz. GPI signature `0xD722` identifies the protocol;
GPO bits 4:3 select the meter. Simulation and OSS are supported; Quartus
comparison is not implemented. Analog kit measurement needs a physical 100 MHz
V11 clock. See `experiments/310_pll_ref100/expected.md`.

The OSS `310_pll_ref100` artifact has SHA-256
`47a52dc1b089aad17ebdbdd3e5ad84246a1bc5b59e80223d5052fa5ca97e25cd`
and size 1,961,320 bytes. nextpnr selected 100→25/50/100 MHz with M=6 N=2
C6=12 C7=6. Its reported reference/25/50/100 Fmax values are
170.097/322.477/340.832/361.141 MHz against 100/25/50/100 MHz constraints.
Utilization is one `altera_pll`, four clock buffers, and one HPS GP. Analog
kit measurement of either new reference is host-only on the designated
DE10-Nano: the onboard oscillator remains 50 MHz. This is OSS packing and
simulation evidence; it does not establish native game acceptance.

`320_pll_phase50` measures four 50 MHz outputs at 0°/90°/180°/270° through
HPS GP. GPI signature `0xD723` identifies the protocol; GPO bits 4:3 select
the meter. Simulation and OSS are supported; Quartus comparison is not
implemented. The meters cannot observe analog phase. See
`experiments/320_pll_phase50/expected.md`.

The OSS `320_pll_phase50` artifact has SHA-256
`f27254b131d950a59df4dbf16b47e6292175270a38c380d51bbe5eea139b35d9`
and size 1,961,842 bytes. nextpnr selected 50→50/50/50/50 MHz with M=12 N=2
C6=6 C7=6. Its reported reference/0°/90°/180°/270° Fmax values are
164.690/325.309/331.675/349.162/343.997 MHz against 50/50/50/50/50 MHz
constraints. Utilization is one `altera_pll`, five clock buffers, and one
HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero while
reset and 4096 after relock for three cycles on each output. The current
`kit.py` close completed development reboot recovery and left the lease free.
This is exact-artifact functional diagnostic acceptance; it does not measure
analog phase or establish native game acceptance.

`330_pll_phase100` measures four 100 MHz outputs at 0°/90°/180°/270° through
HPS GP. GPI signature `0xD724` identifies the protocol; GPO bits 4:3 select
the meter. Simulation and OSS are supported; Quartus comparison is not
implemented. Simulation uses 50 MHz stand-ins for the 100 MHz outputs. The
meters cannot observe analog phase. See
`experiments/330_pll_phase100/expected.md`.

The OSS `330_pll_phase100` artifact has SHA-256
`dda4e7b504484d9ae780ad8b693a44a4d806075d096f90852eb29ff9ac007087`
and size 1,962,130 bytes. nextpnr selected 50→100/100/100/100 MHz with M=12
N=2 C6=3 C7=3. Its reported reference/0°/90°/180°/270° Fmax values are
150.580/319.795/340.020/355.240/362.188 MHz against 50/100/100/100/100 MHz
constraints. Utilization is one `altera_pll`, five clock buffers, and one
HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero while
reset and 8192 after relock for three cycles on each output.

`340_pll_phase45` measures four 50 MHz outputs at 0°/90°/270°/315° through
HPS GP. GPI signature `0xD725` identifies the protocol; GPO bits 4:3 select
the meter. Simulation and OSS are supported; Quartus comparison is not
implemented. The meters cannot observe analog phase. See
`experiments/340_pll_phase45/expected.md`.

The OSS `340_pll_phase45` artifact has SHA-256
`f12594c04fb9eb9716c48146445f680ff565417bb778ac346a9abf428588a366`
and size 1,961,709 bytes. nextpnr selected 50→50/50/50/50 MHz with M=12 N=2
C6=6 C7=6. Its reported reference/0°/90°/270°/315° Fmax values are
164.690/325.309/331.675/349.162/343.997 MHz against 50/50/50/50/50 MHz
constraints. Utilization is one `altera_pll`, five clock buffers, and one
HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero while
reset and 4096 after relock for three cycles on each output.

`350_pll_two` measures two independent PLLs on the 50 MHz V11 reference: a
25 MHz integer PLL and a 12.288 MHz fractional-N PLL. GPI signature `0xD726`
identifies the protocol; GPO bit 3 selects the meter. Simulation and OSS are
supported; Quartus comparison is not implemented. See
`experiments/350_pll_two/expected.md`.

The OSS `350_pll_two` artifact has SHA-256
`fc3bcc138b394435e557c5104df8a8774a413085bda2572b50b24c6f94724cbc`
and size 1,958,206 bytes. nextpnr placed the integer PLL at FPLL (0,14) with
M=12 N=2 C6=12 and the fractional PLL at FPLL (0,31) with M=8 N=1 C6=33. Its
reported reference/25/12.288 Fmax values are 176.056/336.587/334.896 MHz
against 50/25/12.288 MHz constraints. Utilization is two `altera_pll`, three
clock buffers, and one HPS GP. Exact-artifact kit diagnostics on 2026-09-07
returned zero while reset, then 2048 on 25 MHz and 1006–1007 on 12.288 MHz
for three cycles each.

`360_pll_clkena` measures a 25 MHz PLL output through a falling-edge
`cyclonev_clkena`. GPI signature `0xD727` identifies the protocol; GPO bit 3
drives enable. Simulation and OSS are supported; Quartus comparison is not
implemented. Enable setup/hold is not characterized. See
`experiments/360_pll_clkena/expected.md`.

The OSS `360_pll_clkena` artifact has SHA-256
`b3a7af18ec073c366f416fb104ce35ede01a3d997150a38333c57aa22ef17be9`
and size 1,955,834 bytes. nextpnr selected 50→25 MHz with M=12 N=2 C6=12.
Its reported reference/gated Fmax values are 205.804/324.781 MHz against
50/25 MHz constraints. Utilization is one `altera_pll`, two clock enables,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero
while reset, 2048 with enable asserted, and zero with enable cleared, for
three cycles. The current `kit.py` close completed development reboot
recovery and left the lease free. This is exact-artifact functional
diagnostic acceptance; it does not measure analog phase, enable setup/hold,
or establish native game acceptance.

`370_pll_clkena_low` measures the same gated 25 MHz output with enable
power-up low. GPI signature `0xD728` identifies the protocol. Simulation and
OSS are supported; Quartus comparison is not implemented. See
`experiments/370_pll_clkena_low/expected.md`.

The OSS `370_pll_clkena_low` artifact has SHA-256
`6bbf95059d349b3b175b4d608d66f2814e32201949ae7dab447e1ab7520afbd0`
and size 1,955,801 bytes. nextpnr selected 50→25 MHz with M=12 N=2 C6=12.
Its reported reference/gated Fmax values are 199.840/324.781 MHz against
50/25 MHz constraints. Utilization is one `altera_pll`, two clock enables,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero
while reset and at low startup, and 2048 with enable asserted, for three
cycles.

`380_pll_clkena_branch` measures an always-running 25 MHz PLL output and a
gated branch of the same counter. GPI signature `0xD729` identifies the
protocol; GPO bit 4 selects the meter. Simulation and OSS are supported;
Quartus comparison is not implemented. See
`experiments/380_pll_clkena_branch/expected.md`.

The OSS `380_pll_clkena_branch` artifact has SHA-256
`678c63018b2a5bfc54c379835a6b484b7726addeabd100cfe8cd03da4b6bad65`
and size 1,958,267 bytes. nextpnr selected 50→25 MHz with M=12 N=2 C6=12.
Its reported reference/running/gated Fmax values are 191.278/353.232/358.551
MHz against 50/25/25 MHz constraints. Utilization is one `altera_pll`, three
clock enables, and one HPS GP. Exact-artifact kit diagnostics on 2026-09-07
returned zero while reset, 2048 on the running branch with the gate off, and
zero on the gated branch with the gate off, for three cycles each.

`390_pll_clkena_status` measures a gated 25 MHz output and samples `enaout`
on the 50 MHz reference. GPI signature `0xD72A` identifies the protocol.
Simulation and OSS are supported; Quartus comparison is not implemented. See
`experiments/390_pll_clkena_status/expected.md`.

The OSS `390_pll_clkena_status` artifact has SHA-256
`a3ef0e1526723ab50e459cb6645c320a51f7f9a52af7d5bb12be4734a60b6f95`
and size 1,955,725 bytes. nextpnr selected 50→25 MHz with M=12 N=2 C6=12.
Its reported reference/gated Fmax values are 201.572/344.590 MHz against
50/25 MHz constraints. Utilization is one `altera_pll`, two clock enables,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero
while reset and at low startup, 2048 with enable asserted, and matching
sampled `enaout`, for three cycles.

`400_pll_clkena_reg2` measures a two-stage falling-edge clock enable with
sampled `enaout`. GPI signature `0xD72B` identifies the protocol. Simulation
and OSS are supported; Quartus comparison is not implemented. See
`experiments/400_pll_clkena_reg2/expected.md`.

The OSS `400_pll_clkena_reg2` artifact has SHA-256
`bb2a465d23a7a48505e4bce1184c65289f5acfdb76b235d6bf2bd3f1053ea468`
and size 1,955,725 bytes. nextpnr selected 50→25 MHz with M=12 N=2 C6=12.
Its reported reference/gated Fmax values are 201.572/344.590 MHz against
50/25 MHz constraints. Utilization is one `altera_pll`, two clock enables,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-07 returned zero
while reset and at low startup, and 2048 with enable asserted, for three
cycles. The current `kit.py` close completed development reboot recovery
and left the lease free. This is exact-artifact functional diagnostic
acceptance; it does not characterize enable setup/hold or establish native
game acceptance.

`410_dsp_triple` exposes three eight-by-eight unsigned DSP products on the HPS
general-purpose interface: `left * right`, `left * ~right`, and
`left * (right ^ 8'h01)`.
Linux peeks and pokes GPO/GPI; there is no LED. GPO bits `[18:17]` select the
lane and bit 16 selects the high product byte. GPI signature `0xD611`
identifies the protocol. Yosys emits three `MISTRAL_MUL9X9` cells. nextpnr-mistral
places those cells on z-lanes 0/1/2 of one physical DSP site, with RESULT
`0:17` / `18:35` / `37:54`. Memory and PLL remain forbidden. Simulation and
OSS are supported; Quartus comparison is not implemented. See
`experiments/410_dsp_triple/expected.md`.

The OSS `410_dsp_triple` artifact has SHA-256
`135284a611feb26b3da9d2c352b2f601099a8d62d0c3c832d1ca4ae7dbc9ae12`
and size 1,955,082 bytes. Its reported Fmax is 387.747 MHz against the 50 MHz
constraint. Utilization is three `MISTRAL_MUL9X9` (336 available) packed at
`MISTRAL_MUL9X9.32.2.{0,1,2}`, and one HPS GP. Exact-artifact kit diagnostics
on 2026-09-07 returned GPI signature `0xD611` and the expected products for
all three lanes (`0x0A*0x0C` → `0x0078` / `0x097E` / `0x0082`, `0x12*0x34`
→ `0x03A8` / `0x0E46` / `0x03BA`, `0xFF*0xFF` → `0xFE01` / `0` / `0xFD02`).
The current `kit.py` close completed development reboot recovery and left the
lease free. This is exact-artifact functional diagnostic acceptance of three
packed 9×9 lanes; it does not establish native game acceptance.

`420_dsp_mul18` exposes a sixteen-by-sixteen unsigned DSP product on HPS GP.
GPO `[15:0]` is the left operand and `[31:16]` is the right operand. GPI
signature `0xD612`; `[15:0]` are the low 16 bits of the 32-bit product. Yosys
emits one `MISTRAL_MUL18X18`. nextpnr-mistral places one `M18X18P36` DSP.
Memory and PLL remain forbidden. Simulation and OSS are supported; Quartus
comparison is not implemented. See `experiments/420_dsp_mul18/expected.md`.

The OSS `420_dsp_mul18` artifact has SHA-256
`c97928b4a4f67a0bacbc70f108b5350a7c48bbbcbc7739e5b0913c84065e14cf`
and size 1,953,853 bytes. Its reported Fmax is 344.116 MHz against the 50 MHz
constraint. Utilization is one `MISTRAL_MUL18X18` (112 available) and one HPS
GP. Exact-artifact kit diagnostics on 2026-09-07 returned GPI signature
`0xD612` and the expected low-16 products (`10*12` → `120`, `0x100*0x100` →
`0`, `0xFFFF*2` → `65534`, `0xFFFF*0xFFFF` → `1`).

`430_dsp_mul27` exposes a twenty-by-eight unsigned DSP product on HPS GP.
GPO `[19:0]` is the left operand and `[27:20]` is the right operand. GPI
signature `0xD613`; `[15:0]` are the low 16 bits of the 28-bit product. Yosys
emits one `MISTRAL_MUL27X27`. nextpnr-mistral places one `M27X27` DSP.
Omitted DSP controls encode low, so the native multiply is not negated.
Memory and PLL remain forbidden. Simulation and OSS are supported; Quartus
comparison is not implemented. See `experiments/430_dsp_mul27/expected.md`.

The OSS `430_dsp_mul27` artifact has SHA-256
`4a6fc04821a7ee23d286beafc57818678c4f6d38292e13ec37a280f9615adf8f`
and size 1,953,793 bytes. Its reported Fmax is 415.110 MHz against the 50 MHz
constraint. Utilization is one `MISTRAL_MUL27X27` (112 available) and one HPS
GP. Exact-artifact kit diagnostics on 2026-09-07 returned GPI signature
`0xD613` and the expected low-16 products (`10*12` → `120`, `0x10000*2` → `0`,
`0x12345*3` → `27087`, `0xFFFFF*2` → `65534`).

`440_dsp_preadder` exposes `left * (right - preadd)` on HPS GP using the M9
preadder. GPO `[7:0]` is left, `[15:8]` is right, `[23:16]` is the preadd
operand, and bit 24 selects the high product byte. GPI signature `0xD614`.
OSS `chtype`s a blackbox `dsp9_preadder` cell to `MISTRAL_MUL9X9` with
`PREADDER_EN`/`PREADDER_SUB`. Memory and PLL remain forbidden. Simulation and
OSS are supported; Quartus comparison is not implemented. See
`experiments/440_dsp_preadder/expected.md`.

The OSS `440_dsp_preadder` artifact has SHA-256
`0df11dd3639cd9ec9bc701c23c45cd9cd1a9ad62e8406ea6dd205b220bfcda53`
and size 1,953,958 bytes. Its reported Fmax is 383.142 MHz against the 50 MHz
constraint. Utilization is one `MISTRAL_MUL9X9` (336 available) and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-07 returned GPI signature `0xD614`
and the expected products (`10*12` → `120`, `10*(12-2)` → `100`, `255*4` →
`1020`, `16*0` → `0`).

`450_dsp_mac` exposes an eight-by-eight unsigned M18 product plus the 36-bit C
addend. GPO `[7:0]` is left, `[15:8]` is right, and `[31:16]` is the addend.
GPI signature `0xD615`; `[15:0]` are the low 16 bits of `A*B+C`. OSS `chtype`s
a blackbox `dsp18_mac` cell to `MISTRAL_MUL18X18`. nextpnr-mistral places one
`M18X18P36` DSP and maps C on BX groups `{8,9,6,7}` without cascade. Memory
and PLL remain forbidden. Simulation and OSS are supported; Quartus comparison
is not implemented. See `experiments/450_dsp_mac/expected.md`.

The OSS `450_dsp_mac` artifact has SHA-256
`609120e142e5a7a1041a2a255e27c252870811622d3d2d7a29844088464a3ebf`
and size 1,953,744 bytes. Its reported Fmax is 315.060 MHz against the 50 MHz
constraint. Utilization is one `MISTRAL_MUL18X18` (112 available) and one HPS
GP. Exact-artifact kit diagnostics on 2026-09-07 returned GPI signature
`0xD615` and the expected `A*B+C` low-16 results (`10*12` → `120`, `10*12+5`
→ `125`, `255*255+7` → `65032`, `16*16+256` → `512`).

`460_dsp_reg` exposes an eight-by-eight unsigned M18 product with input and
output registers on HPS GP. GPO layout matches `060_dsp_mul`. GPI signature
`0xD616`. OSS `chtype`s `dsp18_reg` to `MISTRAL_MUL18X18` with
`INREG_CTRL_AX`, `INREG_CTRL_AY`, and `OREG_CTRL`. Clock comes from
`FPGA_CLK1_50`; enable and ACLR are omitted so the registers stay enabled and
unused clear stays low. Memory and PLL remain forbidden. Simulation and OSS
are supported; Quartus comparison is not implemented. See
`experiments/460_dsp_reg/expected.md`.

The OSS `460_dsp_reg` artifact has SHA-256
`ae957391e444670a775f371b3261672caa33d76c1b60663ab5d46bc6c36133d1`
and size 1,954,394 bytes. Its reported Fmax is 342.583 MHz against the 50 MHz
constraint. Utilization is one `MISTRAL_MUL18X18` (112 available) and one HPS
GP. Exact-artifact kit diagnostics on 2026-09-07 returned GPI signature
`0xD616` and the expected registered products (`10*12` → `120`, `0x12*0x34` →
`936`, `255*255` → `65025`).

`470_mlab_init` exposes the 32-by-8 MLAB table with preserved power-up
contents on HPS GP. GPO layout matches `040_mlab_ram`. GPI signature `0xD417`.
Address `a` starts as `((a * 73) ^ (a >> 1) ^ 8'hA6)`, so address 0 is `0xA6`.
Yosys maps the initialized table to eight `MISTRAL_MLAB` cells with numeric
INIT. OSS does not inject those parameters. Memory besides those MLABs, DSP,
and PLL remain forbidden. Simulation and OSS are supported; Quartus comparison
is not implemented. See `experiments/470_mlab_init/expected.md`.

The OSS `470_mlab_init` artifact has SHA-256
`900e120d33031695f7f53b2bb74b97c1721d4b6faf9226dfe9ae5b385e75b1f1`
and size 1,953,488 bytes. Its reported Fmax is 407.664 MHz against the 50 MHz
constraint. Utilization is eight `MISTRAL_MLAB` cells and one HPS GP. The RBF is
byte-identical to the earlier INIT-injected 470 artifact. Exact-artifact kit
diagnostics on 2026-09-08 from native Yosys INIT returned GPI signature
`0xD417` with address 0 equal to `0xA6`, all 32 initialized bytes, then
even-address writes that left odd addresses unchanged.

`480_m10k_sdp20` exposes a 512-by-20 M10K simple dual-port table on HPS GP.
Writes use `FPGA_CLK1_50`. Reads use a 25 MHz PLL output gated by
`cyclonev_clkena`. GPI signature `0xD418`. Address `a` starts as
`((a * 73) ^ (a >> 1) ^ 20'hA6)`, so address 0 is `0xA6`. Yosys maps one
`MISTRAL_M10K` with `CFG_DUAL_CLOCK=1`, write `CLK1` and read `CLK2`. nextpnr
packs CLK2 onto CLKIN.1. DSP and MLAB remain forbidden. Simulation and OSS
are supported; Quartus comparison is not implemented. See
`experiments/480_m10k_sdp20/expected.md`.

The OSS `480_m10k_sdp20` artifact has SHA-256
`eaaf1fa9ef298bcf219e4fd70a818563e3f9e6f3a79e3896a6f1baf7a92dfd5e`
and size 1,959,276 bytes. Its reported write-clock Fmax is 495.786 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD418` with address 0 equal to `0xA6`, full-width initialized
reads, a write while the 25 MHz read clock was stopped, resume of the new
value, and both enable holds.

`490_m10k_sdp40` is the 256-by-40 independent-clock table on the same
protocol. GPI signature `0xD419`. Stored words are `{ ~low, low }` so the
second physical data group is covered. The 40-bit input half keeps the write
clock. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/490_m10k_sdp40/expected.md`.

The OSS `490_m10k_sdp40` artifact has SHA-256
`7d626b16ea1aed53358de40abb2a35b7cede1a216d7805ceafba986507d8b76d`
and size 1,958,836 bytes. Its reported write-clock Fmax is 361.402 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD419` with address 0 low half equal to `0xA6`, all 40 data bits
initialized, a write while the read clock was stopped, resume, and both
enable holds.

`500_m10k_be20` exposes a 512-by-20 M10K simple dual-port table on HPS GP with
two independently writable 10-bit lanes. Writes use `FPGA_CLK1_50`. Reads use
a 25 MHz PLL output gated by `cyclonev_clkena`. GPI signature `0xD41A`.
Address `a` starts as `((a * 73) ^ (a >> 1) ^ 20'hA6)`. Yosys maps one
`MISTRAL_M10K` with `CFG_BYTE_ENABLE=1`, `CFG_DUAL_CLOCK=1`, and two `A1BE`
lanes. DSP and MLAB remain forbidden. Simulation and OSS are supported;
Quartus comparison is not implemented. See
`experiments/500_m10k_be20/expected.md`.

The OSS `500_m10k_be20` artifact has SHA-256
`8a03587f198259e3eb5029576bb347a6885c98d904c48b986815cf59be5fb710`
and size 1,959,304 bytes. Its reported write-clock Fmax is 371.747 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD41A` with address 0 equal to `0xA6`, full-width initialized
reads, independent low and high 10-bit lane writes, a zero mask that left
both lanes unchanged, and a full write while the 25 MHz read clock was
stopped.

`510_m10k_mix40r10` exposes a mixed-width M10K table on HPS GP: 256-by-40
writes and 1024-by-10 reads. Writes use `FPGA_CLK1_50`. Reads use a 25 MHz
PLL output gated by `cyclonev_clkena`. GPI signature `0xD41B`. Each 10-bit
lane `a` starts as `((a * 73) ^ (a >> 1) ^ 10'hA6)`. Yosys maps one
`MISTRAL_M10K` with `CFG_MIXED_WIDTH=1`, write `CFG_DBITS=40` and read
`CFG_RD_DBITS=10`. Place-and-route uses router1. DSP and MLAB remain
forbidden. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/510_m10k_mix40r10/expected.md`.

The OSS `510_m10k_mix40r10` artifact has SHA-256
`74f55f5309d4377b7271f78c5c5666360b858693f55b22205f4c8af80ffcb3a1`
and size 1,957,964 bytes. Its reported write-clock Fmax is 523.560 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD41B` with address 0 equal to `0xA6`, initialized 10-bit lanes,
a 40-bit write of four lanes in address order while the read clock was
stopped, an unchanged neighbor, and read-enable hold.

`520_m10k_mix10r40` is the reverse mixed-width table: 1024-by-10 writes and
256-by-40 reads. GPI signature `0xD41C`. Simulation and OSS are supported;
Quartus comparison is not implemented. See
`experiments/520_m10k_mix10r40/expected.md`.

The OSS `520_m10k_mix10r40` artifact has SHA-256
`2db1e33d7c1dd227e2a40f4c505936680bef01542fb9c5a462b1fd4cc23ef2fb`
and size 1,958,633 bytes. Its reported write-clock Fmax is 632.911 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD41C` with address 0 equal to `0xA6`, initialized 40-bit words,
a 10-bit write of one lane while the read clock was stopped, an unchanged
neighbor, and read-enable hold.

`530_m10k_tdp10` exposes a 1024-by-10 M10K true dual-port table on HPS GP.
Port A uses `FPGA_CLK1_50`. Port B uses a 25 MHz PLL output gated by
`cyclonev_clkena`. GPI signature `0xD41D`. Address `a` starts as
`((a * 73) ^ (a >> 1) ^ 10'hA6)`, so address 0 is `0xA6`. Yosys maps one
`MISTRAL_M10K_TDP` with `CFG_ABITS=10` and `CFG_DBITS=10`. nextpnr packs it
as one `MISTRAL_M10K` with `CFG_TDP=1`. DSP and MLAB remain forbidden.
Simulation and OSS are supported; Quartus comparison is not implemented. See
`experiments/530_m10k_tdp10/expected.md`.

The OSS `530_m10k_tdp10` artifact has SHA-256
`54b8814a21b245030bacba604e41bf46d99ef6f2cc9b1e44a93d66da95ef4daf`
and size 1,958,336 bytes. Its reported write-clock Fmax is 338.639 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD41D` with address 0 equal to `0xA6`, initialized reads through
both ports, both writers with own-port NEW_DATA and opposite-port readback,
simultaneous disjoint writes, and enable hold with disabled write suppression.

`540_m10k_tdp20` is the 512-by-20 true dual-port table on the same protocol.
GPI signature `0xD41E`. Simulation and OSS are supported; Quartus comparison
is not implemented. See `experiments/540_m10k_tdp20/expected.md`.

The OSS `540_m10k_tdp20` artifact has SHA-256
`ad62ad9cbd896674ecdfe224f919c5aee613f13325fb01771c6b5d9cab451abb`
and size 1,959,758 bytes. Its reported write-clock Fmax is 244.978 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD41E` with address 0 equal to `0xA6`, initialized 20-bit reads
through both ports, both writers with own-port NEW_DATA and opposite-port
readback, simultaneous disjoint writes, and enable hold with disabled write
suppression.

`550_m10k_tdp_be20` exposes a 512-by-20 M10K true dual-port table with two
10-bit write lanes on HPS GP. Port A uses `FPGA_CLK1_50`. Port B uses a
25 MHz PLL output gated by `cyclonev_clkena`. GPI signature `0xD41F`. Address
`a` starts as `((a * 73) ^ (a >> 1) ^ 20'hA6)`, so address 0 is `0xA6`.
Yosys maps one `MISTRAL_M10K_TDP` with `CFG_BYTE_ENABLE=1`, `CFG_ABITS=9` and
`CFG_DBITS=20`. nextpnr packs it as one `MISTRAL_M10K` with `CFG_TDP=1`. DSP
and MLAB remain forbidden. Simulation and OSS are supported; Quartus
comparison is not implemented. See `experiments/550_m10k_tdp_be20/expected.md`.

The OSS `550_m10k_tdp_be20` artifact has SHA-256
`ddd39418d905972ac0377f56ebffca815e14993ebfccc3ded692a62e45af2c4c`
and size 1,960,927 bytes. Its reported write-clock Fmax is 257.865 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD41F` with address 0 equal to `0xA6`, initialized reads through
both ports, low/high/zero/full masks on both writers with preserved storage,
inferred output hold during writes, simultaneous disjoint masked writes, and
enable hold with disabled write suppression.

`560_m10k_tdp_be16` is the 512-by-16 padded-byte true dual-port table on the
same protocol. GPI signature `0xD420`. Simulation and OSS are supported;
Quartus comparison is not implemented. See
`experiments/560_m10k_tdp_be16/expected.md`.

The OSS `560_m10k_tdp_be16` artifact has SHA-256
`29c47a0cc42db5451ab7cfffc28cb15f21821eb7c5c644af4f51eb4825e5139f`
and size 1,960,895 bytes. Its reported write-clock Fmax is 242.542 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD420` with address 0 equal to `0xA6`, initialized 16-bit reads
through both ports, low/high/zero/full masks on both writers with preserved
storage, inferred output hold during writes, simultaneous disjoint masked
writes, and enable hold with disabled write suppression.

`570_m10k_tdp_mix20_10` exposes a mixed-width M10K true dual-port table on
HPS GP: 512-by-20 port A and 1024-by-10 port B. Port A uses `FPGA_CLK1_50`.
Port B uses a 25 MHz PLL output gated by `cyclonev_clkena`. GPI signature
`0xD421`. Canonical 10-bit lane `a` starts as
`((a * 73) ^ (a >> 1) ^ 10'hA6)`, so address 0 is `0xA6`. Yosys maps one
`MISTRAL_M10K_TDP` with `CFG_MIXED_WIDTH=1` and physical 20/10 ports (Yosys
may exchange primitive A/B). nextpnr packs it as one `MISTRAL_M10K` with
`CFG_TDP=1`. DSP and MLAB remain forbidden. Simulation and OSS are
supported; Quartus comparison is not implemented. See
`experiments/570_m10k_tdp_mix20_10/expected.md`.

The OSS `570_m10k_tdp_mix20_10` artifact has SHA-256
`af567581d02b4a5ef60d4d8fb43ebf88e180c428de215d78d3d70731968ae49d`
and size 1,959,036 bytes. Its reported write-clock Fmax is 313.283 MHz against
the 50 MHz constraint. Utilization is one `MISTRAL_M10K`, one `altera_pll`,
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned GPI
signature `0xD421`, initialized words through both port widths, NEW_DATA with
cross-width readback and preserved neighbors, simultaneous disjoint writes,
and enable hold with disabled write suppression.

`580_m10k_tdp_mix10_20` is the reverse 1024-by-10 / 512-by-20 mixed-width
true dual-port table. GPI signature `0xD422`. Simulation and OSS are
supported; Quartus comparison is not implemented. See
`experiments/580_m10k_tdp_mix10_20/expected.md`.

The OSS `580_m10k_tdp_mix10_20` artifact has SHA-256
`27b7d348eef7466fd48467d2459e273cd31b90fdfcbdd9a9d978e8c2c17ac4ea`
and size 1,959,043 bytes. Its reported write-clock Fmax is 311.526 MHz against
the 50 MHz constraint. Exact-artifact kit diagnostics on 2026-09-08 returned
GPI signature `0xD422` with the same mixed-width probe set.

`590_m10k_tdp_mix16_8` is the padded 512-by-16 / 1024-by-8 mixed-width true
dual-port table. GPI signature `0xD423`. Simulation and OSS are supported;
Quartus comparison is not implemented. See
`experiments/590_m10k_tdp_mix16_8/expected.md`.

The OSS `590_m10k_tdp_mix16_8` artifact has SHA-256
`5b95fd2c63370395e9a57346cc1395f0eeb99727baec7c677ef49fdd25c3abbd`
and size 1,959,209 bytes. Its reported write-clock Fmax is 332.005 MHz against
the 50 MHz constraint. Exact-artifact kit diagnostics on 2026-09-08 returned
GPI signature `0xD423` with the same mixed-width probe set.

`600_m10k_tdp_mix8_16` is the reverse padded 1024-by-8 / 512-by-16
mixed-width true dual-port table. GPI signature `0xD424`. Simulation and OSS
are supported; Quartus comparison is not implemented. See
`experiments/600_m10k_tdp_mix8_16/expected.md`.

The OSS `600_m10k_tdp_mix8_16` artifact has SHA-256
`90c0a149ac9799e94ba6666a88a768d8a4814d9129f7fce1cf4b3c5d9356e8b2`
and size 1,959,145 bytes. Its reported write-clock Fmax is 287.853 MHz against
the 50 MHz constraint. Exact-artifact kit diagnostics on 2026-09-08 returned
GPI signature `0xD424` with the same mixed-width probe set.

`610_pll_frac_7425` measures the checked 50→74.25 MHz fractional-N profile
through HPS GP. GPI signature `0xD742` identifies the protocol. Simulation
models a 25 MHz digital stand-in and a lock delay, not the analog 74.25 MHz
ratio. nextpnr programs M=8 N=1 C6=6 and 32-bit K=`0xe8f5c239`. Simulation
and OSS are supported; Quartus comparison is not implemented. See
`experiments/610_pll_frac_7425/expected.md`.

The OSS `610_pll_frac_7425` artifact has SHA-256
`233511d0a10b02480b41468f4785a762c1dd50e9c4c66908891f351906ec378c`
and size 1,955,855 bytes. nextpnr selected 50→74.25 MHz fractional-N with M=8
N=1 C6=6 and 32-bit K=`0xe8f5c239` (achieved 74249999.83243954 Hz). Its
reported reference/output Fmax values are 195.274/343.879 MHz against
50/74.25006866 MHz constraints. Utilization is one `altera_pll`, two clock
buffers, and one HPS GP. Exact-artifact kit diagnostics on 2026-09-08 returned
zero while reset and 6082–6083 after relock for ten cycles with GPI signature
`0xD742`.

`620_ddr_clock` forwards the 50 MHz reference through a width-one
`altddio_out` onto PIN_W15. GPI signature `0xDD01` identifies the fabric
beat protocol. Simulation copies the reference onto `DDR_OUT`; it does not
model analog DDR registers or pin delay. The kit probe observes fabric
counters, not the forwarded pin waveform. Simulation and OSS are supported;
Quartus comparison is not implemented. See
`experiments/620_ddr_clock/expected.md`.

The OSS `620_ddr_clock` artifact has SHA-256
`3daaca5aa964c73cf383a0d5334284998b399bef8c58c2248e7b1f6b62e92df5`
and size 1,953,412 bytes. nextpnr packed `altddio_out` into `MISTRAL_DDROUT`
on `MISTRAL_IO.89.8.1` (PIN_W15, normal phase). Its reported Fmax is
322.372 MHz against the 50 MHz constraint. Utilization is one HPS GP, two
IO cells, and no PLL, memory or DSP. The current nextpnr pin `eefa26d3`
with Yosys `fca8ca0a` reproduces those same RBF bytes. Exact-artifact kit
diagnostics on 2026-09-09 returned GPI signature `0xDD01` with changing
paired fabric beats for ten samples. Load JSON timed out; GPI and probe
still passed. `stop` completed development reboot recovery and left the
lease free. This does not measure the forwarded pin waveform.

`630_sdr_output` registers `beat[7]` through a dedicated flop onto PIN_W15
with `FAST_OUTPUT_REGISTER ON`. GPI signature `0x5D01` identifies the fabric
beat protocol. Simulation uses the Verilog flop; it does not model analog
GPIO-register delay. nextpnr absorbs that flop into `MISTRAL_SDROUT` on the
existing GPIO BEL. The packed pin is an unclocked timing endpoint. The kit
probe observes fabric counters, not the pin waveform. Simulation and OSS are
supported; Quartus comparison is not implemented. See
`experiments/630_sdr_output/expected.md`.

The OSS `630_sdr_output` artifact has SHA-256
`cf5d712159ccc237b8c19f35530d5241bc2cdcb20d8433596bf3dc198ad78ce4`
and size 1,953,438 bytes. nextpnr packed the dedicated output flop into
`MISTRAL_SDROUT` on `MISTRAL_IO.89.8.1` (PIN_W15). Its reported Fmax is
329.489 MHz against the 50 MHz constraint. Utilization is one HPS GP, two
IO cells, and no PLL, memory or DSP. The current nextpnr pin `2c9f9c5`
with Yosys `fca8ca0a` reproduces those same RBF bytes. Exact-artifact kit
diagnostics on
2026-09-09 returned GPI signature `0x5D01` with changing paired fabric
beats for ten samples. Load JSON timed out; GPI and probe still passed.
`stop` completed development reboot recovery and left the lease free.
This does not measure the registered pin waveform and is not
external-interface timing closure.

`640_sdr_input` captures PIN_Y15 through a dedicated flop with
`FAST_INPUT_REGISTER ON`. GPI signature `0x5E01` identifies the fabric
beat protocol. Simulation uses the Verilog flop; it does not model analog
GPIO-register delay. nextpnr absorbs that flop into `MISTRAL_SDRIN` on the
existing GPIO BEL. The packed register is outside characterized timing.
The kit probe observes fabric counters, not the pin waveform. Simulation
and OSS are supported; Quartus comparison is not implemented. See
`experiments/640_sdr_input/expected.md`.

The OSS `640_sdr_input` artifact has SHA-256
`e450523a6cc81cbc33525d4baf8ea9b8a8efdfa73e9b6c3ce8385194240a759a`
and size 1,953,411 bytes. nextpnr packed the dedicated capture flop into
`MISTRAL_SDRIN` on `MISTRAL_IO.64.0.0` (PIN_Y15). Its reported Fmax is
269.687 MHz against the 50 MHz constraint. Utilization is one HPS GP, two
IO cells, and no PLL, memory or DSP. The current nextpnr pin `a1f8dbb5`
with Yosys `fca8ca0a` reproduces those same RBF bytes. Exact-artifact kit
diagnostics on
2026-09-09 returned GPI signature `0x5E01` with changing paired fabric
beats for ten samples. Load JSON timed out; GPI and probe still passed.
`stop` completed development reboot recovery and left the lease free.
This does not measure the registered pin waveform and is not
input-interface timing closure.

`650_ddr_input` captures PIN_Y15 through a width-one `altddio_in`. GPI
signature `0xDD02` identifies the fabric beat protocol. Simulation uses a
digital stand-in; it does not model analog GPIO-register delay. nextpnr
packs that cell into `MISTRAL_DDRIN` on the existing GPIO BEL. The packed
register is outside characterized timing. The kit probe observes fabric
counters, not the pin waveform. Simulation and OSS are supported; Quartus
comparison is not implemented. See `experiments/650_ddr_input/expected.md`.

The OSS `650_ddr_input` artifact has SHA-256
`e7f2aa1cccf4ee970b6a4e3811faff04aa0b7eb111f76859a3e93646d91d4afb`
and size 1,953,384 bytes. nextpnr packed `altddio_in` into `MISTRAL_DDRIN`
on `MISTRAL_IO.64.0.0` (PIN_Y15). Its reported Fmax is 323.625 MHz against
the 50 MHz constraint. Utilization is one HPS GP, two IO cells, and no
PLL, memory or DSP. The current nextpnr pin `9144784d` with Yosys
`fca8ca0a` reproduces those same RBF bytes. Exact-artifact kit diagnostics on 2026-09-09 returned
GPI signature `0xDD02` with changing paired fabric beats for ten samples.
Load JSON timed out; GPI and probe still passed. `stop` completed
development reboot recovery and left the lease free. This does not measure
the registered pin waveform and is not input-interface timing closure.

`660_ddr_data` drives PIN_W15 through a width-one `altddio_out` with two
changing fabric data nets. GPI signature `0xDD03` identifies the fabric
beat protocol. Simulation uses a digital stand-in; it does not model analog
GPIO-register delay. nextpnr packs that cell into `MISTRAL_DDROUT` on the
existing GPIO BEL, keeping both data nets. Complementary constant data
remains the separate clock-forwarding experiment. The packed register is
outside characterized timing. The kit probe observes fabric counters, not
the pin waveform. Simulation and OSS are supported; Quartus comparison is
not implemented. See `experiments/660_ddr_data/expected.md`.

The OSS `660_ddr_data` artifact has SHA-256
`0caf4bdff0d9019afd30ffc9e014c45d185526d28571ec5a65fc763b101d4843`
and size 1,953,267 bytes. nextpnr packed `altddio_out` into `MISTRAL_DDROUT`
on `MISTRAL_IO.89.8.1` (PIN_W15) with both `D_H` and `D_L` connected.
Its reported Fmax is 363.108 MHz against the 50 MHz constraint.
Utilization is one HPS GP, two IO cells, and no PLL, memory or DSP. The
current nextpnr pin `3e314db0` with Yosys `fca8ca0a` reproduces those
same RBF bytes. Exact-artifact kit diagnostics on 2026-09-10 returned GPI
signature `0xDD03` with changing paired fabric beats for ten samples.
Load JSON timed out; GPI and probe still passed. `stop` completed
development reboot recovery and left the lease free. This does not
measure the registered pin waveform and is not output-interface timing
closure.

`670_altiobuf` captures PIN_Y15 through `altiobuf_in`, drives PIN_W15
through `altiobuf_out`, and drives PIN_V16 through `altiobuf_bidir`.
GPI signature `0xAB01` identifies the fabric beat protocol. Simulation
uses digital stand-ins; it does not model analog pad delay. nextpnr folds
those cells into the existing `MISTRAL_IB`, `MISTRAL_OB` and `MISTRAL_IO`
BELs. The kit probe observes fabric counters, not pad waveforms.
Simulation and OSS are supported; Quartus comparison is not implemented.
See `experiments/670_altiobuf/expected.md`.

The OSS `670_altiobuf` artifact has SHA-256
`2420085fae159e394846dd370be2746576871e8a2eda629e0b77f66224d1d072`
and size 1,953,503 bytes. nextpnr packed `altiobuf_in` into
`MISTRAL_IO.64.0.0` (PIN_Y15), `altiobuf_out` into `MISTRAL_IO.89.8.1`
(PIN_W15), and `altiobuf_bidir` into `MISTRAL_IO.89.9.0` (PIN_V16) with
`I`/`OE`/`O` connected. Its reported Fmax is 339.905 MHz against the
50 MHz constraint. Utilization is one HPS GP, four IO cells, and no PLL,
memory or DSP. The current nextpnr pin `10a8e890` with Yosys `fca8ca0a`
reproduces those same RBF bytes. Exact-artifact kit diagnostics on 2026-09-10
returned GPI signature `0xAB01` with changing paired fabric beats for
ten samples. Load JSON timed out; GPI and probe still passed. `stop`
completed development reboot recovery and left the lease free. This
does not measure pad waveforms and is not I/O-interface timing closure.

`680_m10k_mix20be10` exposes a mixed-width M10K table on HPS GP: 512-by-20
byte-masked writes and 1024-by-10 reads. GPI signature `0xD425`. Locked
Yosys does not infer that combined mapping, so the experiment instantiates
one `MISTRAL_M10K` with `CFG_MIXED_WIDTH=1` and `CFG_BYTE_ENABLE=1`.
nextpnr maps `A1BE[0:1]` onto `BYTEENABLEA`. Place-and-route uses router1.
Simulation and OSS are supported; Quartus comparison is not implemented.
See `experiments/680_m10k_mix20be10/expected.md`.

The OSS `680_m10k_mix20be10` artifact has SHA-256
`bcfd525a14ecffdcb05b56d7244f4400aa2211b33eb4082c240a2b5e4df55282`
and size 1,958,112 bytes. nextpnr packed one `MISTRAL_M10K` at
`MISTRAL_M10K.5.33.0` with independent `A1BE` nets `wbe[0]` and `wbe[1]`.
Its reported Fmax is 537.634 MHz against the 50 MHz constraint.
Utilization is one M10K, one PLL, two clock enables, one HPS GP, and no
DSP or MLAB. The current nextpnr pin `9020c5f4` with Yosys `fca8ca0a`
reproduces those same RBF bytes. Exact-artifact kit diagnostics on 2026-09-10
returned GPI signature `0xD425` with initialized 10-bit lanes, isolated
`BYTEENABLEA[0]`/`BYTEENABLEA[1]` updates, a suppressed zero mask, and a
combined 20-bit write with the read clock stopped. Load JSON timed out;
GPI and probe still passed. `stop` completed development reboot recovery
and left the lease free.

`690_ddr_bidir` drives PIN_W15 through a width-one `altddio_bidir` with two
changing fabric data nets, connected registered inputs and `combout`, and a
constant-high output enable. GPI signature `0xDD04` identifies the fabric
beat protocol. Simulation uses a digital stand-in; it does not model analog
GPIO-register delay. nextpnr packs that cell into `MISTRAL_DDRBIDIR` on the
existing GPIO BEL. The packed register is outside characterized timing.
The kit probe observes fabric counters, not the pin waveform. Simulation
and OSS are supported; Quartus comparison is not implemented.
See `experiments/690_ddr_bidir/expected.md`.

The OSS `690_ddr_bidir` artifact has SHA-256
`e0f3aff0594067031d9296910f382d74413e1a3e40c31582342f7b8049410a16`
and size 1,953,559 bytes. nextpnr packed `altddio_bidir` into
`MISTRAL_DDRBIDIR` on `MISTRAL_IO.89.8.1` (PIN_W15) with `D_H`/`D_L`,
`Q_H`/`Q_L`, `O`, `OE`, and shared `CLK`/`CLKIN` connected. Its reported
Fmax is 334.560 MHz against the 50 MHz constraint. Utilization is one
HPS GP and no PLL, memory or DSP. The current nextpnr pin `d990fb2d`
with Yosys `fca8ca0a` reproduces those same RBF bytes. Exact-artifact kit diagnostics
on 2026-09-10 returned GPI signature `0xDD04` with changing paired fabric
beats for ten samples. Load JSON timed out; GPI and probe still passed.
`stop` completed development reboot recovery and left the lease free.
This does not measure the registered pin waveform and is not
bidirectional-interface timing closure.

`700_m10k_aclr` exposes a 512-by-20 dual-clock M10K table on HPS GP with
fabric `ACLR1` on GPO[5]. GPI signature `0xD426`. Locked Yosys has no
`ACLR` ports on `MISTRAL_M10K`, so OSS attaches GPO[5] after synthesis.
nextpnr maps that net onto `ACLR[1]` and enables the bottom output-clear
register. Simulation checks INIT and write/read; the kit probe checks that
asserting GPO[5] clears sampled `q` while leaving memory contents intact.
See `experiments/700_m10k_aclr/expected.md`.

The OSS `700_m10k_aclr` artifact has SHA-256
`90bea3cb8720ed917818bef87cee79efa38491a47092351b831547b3fb330f77`
and size 1,959,323 bytes. nextpnr packed one `MISTRAL_M10K` at
`MISTRAL_M10K.5.34.0` with fabric `ACLR1` and omitted `ACLR0`. Its
reported Fmax is 470.367 MHz against the 50 MHz constraint. Utilization
is one M10K, one PLL, one HPS GP, and no DSP or MLAB. The current
nextpnr pin `88cda8ae` with Yosys `fca8ca0a` reproduces those same RBF bytes.
Exact-artifact kit diagnostics on 2026-09-10 returned GPI signature
`0xD426`, INIT words, output-register clear on GPO[5], restored INIT
after release, a post-clear write, and restored written data after a
second clear. Load JSON timed out; GPI and probe still passed. `stop`
completed development reboot recovery and left the lease free.

`710_m10k_aclr_prim` instantiates one `MISTRAL_M10K` with `.ACLR1(gp_out[5])`
and `.ACLR0(1'b0)`. GPI signature `0xD427`. Yosys keeps `ACLR1` as a JSON
input; OSS does not patch that port. Simulation uses a digital stand-in that
clears the registered read output. nextpnr maps `ACLR1` onto `ACLR[1]`.
See `experiments/710_m10k_aclr_prim/expected.md`.

The OSS `710_m10k_aclr_prim` artifact has SHA-256
`896dd8c0672891622b3f5f7d53d727f26f7841f34050a7ff8d50b467e9761ea3`
and size 1,959,059 bytes. Yosys `75896890` emitted `ACLR1` onto `gp_out[5]`
and `ACLR0` as constant 0. nextpnr packed one `MISTRAL_M10K` with
`CFG_BYTE_ENABLE=1`. Its reported Fmax is 471.921 MHz against the 50 MHz
constraint. Utilization is one M10K, one PLL, one HPS GP, and no DSP or
MLAB. The current Yosys pin `75896890` with nextpnr `88cda8ae` reproduces
those same RBF bytes. Exact-artifact kit diagnostics on 2026-09-10 returned GPI
signature `0xD427`, INIT words, output-register clear on GPO[5], restored
INIT after release, a post-clear write, and restored written data after a
second clear. Load JSON timed out; GPI and probe still passed. `stop`
completed development reboot recovery and left the lease free.

`720_m10k_aclr_infer` infers a 512-by-20 dual-clock M10K table with an
asynchronous zero clear of the registered read output on GPO[5]. GPI
signature `0xD428`. Yosys maps that reset onto `ACLR1`; OSS does not patch
the port. Simulation checks INIT, write/read and the RTL async clear.
See `experiments/720_m10k_aclr_infer/expected.md`.

The OSS `720_m10k_aclr_infer` artifact has SHA-256
`7d1118689ea7f471ec38a25d96718990e21fdf60ad5aff871f7c506552747961`
and size 1,959,184 bytes. Yosys `da6373c0` inferred `ACLR1` onto `gp_out[5]`
and `ACLR0` as constant 0 with `CFG_BYTE_ENABLE=1`. nextpnr packed one
`MISTRAL_M10K` at `MISTRAL_M10K.5.33.0`. Its reported Fmax is 473.485 MHz
against the 50 MHz constraint. Utilization is one M10K, one PLL, one HPS
GP, and no DSP or MLAB. The current Yosys pin `da6373c0` with nextpnr
`88cda8ae` reproduces those same RBF bytes. Exact-artifact kit diagnostics on
2026-09-10 returned GPI signature `0xD428`, INIT words, output-register
clear on GPO[5], restored INIT after release, a post-clear write, and
restored written data after a second clear. Load JSON timed out; GPI and
probe still passed. `stop` completed development reboot recovery and left
the lease free.

`730_m10k_tdp_tclk` instantiates `MISTRAL_M10K_TDP` with `CLK2` and `B1EN`
tied low. GPI signature `0xD429`. nextpnr folds that constant clock off
`CLKIN[1]`. Simulation checks A-port INIT and write/read. See
`experiments/730_m10k_tdp_tclk/expected.md`.

The OSS `730_m10k_tdp_tclk` artifact has SHA-256
`2e197f83df95392073bd8cb68f14d4e97058e2be3df3800818940160f1326e27`
and size 1,959,552 bytes. nextpnr packed `MISTRAL_M10K.26.2.0` with live
`CLK1` and disconnected `CLK2`. Its reported Fmax is 342.583 MHz against
the 50 MHz constraint. Utilization is one M10K, one HPS GP, and no PLL,
DSP or MLAB. The current nextpnr pin `d8a96b58` with Yosys `da6373c0`
reproduces those same RBF bytes. Exact-artifact kit diagnostics on 2026-09-11
returned GPI signature `0xD429`, initialized A-port words, an A-port
write with the B clock tied off, and an undisturbed neighbour address.
Load JSON timed out; GPI and probe still passed. `stop` completed
development reboot recovery and left the lease free.

`740_m10k_dual_pll` exposes a 512-by-20 dual-clock M10K table plus a second
74.25 MHz PLL. GPI signature `0xD42A`. Default router2 may retry with
router1 when two PLLs and one M10K have less than 10% margin. Simulation
and OSS are supported. See `experiments/740_m10k_dual_pll/expected.md`.

The OSS `740_m10k_dual_pll` artifact has SHA-256
`5489e8e697b1203113b95e76412e7e99b5076bfead0fc1628d58cd52afe55c35`
and size 1,959,462 bytes. Utilization is two `altera_pll` cells, one M10K,
one HPS GP, and no DSP. Router2 produced those bytes. Reported Fmax is
437.254 MHz against 50 MHz and 355.240 MHz against 74.25 MHz. The current
nextpnr pin `9cdc03cc` with Yosys `da6373c0` reproduces those same RBF bytes.
Exact-artifact kit diagnostics on 2026-09-11 returned GPI signature
`0xD42A`, initialized words, a write on the independent read clock, and an
undisturbed neighbour address. Load JSON timed out; GPI and probe still
passed. `stop` completed development reboot recovery and left the lease
free.

`750_dsp18x19` exposes two unsigned 18x19 products from one
`MISTRAL_MUL18X19` cell. GPO `[7:0]` is A, `[15:8]` is B, `[23:16]` is C,
and bit 24 selects the second product. D is the constant `19'd3`. GPI
signature `0xD619`; `[15:0]` are the selected product (`A*B` or `C*3`).
OSS `chtype`s a blackbox `dsp18x19` to `MISTRAL_MUL18X19` because locked
Yosys has no 18x19 cell. Memory and PLL remain forbidden. Simulation and
OSS are supported; Quartus comparison is not implemented. See
`experiments/750_dsp18x19/expected.md`.

The OSS `750_dsp18x19` artifact has SHA-256
`2af0beae2dba60bc85d6a288f6130f7cdbac017b477ad7df89dd1aecf663bdfa`
and size 1,955,194 bytes. nextpnr packed one `MISTRAL_MUL18X19` at
`MISTRAL_MUL18X19.32.8.5`. Its reported Fmax is 322.269 MHz against the
50 MHz constraint. Utilization is one `MISTRAL_MUL18X19` (112 available)
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-11 returned GPI
signature `0xD619` and the expected products (`10*12` → `120`, `7*3` →
`21`, `16*16` → `256`, `5*3` → `15`). Load JSON timed out; GPI and probe
still passed. `stop` completed development reboot recovery and left the
lease free. The current nextpnr pin `d22eaef1` with Yosys `da6373c0`
reproduces those same RBF bytes.

`760_pll_52` measures PIN_V11 50 MHz → 52 MHz through one `altera_pll`.
GPI signature `0xD752`. The 520 MHz feedback profile supplies that rate
as C6=10. Simulation uses a digital toggling stand-in. Memory and DSP
remain forbidden. Simulation and OSS are supported; Quartus comparison
is not implemented. See `experiments/760_pll_52/expected.md`.

The OSS `760_pll_52` artifact has SHA-256
`2de17cd1773f78607b4a1182ce7ee5e5d135b0b88f6a55380a5528877a15487c`
and size 1,955,813 bytes. nextpnr packed `50 MHz -> 52 MHz, direct, M=52
N=5 C6=10` at `altera_pll.0.14.0`. Reported Fmax is 219.539 MHz against
50 MHz and 329.164 MHz against 52 MHz. Utilization is one `altera_pll`
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-11 returned GPI
signature `0xD752` and count 4260 on three successive measurements with
lock asserted and no sampled lock loss. Load JSON timed out; GPI and
probe still passed. `stop` completed development reboot recovery and
left the lease free. The current nextpnr pin `4d055dae` with Yosys
`da6373c0` reproduces those same RBF bytes.

`770_m10k_async_read` instantiates one `MISTRAL_M10K` with a combinational
read port. GPI signature `0xD42B`. Native Yosys emits `CFG_ASYNC_READ`
with a constant-high `B1EN`; nextpnr routes the physical read enable and
keeps `B1ADDR`/`B1DATA` combinational. Read-only cells may fold their unused
write clock to a constant. There is no second PLL. Simulation uses a digital
combinational stand-in. The 1.5 ns host arc is an estimate. Simulation and
OSS are supported; Quartus comparison is not implemented. See
`experiments/770_m10k_async_read/expected.md`.

The current OSS `770_m10k_async_read` artifact has SHA-256
`f28438b7785f88b71a6f9c4e019d2cee3913ca124b8d1d6d314d8244dfc6de4d`
and size 1,959,763 bytes. It reports 458.505 MHz against the 50 MHz
constraint and uses one M10K plus one HPS GP. This is host-only evidence
for the native Yosys/nextpnr pair; no kit result is claimed for this
artifact.

`780_quartus_sdc` measures PIN_V11 50 MHz → 25 MHz through one
`altera_pll`, using Quartus SDC/QSF forms. GPI signature `0xD780`.
Simulation uses the 090 digital toggling stand-in. Memory and DSP remain
forbidden. Simulation and OSS are supported; Quartus comparison is not
implemented. See `experiments/780_quartus_sdc/expected.md`.

The OSS `780_quartus_sdc` artifact has SHA-256
`639d0a9fc10b3a96947bdb64c865b94509ab499be05f47da98ec999fa2fd07ba`
and size 1,955,806 bytes. nextpnr accepted `derive_pll_clocks`,
`get_clocks`, multiline `set_clock_groups`, and `-entity`. Reported Fmax
is 211.551 MHz against 50 MHz and 354.233 MHz against 25 MHz.
Utilization is one `altera_pll` and one HPS GP. Exact-artifact kit
diagnostics on 2026-09-11 returned GPI signature `0xD780` and count 2048
on three successive measurements with lock asserted. Load JSON timed
out; GPI and probe still passed. `stop` completed development reboot
recovery and left the lease free. The current nextpnr pin `1e1745dc` with
Yosys `da6373c0` reproduces those same RBF bytes.

`790_m10k_addrstall` instantiates one `MISTRAL_M10K_TDP` and stalls the
A-port address from GPO[29]. GPI signature `0xD42C`. Locked Yosys has no
stall ports, so OSS attaches `ADDRSTALLA` after synthesis. Packed
`ADDRSTALLA` holds the A-port address while GPO[29] is 0 and samples a
new address while GPO[29] is 1. See
`experiments/790_m10k_addrstall/expected.md`.

The OSS `790_m10k_addrstall` artifact has SHA-256
`3c581d081fbe42e11e5d0dd8bc0b1a2fb33fa7a6be953193d960ec11b0fe131d`
and size 1,959,683 bytes. nextpnr packed `MISTRAL_M10K.26.2.0` with a
routed `ADDRSTALLA` GOUT. Reported Fmax is 342.700 MHz against 50 MHz.
Utilization is one M10K and one HPS GP. Exact-artifact kit
diagnostics on 2026-09-11 returned GPI signature `0xD42C`, INIT `0xA6`
at address 0, a held A-port address while GPO[29] was 0, and the new
address after GPO[29] was 1. Load JSON timed out; GPI and probe still
passed. `stop` completed development reboot recovery and left the
lease free. The current nextpnr pin `74aab451` with Yosys `da6373c0`
reproduces those same RBF bytes.

`800_m10k_out_reg` instantiates one `MISTRAL_M10K` and registers the
B-port read. GPI signature `0xD42D`. Locked Yosys has no output-register
parameter, so OSS sets `CFG_OUT_REG_B` after synthesis. The first sample
after an address change still holds the previous word. See
`experiments/800_m10k_out_reg/expected.md`.

The OSS `800_m10k_out_reg` artifact has SHA-256
`4c6322cb2bd4a72d5ac4c8b5854257b957bc611b6b0b85d57b5f473cc72f3eda`
and size 1,960,392 bytes. nextpnr packed `MISTRAL_M10K.26.1.0` with
`CFG_OUT_REG_B=1` and decompiled `B_OUTPUT_SEL=REG`. Reported Fmax is
241.604 MHz against 50 MHz. Utilization is one M10K and one HPS GP.
Exact-artifact kit diagnostics on 2026-09-11 returned GPI signature
`0xD42D`, INIT at late samples, a held previous word on the first
sample after an address change, and the new word on a later sample.
Load JSON timed out; GPI and probe still passed. `stop` completed
development reboot recovery and left the lease free. The current
nextpnr pin `fd862a2c` with Yosys `da6373c0` reproduces those same
RBF bytes.

`810_m10k_async_defaults` instantiates one `MISTRAL_M10K` with a
combinational read port packed to the initialized async defaults. GPI
signature `0xD42E`. Locked Yosys still emits a clocked read enable, so
OSS sets `CFG_ASYNC_READ` and drops `B1EN` and `CLK2`. See
`experiments/810_m10k_async_defaults/expected.md`.

The current OSS `810_m10k_async_defaults` artifact has SHA-256
`aa8c542e0ae22e60c1c975fe526a13fc0ea638b166107d7945993d227f9b5ef9`
and size 1,959,790 bytes. It reports 422.476 MHz against the 50 MHz
constraint and uses one M10K plus one HPS GP. This is host-only evidence
for the native Yosys/nextpnr pair; no kit result is claimed for this
artifact.

`820_m10k_async_enable` instantiates one `MISTRAL_M10K` with a
combinational read port and a packer-generated constant-high
`ENABLE[0]`. GPI signature `0xD42F`. Locked Yosys still emits a clocked
read enable, so OSS sets `CFG_ASYNC_READ` and drops `B1EN` and `CLK2`.
See `experiments/820_m10k_async_enable/expected.md`.

The current OSS `820_m10k_async_enable` artifact has SHA-256
`51716efaf9d0367efbb99ba8fb69bc41e3d31bfe8d807fdbfd562d45fefc3291`
and size 1,959,856 bytes. It reports 464.037 MHz against the 50 MHz
constraint and uses one M10K plus one HPS GP. This is host-only evidence
for the native Yosys/nextpnr pair; no kit result is claimed for this
artifact.

`830_pll_frac_27` measures PIN_V11 50 MHz → 27 MHz through one
`altera_pll` with `fractional_vco_multiplier("true")`. GPI signature
`0xD827`. The rate is a generic calculator profile, not a Quartus
compatibility word. See `experiments/830_pll_frac_27/expected.md`.

The OSS `830_pll_frac_27` artifact has SHA-256
`8d6d433e64ab4c77faae85c2ed7c9393564e34057617d30bb435414c198aa7e6`
and size 1,955,806 bytes. nextpnr packed FPLL (0,14) with N bypass, M=8,
C6=15 (high 8/low 7, odd-duty), fractional word `0x1999999a`, DSM
enabled, BWCTRL 7, and auxiliary bandgap powerdown. Reported Fmax is
219.394 MHz against 50 MHz and 329.164 MHz against 27 MHz. Utilization
is one PLL, two clock enables, and one HPS GP. Exact-artifact kit
diagnostics on 2026-09-12 returned GPI signature `0xD827` and count 2212
on three successive measurements with lock asserted and no sampled lock
loss. Load JSON timed out; GPI and probe still passed. `stop` completed
development reboot recovery and left the lease free. The current nextpnr
pin `91420055` with Yosys `ec34fcf3` reproduces those same RBF bytes.

`840_m10k_rdw` instantiates one `MISTRAL_M10K_TDP` with an explicit
same-port `NEW_DATA_NO_NBE_READ` contract. GPI signature `0xD840`.
Locked Yosys omits the RDW parameters, so OSS sets them after synthesis.
See `experiments/840_m10k_rdw/expected.md`.

The OSS `840_m10k_rdw` artifact has SHA-256
`1188a95b284a175cc4cdc0ded85ec2404928f93415630e559b8716dda56a00e2`
and size 1,959,341 bytes. nextpnr packed `MISTRAL_M10K.26.1.0` with
`CFG_RDW_MODE_A`/`CFG_RDW_MODE_B=NEW_DATA_NO_NBE_READ`,
`CFG_RDW_MODE_MIXED=DONT_CARE`, and decompiled `TRUE_DUAL_PORT=1`,
`A_DATA_FLOW_THRU=1`, `B_DATA_FLOW_THRU=1`. Reported Fmax is 377.643 MHz
against 50 MHz. Utilization is one M10K and one HPS GP. Exact-artifact kit
diagnostics on 2026-09-12 returned GPI signature `0xD840`, INIT `0xA6`
at address 0, same-port NEW_DATA write-through of `0x155` at address 7,
and an undisturbed neighbour. Load JSON timed out; GPI and probe still
passed. `stop` completed development reboot recovery and left the lease
free. The current nextpnr pin `47c4251a` with Yosys `ec34fcf3` reproduces
those same RBF bytes.

`850_hps_location` instantiates one `cyclonev_hps_interface_peripheral_i2c`
without an RTL BEL attribute. QSF `HPS_LOCATION
HPSINTERFACEPERIPHERALI2C_X52_Y60_N111` must pack
`cyclonev_hps_interface_peripheral_i2c.52.60.0`. GPI signature `0xD850`.
See `experiments/850_hps_location/expected.md`.

The OSS `850_hps_location` artifact has SHA-256
`ec6e0f803c6c122eb69bb36b4be56c910ef7d6fec8f0391f9db8507f808f5890`
and size 1,952,976 bytes. nextpnr packed
`cyclonev_hps_interface_peripheral_i2c.52.60.0` from the QSF assignment.
Reported Fmax is 621.891 MHz against 50 MHz. Utilization is one HPS I2C
cell and one HPS GP. Exact-artifact kit diagnostics on 2026-09-12
returned GPI `0xD85000A6` on three successive reads. Load JSON timed
out; GPI and probe still passed. `stop` completed development reboot
recovery and left the lease free. The current nextpnr pin `a3e9b19a`
with Yosys `ec34fcf3` reproduces those same RBF bytes.

`860_m10k_selectors` instantiates two dual-clock `MISTRAL_M10K` cells with
live `CLK1`/`CLK2` and distinct INIT. GPI signature `0xD860`. See
`experiments/860_m10k_selectors/expected.md`.

The OSS `860_m10k_selectors` artifact has SHA-256
`14b04913dc29ac9ce519ad4d349e9c48ac2effd8a997f72fcad3d70b1c290832`
and size 1,963,043 bytes. nextpnr packed `MISTRAL_M10K.26.1.0` and
`MISTRAL_M10K.26.2.0` with live `CLKIN.0`/`CLKIN.1`, `ENABLE.0`/`ENABLE.1`,
`WREN.0`, and independent-clock `BOT_CLK_SEL`/`BOT_1_*` selectors.
Reported Fmax is 325.627 MHz against 50 MHz. Utilization is two M10Ks
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-12 returned
GPI `0xD86000A6`, bank-0 INIT, distinct bank-1 INIT, and a bank-0 write
that left bank 1 undisturbed. Load JSON timed out; GPI and probe still
passed. `stop` completed development reboot recovery and left the lease
free. The current nextpnr pin `8bd48754` with Yosys `ec34fcf3` reproduces
those same RBF bytes.

`870_m10k_narrow` instantiates one 8192-by-1 `MISTRAL_M10K_TDP`. GPI
signature `0xD870`. See `experiments/870_m10k_narrow/expected.md`.

The OSS `870_m10k_narrow` artifact has SHA-256
`1c0cffd2b67283a2cab36edf4a343c69b4e6b24b93f0102927e293441bbf5431`
and size 1,957,568 bytes. nextpnr packed `MISTRAL_M10K.26.1.0` with
`CFG_ABITS=13`, `CFG_DBITS=1`, `CFG_TDP=1`, scalar `A1DATA`/`A1Q`,
decompiled `TRUE_DUAL_PORT=1`, `A_DATA_WIDTH=1`, live `CLKIN.0`/`CLKIN.1`,
and `ENABLE.0`/`ENABLE.1`/`WREN.0`/`WREN.1`. Reported Fmax is 446.628 MHz
against 50 MHz. Utilization is one M10K and one HPS GP. Exact-artifact kit
diagnostics on 2026-09-12 wrote 0 at address 0 and 1 at address 7 and
confirmed the neighbour. Logical INIT order does not match the physical
8192x1 map. Load JSON timed out; GPI and probe still passed. `stop`
completed development reboot recovery and left the lease free. The current
nextpnr pin `2d3c216a` with Yosys `ec34fcf3` reproduces those same RBF
bytes.

`880_m10k_async_rom` instantiates one 1024-by-10 read-only `MISTRAL_M10K`
with a folded write clock. GPI signature `0xD880`. See
`experiments/880_m10k_async_rom/expected.md`.

The OSS `880_m10k_async_rom` artifact has SHA-256
`eb7ae06559e8d716bd89ba2dd05015720611b834ac91b1b92c38522a1a7b4ab9`
and size 1,955,929 bytes. nextpnr packed `MISTRAL_M10K.26.79.0` with
`CFG_ABITS=10`, `CFG_DBITS=10`, `CFG_ASYNC_READ=1`, no `CFG_BYTE_ENABLE`,
a borrowed live `CLK1`, decompiled `TOP_CE0_SEL=1`, `BOT_CLK_SEL=1`,
`A_DATA_WIDTH=10`, and routes on `CLKIN.0`/`CLKIN.1`/`ENABLE.0`.
Reported Fmax is 918.274 MHz against 50 MHz. Utilization is one M10K
and one HPS GP. Exact-artifact kit diagnostics on 2026-09-13 returned
GPI `0xD88000A6` and INIT words at low, mid, and last addresses. Load
JSON timed out; GPI and probe still passed. `stop` completed development
reboot recovery and left the lease free. The current nextpnr pin
`9cbbf735` with Yosys `ec34fcf3` reproduces those same RBF bytes.

The current `kit.py` close completed development reboot recovery and left the
lease free. This is exact-artifact functional diagnostic acceptance of M18
multiply, native M27 multiply with omitted controls, M9 preadder subtract,
M18 `A*B+C`, registered M18 with omitted enable/ACLR, initialized MLAB
contents, independent-clock 20-bit and 40-bit M10K simple dual-port RAM,
20-bit M10K byte-enable lanes with independent clocks, mixed-width 40↔10
M10K simple dual-port RAM, mixed-width 20-to-10 M10K byte-enable lanes,
independent-clock 10-bit and 20-bit M10K true
dual-port RAM, independent-clock byte-masked 20-bit and 16-bit TDP M10K RAM,
independent-clock mixed-width 20/10, 10/20, 16/8 and 8/16 TDP M10K RAM, the
50→74.25 MHz fractional-N PLL profile, dedicated 50 MHz DDR clock
forwarding (fabric GPI only), dedicated SDR output registers (fabric GPI
only; GPIO-register timing uncharacterized), and dedicated SDR input
registers (fabric GPI only; GPIO-register timing uncharacterized), and
dedicated DDR input registers (fabric GPI only; GPIO-register timing
uncharacterized), and dedicated DDR bidirectional I/O registers (fabric GPI
only; GPIO-register timing uncharacterized), and M10K asynchronous output
clear (fabric GPI only), and an explicit `MISTRAL_M10K` primitive with
Yosys-emitted `ACLR1` (fabric GPI only), and an inferred `ramstyle=M10K`
asynchronous read-output clear (fabric GPI only), and a TDP M10K unused-clock
TCLK fold (fabric GPI only), and a dual-PLL M10K design on default
router2 (fabric GPI only), and native 18x19 dual products (fabric GPI
only), and a 50→52 MHz integer PLL on the 520 MHz feedback profile
(fabric GPI only), and a combinational M10K read port with packer
`ENABLE[0]` (fabric GPI only), and a 50→25 MHz PLL routed with Quartus
SDC/QSF forms (fabric GPI only), and a TDP M10K A-port address stall
(fabric GPI only), and a registered M10K B-port read (fabric GPI only),
and a combinational M10K read packed to initialized async defaults
(fabric GPI only), and a combinational M10K read with packer
constant-high `ENABLE[0]` (fabric GPI only), and a 50→27 MHz
fractional-N PLL from the bounded calculator (fabric GPI only), and a
TDP M10K same-port NEW_DATA write-through (fabric GPI only), and an HPS
I2C cell placed from QSF `HPS_LOCATION` (fabric GPI only), and two
dual-clock M10Ks with unique sites and packed `ENABLE.1` (fabric GPI
only), and an 8192x1 true-dual-port M10K (fabric GPI only), and a
1024x10 read-only async M10K ROM (fabric GPI only). It does
not establish native
game acceptance.

## FES ColecoVision first slice

`cores/fes-coleco` is the next FES emulator bring-up after Pong and ZX81. It
uses the [MiSTer ColecoVision core](https://github.com/MiSTer-devel/ColecoVision_MiSTer)
as a system reference, but is a reduced Verilog-first adapter around the
existing `fes.simple-computer` 1.0 mailbox. The first slice contains a TV80
Z80-compatible CPU, the Coleco reset/cartridge/RAM map, bounded TMS9918-style
Graphics I and Graphics II video, two active-low controller views and the FES
fixed-video shell. A raw 1–16 KiB cartridge image is mirrored through the
16 KiB `0x8000–0xffff` aperture. Audio, BIOS services, expansion hardware, bank
switching, full VDP modes, cycle-perfect timing and retail-cartridge
compatibility remain outside this slice.

Graphics II covers normal 8x8/16x16 sprites, magnification, early-clock
positioning, signed/clipped X coordinates, transparency/priority, four visible
sprites per line, collision and fifth-sprite status. The VDP uses four coherent
VRAM copies with broadcast CPU writes, registered read-ahead and a serial SAT /
pattern walker. Two alternating framebuffer line banks use packed 4-bit M10K
entries for pixel and visibility metadata; publication is interlocked with the
registered raster coordinate.

The serial renderer, replicated VRAM, registered request/wait schedule, packed
line banks, sequential clear and publication interlock are deliberate RTL
scaling accommodations shared by both compiler lanes. The original procedural
sprite loop expanded to roughly 42K mapped combinational cells; the registered
one-column/repeat schedule fits the fixed system-clock budget. The Coleco OSS
recipe uses its core-local lock with Yosys `da6373c0`, nextpnr-mistral
`2d3c216` with `--router gpu` and seed 5, and Mistral `b28e30a`; the selected
toolchain enables the HIP device backend. The Quartus wrapper retains
the literal `altsyncram` mode `NEW_DATA_NO_NBE_READ`; OSS preserves the
registered semantic schedule rather than that vendor literal. No missing
nextpnr BEL or pack feature is implied.

The open diagnostics and focused simulations exercise CPU-driven VDP writes,
controller modes, sprite status and the exact 720p frame in both conditional
lanes. The build recipes require a clean checkout and seal format-2 packages;
they never program hardware. Parent selection and exact-kit acceptance are
recorded in the FES validation documents.

### OSS/Yosys/nextpnr workarounds

These are the portability accommodations to hand to the Yosys/nextpnr/Mistral
owner. The RTL scheduling choices are shared by both compiler lanes; entries
marked as path-specific are not requirements of the other lane.

| Boundary | Current accommodation and ownership |
| --- | --- |
| Toolchain selection | The repository-wide lock remains on current mainline Yosys/nextpnr. Coleco's OSS recipe selects `cores/fes-coleco/toolchain.lock`, builds it under `build/toolchain/fes-coleco`, and enables the HIP device backend; Quartus uses its own vendor tools and needs neither lock. |
| Verilog/VHDL frontend | OSS uses Verilog TV80/T80pa with `TV80_REFRESH=1`; Quartus may retain its VHDL T80pa path. This is an OSS frontend choice, not a nextpnr gap. |
| Machine RAM | Both lanes use registered-address RAM semantics. OSS selects `coleco_dpram` with registered `ram_style="m10k_tdp"`; Quartus uses `altsyncram`. Default simulation alone keeps asynchronous reads. |
| Registered media bridge | Both lanes prime the mailbox result, delay the cartridge write address, flush the final byte, and re-arm on `media_ready` falling or reset rising. This is required by the registered memory schedule in both lanes. |
| VDP multi-read VRAM | A single VRAM with one CPU port and three combinational raster reads fails OSS mapping and leaves Quartus with an oversized direct-memory implementation. Both lanes use four coherent copies, broadcast CPU writes, and pipelined name-to-pattern/color reads; the fourth copy feeds the serial sprite walker. |
| Sprite line banks | Both lanes use alternating 256-entry packed 4-bit M10K entries, registered renderer read/write phases, a sequential clear and a raster-coordinate publication interlock. This keeps the renderer inside the system-clock budget and avoids publishing a line into the preceding framebuffer row. |
| Read-during-write mode | Quartus 17.0.2 rejects `OLD_DATA` for the bidirectional packed sprite shape, so the Quartus primitive uses `NEW_DATA_NO_NBE_READ`. The renderer consumes `q_a` one phase later; OSS preserves that schedule without depending on the Quartus literal. |
| Sprite rendering | The procedural 16x2 loop expanded to about 42K mapped combinational cells and stalled routing. Registered column/repeat counters issue one source-pixel read/write pair per system clock and pack pixel, occupied and visible metadata, reducing the measured fabric to about 3.1K ALUT cells. This source-level scaling is shared by both lanes. |
| Bulk initialization | `initial` loops over 16 KiB VRAM, cartridge RAM or the 49,152-entry framebuffer create large memory initialization structures. The bring-up initializes scalar state only and clears active line storage sequentially. |
| Reset image | OSS consumes tracked byte-per-line `coleco_reset_rom.hex`; Quartus `altsyncram` consumes tracked range-form `coleco_reset_rom.mif`. This is a file-format split, not a different reset image. |
| PLL and I²C | Both retain the two existing `altera_pll` wrappers. Quartus uses tri-state HDMI I²C; OSS uses `MISTRAL_IO` open-drain pads and the HPS I²C BEL `cyclonev_hps_interface_peripheral_i2c.52.60.0`. |
| Constraints | OSS uses only its accepted pin QSF and 50 MHz `clocks-oss.sdc`; nextpnr derives PLL clocks. Quartus retains `HPS_LOCATION`, clock groups and the full SDC. |
| Route pressure | The OSS reproduction is `5CSEBA6U23I7`, nextpnr `2d3c216`, `--router gpu`, seed 5, no `--tmg-ripup`, at 74.25 MHz. Seeds 3, 4 and 5 passed host routing. The sealed recipe requires `backend hip:<device> ready` and rejects CPU-reference fallback; no missing BEL or pack feature was identified. |

The concrete build entry points are `make build-fes-coleco-quartus` and
`make build-fes-coleco`; both require a clean source checkout, seal format-2
packages and never program hardware. Exact-artifact kit acceptance remains a
separate FES integration step.

## Standalone Pong game

`cores/pong/rtl/pong_game.sv` implements a deterministic 320x240 game module.
It is not a programmable MiSTer core: board timing, the HPS interface, HDMI
and audio transport require a separate compatible wrapper.

One player moves the left paddle with Up/Down (opposing inputs cancel), against
an opponent moving at most one pixel per frame. Paddle impact position determines
upward/downward return, with slower center shots and faster edge shots. Start
begins a rally on a fresh press; a point returns the ball to its center serve position and waits for
another press. Decimal scores wrap after nine. Reset clears scores and restores
the same positions and initial left/down trajectory every time.

The synchronous interface takes `clk`, `reset`, `frame_tick`, `up`, `down`,
`start`, `freeze`, 2-bit `paddle_speed` and 10-bit `pixel_x`/`pixel_y`. A one-clock `frame_tick` advances game
state; raster coordinates select RGB pixels independently. Outputs are 8-bit
`red`/`green`/`blue`, `tone`, `playing`, integer top-left positions
`ball_x`/`ball_y`/`player_y`/`ai_y`, and 4-bit decimal scores
`player_score`/`ai_score`, plus single-clock `player_return` and `point` event
pulses. Speed enums 0/1/2 select 2/4/6 pixels per frame; movement saturates at
0 and 208 for all three speeds. Freeze holds positions, scores, serve/start
state and tone counters; raster generation remains independent. The original
MiSTer wrapper ties freeze low and selects normal speed (enum 1).
Paddles are 4x32 at x=12 and x=304; the ball is 4x4.
Out-of-range raster coordinates produce black. Seven-segment score glyphs
appear above the playfield. `CLOCK_HZ` sets a 1 kHz square-wave collision/score
tone lasting approximately 100 ms; set it to the wrapper's actual game clock
(at least 2 kHz). The tone is a logic signal, not an audio device interface.

Run `make sim-pong VERILATOR=/absolute/path/to/existing/verilator` (or omit
the override when Verilator is on PATH). This uses the installed compiler,
never bootstraps a toolchain, and writes `build/sim/pong/` and
`build/sim/pong-video/`. Its real RTL harness checks reset, input bounds,
frame gating, start/restart, wall and paddle
bounces, both scoring sides, pixels and tone expiry. The test uses an 8 kHz
clock parameter to exercise sound cheaply. The same target tests the separate
`pong_video.sv` raster: 320x240 active pixels, 424x262 totals, and a pixel enable
every three cycles of a 20 MHz clock (approximately 60.01 Hz). That module
supplies coordinates, syncs, active-video indication and one frame tick. Neither
module includes the board/HPS wrapper. Simulation is not hardware evidence.

## FES GP and fixed 720p Pong shell

`cores/fes-pong/rtl/fes_gp.v` implements `fes.simple-game` 1.0 on the Cyclone V
HPS general-purpose port. Its clock is the 74.25 MHz pixel domain. Ports include
32-bit HPS `gpo`, fixed 128-bit `build_id`, 32-bit FPGA `gpi`, gameplay
`game_reset`, `game_frozen`, `buttons[7:0]`, and `paddle_speed[1:0]`, plus
`player_return`/`point` inputs from the shared game. FPGA configuration state starts with a
stable `0xF5` signature, ACK zero, gameplay held in reset and neutral input.
The request toggle crosses two `async_reg` stages. The state machine consumes
the held payload only when that synchronized toggle differs from the retained
ACK, updates response and ACK together, and never changes ACK for gameplay
reset. Invalid opcode, index and argument responses have no gameplay effect.

The checked-in `cores/fes-pong/generated/fes_gp.vh` is the unedited Verilog
emitter output from `mister-packages/packages/abi/fes_simple_game.yaml`.
`cores/fes-pong/generated/persistence-exchanges.json` is the unedited shared
persistence fixture. It declares request toggle zero, synthetic build ID
`00112233445566778899aabbccddeeff`, and all four live capability bits (15).
The retained `exchanges.json` remains the original volatile fixture with bits
0/1 only; the persistent core runs the newer sequence. `make sim-fes-pong`
verifies exact GPI words, varied host-to-FPGA edge placement, field holding,
one effect per toggle and error isolation.

## FES simple-computer mailbox

`cores/fes-zx81/rtl/fes_computer_gp.v` implements `fes.simple-computer` 1.0 on
the same GPO/GPI transport. It holds execution in reset, keeps eight active-low
keyboard rows at `0x1f`, and accepts a 1..16384-byte media blob through
begin/data/commit. Hold reset clears in-flight media and the keyboard; a
committed blob stays. The checked-in `cores/fes-zx81/generated/fes_simple_computer.vh`
and `exchanges.json` are unedited mister-packages outputs. `make sim-fes-zx81`
plays that fixture and checks keyboard/media side effects.

## FES ZX81 machine simulation

`cores/fes-zx81/rtl/zx81_machine.sv` is the first-slice ZX81 extracted from
MiSTer-devel/ZX81_MiSTer `ZX81.sv` at Release 20260603: 16 KB RAM, PAL, no
CHROMA/QS/YM2149/joystick. Keyboard rows and `.p` tape bytes come from the GP
mailbox. Character ROM bytes are `cores/fes-zx81/rtl/zx8x.hex`, converted from
the pinned `rtl/zx8x.mif`. The Z80 is TV80 (`66a131c`) wrapped as `T80pa` with
Sorgelig half-cycle `CEN_p`/`CEN_n` timing, WAIT via CEN gating, and
`TV80_REFRESH`. NMI is sampled every clock, matching T80.vhd. `CEN_p` is
3.25 MHz from the 52 MHz enable divider.

`make sim-fes-zx81` also runs `Vzx81_machine`, which waits until NEW has built
a display file at `D_FILE` starting with `0x76`, the CPU has HALTed for slow
display, and the ULA has emitted visible pixels. It then types `LOAD ""` on
the 40-key matrix (J, SHIFT+P, SHIFT+P, ENTER) and checks that the `$0347`
tape-loader patch consumes a 16-byte `.p`. `LOAD ""` always hits that
patch: a committed mailbox blob is copied into RAM; with no blob the
patch sets carry immediately so BASIC reports `0/0` instead of hanging
in the original cassette waiter with the display off. A 720p raster module
`zx81_video_720p.v` integer-scales the 6.5 MHz capture into 1650×750 timing.
This is simulation, not a Quartus RBF or kit result.

## FES ZX81 Quartus bring-up

`make build-fes-zx81-quartus` is the Quartus Prime Lite 17.0.2 recipe for
`fes.zx81` 1.0.0. It is not a Mistral/nextpnr payload. The board shell
`cores/fes-zx81/rtl/top.v` uses two `altera_pll` cells from the 50 MHz V11
reference: 52 MHz system (T80, ULA, mailbox) and 74.25 MHz pixel (HDMI
1650×750). HDMI RGB/HS/VS/CLK pins and U10/AA4 match FES Pong. The HPS I2C
cell is at `HPSINTERFACEPERIPHERALI2C_X52_Y60_N111`; `out_clk`/`out_data`
pull SCL/SDA low and `scl`/`sda` read the pads (Quartus assign-to-Z in place
of Pong's `MISTRAL_IO`). The Z80 is VHDL T80pa from ZX81_MiSTer Release
20260603; Verilator keeps TV80.

The compile defines `QUARTUS=1`. ROM, 16 KB RAM and the 16 KB media blob
instantiate `altsyncram` bidirectional dual-port M10K with unregistered
outputs; ROM init is `zx8x.mif`. Simulation keeps inferred combo-read RAM
and `zx8x.hex`. The 720p capture buffer is a one-dimensional M10K array
written on `clk_sys` and registered on `pixel_clk`.

The recipe requires `QUARTUS_ROOTDIR`, version 17.0.2, a clean checkout and
tracked inputs. It writes canonical `build/fes-zx81-quartus/build-inputs.json`
before compile, embeds that record's 128-bit id as `BUILD_ID`, runs
`quartus_sh --flow compile top`, requires TimeQuest multicorner
Setup/Hold/Recovery/Removal/Minimum Pulse Width worst-case slack ≥ 0 with
the 52 MHz system clock and the derived 74.25/74.27 MHz pixel clock named,
then seals `manifest.toml` + `core.rbf` through the existing format-2
exporter. Failed compiles delete the RBF, manifest and passing summary.
The command never programs hardware. Quartus remains the kit-proven
bring-up lane.

## FES ZX81 OSS package

`make build-fes-zx81` is the Yosys/nextpnr-mistral recipe for the same
`fes.zx81` 1.0.0 package. It authenticates the pinned tools, writes
`build/fes-zx81-oss/build-inputs.json` before synthesis, and embeds that
record's 128-bit id as `BUILD_ID`. Synthesis is `synth_intel_alm` with
M10K allowed and DSP/MLAB forbidden. ROM, RAM and media use native
asynchronous-read M10K tables; the two-write media path uses native
asynchronous TDP M10K. The 720p capture buffer is a dual-clock M10K SDP. The
Z80 is Verilog T80pa/TV80.
HDMI I2C uses Pong-style `MISTRAL_IO` open-drain pads at BEL X52/Y60
(`QUARTUS` is not defined). Place-and-route uses `constraints-oss.qsf` and `clocks-oss.sdc`.
The QSF omits Quartus `HPS_LOCATION`; the SDC constrains only the 50 MHz
reference and nextpnr derives the PLL outputs. The Quartus files keep
`HPS_LOCATION`, `derive_pll_clocks` and asynchronous clock groups.
nextpnr `5909feb5` forms the 50→52 MHz integer on the 520 MHz feedback
profile (`M=52 N=5 C6=10`). Place-and-route uses the deterministic seed order
10, 5, 12, 2, 7, 1, 3, 4, 6, 8, 9, 11, 13, 34 with heap timing weight 300,
criticality exponent 5 and `--router gpu`, nextpnr's connection-based
router with a pure-delay timing-repair phase (merged PR #66; the
repository toolchain builds it without a GPU and its host backend
produces the same routing a GPU would). `--timing-allow-fail` permits an early
estimate to miss while the recipe checks final signoff and records the first
passing seed. This keeps native async-M10K address paths within the 52 MHz
system constraint. The recipe requires two
`altera_pll` cells (52 MHz system and 74.25 MHz pixel). Also required: the HPS GP
mailbox, the I2C bridge,
and at least one M10K. It seals the format-2 exporter only when both
clocks meet their constraints. The command never programs hardware.

A sealed OSS package has been used for a **hardware diagnostic** on the
designated kit (BASIC, sofa keyboard, empty `LOAD ""` → `0/0`, committed
`.p` → `10 PRINT "OK"`). That is not exact-artifact hardware acceptance
and does not inherit the Quartus bring-up result (the diagnostic used TV80
and the former registered-M10K workaround). A GPU-routed package of that
same registered-M10K recipe base (nextpnr 9c751533, misteross 9ad19189)
also booted to the ZX81 editor on the kit on 2026-09-12 and answered
`PRINT` + NEWLINE with `0/0` through the host keyboard route. The current
native async-M10K recipe has **not** booted on the kit: its sealed packages
`74ef917a` (`--router gpu`, seed 2) and `247e2af4` (unchanged `router1`
control, seed 6, same toolchain) both load, pass signoff and show only a
black 720p frame for 40 s, while the older package re-loaded afterwards
shows the editor within 5 s. Evidence: FES `out/gpu-router-kit-diagnostic/`.
The regression is in the recipe or toolchain, not the router choice.
FogCast library install/launch of that package
is a host concern; this recipe only seals the `.fcore`.

### ZX81 OSS toolchain gaps

These are the Yosys/nextpnr-mistral/Mistral limits the ZX81 recipe currently
works around. A toolchain change that removes a gap should delete the
matching workaround rather than keep both.

| Gap | Observed failure | Current ZX81 workaround |
| --- | --- | --- |
| Combo-read block RAM | `assign q = ram[addr]` with `synth_intel_alm -nolutram` previously became LUT RAM. ABC ran 25+ minutes on an 8 MB XAIG / 23 MB symbol file and did not finish. | Native Yosys async M10K inference maps 10/20/40-bit SDP and two-write/two-read TDP shapes; the OSS recipe uses `ramstyle="M10K"` and nextpnr routes flow-through reads. Quartus keeps `altsyncram`. |
| SDC subset | `ERROR: Unsupported SDC command 'get_clocks'` on the Quartus `set_clock_groups` / `derive_pll_clocks` file. | `clocks-oss.sdc` is only `create_clock` on `FPGA_CLK1_50`. nextpnr derives PLL outputs. |
| QSF `HPS_LOCATION` | Internal HPS I2C previously ignored the Quartus instance assignment. | nextpnr now converts `HPSINTERFACEPERIPHERALI2C_X52_Y60_N111` to `cyclonev_hps_interface_peripheral_i2c.52.60.0`. ZX81 OSS still also sets the RTL `BEL`. |

What already works in this design, so a toolchain fix should not regress it: two independent `altera_pll` cells on PIN_V11; 8-bit 16 K `m10k_tdp` infers 16 `MISTRAL_M10K_TDP` cells in under a second; Pong-style `MISTRAL_IO` HDMI I2C at X52/Y60.

Verilog T80pa/TV80 is an OSS language choice, not a nextpnr packing gap. Quartus keeps VHDL T80pa.

The format-2 package is `fes.pong` version 1.1.0 and requires
`fes.persistence.words` 1.0 and `fes.pong.progress` 1.0 in addition to gamepad
and fixed video. Base ABI and transport remain 1.0. Data-info opcode 7 reports
[2,1,1,0] for word count, layout tag, major and minor. Persistent words are the
speed enum (default 1) and best rally (default 0). Gameplay reset clears the
current rally but never the restored words. Player-return events increase the
current rally and immediately update best, both saturating at 65535; either
point event clears current. Display scores remain independent and wrap at 9.

Data-control opcode 4 selects freeze/begin/commit/resume with arguments 0–3.
Freeze first holds the game, drains its registered event pulse, then latches
both snapshot words and acknowledges; reads (opcode 5) are available only
while frozen. Repeated freeze retains the original snapshot. Resume releases
freeze without resetting gameplay. Begin requires held gameplay reset and
clears the staging bitmap. Writes (opcode 6) populate two staging words; commit
requires both words and a valid speed before atomically publishing either.
Invalid-speed validation occurs at commit, allowing a corrected staging write.
Commit closes staging and leaves reset held for ordinary gameplay release.
Invalid opcode/index/argument/state leaves live data and control unchanged.
Gameplay hold-reset or release discards unfinished staging; reset also ends
freeze. Volatile launches may release defaults without restoring.

The simulation covers restore [2,17], failed/partial commits, fresh staging,
invalid controls/indexes, reset preservation, speed saturation, and defaults.
The production board-top harness drives actual collision logic at directed
positions to test final-edge freeze draining, immediate unfinished records,
65535 saturation, both point events and unchanged decimal score wrap. These
are digital host checks, not timing or physical acceptance.

`cores/fes-pong/rtl/video_720p.v` advances one pixel on every supplied pixel
clock: 1280 active, 110 front porch, 40 positive-sync clocks and 220 back porch
for a 1650-clock line; 720 active, 5 front-porch, 5 positive-sync and 20
back-porch lines for a 750-line frame. It maps a 320x240 game image at 3x scale
into horizontal pixels 160 through 1119, emits black in both 160-pixel side
bars, drives RGB888 plus data enable, and keeps its counters independent of
gameplay reset. `fes_pong_core` connects the existing `pong_game` directly to
that pixel domain with `CLOCK_HZ=74250000` and one frame tick per raster frame.

`cores/fes-pong/rtl/top.v` exports `FPGA_CLK1_50`, `HDMI_TX_CLK`,
`HDMI_TX_D[23:0]`, `HDMI_TX_DE`, `HDMI_TX_HS`, `HDMI_TX_VS` and the
bidirectional `HDMI_I2C_SCL`/`HDMI_I2C_SDA` pins. It instantiates
the HPS GP primitive without fabric SDRAM and clocks the mailbox, gameplay and
raster from the same pixel clock. The GP request toggle remains the only
asynchronous HPS signal synchronized into that domain; accepted reset and button
state therefore reaches the game as one registered vector without an internal
multi-bit clock crossing. If the pixel clock is absent, the initial signature,
ACK-zero, reset and neutral-button state remains visible, but new requests do
not ACK. Activation consequently fails instead of accepting a core whose video
clock is stopped.

The HDMI control path uses `cyclonev_hps_interface_peripheral_i2c` at the
explicit `BEL` site `cyclonev_hps_interface_peripheral_i2c.52.60.0`, connecting
Linux's existing HPS I2C controller to SCL U10 and SDA AA4. Each explicit
`MISTRAL_IO` has constant-zero data, the matching HPS low-enable on OE, and
pad feedback returned to the HPS. This preserves low-or-release behavior
through OSS synthesis; neither line may actively drive high. The source `BEL`
attribute still places the internal hard block. nextpnr also honors QSF
`HPS_LOCATION` for this I2C cell (experiment `850_hps_location`).
Simulation covers all combinations of HPS and external-device low enables,
with digital pull-ups and observable drive intent; it does not model analog
bus timing or replace hardware validation.

Top has the synthesis parameter `BUILD_ID[127:0]`. The standalone build recipe
overrides that parameter with the 32 hexadecimal digits of the build-record ID; identity
indices 8 through 15 expose successive source-order byte pairs with the low byte
first. The all-zero default identifies an unset simulation/build integration
value rather than an accepted artifact.

The test target uses the pinned external Verilator when supplied through
`VERILATOR=...`. Its controllable `board_models.v` drives reference and pixel
clocks with independent phases and exposes the HPS GP boundary so `board_tb.cpp`
can test the production top. It verifies that reference-only clocks cannot
advance the pixel-domain mailbox, then commits multi-bit buttons and a
reset/button-clear vector immediately around frame tick. This model does not
model 74.25 MHz, PLL lock, or hardware. Production `pixel_pll.v` uses the same
checked 50→74.25 MHz single-output fractional-N declaration as
`610_pll_frac_7425`: direct operation, zero phase, 50% duty and
`fractional_vco_multiplier="true"`. Integer mode is not accepted for this
rate. `constraints.qsf` assigns the DE10-Nano 50 MHz input and the ADV7513
RGB888, DE, sync, pixel-clock and I2C pins.

`scripts/build_fes_pong.py`, invoked by `make build-fes-pong`, is the sole
standalone recipe. Its source set is `pixel_pll.v`, `top.v`, `fes_gp.v`,
`video_720p.v` and the existing `pong_game.sv`, with the generated ABI include
directory. Yosys receives the build-record-derived 128-bit `BUILD_ID` and
forbids BRAM, LUTRAM and DSP inference. nextpnr targets `5CSEBA6U23I7` with
seed 1, the task-local QSF, the 50 MHz board SDC and an explicit 74.25 MHz
target; all outputs stay under `build/fes-pong/`.

Before synthesis, the recipe requires a clean source checkout, checks every
recipe/source/constraint/ABI/lock input is tracked and non-symlinked,
authenticates Yosys, Mistral and nextpnr-mistral against the recipe's expected
commits and `toolchain.lock` through their canonical cache stamps and executable
digests, then writes canonical `build-inputs.json`.
The record uses `scripts/build_fes_pong.py` as its recipe,
`cores/fes-pong/generated/fes_gp.vh` as its tracked ABI definition, and an empty
dependency map because the build is self-contained in this checkout.

Export remains unreachable until the routed JSON contains top, synthesis and
utilization each show exactly one `altera_pll`, one HPS GP primitive and one
HPS I2C primitive, no
forbidden memory/DSP synthesis cell or utilization resource is used, the known
`cyclonev_oscillator` utilization row is present with zero use, and the route
log proves normal completion. Synthesized and routed evidence must preserve the
I2C low-or-release topology and pad feedback; routed evidence must use the exact
HPS site and U10/AA4 pads. The single sequential timing domain must meet its
74.25 MHz pixel constraint. The 50 MHz reference has no sequential Fmax row;
the recipe instead requires the tracked SDC's exact 20.000 ns constraint, its
application in the route log, and identical fixed fractional PLL parameters in
the synthesized and routed designs. After creating the deterministic manifest, the recipe
reauthenticates tools and the clean source before calling the Task-3 exporter. A failed
build retains the pre-synthesis input record and diagnostic reports but removes
the RBF, manifest and passing summary so they cannot be mistaken for an
exportable result. The recipe checkpoint itself has no FES Pong RBF, physical
video result or hardware-support claim.

## Pong MiSTer wrapper and Quartus build

`cores/pong/Pong.sv` connects the game/raster to the framework selected in
`cores/pong/framework.toml`: Template_MiSTer revision
`3ea1134cf05d62c2b1db30362277a823d739ced2`. This is a source-only framework pin,
not a fabricated upstream game/release-RBF pin. `sys/` is staged unchanged,
including the board pins, HPS I/O and video/audio infrastructure. Its existing
PLL supplies the 20 MHz game/video clock.

The wrapper reports core identity `Pong` and requests no media. The low word of
MiSTer joystick command `0x02` drives Up bit 3 (`0x0008`), Down bit 2 (`0x0004`)
and Start bit 7 (`0x0080`). Status bit 0, the framework reset input, or its user
reset button resets the game; normal profile reset words are assert/initial
`0x0001`, release `0x0000`. Raster synchronization continues through game reset.
The game tone drives identical signed left/right samples (zero while silent).
The native runtime remains responsible for enabling video/audio and lifecycle.

```sh
make stage-pong
QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0 make build-pong
```

Both commands accept `PONG_FRAMEWORK=/absolute/path/to/clean/template-checkout`
to reuse an existing checkout at the pin. Otherwise they clone into
`build/frameworks/template/`. They reject a dirty or wrong-revision checkout.
Staging archives the pinned Git tree, overlays the local Pong files, and fixes
the build date to the value in `framework.toml` through a project-level pre-flow
hook, without changing `sys/build_id.tcl`. It recreates only
`build/rebuild/pong/project/`; do not edit that generated tree.

`build/rebuild/pong/inputs.json` records hashes of local RTL and build helpers,
the framework revision, and every staged source file. A successful Quartus run
publishes `pong.rbf` and `build.json` there, with artifact size/hash, compiler
version, command and the input-record hash. `quartus.log` records compiler
diagnostics. A stage-only or failed compile produces no new success receipt.
The command uses an explicitly configured installed Quartus 17.0.2; it does not
bootstrap tools, deploy, or program hardware. Repeatable input staging is not
a claim of independently reproduced RBF bytes. Hardware acceptance is separate.

The 2026-09-06 local wrapper build completed full Quartus 17.0.2 Lite
compilation in 2m46s with 0 errors and 56 warnings. The resulting `pong.rbf`
is 2,437,696 bytes, SHA-256
`1567e5ea4db1f18b9f23b48e7a4b7604024a998ddf1378bf77fe5968e00c64d1`.
Its input record SHA-256 is
`63624330d39067eaa46a266225918c0effdd0924ac830bed97a11d94b46648bb`.
Those records identify the exact local sources used at build time. Parent
component selection and image acceptance are tracked separately by FES.

`project/output_files/Pong.fit.summary` reports 7,803/41,910 ALMs.
`project/output_files/Pong.sta.summary` reports positive slack for every listed
setup/hold/recovery/removal/pulse-width domain; worst setup is 0.399 ns and
worst hold is 0.247 ns. Warnings include inherited framework connectivity,
unused timing filters and PLL lock outputs, plus score-width narrowing in
the game. These reports do not establish physical output or input behavior.

Post-build hashing found all 57 staged `sys/` files unchanged and no local
source drift. Quartus changed only the staged `Pong.qsf` input, replacing its
`LAST_QUARTUS_VERSION` metadata from `17.0.2 Standard Edition` to
`17.0.2 Lite Edition`. The input record deliberately retains the original
staging hash. No second independent RBF build has been run. The diagnostic
image subsequently passed native Pong gameplay, Up/Down/Start controls, HDMI
audio, Stop/relaunch and switching with Mega Drive and SNES without rebooting.

## Artifact boundary

The integration outputs are:

```text
build/oss/<experiment>/top.rbf
build/oracle/<experiment>/top.rbf
build/cores/<name>/releases/*.rbf
build/rebuild/<name>/<name>.rbf
build/current/<name>.rbf
build/bundles/megadrive/<rbf-sha256>/megadrive.rbf
build/bundles/megadrive/<rbf-sha256>/megadrive-rbf.toml
build/packages/<package-id>/manifest.toml
build/packages/<package-id>/core.rbf
build/packages/<package-id>.fcore
build/packages/<package-id>.build-inputs.json
```

The core workflow has three separate operations:

```sh
make fetch-core CORE=megadrive
make rebuild-core CORE=megadrive
make select-core CORE=megadrive
make export-core-bundle CORE=megadrive
```

Rebuild compiles the pinned source. Select copies an operator-chosen artifact
to the mutable `build/current/megadrive.rbf` convenience path. Export validates
the rebuild against its closed comparison evidence and writes an immutable,
content-addressed directory containing exactly `megadrive.rbf` and
`megadrive-rbf.toml`. FogCast receives the exporter's printed bundle path under
`build/bundles/megadrive/<rbf-sha256>/`, rather than a mutable rebuild or
selection path. No attestation record, run ID, recovery journal, or
fault-injection result is required.

Building an RBF never touches hardware. Ordinary native bring-up claims the
kit with `scripts/kit.py` and streams the RBF through the target
`POST /v1/development/rbf` path (`load_development_rbf`) under a held lease.
The FogCast host uses the same lease for launches and
`POST /api/v1/session/development-rbf`. That path programs the FPGA manager,
then probes MiSTer SPI identity on the same FPGA-manager GPO/GPI pair the HPS
general-purpose experiments use. A non-MiSTer image does not satisfy the
probe; Stop restores idle with the existing development reboot handshake.
`make program` remains a separate Main-FIFO or JTAG diagnostic outside this
protection and is not the native kit path.

## Pinned core trees

The lock also selects SNES revision `93d359e6f23c734ae3928984e88bed1d9b53cbac`
and its hashed upstream `SNES_20260823.rbf`, copied from mister-packages.
`make fetch-core CORE=snes` uses the existing named-core fetch lane. The local
SNES seed-1 rebuild completed compilation but failed timing. A separately staged
seed-3 diagnostic passed all timing checks (minimum setup 0.240 ns, hold
0.243 ns) and native LoROM/HiROM gameplay, controls, HDMI audio and switching
checks in FES. Its 4,440,332-byte RBF SHA-256 is
`fdd6d3c51cf3662cb59c5250eee8d4aa48fdab14a272c756fb892677d5ff1226`.
The diagnostic changed only the staged QSF seed; the normal rebuild recipe
now explicitly selects seed 3; the frozen diagnostic is still separate from
a new normal build and its acceptance. Bundle export accepts Mega Drive, SNES,
Pong and the native NES cartridge slice. NES uses the locked upstream `NES.qpf`
project and the generic rebuild recipe; its source and release RBF are verified
here, while timing and hardware acceptance remain a separate step.

`cores.lock` is the upstream version pin: git identity plus the official
release RBF hash. `make fetch-core` checks out that exact commit under
`build/cores/<name>/` and hashes the official RBF. That hash check is the
lock test. It does not clone `HEAD`, does not reset dirty trees, and does
not run Quartus.

`make rebuild-core` copies the fetched tree into
`build/rebuild/<name>/project/` (excluding `.git` and prior compile
artifacts) and runs Quartus Prime Lite 17.0.2 `quartus_sh --flow compile`
on the locked project. The staged copy pins `BUILD_DATE` to the YYMMDD
from the locked release name (`MegaDrive_20260603.rbf` → `260603`) via
`MISTER_BUILD_DATE`; override with `--build-date`. The fetch checkout is
not modified. The produced RBF is copied to
`build/rebuild/<name>/<name>.rbf`. Its hash is recorded next to the
upstream hash; they are not required to match. Quartus is never taken from
`PATH`; `QUARTUS_ROOTDIR` is required.

The Mega Drive Lite rebuild has been loaded on real MiSTer hardware, so
fetch → Quartus 17.0.2 → RBF is a working path. Upstream remains the
fallback if a later rebuild is broken.

`make select-core` copies the rebuild to `build/current/<name>.rbf`.
`ARTIFACT=upstream` falls back to the official release. This selection is for
operator use and is not the FogCast release handoff.

`make export-core-bundle CORE=megadrive` accepts the pinned Mega Drive
revision and the MiSTer ABI. It rehashes the rebuild and recipe, validates the
closed `compare.json`, writes the two-file bundle under its RBF digest, removes
all write bits from the files and directory, and prints the absolute bundle
path. FogCast owns which exported RBF is installed on a target.

## Shared native kit client

`scripts/kit.py` is a thin operator client of FogCast's target lease and native
RBF upload APIs. It retains one in-memory lease during an interactive session,
renews every 20 seconds, streams regular RBF files with an explicit bounded
length (1 byte–32 MiB), and releases on exit. A development-RBF `CORE_TIMEOUT`
after programming keeps that lease so the operator can inspect a non-MiSTer
image before Stop. Stop follows FogCast's development reboot handshake when
the target reports `reboot_required`: it records `boot_id`, posts
`/v1/development/reboot`, and waits for a new boot ID and a free lease. A
successful reboot ends that lease because the target agent restarts. Release,
EOF, or Ctrl-C Stops first when a development image was loaded, so that
handshake runs instead of a raw release after a non-MiSTer bitstream. It stores
no credentials or lease database. FogCast remains authoritative for expiry,
takeover, serialization and cleanup; libmister-runtime performs the physical
transition. See the README's shared-kit commands. Direct `make program` remains
a maintenance bypass outside this protection, and compilation never acquires a
lease.

## Bundle validation for Pong, SNES and NES

The same eleven-field format-1 manifest serves all four systems. SNES and NES
identify their pinned upstream repository/revision and
`scripts/rebuild_core.py`. Pong identifies `https://github.com/DeanoC/misteross`,
the clean checkout's exact HEAD, and `scripts/build_pong.py`; its build input
record binds every local RTL/helper hash and the pinned framework. Export
rechecks those hashes before publication.

SNES's normal recipe selects fitter seed 3 in the staged QSF only. SNES and
Pong builds require finite, nonnegative slack and TNS for every listed result,
including setup, hold, recovery, removal and pulse-width analyses. Their build
receipts include the timing rows and summary hash; export revalidates the report
against the receipt and artifact. SNES also records its seed and recipe hash.
A compiler success without these timing results cannot produce a new exportable
receipt. Generated reports and bundles are local artifacts; FES owns selection
and exact-image hardware acceptance.

## Format-2 package boundary

`scripts/core_package.py` is the host inspector for format-2 package directories
and `.fcore` archives. Its public Python API is
`read_package(path: Path) -> CorePackage`,
`encode_manifest(fields: dict) -> bytes`, and
`package_identity(manifest: bytes, payload: bytes) -> str`. `CorePackage` exposes
the original `manifest_bytes`, parsed `fields`, bounded `payload_bytes`, and
`package_id`. The inspector validates every manifest field and payload digest;
an unknown well-formed ABI remains inspectable. A directory has exactly two
regular non-symlink entries. An archive is at most 33 MiB and is exactly two
canonical uncompressed POSIX ustar regular-file members, manifest first, with
zero member padding and exactly two final zero blocks. Alternate paths, links,
extensions, extra members, base-256 sizes and trailing bytes are rejected.

Repository URI syntax uses the host-only vendored
`rfc3986-validator` 0.1.1 module from
`https://github.com/naimetti/rfc3986-validator`, followed by the manifest's
lowercase `https://` and no-literal-userinfo authority policy. The unchanged
vendored module is `scripts/rfc3986_validator.py`, SHA-256
`95fc6d48642f111952b25c040947765bccba669210c8c140b7ed9647fd7e470c`.
Its MIT terms are retained in `scripts/rfc3986_validator.LICENSE`, SHA-256
`94e53eb4b94a5d33a7e66b0abb143ee95f4ec96f36ea4c54794ae6a46e624f04`.
This adds no installed Python or target dependency. Manifest parsing and
pre-synthesis build-record encoding share the same validator.

`scripts/export_core_package.py` provides
`export_package(manifest: bytes, payload: Path, destination: Path) -> Path`.
The destination is a package-store directory. Export derives the package ID from
the exact manifest and payload bytes, then publishes the read-only directory,
matching `.fcore`, and external `.build-inputs.json` with no-replace atomic
renames. A pre-existing result is reused only after all three outputs and their
permissions are verified byte for byte. Format 1 continues to use
`scripts/export_core_bundle.py` and its existing RBF-digest store.

The format-2 build record is canonical UTF-8 JSON (sorted keys, compact
separators, one final newline), at most 65,536 bytes, with this closed schema:

```json
{
  "format": 1,
  "repository": "https://example.invalid/source",
  "revision": "<40 lowercase hex>",
  "recipe": "relative/tracked/build.py",
  "recipe_sha256": "<64 lowercase hex>",
  "abi_definition": "relative/tracked/abi.json",
  "abi_definition_sha256": "<64 lowercase hex>",
  "dependencies": {"relative/checkout": "<40 lowercase hex>"},
  "tools": {"tool-id": "exact version or commit identity"},
  "parameters": {"parameter-id": "string, signed 64-bit integer, or Boolean"}
}
```

`encode_build_record(fields: dict) -> bytes` constructs the record before
synthesis; `build_identity(record: bytes) -> str` returns the first 16 bytes of
its SHA-256 as 32 lowercase hex digits for the manifest and RTL. The exporter
discovers the main Git root from the RBF path. It requires that checkout to be
clean at `revision`, checks its origin against `repository`, requires the recipe
and ABI definition to be tracked regular files without symlink components, and
checks their digests. Each dependency path is likewise relative, non-symlink,
clean, and at its recorded commit. `tools` records exact identities, while the
Task-10 build lane is responsible for authenticating the invoked tools and
timing result before export. Generated or ignored build products may include the
RBF and adjacent record, but they cannot stand in for tracked recipe or ABI
inputs. Empty `dependencies` is valid for a self-contained source tree.
