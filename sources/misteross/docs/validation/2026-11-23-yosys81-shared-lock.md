# Yosys #81 shared and per-core lock validation

Date: 2026-11-23 (Europe/Sofia)  
Base: `ebe2477b878477be197112a411646be59efa982a` (`origin/main`, after merge)  
Candidate source revision used for included producer builds: `247e30c12b1b9ceaa42d0a301882fe2dca3193a3`  
Yosys: `3a5accbd78c526ab7bae32640044a2e062a3cfc9` → `d273c1cb774f70d58c890745ac1b7dd106d32a26`, squash of DeanoC/yosys#81 (rehash hashlib on insert; fixes threaded OPT_CLEAN crash yosys#77).

## Scope and toolchain provisioning

The shared `toolchain.lock` and core-local locks for Apple II, Atari ST, Coleco, Menu, Pong, RAM Tester, RISC-V, SG-1000, SMS and Spectrum select d273. Coleco moved from `5391eeb1`; its socket-v2 producer was built on both pins. The ZX81-family, C64 and PC/XT locks remain at `3a5accbd`. The separate i386 and pc386 locks are untouched for the independent `yosys81-pin-bump` job.

All producer invocations used the normal `CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains`; no alternate cache root was set. The pinned Yosys and dependent nextpnr/Mistral tool slots were provisioned and authenticated through the registered producers. Build summaries and package manifests record tool commits and executable digests. Builds used the production HIP route where supported. No kit was accessed.

Included package and splash artifacts were rebuilt from source `247e30c12b1b9ceaa42d0a301882fe2dca3193a3`. Netlist drift and OPT_CLEAN comparisons were measured earlier at `adae9de9`; intervening changes were documentation and stale test expectations, so HDL, constraints and generated netlists are unchanged. After the builds, the branch merged `origin/main` at `ebe2477b878477be197112a411646be59efa982a`. That merge updates PC/386 and Verilator simulation wrappers, but no included producer inputs; artifacts remain valid. The merged-source full test suite was rerun. Excluded ZX81-family candidate measurements also come from `adae9de9`.

## Passing package and bitstream results

Every row has a different synthesis netlist byte stream (`netlist_identical=false`). Byte drift is expected from the changed hash-table iteration order and subsequent optimization/mapping; the next section records structural changes and unused logic cleanup. All passing production builds met their normal route and seal gates. `package` is the content-addressed package directory ID. Payload is the package RBF SHA-256, or the sealed splash RBF hash.

| Core | Old BUILD_ID | New BUILD_ID | Old package | New package | Old payload SHA-256 | New payload SHA-256 |
| --- | --- | --- | --- | --- | --- | --- |
| Menu | `92ae48c4e6433b21f8712c18a0575a05` | same | `d14a0ba1ffbc04e8b2850658d19a86ee2d8053f0105bab585bd0587baede35f4` | `0f1f1f3c221e0bf22acd2187ce92299d7bf1807da40652dcf069043eed076f80` | `f7b3076ee2c126fb5f1f99befae9a0e4bdef3ae10133044f82f519b9d17f8c14` | `266608155adf540feaced7ce8782d1c0472e19cf90b2affb535c1c3da9d7d005` |
| Pong | `745a6f5f911a5acb39262d5b4d11153f` | same | `0e9116b28e6bd81129eaeeab854e581085b3a6c67b51f59f13e731d39ac235cc` | `883693c8ebb8e24e1843fb2f06cd7d103f5544f87008035eef33743ec6c669d9` | `0703da2c5f7ab57c7ff036d03c7715dd8c7dcf53bf62ade45568340e7fa9e2b6` | `ef6a50b1f2901d1d974f9ebe0d288da847f2ea040466aa4f77a77d1c896ff31a` |
| Apple II | `7f657f92d4c8d1069bd8f23da903a547` | same | `404aa2a81cdf6420f5e00ba843fe89110e24ddccc07ff3364adc6c0a50f001a0` | `f9f96a8ff484d49df82d6528932a3313f9fbd610463ec112df6f13ecbad66f05` | `a5793fdf9a5d2b72045519265008851faca26c7d1c6e4ac6d8e1d682847ec50b` | `3ddd5598988cbb64e3d3fdf6e84e6df57c0c3a8e8611af599ca36c627eaf99da` |
| Atari ST | `e2c3db4c45bef1e24be68b3e9faadd01` | same | `97e5e5e40600d81f5ca768315af538b17e3f66d52e8129262de3c14f442ff2e6` | `9422c86bd45408ad1cec9cb988bbc007566674d217fefc2975fa56f6b0c2a7e8` | `28c3c016039dc218e0e0ddddd297aa8bdc54b046a1614c200b22eb7233bbafc6` | `d2fe393124d7dde895660ff1a981b5a6f8b2297cf52c07bb9aa8527fd1b246bb` |
| Coleco socket-v2 | `3916308c77b61aaf97515c9212d672da`* | `65ac416075f795224c765fe49c708536` | none (old producer missed timing) | `352d0f3e948c8d2e34f1ac1b83cc27ffc7b35b0398fa64bd61dd0438b8cc1435` | none | `dadd8f7dc07ec93ba7b47629d765213f7bec7b611fe5cdc5b40a2ffb86c79dfe` |
| RAM Tester | `64ccfac61752660e6c5a2c772bf6c9d9` | same | `18fd8e6eeb1900a180b0338ace653470bc0f8784290efc9f230e615540cc5170` | `beae4630a3e4874240bd9fb08ef1cfde5b42bb1198be64fbf69767624c1e523f` | `f36df69c27bfc61834ef62c5af758a2f6025ec76c02a0eeafb4e96c269b4bda3` | `564d11fe544da60c8e5de6bd032d413c7be1126060c1e43d496b5825690be815` |
| RISC-V | `c687fd8b427e3754e7656f06b7c56c04` | same | `7c7f9b7229adc8822a7af6d013d6c45c9e1740fecce2db09e6fc050c36aa67d6` | `e21f5d2bf6008e398ea43bf74e74cf5b24bcca705d7bba03562f97c03dd5d408` | `a7322b99a8cebee51ca406ead5ad318594d72418e152bdc541c3192532943628` | `5b241cac3a8329425ccf38358a104a2b61128bf8e8e0c97f35e35632f7bf0f06` |
| SG-1000 | `e5c109107ff51c0aa222ff47902c60e6` | same | `e5ac00d80d759667550b451cf41d996b8206df0ca58517875a345cd64d89812c` | `42c463c1a73852ddb1bd276090d0167cf8009e24f819d045c931da12536cf711` | `73b5f50b644533f4b5a444918a25616eaedbec71f495408deef450487e657e06` | `0cb1e64fe25e23686526ca5382c0da29f8fb07a81a49b88286d0d8a024427663` |
| SMS | `3dbbb825f01bed54747f65c044a06901` | same | `672838d8275229c2ad44131bad33fa20abeec27a29fa9896ca6589c7936e1dcc` | `029f830e6181faaf698e282254c50fee52e2f83fc435b8666aa0a2b5239bd0a2` | `43fac2b3e7f46cfd38bc36a5d2eb3080c60441215a2dc7efcf383d1c0dadc03d` | `31c57c18f304c8018d2c3cf6c2229b94c8d8ccf181cd5e4006291bc2748ec723` |
| Spectrum | `e54659a416ed8dc1dc85153f2ed0f458` | same | `e1e7ab896073abd47b25a21a11c9857e661fcd020a5c25bf30108f24bb163750` | `0e976cb562280b90ea5820901725e0b81badb36fd96ea5a87a1852991383cdb7` | `77a3a3912484f0d3f0aa653a12c1196ae3a558f891ffddd3396d1257d0a85c53` | `b6ebdb2c2073b062e8e49243826750e8dae977b046c0a9a7a62f36fa060181fa` |
| Sealed splash | `1a86c8f1dc9ea5e71965e0db1fb00588` | `37b81f5bc7fbcfb72a60534d58f028d9` | tracked RBF | tracked RBF | `64f1f16daedba4ed19179bd5d7c6f3eb3058a688d15441c8c8d89e113060f182` | `45be3862a3e419d051b71d5170a39b8894c6f33a9a8f13d6be58d68940f00beb` |

*The old Coleco ID was recomputed from retained `build-inputs.json` with the producer's `build_identity` function because the timing-failing producer wrote no summary/package. The old pin produced no passing Coleco route/package; d273 passed.

## Netlist drift and cleanup accounting

The parsed `synth.json` outputs are compared from the same source closure and device flow. Cell and netname totals below span emitted modules. The final OPT_CLEAN pass removed unused internal cells/wires only; no top-level output or live state assignment was removed. The LUT-type changes are mapped-logic structure changes after the hash-order-dependent optimization traversal, not evidence of functional failure. Simulation/frontend tests and normal producer/timing gates are the functional checks. Removed cells were dead logic; removed wires were unused internal aliases/nets with no live consumers. Generated internal names also change with iteration order.

| Core | Old cells / netnames / modules | New cells / netnames / modules | OPT_CLEAN old cells/wires removed | OPT_CLEAN new cells/wires removed | Largest mapped-cell changes (new minus old) |
| --- | ---: | ---: | ---: | ---: | --- |
| Menu | 9687 / 5708 / 62 | 9681 / 5593 / 63 | 758 / 143014 | 793 / 143619 | ALUT_ARITH -86, ALUT5 +65, ALUT3 +34 |
| Pong | 1453 / 2246 / 57 | 1494 / 2325 / 58 | 74 / 21095 | 73 / 21317 | ALUT_ARITH +36, ALUT4 +6, ALUT6 -6 |
| Apple II | 7444 / 5789 / 61 | 7427 / 5773 / 62 | 621 / 117522 | 580 / 117144 | ALUT4 +151, ALUT6 -109, ALUT5 -32 |
| Atari ST | 29146 / 19543 / 67 | 29180 / 19460 / 68 | 2241 / 505933 | 2227 / 506060 | ALUT3 +104, ALUT5 -103, ALUT4 +34 |
| Coleco | 5413 / 4500 / 61 | 5420 / 4471 / 62 | 366 / 115707 | 350 / 115616 | ALUT4 +112, ALUT5 -45, ALUT3 -41 |
| RAM Tester | 25029 / 10659 / 57 | 25081 / 10711 / 58 | 3190 / 396869 | 2942 / 396642 | ALUT6 +228, ALUT2 -236, ALUT4 -142 |
| RISC-V | 3941 / 3192 / 61 | 3870 / 3043 / 62 | 373 / 59262 | 268 / 58837 | ALUT4 +74, ALUT6 -65, ALUT5 -77 |
| SG-1000 | 6889 / 5467 / 61 | 6916 / 5543 / 62 | 592 / 143008 | 590 / 143039 | ALUT5 -89, ALUT6 +48, ALUT4 +29 |
| SMS | 6673 / 5387 / 61 | 6712 / 5406 / 62 | 552 / 133003 | 539 / 133019 | ALUT4 -36, ALUT_ARITH +29, ALUT6 +22 |
| Spectrum | 14604 / 10135 / 62 | 14480 / 10062 / 63 | 1340 / 278319 | 1272 / 278255 | ALUT6 -42, ALUT2 -32, ALUT4 -26 |
| Splash | 729 / 1502 / 57 | 731 / 1503 / 58 | 9 / 7375 | 9 / 7380 | ALUT3 +2, ALUT4 -1 |
| ZX81 candidate (excluded) | 10961 / 7758 / 62 | 10916 / 7782 / 63 | 1066 / 200598 | 1105 / 200653 | ALUT6 +127, ALUT5 -88, ALUT3 -87 |
| ZX80 candidate (excluded) | 10103 / 7014 / 62 | 10225 / 7223 / 63 | 1027 / 185244 | 1058 / 185656 | ALUT6 -175, ALUT3 +159, ALUT4 +132 |
| TS1000 candidate (excluded) | 11009 / 7799 / 62 | 10912 / 7756 / 63 | 1089 / 201078 | 1090 / 200811 | ALUT3 -138, ALUT5 +77, ALUT2 -43 |
| TS1500 candidate (excluded) | 11022 / 7835 / 62 | 11013 / 7848 / 63 | 1068 / 203481 | 1100 / 203515 | ALUT3 -99, ALUT6 +79, ALUT4 -29 |

Direct `synth.json` SHA-256 comparisons (all differ):

| Core | Old netlist SHA-256 | New netlist SHA-256 |
| --- | --- | --- |
| Menu | `f7aa531fc2698993842354406f2334c7b194ddc7d3ec5aeef51939568ce3ddb8` | `3d43ca9f6c021b6b017e5c5de24c86cb6d657fd324a23d27afecaa9b43e2240d` |
| Pong | `39a801241820672e7a61ff385b53bc0f3d875133aa2e9335d829a929a8b86c25` | `2ce3bcb2a8f04ceee7f6839c38cd77b4f8a7aba1b753db9bca8bc076faa2e5d5` |
| Apple II | `6b50bc8325e203ec9431148503e5a90e79bef11231ca56dfae3a81021c9d46d5` | `85be5f6398b46cca299f539908fe31ed7906ceb42d1a3159b87a22a94221549c` |
| Atari ST | `31e11e86896ebc75edd2f579b04c2addcf44977625006580041c284182f7fce9` | `a26233b87add052a1eac34ddbe74e729192f6ec7ecc9247f631058ba20f1c347` |
| Coleco | `1631ffe844a42067b76d61d87ab047a23fd4abc66924c0b7b0ee50f112ab2ec2` | `eb3ed32e79c4a4e5fbb6cea8c50ef3d044794aa758c7a86da54088bba3588aed` |
| RAM Tester | `95c87832a37dea7e2a2d9436d02740d81899398f0f46057a086a49be7a7cc93e` | `0443fec85c80bc00bdc70881ae166d383f17cccf55c662101808ccfd8dbf6235` |
| RISC-V | `5d76a0382a5a1aea068e5778da066303b4140498c4c545f354ed022125202e05` | `0ae1de40585a805a9a6c61755854ea14f4c35922b0174c7e8a3014c7a8f2e008` |
| SG-1000 | `4ca0ee8f6957d6cbbaa8f3775bd2af7d84a709da782dec07a00cfa10145fa010` | `87047c5b08b6f6b8d97202a071ab026279f242f67feac36ae1ca42f4a1983c4a` |
| SMS | `f0da0d7581634733f7cb34d11a01c62bc6b7e4cf1d54f7d0d5bfb44557a7cb67` | `4158504e40ba28e34697ed03a327196c1a177ba80b29021b418bd7b309f1b07a` |
| Spectrum | `adbf37e2b3f1d3c3c3527affdc7594b022e83cc10e289b9b1e33b4fba16e1efc` | `9601b254ea3825154180a7080bd0559e9589ccd0bf7fc0e2993b5f797c92c67a` |
| Splash | `993233ee256fc2f28f0f5995a43eb41366c71739bd7a8dd08212555e3bb2b092` | `ae09993c53ffba438722af00ae3c692668cc14d919205cf50a9749805ebd1a27` |
| ZX81 candidate | `2c6b47a028ca641653978bcc654fe437583e6c328b6dbd1b20cff66afaebcca7` | `81f85abc06cc6eba403352d0008e8392506f216d4df9f404b48be7767a5c1e04` |
| ZX80 candidate | `ff76ceb0ed7a7b2ceab8d51e1ec29c7b7d107231377cd8eb8b7afa0cbcbdd931` | `6516f32a42b2806fbe1377893f882ce87e4fa8a096f231848128affc83e2643c` |
| TS1000 candidate | `19b31e713aac050880323e1988b2496782081e262828de733071fd29f053e080` | `4e6dacb2293d62a1a337ea7e18a7d6f91e936268af54e718ed500f527b21629f` |
| TS1500 candidate | `e190418c5e4e2162b446ce808d8fd92877d00d3e0bb16bd8e7fcb38e6377b8c8` | `6048c6b31e073c1525a86bda3493dc23a921dbbc3d0c0e3ae160d76dd3187852` |

## Exclusions and issue drafts

- **C64:** keep its lock at `3a5accbd`. d273 exits synthesis with a latch error on generated loop variable `$fordecl_block$14517.i [2:0]` in `c64_vic_sprite_pixels`; old-pin synthesis completes. Local issue draft `/home/deano/fes-hil/yosys81-shared-bump/issue-draft-c64.md` (not posted).
- **PC/XT:** keep its lock at `3a5accbd`. The matched d273 direct diagnostic route misses system/video clocks; the old-pin route passes. Local issue draft `/home/deano/fes-hil/yosys81-shared-bump/issue-draft-pcxt.md` (not posted).
- **ZX81 family:** keep its shared lock at `3a5accbd`. ZX81, TS1000 and TS1500 candidate producers passed; ZX80 exhausted all 15 configured seed/weight combinations and missed its 52.224 MHz system requirement. Best was 52.0888 MHz at seed 3 / weight 2000 (worst ratio 0.9974); old build passed at 53.7808 MHz. One lock serves all four models, so none advances in this PR. Local issue draft `/home/deano/fes-hil/yosys81-shared-bump/issue-draft-zx80.md` (not posted).
- i386 and pc386 remain untouched for the separate `yosys81-pin-bump` job.

## Tests and hardware boundary

- Full misteross suite: `python3 -m unittest discover -s tests -v` — 2843 tests, 91 skipped, pass.
- Focused pin documentation/lock checks: 60 tests, 2 skipped, pass; `tests.test_pnr_recipes`: 29 tests, pass.
- Yosys-enabled focused Apple II, SG-1000, SMS, ZX81, PC/XT audio CDC, PC/XT boundary equivalence and Yosys netlist suites: 96 tests, pass with d273 Yosys and ABC from the normal cache.
- Coleco Yosys-enabled producer and SGM checks: 53 tests, pass with d273 Yosys and ABC from the normal cache.
- HIL is required for Menu, Pong, Apple II, Atari ST, Coleco, RAM Tester, RISC-V, SG-1000, SMS, Spectrum and the sealed splash because package/payload or BUILD_ID changed. No HIL was run and no kit was touched.
