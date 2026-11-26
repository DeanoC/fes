# DE25 486DX fit results

The [method](method.md) defines admission for each rung. The producer,
fitted timing, and the loaded-route measurement are independent inputs to
that decision.

## Initial C0 = 34 sweep

The candidate is `3df878912962ede8557166d56c93ff8896869943` at 92.156863 MHz.
Seed 1 ran with Quartus Pro 26.3.0 build 114 on Powerboat. Its fitted PLL
reported M = 188, N = 3, C0 = 34 and C1 = 255.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Producer |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,138.5 | +0.597 ns | 0.000 ns | +7.148 ns | +0.034 ns | 3.252 ns | Refused |
| 2 | 45,176.2 | +0.574 ns | 0.000 ns | +7.265 ns | +0.029 ns | 2.996 ns | Refused |
| 3 | 45,109.5 | +0.660 ns | 0.000 ns | +6.378 ns | +0.109 ns | 2.863 ns | Refused |
| 4 | 45,126.1 | +1.044 ns | 0.000 ns | +6.845 ns | +0.056 ns | 2.754 ns | Refused |

The worst setup path is from `mult_36~mac~AY_REG` to `add_763~211` on the
machine clock in the slow fix4 0°C model. The synthesis report traces
`mult_36` to `cpu_x87_core_fast_p01`. The fitted DSP inputs trace to
`cpu_x87_core_fast_req[104]` and `[43]`. This is the x87 multiply tile to
adder path, with positive routed slack on this seed.

The producer stopped on `TMC-20602` while checking reviewed findings.
36 of 78 endpoint-tension advisory rows matched no basis, including
`system|_36681_[49]~BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I20_dff`.
The generated CPU netlist maps its stem to `cpu_m1_out[119]`. This seed
fails admission regardless of the raw slack. The retained fitted summary
SHA-256 is `ee0de14e58a8a64815b266bf64c2b6042ad58b29adc95d928deefeaab02e19a1`.
The fitted machine netlist SHA-256 is
`5e49a28b2b28b7fc7b04432e104ea4a774eedac49b6c1ec9d6787ada2ae5a1ef`.
All four original seeds retained these same machine netlist bytes.
The original rung is refused because all four failed producer admission.
Its retained `c34/summary.json` SHA-256 is
`e9d5104725d0a66fdddbb6c4586031b5a66f618dbdd64735bcabd468dc9f5eb2`.

Seed 2 also reports nonnegative timing summary rows. Its producer stopped
on the first unrecognized `TMC-20602` row for `cpu_m1_alias~26`. The full
report has 73 rows. The original basis covers 44, the seed-1 retiming
basis covers 21, and eight fitter variants needed review. Its first
LOADED report command failed the Quartus lease audit before starting
TimeQuest. The replacement leased report passed at all four corners.
The worst setup row runs from a retimed copy of the x87 operand distance
into a fitted shifter register, with +0.574 ns at slow fix4a 0°C. The
seed-2 timing summary SHA-256 is
`a1c1628ae8d94bbc6f97f94f7b6a5d713c188a796861e9ce15a5d723f3d22416`.

Seed 3 reports 165 timing summary rows with nonnegative slack. Its worst
setup is +0.660 ns from `cpu_x87_core_fast_s4[177]` to a duplicate of
`cpu_x87_wrd` at slow fix4 0°C. The producer refused `TMC-20602` at
`_36681_[49]`. The original basis covers 34 of 60 rows. The seed-1
extension covers 20 and the seed-2 variants cover two. The remaining four
map to an M1 translation qualifier and bits 5–7 of the latched AT port.
The seed-3 timing summary SHA-256 is
`794f61bb6bb04fc09f119f8c3f827ab7d0db25e3bd628db54c5e491d4f996b2e`.

Seed 4 also reports 165 timing summary rows with nonnegative slack. Its
worst setup path is from a DSP tile retiming register (`mult_42~mac`)
to the x87 writeback mantissa (`cpu_x87_wm[5]`) at slow fix4a 0°C.
The producer refused `TMC-20602` at `cpu_dmem_read~0`. The original
basis covers 27 of 47 rows. The earlier retiming review covers eight,
and twelve additional rows trace to memory/writeback control, the AT port
latch and AT cycle strobe. The seed-4 timing summary SHA-256 is
`34d24da4e2ee4ed5b30b46d254e286912e8bd387cca41c8a589c0f19fdeb7119`.

### Retimed endpoint review

The retained project passed the all-corner timing recheck and the LOADED
route budget. Its maximum LOADED data delay plus absolute skew was
3.252 ns against 3.851 ns available at this clock.

The 36 unmatched tension rows each named one fitted register clocked by
the machine PLL at all four corners. The trace found 288 setup and 288
hold paths in the machine domain, plus the ownership and asynchronous
edge checks. All 1,008 checks passed. Worst setup was +1.949 ns and worst
hold +0.068 ns across the traced paths. The `reduce_nor_1931` register
sits between a `cpu_m1_s` alias and `cpu_l1i_page_new[2]`. The other
generated names match observed CPU controls, apart from the FDC decode
register traced to `pc386_at_core.sv`.

I edited the report wrapper after it started. The last supplemental path
collection failed after it wrote the all-corner, LOADED and tension
results. The tension checker exited zero and the retained trace passed
again with both machine-clock checks. These are the retained SHA-256
digests from seed 1.

| File | SHA-256 |
| --- | --- |
| `loaded.tsv` | `1a06ac7568f1009d52b1a9dd58826c8c459ecfee01297e369f8fc5bfcec4e3e5` |
| `tension-paths.tsv` | `051782fd61d9cf041f1e0e3fe2311979bf923f916dc052bf9779ac1575a759cc` |
| `tension-summary-same-clock.json` | `4accea27c7e94f21046719c716de1be07706f8feddfc790b686d28b2a3044070` |

Seed 2's LOADED maximum is 2.996 ns against 3.851 ns available. The
29 non-original advisory endpoints each resolve to a unique machine
register. All 812 trace checks passed, with worst incoming or outgoing
setup of +1.353 ns and hold of 0.000 ns. The eight new forms cover the
M1 transaction request, the M1 valid bit, an L1D stall qualifier and the
AT floppy-drive port decoder. The generated machine netlist SHA-256
matches seed 1 exactly.

| Seed 2 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `26307ec4767003e1f67ab415ad0d09845d94a0469b6e73cb0fb3183a2406cfb5` |
| `tension-paths.tsv` | `7cf75abe876a7d5b4c7ea873df19f013ee64660593d6ab38518344790a7a0af6` |
| `tension-summary-synchronous.json` | `6283c8a77ee207fba27a0a0c6df669c969f50c79fcdcadd5421f45a18521b3e6` |

Seed 3's LOADED maximum is 2.863 ns against 3.851 ns available. All
728 endpoint checks passed for the 26 non-original advisory rows. The
worst traced setup was +2.063 ns and the worst hold was +0.053 ns.

| Seed 3 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `56482bcca02d799e3c17298cccb045693d49076aa7251b370cce739e798d0406` |
| `tension-paths.tsv` | `9d02685378029ca5267ba6964514666c9b90091d7b5346534ced71193e775e43` |
| `tension-summary.json` | `28ca31f7216e77cc76a962141a6254863d9f546e9d0d473d0cec02eecbc99056` |

Seed 4's LOADED maximum is 2.754 ns against 3.851 ns available. All
560 endpoint checks passed for the 20 non-original advisory rows. The
worst traced setup was +1.626 ns and the worst hold was +0.098 ns.

| Seed 4 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `ff8402618bf0a3161579faf545d34f209ae2dc02ff454887b2bad7e9fd57cca4` |
| `tension-paths.tsv` | `0e7ee567512a0694a1ade85053412ec5416beda4dd72974ab6191acb851047c7` |
| `tension-summary.json` | `3fcffdeb7ede343f03a8451c634a387fd97c85858e4a1b6a202d0922a6c97c90` |

The pc386 basis now covers these traced retiming forms for `TMC-20602`.
The seed-1 report matches 42 rows under the original basis and 36 under
the new one, with each row matched once. The rung collector requires a
passing fitted clock and corner audit for every row matched by the new
basis. The registered producer checks that the prepared project and build
record name the same model. A 486SX fit still has to match the original
`TMC-20602` basis before package admission. The retained seed-1 build
remains refused. The updated candidate
needs four fresh fits through the producer. The seed-1 repair job queued
on the alternate host was canceled before it acquired a slot after seed
2 exposed other fitter variants. Finish the original sweep and resolve
its findings before starting the four repair fits.

[Altera TMC-20602 rule](https://resources.altera.com/quartushelp/20.1/da_rules/tmc_20602.htm)
and [TimeQuest clock query](https://docs.altera.com/r/docs/683432/26.3/quartus-prime-pro-edition-user-guide-scripting/get_clocks-quartus-sdc).

## First C0 = 34 repair sweep

Revision `e255d4e3a09fd29ad2cfeed465bf49a15e1c17ce` ran at the original
Powerboat checkout path. Its first seed elaborated the same machine
netlist bytes and fitted at 44,943.3 ALMs. The worst setup, hold,
recovery, and removal were +0.543, +0.002, +7.199, and +0.041 ns.
The LOADED route measured 3.011 ns against 3.851 ns available.

The producer refused `TMC-20602` on two further spellings of the
previously reviewed `machine_at_select_fdc` and `machine_at_cycle_strobe`
registers. The two names belong to the FDC port decode and AT I/O-cycle
qualifier. The trace passed all 308 checks for the 11 non-original rows,
with worst setup of +2.177 ns and hold of +0.211 ns. The retained producer
verdict is refused. The next revision checks the actual fitted clocks and
paths for the revised basis during the registered DX build, before package
admission.

| Seed 1 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `f8a03ead56ff9f647e9d3c55ebf13aba7036303d011aab1905fe44db77b6217d` |
| `tension-paths.tsv` | `ccb511b309650a3a7dc0ae4873281a9c82c2833cfe57a710f4f12bc945a55a1f` |
| `tension-summary.json` | `47f5e452e5d9e97a3ac6ea17c2560d93b8458345d5044523c0c6822c4157c5e3` |

Seed 2 fitted at 44,994.1 ALMs with worst setup +0.523 ns, hold 0.000 ns,
recovery +6.419 ns and removal +0.019 ns. The LOADED route was 3.460 ns.
The producer refused `TMC-20602` on `cpu_m1_out[20]`. Nine unrecognized
rows included additional M1 paging and access qualifiers, M1 valid and
two AT decode registers. The fitted launch and capture paths place
`add_146` and `reduce_or_2303` in the M1 end-address and access-type
expressions, respectively. These two names are Quartus-generated.
All 868 checks passed for the 31 retimed rows, with worst traced setup
of +1.720 ns and hold of +0.067 ns.

| Seed 2 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `ffa8fdbc317d4b75d8d04475b7cfb0bec3ae83b2f5f18f94c60a66c61b16e128` |
| `tension-paths.tsv` | `09a4f63ca5cc53b4f6d51ddfb38f976f78fe1a4eecd03efcb8760c7abe359d4f` |
| `tension-summary.json` | `1cf10b114aacca43ad9b591eaf53914f528f4df1fca2e82e0389c02ce255784c` |

Seed 3 fitted at 44,820.1 ALMs with worst setup +0.491 ns, hold 0.000 ns,
recovery +6.583 ns and removal +0.094 ns. The LOADED route was 2.584 ns.
The original producer refused `TMC-20602` at the new `cpu_m1_tx` clone.
The fitted trace resolved all 26 retimed rows and passed 728 checks.
Worst traced setup was +2.228 ns and worst hold was +0.063 ns.
The additional compiler names are scoped to the traced store-wait adder
paths and AT decoder slice. The fitted paths identify those cones. The
generated netlist has no individual RTL alias for the AT fitter names.

| Seed 3 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `ac124f0d70d96ed0db3306285df658d4509fa33749fa07b82f8b32686049f2b7` |
| `tension-paths.tsv` | `2d5033ede0cfa952db9e93f48c190c5336bbfdee31d049ee5446de4f04a9b604` |
| `tension-summary.json` | `ad59c8fefd338322f92ff31ead0505a2053ada638ff4fff23251a25d6d87118f` |

Seed 4 fitted at 45,160.1 ALMs with worst setup +0.866 ns, hold 0.000 ns,
recovery +7.324 ns and removal +0.030 ns. The LOADED route was 2.770 ns.
The producer refused `TMC-20602` on an additional clone of `cpu_m1_tx`.
The other two new forms came from the FDC selector and latched AT port
bit 8. All 364 routed checks passed for the 13 retimed rows. Worst traced
setup was +1.870 ns and worst hold was +0.157 ns.

| Seed 4 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `af6e0873b06943c2dea899f83140d8f33889a7e15671aa21489628f6df252068` |
| `tension-paths.tsv` | `9f0c2b7da885816d6366bc6feb033f32b2ad856567e7cf7dc5141962826e2c1e` |
| `tension-summary.json` | `23fcffdae8f6881f08680a303a212ca4d98d4baf9eb7c562511fb5b19c9af7f6` |

All four builds of this revision are refused by the producer. The
retained `c34-reviewed/summary.json` SHA-256 is
`258817539e09ec306f150a8ffba6ec09cabce63dd364b1d6a7da4ba2220a663e`.

The alternate builder's authenticated Yosys has the same locked commit
but a different binary digest. A preflight in the same absolute checkout
path generated different Verilog beyond the Yosys banner and source
attributes. The current fit sweep uses Powerboat's original Yosys binary
and checkout path.

## C0 = 34 fit with registered proof

Seed 1 at `d18a5a45b91236abc4efac7607c84e97735eab07` used 44,867.8 ALMs
and reported worst setup +0.673 ns, hold nonnegative, recovery +7.503 ns
and removal +0.027 ns. The LOADED route measured 2.485 ns. The producer
refused `TMC-20602` before running its package proof. The fitted report
introduced `i48682~0_C4` and a C4 spelling of `_17339_~1`, the earlier
L1D stall qualifier. The independent endpoint trace passed all 868
checks, with worst setup +1.817 ns and worst hold +0.062 ns.

| Seed 1 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `a9f017a717eaadca835447c59028e7fda0b8a1a53c781e8d2e89e77dcb663a13` |
| `tension-paths.tsv` | `d7c2789cde5030600c983bf6e321849ff6683094ed39bc8fe1bbff2b5a8fd6f1` |
| `tension-summary.json` | `f2933d200ee4bc390c8dd4581994fd91d919e76de6f656b03452b21c688e1078` |

Seed 3 at the same revision used 44,974.5 ALMs. It reported worst setup
+0.857 ns, hold 0.000 ns, recovery +6.968 ns and removal +0.124 ns. Its
LOADED maximum was 2.602 ns. The producer refused a generated
`reduce_nor_2117` passthrough. The independent trace passed 1,568 checks
for 56 subjects, with worst setup +1.391 ns and hold +0.005 ns.
The registered-proof pilot ran seeds 1 and 3. Both remain refused.

| Seed 3 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `d2b00d94a3c136dda8c41cc34384c2166bf441e363c46fe8b042a121fb6b9d53` |
| `tension-paths.tsv` | `fe482b0803e91d5bdf9e002dd7b165b6f9747bb93130c74ef237505a432eb169` |
| `tension-summary.json` | `a65143b9d0c51a7578e74ae4e4246fb14f835766548625abff98bfca52ed32cd` |

### Fitted placement and clock-domain proof

The fitter has emitted new advisory names on otherwise byte-identical
machine netlists. To test the same-clock reasoning directly, the alternate
builder opened a verified copy of seed 1's QDB with the same Quartus STA
binary. The probe enumerated each register's data keepers and checked
both directions for inter-clock and false paths in setup and hold at
all four operating conditions. It also saved the detailed unconstrained
path report. Each timed register's intrinsic edge to its own Q pin was
checked by source, destination and node types.

All 248 checks for the 31 advisory subjects passed. None of those
subjects appears in the detailed unconstrained path report. The separate
LOADED flag control exposed the same foreign-clock keeper and the cut
to `machine_presenter_loaded_sync[0]` in both setup and hold at each
corner. The structural basis applies to the exact pc386 system hierarchy
and observed fitter placement forms in the registered DX producer. It
requires this proof alongside the numerical timing proof. Each refused
build remains recorded at its original revision.

| Structural evidence | SHA-256 |
| --- | --- |
| `trace_structural_v3.tcl` | `216a6401735d139956458b7cf6c28e4a62d7ec8d81dd75da13c3cc52d9fea385` |
| `d18-s1-targets-with-control.txt` | `9ee7b5fff1ef329bcd0553a613810cfcb1e7499373d7d5a02facdf13ac7de6f6` |
| `d18-s1-structural-v3.tsv` | `3dfcbd9b80931387e7f813741dff9ba0f29ae2a1f1a49e18669ccca76c88c180` |
| `d18-s1-structural-v3-ucp.rpt` | `5dd5c91628f409865cc2bd0abe0e25b81a483e1667687f6df60a5e6571d6a4f3` |

The combined registered TimeQuest script also passed on the seed 1
copy, with 868 numerical and 248 structural checks. On the seed 3 copy,
it checked seven enable and clear derivatives without location suffixes.
All 196 numerical and 56 structural checks passed. Worst traced setup
and hold were +2.398 and +0.053 ns. Neither combined report listed an
advisory subject in the unconstrained path tables.

| Combined evidence | SHA-256 |
| --- | --- |
| Seed 1 structural rows | `98b66456ff3a6a3efdcc849de4b8db90bacb219403efdab48b45d6d8749a646d` |
| Seed 1 check summary | `b29d193d3d54c7fe3f0d30cf7296f6ab607ba538c837934b2645d341cd32ef53` |
| Seed 3 enable and clear numerical rows | `b0163899cb9dab6318bed890d0be7ab87887dbef14da73a6768304b5b0e2b6c5` |
| Seed 3 enable and clear structural rows | `9f106ca50bbb95106ce846f142bda2ea587f11ea3a86d4e38de39b58f4fd0a84` |
| Seed 3 enable and clear unconstrained report | `4412744f4d373f86e8b33ca6e53433348c1157af2481cdc73dc936cbd42d97f1` |
| Seed 3 enable and clear check summary | `69dd14e7aa118da7f3be4456471e4222cfbb69f3f35517060b617e72d46e500d` |

[TimeQuest path queries](https://docs.altera.com/r/docs/683432/26.3/quartus-prime-pro-edition-user-guide-scripting/get_timing_paths-quartus-sta?contentId=TFyfmhCSrpOWMKC0o_7ZTg)
and [unconstrained path reporting](https://docs.altera.com/r/docs/683432/26.3/quartus-prime-pro-edition-user-guide-scripting/report_ucp-quartus-sta?contentId=qCZEnfJdI2kgidW0dOO2KA).

## C0 = 34 sweep with the structural proof

The candidate is `b6310cd4cb7f9eb9f7c5cb4e1462ebb9708024cc`. Seed 1
passes the registered producer, ordinary package seal and independent
rung collector. It used the original machine netlist bytes, reached
44,883.4 ALMs and retained the intended PLL counters. The worst all-corner
setup, hold, recovery and removal were +0.403, 0.000, +6.896 and +0.043 ns.
LOADED measured 2.285 ns against 3.851 ns available. The live proof for
12 placement registers passed 336 numerical and 96 structural checks,
and the detailed unconstrained report excludes those subjects.

| Seed 1 artifact | SHA-256 |
| --- | --- |
| `summary.json` | `f846826647aaebf00e8d2c6a00082f4f1ed5e5d09a64cc5248fd7bf0cd0bc7ec` |
| `retimed-tension/receipt.json` | `96fd5ac8d87b2d47145a26a851d02422d7802a88a7206f38a7712d2d20c066d5` |
| `core.rbf` | `af47e9d61c07e6b55d803a4fa20783b5e5bb54e37144880563dffc008c963158` |
| Sealed `.fcore` | `d2b3bce9aacc4cdcfe593dad7734cd9a876a6c98f2799fb60be6e89935478d5d` |

Seed 3 also passed the producer, seal and independent collector. It used
45,134.9 ALMs and reached +0.554 ns setup, 0.000 ns hold, +6.468 ns
recovery and +0.017 ns removal. LOADED measured 2.577 ns. All 644
numerical and 184 structural checks passed for its 23 placement
registers.

| Seed 3 artifact | SHA-256 |
| --- | --- |
| `summary.json` | `c5b4986a5d70f099a0712b30a463212584cd7073063d66b73826ee7f2ea3ec67` |
| `retimed-tension/receipt.json` | `487b77819fe3c7ccbc2ef8b20d917c89249088c93acb94fb497e306acd8e7412` |
| `core.rbf` | `b7aabaa5465a46457f41cd9d6d4631831ef7fa1c069d2ecc84ab72d9d21c9f4b` |
| Sealed `.fcore` | `7d1e0e7d69cd968d1507c3078ea973e63fda18cacdd62a9550fe468321709f1a` |

Seed 2 used 45,065.9 ALMs and measured +0.667 ns setup, 0.000 ns hold,
+7.156 ns recovery and +0.092 ns removal. LOADED measured 2.929 ns.
The producer refused one `TMC-20602` row at
`machine_at_cycle_strobe~1_C8_X79_Y8_N0_I4_dff`, the AT I/O-cycle
qualifier in a C8 route register. On a verified copy of that fitted
database, the combined proof passed all 672 numerical and 192 structural
checks for 24 subjects. The C8 register's worst traced setup and hold
were +2.373 and +0.337 ns. The copy retains the same seed-2 producer
refusal and supplies the independent route evidence.

| Seed 2 artifact | SHA-256 |
| --- | --- |
| Refused build `summary.json` | `c4b97abebcb770f176f087eb10410b33a0996fde527630e1106d9173d76a939b` |
| Independent numeric `tension-summary.json` | `a1a7009ad067ed77d19288bdf15502813737f2e419724aa44e1ebe4ce224fa0e` |
| Fitted-copy structural `receipt.json` | `4fe2d6fd37a9fec7360de5afbad59a15b5aedc83129638d6031097b1e3efcc8a` |

Seed 4 passed the producer, seal and independent collector. It used
45,022.3 ALMs and measured +0.766 ns setup, 0.000 ns hold, +7.288 ns
recovery and +0.028 ns removal. LOADED measured 2.614 ns. All 728
numerical and 208 structural checks passed for 26 placement registers.

| Seed 4 artifact | SHA-256 |
| --- | --- |
| `summary.json` | `8bf36ea498afac9e7364b88be74251a48b14735dbc71529d5d795fef01f42315` |
| `retimed-tension/receipt.json` | `9216ad363202490811dbea23020b975f2661b0831fd5987cde01f6e3c3d58f77` |
| `core.rbf` | `ff45c884703db2cf1ec896e0fb06cc6afdd14ed5b8f807ad036a21c943011f48` |
| Sealed `.fcore` | `cd03126d27c63eba9feccfe89dd7d7469acf490ecb79f2542afda61ff91f15e7` |

The four-seed aggregate is refused solely on seed 2's admission.
All four emitted the original machine netlist bytes. The retained
`c34-structural/summary.json` SHA-256 is
`393966185dc898beb43898e1887dfb09faaad25176b0eb0a6a5ef00ef9bbe7e3`.

The retained seed-2 `golden_top.fit.route.rpt` enumerates the
general routing interconnects available on this device. Its SHA-256 is
`3baabfcf36999760af86cb76a9e4b9b653a08e10d182b601b40f99fdcabd1f46`.
The updated DX basis recognizes exactly the reported general route
classes `C1`, `C4`, `C8`, `R0`, `R1`, `R2`, `R4`, `R6` and `R12`,
along with the already measured input mux form. All 303 located
advisory rows audited across retained reports agreed with the DRC
`Register Location` column. The producer now checks that agreement
for every located subject before its live proof. This candidate needs
four fresh producer builds at C0 = 34.

## C0 = 34 device geometry sweep

The candidate is `1106c799322514432b4631fc17439be4055043dc`. All four
seeds passed the geometry check, live proof, ordinary producer and
seal, and independent LOADED measurement. All four use the original
`pc386_system.v` bytes and the intended PLL counters.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Producer and seal |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,187.7 | +0.742 ns | 0.000 ns | +7.197 ns | +0.115 ns | 2.554 ns | Pass |
| 2 | 45,049.8 | +0.626 ns | 0.000 ns | +6.651 ns | +0.056 ns | 2.482 ns | Pass |
| 3 | 44,783.4 | +0.852 ns | 0.000 ns | +6.969 ns | +0.025 ns | 2.955 ns | Pass |
| 4 | 45,205.0 | +0.407 ns | 0.000 ns | +6.453 ns | +0.165 ns | 2.750 ns | Pass |

The four fitted seeds qualified C0 = 34 at 92.156863 MHz. The retained
`c34-geometry/summary.json` SHA-256 is
`a5b2d19aa3b964ccd9ad40852b3bb46067ce3dc782bfcec9220ae5ee089eab7b`.
The most constrained seed measured 2.955 ns against the 3.851 ns LOADED
budget. The live proofs covered 14, 30, 28 and 19 placement registers.
Every structural and timing check passed.

| Seed | Seed summary SHA-256 | Structural receipt SHA-256 |
| ---: | --- | --- |
| 1 | `0e83430c8b35a43c9c8ab90977f4f436db4bbf45096d048ae7e54fe0b7559b23` | `082e3ba76ddceddb1e5052cd29d8d72917cba0d97ff1671140eff0d666be4520` |
| 2 | `a1f5391678ce77a2bae7bbbcdc7a39195dd02525d276ec9d7a9af4710bcdc3fb` | `3622f2d5b9262fec7c9a4a4b1a0d30d4b73698c2d76332a9cf3c209cbd38b983` |
| 3 | `2132c8d815efd2d8d2b9db07252f579a269635d7894e1bcc244f349126c29210` | `a9ebf2ffbe1fac65a0ef6f8d1c81d9a315b26f445a7797108918aff2c5616fb4` |
| 4 | `9b75ef93b32ceab0c453a1adbf4c1007b29cf5f88565e6037fd8f0ed712fc767` | `57eaf6c16284ce2b8acbe67591330fae5a79a70337a16fb2fb6d5b57c5631755` |

## C0 = 32 initial sweep

Revision `27a9104b408c3c3384988b3b063606ec1559144f` sets 97.916667 MHz
with the [reviewed PLL and Qsys inputs](parent-c32.txt) and the original
five-clock presenter interval. At this clock, the LOADED budget is
0.660 ns. The first generated fitter project passed the Qsys parent
comparison against the qualified C0 = 34 seed 1 project. The resulting
parent SHA-256 is
`2e057d8133607240a432478d8f102746eeea85447447d8515e506aa3de52aaa4`.
The retained `c32-first/parent-s1-audit.txt` SHA-256 is
`879bcff3a7bb18932f309ce4bbc18a371a90a1c69542c04ef89f52abeb5ce862`.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Producer and seal | Rung verdict |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |
| 1 | 45,145.9 | +0.814 ns | 0.000 ns | +6.549 ns | +0.073 ns | 3.331 ns | Pass | LOADED miss |
| 2 | 45,050.9 | +0.319 ns | +0.002 ns | +6.680 ns | +0.029 ns | 3.184 ns | Pass | LOADED miss |
| 3 | 45,117.6 | +0.537 ns | 0.000 ns | +6.697 ns | +0.122 ns | 2.921 ns | Pass | LOADED miss |
| 4 | 45,114.6 | +0.952 ns | 0.000 ns | +6.597 ns | +0.018 ns | 2.647 ns | Pass | LOADED miss |

All four seeds miss the LOADED budget with nonnegative timing at every
reported corner. All use the same machine netlist SHA-256,
`065d7fd0362caf774176ed6d08f9ed7dcb121351460ba196a52d7a73291016e9`.
The retained `c32-first/summary.json` SHA-256 is
`cf0bfdd78574b06be4dc06c66e843f4c09d283dd05ed5adc55d7216af5969cce`.

| Seed | Refused rung summary SHA-256 |
| ---: | --- |
| 1 | `55d83e00d3eaa79ff3862a5f528778572ce699748ea22c940d28be861d1e4595` |
| 2 | `2022fa4fcfcb50d369bfdb2cd6a90f8127392874c064701d647fc92be0c459f1` |
| 3 | `eddba10f82dbf89e9c9868333a9f5d8481344aa84beb98e67ae46f3d6f67bb84` |
| 4 | `4b306c662801b1e90d48780214af740661af0538eebb27ae8ebcb9af9de55e47` |

The flag route runs from `machine_presenter_loaded_flag` to the first
pixel-domain synchronizer stage. The original presenter can publish
another report after five machine cycles. At C0 = 32, that interval is
too short for the measured flag route plus the existing three pixel
cycles and the 10 ns payload allowance.

Revision `7e3227f68081a30c8d920a060eeaa930ab880357` derives the DX
presenter with at least eight machine cycles between report changes.
The digital tests cover report coherence, reset, a new epoch during a
completed hold and raster deadlines. The original timing constraints
and 3.9 ns maximum LOADED allowance are used for the fresh four-seed
fit. The PR carries the qualified C0 = 34 source during this trial.

## C0 = 32 eight-cycle sweep

The recorded eight-cycle interval and exact derived presenter digest
match the fitted inputs of `7e3227f68081a30c8d920a060eeaa930ab880357`.
The seeds emit the same machine netlist,
SHA-256 `ad0a331d6f02c529ac55ed93542764fbc5576345f25c61064085a551a836fd33`.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Producer and seal |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,187.1 | +0.915 ns | 0.000 ns | +7.281 ns | +0.136 ns | 2.554 ns | Pass |
| 2 | 45,012.9 | +0.500 ns | 0.000 ns | +7.189 ns | +0.174 ns | 2.228 ns | Pass |
| 3 | 44,839.3 | +1.059 ns | 0.000 ns | +6.196 ns | +0.077 ns | 3.700 ns | Refused |
| 4 | 45,018.1 | +0.258 ns | 0.000 ns | +7.203 ns | +0.004 ns | 2.246 ns | Pass |

| Seed | Seed summary SHA-256 |
| ---: | --- |
| 1 | `a4bfdad3263cf0fc1fbea59ca749d2d1b0610cdf1bce506ab97b89f951565fa7` |
| 2 | `f9f1342caa0769a5e354a4bee7b0e5b093847f35142e04f2592d9d5763ef1332` |
| 3 | `2710ba401d2d2b6d58273c791bd2b004312e1d12686b9e93d94537b3c263f19c` |
| 4 | `39e34fee13371e7cb965e78f01a6dabd648bb9d3447957a4939be20e8e01d887` |

Seed 3 met the numerical and LOADED limits. The producer refused
`cpu_biu_reset~_Duplicate_2_BLOCK_INPUT_MUX_PASSTHROUGH_X50_Y18_N0_I20_dff`
because its optimizer-generated qualifier fell outside the previous
spelling list. The combined proof on a verified fitted copy passed
1,204 numerical and 344 structural checks for its 43 subjects.
The exact duplicate had one incoming and 17,059 outgoing machine-clock
keepers at all four corners, with worst traced setup +1.862 ns and
hold +0.133 ns. The detailed checks found no inter-clock, cut or
unconstrained path for this register.
The retained `duplicate-result.json` SHA-256 is
`138953b58584bf33bb5dcf483c292fd54e6671af1a24f6f7611dab04d91e924a`.

The four-seed aggregate passed the numerical and LOADED budgets on all
seeds and failed ordinary producer admission on seed 3. The retained
`c32-guarded/summary.json` SHA-256 is
`c17f5b1973160d682e502d4066a2f2100bf526fc657ca7876e2348f0ef00006e`.
The repair accepts flat compiler-generated qualifiers before the
device route class and coordinate suffix. The qualifier only identifies
the subject to resolve in the exact fitted graph. Each site must agree
with the DRC coordinates and pass the same ownership, all-corner slack,
connectivity, and cut and unconstrained path checks.

### C0 = 32 qualifier rerun

All four fresh seeds qualified 97.916667 MHz at
`54fe2d45ac1d97779aa81584d87c90853e84b066`. The fitted netlist SHA-256
is `ad0a331d6f02c529ac55ed93542764fbc5576345f25c61064085a551a836fd33`
for all four, matching the preceding eight-cycle sweep. The intended
PLL counters, ordinary timing and sealing, and the independent LOADED
gate passed for every seed. The largest LOADED measurement was 3.180 ns
against the 3.9 ns cap.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,014.0 | +0.310 ns | +0.001 ns | +7.267 ns | +0.064 ns | 2.255 ns | `7c248185ea5d6cc166d213260d4f4bb47f6bab5117d4581d114eda0bea13b362` |
| 2 | 45,146.1 | +0.806 ns | 0.000 ns | +7.122 ns | +0.114 ns | 2.455 ns | `2eb01945e486a990009dd465e4af04827bacb4577f3aed7726bb693bc8fcf7a0` |
| 3 | 45,089.0 | +0.348 ns | +0.001 ns | +6.702 ns | +0.104 ns | 3.180 ns | `f3c55f16ac1c9b91b04c1223a7d5c89b9987d716de3b6d3687b5491c5bce461d` |
| 4 | 44,810.3 | +0.553 ns | 0.000 ns | +6.968 ns | +0.059 ns | 2.831 ns | `9d15df0c5e6ac0ebda18266decbc8e1d19a9f99e36c37e002b0a46d4de9bbf8e` |

The retained `c32-qualifier/summary.json` SHA-256 is
`0dbfb9dfbe46203a0ccebff68189576dedf7ff1208b21fe403cc5f3229e2635f`.
The exact software comparison from `7e3227f6` to `54fe2d45` matched
all exercised sources and generated simulation inputs. G2, G4 and G5 pass.
Quake announced `DEMO1.DEM` at guest time 13 seconds and rendered the
first inspected Necropolis demo frame at 16 seconds after passing the
prior FNINIT stall. The first-frame receipt SHA-256 is
`97f4feecad82139755b224bf90017442d9709c9f8d32b7568c57a7c2975b2937`.
The [full RTL window](quake-c32-full.json) ended at 24.600 guest seconds.
The [functional replay record](simulation.md#c0--32) binds these results
to the timing-qualified candidate.
The [DE10 comparison](de10-54fe2d45.txt) matches raw netlist bytes and all
four identities. The [D12 comparison](d12-54fe2d45-selection.txt) matches
all seven package selections against the frozen base and measured B.

## C0 = 30 generation preflight

The [generation preflight](parent-c30.txt) reviews the Qsys parent for
104.444444 MHz. The preflight passed the pinned raw input checks and
found one interconnect module rename. The fitter trial uses the
reviewed clock pins and the eight-cycle presenter at
`78123e99621d0fca0cbdd3f16031e9691e12cf3a`.
The actual Powerboat seed 1 project reproduced this generation review
with parent SHA-256
`122e45081148f52a0d779568049425712dbdbc64d87da1642603a87ce347a657`.
The retained `c30-first/parent-s1-audit.txt` SHA-256 is
`9a9f32377236b48fc4aa9da9353fc530b99c87914bed8189320c34385850a350`.

### C0 = 30 initial fits and CDC diagnosis

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 44,785.2 | +0.537 ns | 0.000 ns | +6.676 ns | +0.062 ns | 2.656 ns | `969bffd9605ce5ae3625819890e24abeedd950b1b1675c0c834298796474bf70` |
| 2 | 44,882.5 | +0.801 ns | +0.001 ns | +6.777 ns | +0.175 ns | 2.720 ns | `8b14c01e90a9ac3c97c25f5b75e499f8bc55da51bf05cf2a95ae30e28f4345a9` |
| 3 | 45,173.8 | +0.343 ns | 0.000 ns | +6.892 ns | +0.015 ns | 2.264 ns | `191cb1162dc44c628618f08c634edc81088c1bd7319bc22f66d069027e1cdd4f` |
| 4 | 45,039.4 | +0.736 ns | 0.000 ns | +6.836 ns | +0.159 ns | 2.263 ns | `aaf922f4199d18fe503d81fbacab95a369471d7dfbcf2f3129fb087621c74814` |

All four seeds met the numerical and independent LOADED budgets.
Each was refused by the producer for ten `CDC-50003` rows across seven
held display, GP, window and counter bundles. TimeQuest reported the
machine clock at 9.574 ns. The existing 10 ns bound exceeded the shorter
clock period and, for inbound bundles, the destination period. This was
the sole unreviewed rule on every seed. The retained
`c30-first/summary.json` SHA-256 is
`b2304b329496e3ca915d992482d4b1c1c7fe6e85a5d1c06560c49c7b6a40ca1b`.

On two byte-verified copies of seed 1's final snapshot, Quartus Pro 26.3
reproduced the ten original violations and reported zero with revision
`2b80daf824ca3b91e09c1ded4680b676fb0a8479`'s 9.573 ns bound. The seven
bundles retained identical path coverage across four corners. Worst
replayed setup, hold, data delay, net delay and max-skew slack were
+0.808, 0.000, +2.843, +2.843 and +6.272 ns, respectively.
The retained `c30-cdc-qdb-replay/summary.json` SHA-256 is
`104e06f1e94a5ea94269446384d5e6e6805694a12755a184c62328628b859e01`.
This replay uses the retained placement and route. Fresh fits run the
ordinary producer, all timing corners and the independent LOADED check.

The change tightens the four existing maximum constraints only on the
seven bundles. The bound is the shorter of 10 ns and the machine period,
rounded down with one picosecond of margin. The receiving endpoint
selectors, minimum delay and first-stage synchronizer cuts are the same
as in the original fits. See [Altera's CDC-50003 constraint guidance](https://docs.altera.com/r/docs/683369/current/an-919-improving-quality-of-results-with-design-assistant/clock-domain-crossing-and-reset-domain-crossing-rules).

At `2b80daf82`, the same-checkout DE10 raw netlist and design, evidence,
record and toolchain identities match the frozen merge base. The DE10
receipt SHA-256 is
`f759df1afffa3c08193bd16b4fb86e7b18c9cb0a66c6d360d6479f420110ab32`.
All seven D12 selections match both the base and fitted comparison phase
B. The two selection reports share SHA-256
`76ce53e2bafcea6303720ae12e5094f49457f350ec1882492a374c46094d7982`.
This carries the six measured D12 package matches. PC/XT's RBF comparison
remains open under [#2182](https://github.com/DeanoC/fes/issues/2182).

### C0 = 30 rerun with the clock-period bound

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,156.0 | +0.237 ns | 0.000 ns | +6.771 ns | +0.041 ns | 2.225 ns | `b5a2c8ae7da1615e5dc2a2bb528da5d6c99105501254c1c97531a4e847ff43f5` |
| 2 | 45,084.1 | +0.600 ns | 0.000 ns | +6.912 ns | +0.060 ns | 2.800 ns | `4864dee1b6e43737a6b2bb9576ff18343520609818d31ea8270c1741836ad18d` |
| 3 | 44,928.4 | +0.468 ns | 0.000 ns | +6.623 ns | +0.018 ns | 2.299 ns | `882a290ce8a3ee6fe12bb4eb0f60f2d81291d7ad53a8f4c124bcb0d923e77488` |
| 4 | 45,119.2 | +0.835 ns | 0.000 ns | +6.917 ns | +0.038 ns | 2.717 ns | `beb128524755855eaad5346810526ba5593cb42849283b52b287c0c1b0965d61` |

All four fresh seeds qualified 104.444444 MHz at
`2b80daf824ca3b91e09c1ded4680b676fb0a8479`. Quartus reported zero
`CDC-50003` violations, and all four passed ordinary timing and sealing
and the independent 3.9 ns LOADED gate. The raw machine netlist SHA-256
`efa913c2116c646dafdb6eced89fd89b589de247d614cbd964d748c75e35653b`
matches the original sweep at the same rate on all four seeds. The
retained `c30-cdc/summary.json` SHA-256 is
`5877bc9cde957bf535eb557daa48117a113b71ebe4f3dfadfc0654af41d8cf8a`.
The worst routed setup paths on seeds 1 and 2 capture into the x87
fast-result register. Seed 2's path launches from the fast-request
register. The generated machine netlist identifies these as `_36691_`
and `_36685_`, respectively.

## Upper-rung generation preflights

The [generation preflight](parent-c28.txt) reviews the Qsys parent for
111.904762 MHz. The ten other generated source digests match their
pins. The same module rename accounts for the complete parent and
window interconnect comparison. This record covers generation only.

Chip also generated [C0 = 26 at 120.512821 MHz](parent-c26.txt) from
`318cc3047b95dbec5bd71f87d7d42f9866b09202` and
[C0 = 24 at 130.555556 MHz](parent-c24.txt) from
`d0669db758897dd15947998fc08977be8c852166`. The PLL HDL and Tcl agree on
all counters. At both rungs, all ten other source inputs match the C28
pins. A window interconnect module rename accounts for each byte change
in the parent and interconnect. Both producers stopped at the expected
old parent pin check before fitting. The archived raw inputs and review
reports were verified after transfer to Powerboat.

## C0 = 28 first fits

The Powerboat seed 1 build at `24969db5f1225d5b4edc64597dda3bed55c7a878`
reproduced the reviewed 111.904762 MHz Qsys parent and all ten other raw
input pins. The `c28-first/parent-s1-audit.txt` SHA-256 is
`707b693ba6f504a360261dd750eb362e5a2018c90c298b13a3e45c6488a84556`.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,180.0 | +0.098 ns | 0.000 ns | +6.171 ns | +0.027 ns | 2.628 ns | `82344857d65ea78f06b24865b517be90c4384f50336dc9ec2b4a089c1c89770c` |
| 2 | 45,154.4 | +0.345 ns | 0.000 ns | +6.062 ns | +0.057 ns | 3.196 ns | `22b2d85f94ab761a59b9b2c06be004abfee96c9b677765b95a4c65a529326c7e` |
| 3 | 44,929.1 | +0.276 ns | +0.001 ns | +6.474 ns | +0.032 ns | 2.690 ns | `2a09eef932a5e6fc74a8cef09c9affa1a601ca2eb7e3bbe890bb594f704014ec` |
| 4 | 45,105.9 | +0.142 ns | 0.000 ns | +6.311 ns | +0.167 ns | 2.312 ns | `906f79737005d875d0f355d696a956ec22f75560672ade72fdd4c2844aa8d1bd` |

All four seeds reported zero `CDC-50003` violations and passed the numerical
and independent LOADED checks. Seeds 2 and 3 also passed the ordinary
producer and seal. The retained `c28-first/summary.json` SHA-256 is
`bea561b15cd4f40590cc20ecbb1e0919978c48390202e87809cc62b0ef340f59`.
Seed 1's worst setup path runs from the x87 register file to the enable on
`cpu_x87_sh_src[10]`, through the store conversion's shift decision in
`S_PRE`. Seed 2's lead paths traverse operand B's leading-zero count and
exponent adjustment in `A_PNA`. Both cones are in the x87 wrapper.
Seed 1's producer refused the fit on one uncovered `TMC-20602` row named
`cpu_halted~DUPLICATE_Duplicate`. The existing fallback reproduced all 980
numeric checks across the 35 non-original tension subjects. A separate
Pro 26.3 replay on a byte-verified copy of the fitted database passed all
28 numerical and eight structural checks for the exact new node. Its
7,050 outgoing and 65 incoming data keepers all use the machine clock.
Worst setup and hold slack were +2.049 and +0.147 ns. The detailed
unconstrained-path report contained no match for the node. The retained
`c28-retimed-proof/s1/receipt.json` SHA-256 is
`eccec60883691fd767c28b1cbab6c56a29d7d6b1d7a34474999fec5ba3e4cf73`.

Seed 4 was refused on `cpu_int_wait~DUPLICATE_Duplicate`, at `(51, 25)`.
Its separate copied TimeQuest replay passed 28 numerical and eight
structural checks. All 22,414 outgoing and 305 incoming data keepers
use the machine clock. Worst setup and hold were +1.876 and +0.068 ns,
and the detailed unconstrained-path report contained no match for the
node. The original fitted project remained byte-identical. The retained
`c28-retimed-proof/s4/receipt.json` SHA-256 is
`61daadc6cecd8ffe13be3da51527eebb9768ed0daa8480f377a9da71de13279b`.

Revision `cb91390dbf936393e6e7e4640fe3717d58c193bc` nominates only those
two exact spellings for the existing DX-only live proof. The plain
registers and previously reviewed copies retain their existing basis.
The fitted proof checks exact clock ownership, keeper topology, path
coverage and all four corners before admission. The candidate passed
86 focused tests and `make check`. Four fresh fits started at this revision.
The exact same-checkout DE10 raw netlist and all four identities match
the frozen base. The DE10 receipt SHA-256 is
`1f5af313232d8b09d40af364a8d13eacf15db8f5f2c69b464e654c9ce31af732`.
All seven D12 selections match the frozen base and measured B, with
selection report SHA-256
`76ce53e2bafcea6303720ae12e5094f49457f350ec1882492a374c46094d7982`.
The exact 111.904762 MHz software replays also started at this revision
while the four fits run.

### C0 = 28 rerun with the duplicate-register proof

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,011.6 | +0.329 ns | +0.001 ns | +6.133 ns | +0.002 ns | 2.296 ns | `4fb4223bda591888b8c5b0e18b83816d3e183ce36089fddf26c1760cb75133e2` |
| 2 | 45,193.0 | +0.332 ns | 0.000 ns | +6.022 ns | +0.059 ns | 3.106 ns | `3b9bec73efd8033519d126871d568d7ddb4a04325615a3b0de2e70c35bae1339` |
| 3 | 45,158.7 | +0.026 ns | 0.000 ns | +6.498 ns | 0.000 ns | 2.659 ns | `e0d56667a187e5339c27eaed3f48a070ae123264a4250f32e1fcedf90c17ef05` |
| 4 | 44,985.1 | +0.227 ns | 0.000 ns | +6.338 ns | +0.035 ns | 2.555 ns | `715ca777d63946c8586a071684f65945277de5e690da3e5b846223e158c6614b` |

All four fresh seeds qualified 111.904762 MHz at
`cb91390dbf936393e6e7e4640fe3717d58c193bc`. Each passed the normal
producer, seal and independent LOADED check. The raw machine netlist
SHA-256 `1e2dd2c0c79093c0a82a5ee9d3d117489bd5904fd896c9eb1e34c78d21300425`
matches the original C28 sweep. The retained
`c28-duplicate-proof/summary.json` SHA-256 is
`65d7428d59706adedfc6be34dfcea2a80d7f64ab4a7b1b682535c012bbab34b3`.
Seed 3's +0.026 ns lead setup path ends at `cpu_x87_trans_t_wdata[23]`
in the transcendental engine. The [exact-clock G2 replay](sim-c28-g2.json)
and [G4 replay](sim-c28-g4.json) passed. Quake completed its full 20-second
RTL window and exited 0. The draft carries this 111.904762 MHz checkpoint
while the upper rungs are in progress. The [DE10 comparison](de10-cb91390d.txt)
and the [seven D12 selection comparisons](d12-cb91390d-selection.txt)
are exact to the fitted source.

## C0 = 26 first fits

The 120.512821 MHz sweep started at `fd953c9731d3c18a335dc8737f9a1a8dcecfecec`.
Powerboat seed 1 generated the reviewed parent and all ten other raw
input pins before fitting. The `c26-first/parent-s1-audit.txt` SHA-256 is
`c33f2deb4be012c8ab69e58380f3c8b2ab0774e2d214aff3d23662c67a44a998`.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,071.0 | +0.005 ns | 0.000 ns | +5.537 ns | +0.096 ns | 3.018 ns | `512fe86159457a3bbea009a19f4be789cc34a6b21392b25058d524af3f97bb3c` |
| 2 | 44,958.8 | +0.008 ns | +0.001 ns | +5.400 ns | +0.018 ns | 3.274 ns | `bdf66f2a19f202ca0d9a6c652873f2a4fea6b8f13e71b4d41731fb46d17ea0ca` |
| 3 | 44,851.8 | +0.011 ns | 0.000 ns | +5.281 ns | +0.067 ns | 2.800 ns | `5234fd0d9ba4a03c4628f3024da969b5a0e9a504e9d083989799b801ea5f63fd` |
| 4 | 45,025.1 | +0.019 ns | 0.000 ns | +5.468 ns | +0.064 ns | 2.833 ns | `f70cc9dd35fe857adb06cb2cf63b36bc06d78a9aacd5e2f735a62017b39fc1c5` |

Seed 1 met the numerical and LOADED budgets. Its worst setup slack is
on the held machine-to-pixel display command path from `payload[21]`
to `pixel_index[5]`. The worst machine capture path is the held count
transfer from `pixel_counts_copy[54]` to `pixel_counts_snapshot[54]`
at +0.110 ns. The leading strictly machine-clock path runs from the
window bridge port 2 `head` to the arbiter's `writes_owed[7]` at +0.133 ns.
The fit reports zero `CDC-50003` violations.

The registered producer refused seed 1 on unreviewed `TMC-20602` rows
for `cpu_l1d_stall~RTM_3SCLR_dff`, `cpu_m2_load~RTM_3` and
`cpu_halted~DUPLICATE_Duplicate_1`. Each exact register passed the
fallback's 28 numerical checks. The private Pro 26.3 replay passed all
84 numerical and 24 structural checks. Each of the three subjects had
only machine-clock data keepers, and the detailed unconstrained-path
report contained none of them. Worst setup and hold were +0.829 and
+0.123 ns. The original fitted database remained byte-identical. The
retained `c26-retimed-proof/s1/receipt.json` SHA-256 is
`9d37fb736014c727e17b57cd05cad77cb48a07253cf1a3cf98074d3a10ac06bf`.

Seed 2 passed the normal producer, seal and LOADED check. Its worst
machine-to-pixel setup path is the held display command `payload[7]`
to `pixel_argument[7]`. Its worst machine capture is the held count
transfer from `pixel_counts_copy[57]` to `pixel_counts_snapshot[57]`
at +0.013 ns. The worst intra-machine path is the x87 fast request to
fast result at +0.190 ns. Seed 2 reported zero `CDC-50003` findings and
no uncovered TMC rows. Seed 3 also passed the ordinary producer, seal and
LOADED check. Its +0.011 ns worst path is another held counter snapshot
transfer, from `pixel_counts_copy[48]` to `pixel_counts_snapshot[48]`.
Seed 4 passed the same gates and its +0.019 ns worst path is counter bit 58.
All four seeds met the numerical and independent LOADED budgets. The
retained `c26-first/summary.json` SHA-256 is
`be414a1e3ad8120ac8096e0ccf05eba0bbebd1fae3b6a80b6cd95e524faa6a45`.

### C0 = 26 rerun with the control-register proof

Revision `c18e38c886284fecabe903866fe77f9e040d1ca7` sends the three measured
names through the existing DX-only live proof. It passed 86 focused tests
and `make check`. The exact same-path DE10 netlist and all four identities
match the frozen base. The DE10 receipt SHA-256 is
`0e6df424b05ee734671fd0a0a458cb746e9abca5e8e3a744bd92c27190f2f82d`.
The seven D12 selections match the frozen base and measured B. Four fresh
fits started at this revision.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,228.9 | +0.025 ns | 0.000 ns | +5.339 ns | +0.081 ns | 2.447 ns | `c5ddf341f3b13b6d3679625864f036caa499fd17c2fdd4eab0e394f9640b264f` |
| 2 | 45,235.4 | +0.017 ns | 0.000 ns | +5.494 ns | +0.169 ns | 2.349 ns | `3b251d4cb63385bf4dc741e3c0a09afb054011cae756853c67896ae884d624f5` |
| 3 | 44,950.0 | +0.005 ns | 0.000 ns | +5.611 ns | +0.012 ns | 2.742 ns | `950be4d265198e94331150221346658f1b8dde64ae7af85eafe5828c69b8cb7e` |
| 4 | 45,079.4 | +0.008 ns | 0.000 ns | +5.332 ns | +0.207 ns | 2.585 ns | `390484b47efb02966ead0c8f1b536235aa5ae12d36806e72175d66c8e486c906` |

Seed 1 was refused on three new `TMC-20602` names, `cpu_int_wait~DUPLICATE_Duplicate_1`,
`machine_at_decoder_cycle_port[10]~RTM_1DUPLICATE` and
`machine_at_decoder_state[1]~RTM`. The complete copied replay passed 84
numerical and 24 structural checks. The original stayed byte-identical.
The `c26-control-retimed-proof/s1/receipt.json` SHA-256 is
`e1225d9cd00d5bb47459c7c0f2182fb7a4b1371d9f5b07b273d5aa8983c2c313`.
Seeds 2 and 3 passed the ordinary producer, seal and independent LOADED
check. Seed 4 was refused on `cpu_l1d_q_tx~RTM_1SCLR_dff`. It passed its
28 numerical checks, with worst setup +1.343 ns and hold +0.075 ns.
The copied replay also passed eight structural checks. Every data keeper
used the machine clock and the complete unconstrained-path report
contained no match for the register. The `c26-control-retimed-proof/s4/receipt.json`
SHA-256 is `217960740d59ecddfd26dcf91eac5179d89a98bee761508d94857b1c4b8dea61`.
All four seeds passed the numerical and independent LOADED budgets.
The `c26-control-proof/summary.json`
SHA-256 is `908d4cd891d09b61eaf1ccee966c6c7594d5111445918d9091d64dfa406942ef`.

### C0 = 26 rerun with the measured retiming spellings

Revision `9259873f54389b4956430acf9c01942f3f1af8ae` matches the measured
unlocated retiming suffixes on existing control-register stems. Each
matching fitted register goes through the DX-only live proof. The
same-path DE10 raw netlist and all four identities match the frozen base,
and all seven D12 selections match the frozen base and measured B. The
DE10 receipt SHA-256 is
`cb6a36cfa3b3700911ab3afc1b30ad9e42d52b7e34452e46fc86c5108eb248f3`.
Seeds 1, 3, and 4 passed the ordinary producer, seal, and independent
LOADED check. Seed 2 passed the producer and seal and exceeded the
independent LOADED limit.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,012.0 | +0.007 ns | 0.000 ns | +5.279 ns | +0.092 ns | 2.511 ns | `48973014f1fc038b27ab63dd93de6eceb2927206c89dbd665c66e7aae6a3d2f9` |
| 2 | 45,051.9 | +0.022 ns | 0.000 ns | +5.302 ns | +0.029 ns | 3.999 ns | `3cebe17e0bb94d3f0205f5421642f8bb9bc88af7dca43774e5212764f580561f` |
| 3 | 45,148.9 | +0.001 ns | 0.000 ns | +5.698 ns | +0.029 ns | 2.499 ns | `7e86e89e4e4acc9faad87b29f0c2782ea35b8ada51c4186ec8c8cf63134e6f5e` |
| 4 | 45,155.5 | +0.005 ns | 0.000 ns | +5.358 ns | +0.042 ns | 2.649 ns | `0ecc7d622e23e45dadc215f70aaab7e6e2f76aeebcd099eaff6e1850c8787190` |

Seed 2's `machine_presenter_loaded_flag` to `machine_presenter_loaded_sync[0]`
route measured 2.474 ns of data delay and 1.525 ns of absolute clock
skew in the slow fix4 0°C model. The sum exceeds the 3.900 ns limit by
0.099 ns. The slow fix4a 0°C model also exceeded the limit, at 3.996 ns.
These measurements refuse the seed. The row and epoch payload is a
separate crossing. The retained `c26-name-proof/summary.json` SHA-256 is
`cf36d93bb2ab7d2c723e175d3daf5d4da1a819a63ef458c33ac0d1788008bb41`.

Revision `62012d5b5520ca56e3a1a4f515c002766b52c6b3` adds a 2.250 ns
fitter data-delay target to the same false-pathed LOADED flag hop for
the 120 and 130 MHz rungs. It reserves 1.650 ns for skew in the
independent 3.900 ns allowance. The independent check still reports data
delay and absolute clock skew in four corners. The five synchronizer cuts
are identical in a Tcl probe at all six rung periods. DE10 netlist bytes and
all four identities match the frozen base, with receipt SHA-256
`cac1fbbd7109183f0366421387ca2c70408538b67ae87e1b4a6aa66f95d07427`.
All seven D12 input captures match the frozen base and phase B.

The Pro 26.3 copied-project replay on `chip-30x0-ai-dev` reproduced the
original four `-false_path` measurements and reported one path per corner
for the separate 2.250 ns target. The target's slack on the old fit was
+0.104 ns at slow fix4 100°C, −0.177 ns at slow fix4 0°C, −0.176 ns at slow
fix4a 0°C, and +0.869 ns at fast fix4 0°C. The datapath report includes the
intermediate logic cell and the register setup term. It excludes clock
network delays. The retained `c26-loaded-route-datapath-chip/receipt.json`
SHA-256 is `4136a0d2506b9b88991565e4b493e805a7de0b4335f0e6ee0981c7594060f4a9`.
Powerboat reproduced all eight timing lines, with log SHA-256
`be3ce2d59a04555a6d3118d674232501eae8a8d9f682ebc41348a4cfffd3d817`.
The four-seed rerun starts with seed 2 at `62012d5b` on the same host and
checkout path. `make check` and 40 focused tests passed at that revision.

### C0 = 26 rerun with the LOADED datapath constraint

Seed 2 staged the exact revised SDC, SHA-256
`82eb66ea36764179c201ee1d9f625e7af25df9b26499efb6c080b1a2b30f7d07`.
The raw Yosys machine netlist matches the previous sweep, with SHA-256
`aaeed478d95144545948989a41be07f1a30a17d634a45ceb68a63cfa0321de01`.
The Qsys parent audit reproduced the earlier Powerboat receipt, SHA-256
`c33f2deb4be012c8ab69e58380f3c8b2ab0774e2d214aff3d23662c67a44a998`.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Seed summary SHA-256 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,193.7 | −0.042 ns | 0.000 ns | +5.569 ns | +0.009 ns | 2.577 ns | `a1f897e36550ac201fdf7d821662d5cabc7e47965c1d0f4b68582bbf83ee0af9` |
| 2 | 44,915.2 | −0.096 ns | 0.000 ns | +5.335 ns | +0.079 ns | 2.669 ns | `32dbdb3ce3ff69d9bcd05358a90a941be8b9c42aa2b35635d60b8e674500eb59` |
| 3 | 44,875.4 | +0.026 ns | 0.000 ns | +5.769 ns | +0.211 ns | 3.380 ns | `e1f9621970a78e83dc1c47f7d3eeef15b649bce8570384fa27826d4146db0f67` |
| 4 | 45,059.3 | +0.005 ns | +0.001 ns | +5.607 ns | +0.007 ns | 2.506 ns | `bc175e405db1b71ca60cf450194f9bdd964035809a61223371f5414e10ca699f` |

The independent LOADED gate passed on both seeds. Seed 2's worst corner
measured 1.188 ns of delay and 1.481 ns of absolute skew. The registered
producer refused both seeds on numerical setup timing.

Seed 1 has one failing transfer, from the display's held command
`payload[3]` to `pixel_argument[3]`. Its slow fix4 0°C path has 5.276 ns
of data delay, 1.502 ns of adverse skew, and 1.550 ns of uncertainty.
The same path has −0.033 ns slack in slow fix4a 0°C. The reply transfer
is a separate path with +0.006 ns slack at the tightest corner.

Seed 2's slow fix4 0°C model has 39 violating setup endpoints. Of these,
35 are the machine-clock path from the x87 DSP multiply tiles
`mult_36~mac~AY_REG` and `mult_37~mac~AY_REG` to fitted copies of
`_36692_[150]`, generated from `cpu_x87_core_fast_res[155]`. The other
four failures include paths in the x87 format selection, fast-result
writeback, and integer decode enable. They also fail in slow fix4a 0°C,
whose worst setup slack is −0.050 ns. Both seeds finished with
nonnegative hold, recovery and removal, and zero CDC-50003 findings.

Seed 3 passed all gates. Seed 4 passed the ordinary timing and independent
LOADED checks. Producer admission failed on the sole unmatched advisory
`cpu_m2_load~RTM_3ENA_dff`. The copied final database passed 28 numeric
and eight structural checks on that register, with setup +0.841 ns and
hold +0.073 ns. All 22,891 data keepers used the machine clock. The
detailed unconstrained-path report excluded the register. The retained
`c26-loaded-cap-retimed-proof/s4/receipt.json` SHA-256 is
`58fdd714daf8e7f1c5ecc4fc89bcb14fe4765d5dde9d880cc12c98ffb7a311e9`.
The retained
`c26-loaded-cap/summary.json` SHA-256 is
`f310bd24757c6886d9cf9f4bf99fb192aaad3d470064fca9e0904244d566f24e`.

The next candidate, `80de4c6af4811e1ab934af504d22e6a3897eae58`, selects
the existing seven-stage x87 fast pipeline at the two upper DX clocks.
The active operation still waits for the backend result and can take six
additional cycles. It also adds a clock-relative datapath target to the
display command word only, 5.046 ns at 120.512821 MHz. A Tcl comparison
preserves the command's clock-aware setup and hold constraints and all
five constraints on the display reply. All 43 focused tests passed for
the pipeline selection. `make check` also passed with the display
constraint. The exact combined source reproduced the DE10 netlist and
four identities, with receipt SHA-256
`0bc6a25f2cddfd7529cc9d50e5e90794dcf3e36994ef187fda8d553cf3c4f4eb`.
All seven D12 input selections still match the frozen base and phase B.

On the copied seed 1 database, Quartus reported the original 8.296 ns
clock-aware setup constraint and the new 5.046 ns datapath constraint.
All 31 command paths stayed visible at every corner. The 248 setup and
hold rows and existing timing exceptions reproduced, and both
CDC-50003 checks returned zero findings. The measured flag cuts also
matched. The old route misses the new bound by 0.240 and 0.239 ns in
the two slow 0°C corners. The retained
`c26-command-datapath-qdb-replay/receipt.json` SHA-256 is
`70b04683bae6f75ae6418e1da1341ea5e45a1428d976074777e79c931880c154`.

The exact 120.512821 MHz simulations started in parallel with these
fits. Both compiled game models use the guarded eight-cycle presenter.

## C0 = 24 prepared inputs

The 130.555556 MHz candidate `fb398cee10f08a2a07af695fed453b417d05e037`
applies the reviewed PLL and Qsys parent pins directly on `9259873f`.
Its raw DE10 netlist and design, evidence, record and toolchain identities
match the frozen base. The DE10 receipt SHA-256 is
`11e02b355c9df0abf7eecbcefe644dbd7a6f399b345ed098f61b885dad599d33`.
All seven D12 input selections match the frozen base and measured phase B,
with comparison SHA-256
`76ce53e2bafcea6303720ae12e5094f49457f350ec1882492a374c46094d7982`.
Revision `e707acc6cfaf683b0bd6eff0b6cb0a6e44d34536` keeps those exact clock
files and adds the same LOADED flag data-delay target as `62012d5b`.
Fresh comparisons passed for the DE10 netlist and four identities and
all seven D12 input selections. The DE10 receipt SHA-256 is
`0abb98bab58faecff2f627c0ee94716b02c073d361b8e069cccd03d381f63cbd`.
The D12 receipt SHA-256 is
`5f8fb29652b306c98ceaf5f4298112820a851bf2f767c92b58fc2e2bc249546f`.
Revision `b7dcf242408358db8d57075d486caeb989bebe19` adds the exact 130 MHz
clock files to `80de4c6a` and selects the same pipelined x87 wrapper. The
combined C24 receipt confirms the pinned PLL and parent digests, unchanged
SDC, 43 focused tests, DE10 netlist and identity equality and all seven
D12 selections. Its SHA-256 is
`db5d3c67fd8862a4a52ec0813456961a0f754076579ae5d770635771b60e1d8a`.
The C0 = 24 fits follow the C0 = 26 qualification.

## Main integration after the floppy-drive change

[PR #2101](https://github.com/DeanoC/fes/pull/2101) changed the shared
AT core and exposed an outdated raw source pin in the PR's merge CI.
The next C26 validation branch merged main at
`2f19382bfe294d23a99742adebabd09b0ebc2793`. Revision
`d6f0c0169c4a4835fd60e1ee5f7b3a8c76be0e49` binds the signoff basis to the
new AT source. The new FDC written bits use the machine clock and are
left unconnected by the DE25 ATA-only derivation. The existing AT DMA2
derivation and its anchor still match. `make check` and 22 focused tests
passed, including both formerly failing signoff-pin checks.

Fresh DE10 netlists match the new merge base byte for byte, and all
four recorded identities agree. Both netlists have SHA-256
`93b03a57c88b281db390847952fa4fd57a9e75edde026344381e3c37a126ddbd`.
The receipt SHA-256 is
`0297bacf7eb0809a3709da07a561f0c9b0b8c10825c5ee5b5a237c3eb9eed44c`.
The timing and game receipts above use the earlier merge base. The
same-path locked Yosys comparison also found a changed raw DE25 machine
netlist. The synthesized ATA-only machine retains verifier pass-through
state and muxing. The integrated machine needs fresh fits and game
replays.

The integrated C26 rerun uses `4fe7bada67a8e6689bc4c3dc480a844db72e0076`.
The measured `RTM` enable suffix selects the existing DX-only live proof.
Its per-fit topology, numerical and unconstrained-path checks still apply.
The fresh DE10 receipt reproduces the same netlist and four identities
against both `2f19382b` and `d6f0c016`. Its SHA-256 is
`5f45323f1bb6b26f42a8bef5e5370df62bc1631ffd185dd8c007a3bc72b4faa8`.
`make check` and 48 focused DX, signoff, window and producer tests
passed on this integrated source.
The first seed queued was seed 2. The provisional G4 and G5 replay uses
the new shared machine source with the DE25 ATA-only boot and drive
settings. The next C24 ref `7b3e760deb0b10f7cb0bb824586ce8aae15359fe`
contains the verified 130 MHz clock files on this exact C26 source and
remains gated on its qualification. The 130 MHz ref reproduced the
DE10 netlist and four identities against the new base and C26, and
matched all seven D12 selections against both. The receipt SHA-256 is
`540e9f23d543c8b4f4ae2def45be0a0f2110dcb9a6bc6dc8c31cc8fc76566531`.

Seed 2 fitted 45,127.6 ALMs and 42 DSPs. Worst ordinary setup was
+0.004 ns and worst hold was 0.000 ns. Independent LOADED was 2.820 ns
and no unreviewed CDC rule failed. The registered producer refused the
seed because nine display-command paths missed the 5.046 ns data
delay bound, with −0.178 ns worst slack and −0.839 ns total negative
slack at slow fix4 0 °C. The worst path, `payload[11] → pixel_argument[11]`,
had 4.900 ns of interconnect delay and 0.308 ns clock-to-Q. Eight of the
nine failing pairs occupy adjacent tiles. The fitter reported about
3.4 ns of hold-routing delay on several command bits, and independent
fast-corner bus hold slack was as low as +0.005 ns. The fitter recognized
both the 5.046 ns data bound and the 8.296 ns setup bound while running
with router timing optimization at Normal. The seed summary SHA-256 is
`6f285be5f2574fe0471f56df306e18c8d0edee5210f59f6eed1b277a62ebe59a`.

Revision `3fa87187a579d401dfbc0f739e29b665b45c480a` increases router
timing optimization to Maximum for the 486DX with C0 = 26 or 24.
Registered seed 2 preparation from the same vendor archive adds that
single assignment to the pre-generation compiler QSF. The SDC is
byte-identical at SHA-256
`4bf5d251c642ff4bf195b45e55e7593b22fc7ebf34c7200091a4b1330e18457d`.
A separate dry build of the original source at its original BUILD_ID
changes anonymous names in the Yosys netlist when staged in a different
checkout. The two registered runs therefore provide independent candidate
evidence.
The dry-build control receipt SHA-256 is
`df75ac5eb067fa6cff50c356149fe2eb6e9b7fff1ca491bb33a2b878f332da4c`.
The exact candidate must pass the normal timing, seal and LOADED checks.
The same-path DE10 control still matches all netlist bytes and four
identities against `2f19382b`. Its receipt SHA-256 is
`47b339aa08947248e983b8f89318ba0ad30532201e8cec77ad0a971bdca39ec5`.
All seven DE25 D12 input selections match `4fe7bada`.
The C0 = 24 child `bbf65586855a8c0abf0a1be082ce9aae6d75c9e7`
has the expected 130.555556 MHz clock inputs and remains gated on C0 = 26.

## Running a seed on either builder

From `sources/misteross`, the validation wrapper runs one registered seed
and collects the reports inside the same capped lease. Set `QUARTUS_ROOTDIR`
to the pinned Pro installation and choose the toolchain slot for the host.
Supplemental TimeQuest reports use a private copy to preserve the compiled
database and producer receipt.

```sh
python3 scripts/quartus_jobs.py run --edition pro --cap-minutes 300 \
  --label 486dx-2263-c34-s2 -- \
  bash ../../docs/validation/2026-11-25-de25-486dx-clock/fit_seed.sh \
  "$results" c34 2 "$vendor_archive" "$cache" "$gpu_router"
```

For `quartus_jobs.py remote`, use the pushed candidate, replace `run` with
`remote`, and pass `'{run}'` and `'{cache}'` for the result and cache roots.
Capture the host and run directory for each seed and keep the evidence on
its build host until the per-rung comparison is recorded.

Run `collect_fit.py DIVIDER BUILD_DIRECTORY LOG_DIRECTORY --seed SEED`
on each builder. Keep its JSON even when the seed fails. Combine all four
reports with `collect_fit.py DIVIDER --seed-reports SEED1 SEED2 SEED3 SEED4`.
The collector requires four distinct seeds at the same committed revision.

## Software checks

`make test-changed TEST_CHANGED_ARGS='--base da4acd2ac'` at `2902c12fe`
passed the parent suite and found three FPGA test failures. The pc386
producer fixture still expected the original 61.437908 MHz DX clock.
It now checks the clock and audio ratio of the selected DX model. The
three focused pc386 producer tests and all 16 DX tests passed after that
correction.

The other two failures reproduced at the merge base. The DE25 PC/XT
standard-shell test and PC/XT PPI reset test use waivers the system
Verilator 5.032 cannot read. The PPI reset test passes on 5.054. The
DE25 video compile has an additional reproducible 5.054 failure tracked
in [#2275](https://github.com/DeanoC/fes/issues/2275). Main fixed that
failure in [#2292](https://github.com/DeanoC/fes/pull/2292).
