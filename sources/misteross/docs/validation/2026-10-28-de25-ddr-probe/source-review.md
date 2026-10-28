# DE25 HPS/DDR source boundary review

This is a read-only source audit for the prospective DDR probe and interactive
MENU allocation contract. It establishes architectural and generated-source
facts, not an observed boot, active SMMU configuration, Linux device binding,
firewall permission or DMA admission. No board, kit, FPGA database or harness
was accessed. The [shared contract](../../../../mister-packages/docs/de25-ddr.md)
must retain those gates.

## Retained inputs

The inspected generated project is
`out/dev/de25-nano-bringup/out/de25-hps-reset-4ab2472b0b/project`, produced by
`4ab2472b0b22b64d8756fd7c6e83d117e794b214` with Quartus Prime Pro
26.3.0 Build 114. Its SOF SHA-256 is
`2d93db15c2a525a8416916fab8f7018f44488be27a2aaf7507324cdf7e8c663c`.
Unless a section explicitly names the newer probe producer, generated-source
paths and line numbers below are relative to that project.

The locked [Terasic revision B resource ZIP](https://dl2.terasic.com/resources/de25-nano/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip)
is SHA-256 `f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`;
the recipe selects `Demonstration/SoC_FPGA/GHRD/` and device
`A5EB013BB23BE4SCS`. Preserved boot input identities and the unmatched
factory/custom-HPS pairing are recorded by the separate
[boot inspection](../../../../../docs/validation/2026-10-28-de25-boot-contract/README.md).

## Architectural address domains

Agilex 5 HPS TRM 814346, version 26.1.1,
[§10.2.2, Table 312](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/total-address-map-tabular?contentId=hmoBygBowcGaidQkdM1NhA),
specifies these global CPU apertures:

| Target | Base | Aperture size |
| --- | --- | --- |
| Lightweight HPS-to-FPGA | `0x00_2000_0000` | 512 MiB |
| Full HPS-to-FPGA | `0x00_4000_0000` | 1 GiB |
| Full HPS-to-FPGA | `0x04_4000_0000` | 15 GiB |
| Full HPS-to-FPGA | `0x44_0000_0000` | 240 GiB |
| SDRAM | `0x00_8000_0000` | 2 GiB |
| SDRAM | `0x08_8000_0000` | 30 GiB |
| SDRAM | `0x88_0000_0000` | 480 GiB |

The [F2H/F2SDRAM map, §10.2.3, Table 313](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/total-address-map-for-f2h-and-f2sdram?contentId=X3wb0SOf~UoHCP3AWnJRMA)
gives the SDRAM global windows for F2SDRAM and the 1 TiB global address space
for F2H. The configured port widths still restrict what this particular fabric
can express. These architectural sizes do not describe installed RAM capacity.
No formula for folding the higher HPS-to-FPGA apertures into local fabric
addresses was established by this audit.

In the retained generated design, `qsys_top/qsys_top.sopcinfo:56073–56089`
maps the lightweight bridge to the peripheral bridge at relative zero, and
`:53061–53076` maps the mailbox at relative `0x100a0`. Combining the documented
lightweight aperture with this local map yields architectural candidate
`0x200100a0`. Likewise, the proposed probe's relative `0x10100` would yield
`0x20010100` only with its matching generated map. Neither candidate establishes
boot compatibility, bridge enablement, an admitted Linux resource or permission
to use `/dev/mem`. The raw Linux DT has no such endpoint or bridge window.

## Actual generated interface and transaction attributes

`hps_subsys/ip/hps_subsys/agilex_hps.ip:7952–7970` selects 128-bit/38-address-bit
HPS-to-FPGA and 32-bit/29-address-bit lightweight HPS-to-FPGA. Lines
`9377–9410` select 256-bit/32-address-bit F2SDRAM and 256-bit/32-address-bit
ACE5-Lite F2H; both expose no System MMU ports (`f2sdram_SMMU=false`,
`f2s_SMMU=false`). The generated port declarations in
`hps_subsys/hps_subsys/synth/hps_subsys.v:11–67,132–160,198–209` agree.
The HPS component reference manual's
[F2SDRAM subordinate section](https://docs.altera.com/r/docs/813752/26.1/hard-processor-system-component-reference-manual-agilextm-5-socs/fpga-to-sdram-subordinate?contentId=QrZ7xg9oEn_mu8lPK61t4w)
describes AXI4 noncoherent access; its 32-bit example can address the first
2 GiB DDR global window. The
[F2H subordinate section](https://docs.altera.com/r/docs/813752/26.1.1/hard-processor-system-component-reference-manual-agilex-5-socs/fpga-to-hps-subordinate?contentId=vWhtybTmcFuEJmgFhpjIew)
describes the separate ACE5-Lite/cache-coherency-translator path. Enabling that
other port does not make F2SDRAM coherent.

The retained adapter
`hps_subsys/ip/hps_subsys/f2sdram_adapter/f2sdram_adapter_10/synth/f2sdram_adapter_f2sdram_adapter_10_ebbsmiq.v:129–159`
passes the 32-bit AXI byte addresses unchanged, supplies read/write AxUSER
`0xe0`, AxCACHE `2`, and AxPROT `0b011` (privileged, nonsecure, data).
The MPFE wrapper
`hps_subsys/ip/hps_subsys/agilex_hps/intel_sundancemesa_mpfe_100/synth/agilex_hps_intel_sundancemesa_mpfe_100_u4663zy.sv:740–743,922–925`
expands user signals to `{17'h3, user[7:0]}`. Its lines
`1077–1080,1228–1231` zero-extend the addresses; they add no DDR base.

Installed official component source
`/data/altera_pro/26.3/ip/altera/intel_hps/sm/common/parameters_data.tcl:858`
maps AxUSER bit 8 to AxMMUSECSID and bits 24:9 to AxMMUSID. Therefore this
wrapper supplies SECSID `1` and SID `1`. The GUI option disables exposed MMU
ports; it does not prove that the MPFE TBU is bypassed at runtime.

The TRM's [§12.2.6.3 AxUSER connectivity](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/axuser-bit-connectivity?contentId=wC2dHK6jCKm_Mgrcgth34Q)
explains the routing/master-ID convention `0xe0` and SID/security transport.
Bit 8 set uses AxPROT's security setting; here the transaction remains
nonsecure. `0xe0` does not establish translation bypass. The TRM's
[§11.5.4.1 TBU interface](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/transaction-buffer-unit-interface?contentId=g1aFQI3joZYcYR61GOj4oA)
distinguishes translated virtual addressing from physical addressing through
TBU bypass. Its
[§4.3.6.2.2 StreamID assignment](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/step-1-assign-streamids-configured-in-quartus-not-in-atf?contentId=pMmf8CmPUBBmyQ0zbmOV7g)
requires the F2SDRAM StreamID to come from fabric, rather than a System Manager
StreamID register.

## DMA, translation and firewall gates

The exact pinned Linux DTB is SHA-256
`d9bd893fc94e45359fb442ca06741aa63c5881e9376473fb9397ebf6d6be4d13`.
Its `/soc@0/iommu@16000000` is an enabled `arm,smmu-v3` node, phandle `9`,
with one IOMMU argument cell. Enabled Ethernet at
`/soc@0/ethernet@10810000` has `iommus = <9 1>`. There is no fabric DMA device
binding. The tied fabric SID therefore overlaps an active DT StreamID
declaration; the actual translated path, secure/nonsecure context and any
routed conflict remain unproved. Assigning a new Linux device SID alone cannot
change the tied hardware SID.

A physical/bypass transaction must use an accessible physical DDR global
address; this configured 32-bit port cannot express the higher DDR windows.
A translated 32-bit DMA address can be an IOVA below `0x80000000` with backing
in DDR. Thus the descriptor must bound the entire DMA address plus length to
32 bits without assuming every DMA handle is a CPU physical address or imposing
a physical-DDR lower bound on an IOVA. Allocation ownership, the device's
actual translation domain/bypass mode and mapped backing are separate proof
obligations. The TRM's
[§4.3.6.4.5 F2SDRAM DT integration](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/device-tree-f2sdram-bridge-fpga-master-integration?contentId=aOAQ62QcYgHqNRLOrmPpZg)
requires matching hardware/DT StreamIDs, DDR buffers and appropriate coherence
declarations. A Linux coherent allocation does not change the fabric path into
a coherent interconnect.

The [TRM §13.4.2.4.3 F2SDRAM firewall](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/f2sdram-firewall?contentId=JZo73fDtBno9kTQOAdevlQ)
is after MPFE_TBU, supports eight regions with 64 KiB alignment/granularity,
and checks the `0xe0` master identity. This adapter's nonsecure traffic is
restricted to enabled regions. Translation success therefore cannot replace
proof that the resulting physical DDR backing is permitted by that firewall.

## Firmware source evidence does not establish runtime state

The preserved U-Boot payload banner identifies revision
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`. At that revision:

- [Firewall DT initialization](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/dts/socfpga_agilex5-u-boot.dtsi#L356-L372)
  defines `0x10d21300` and `0x10d21200` as lightweight/full bridge firewall
  CSRs. They are not CPU bridge memory apertures.
- [SPL initialization](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/mach-socfpga/spl_agilex5.c#L76-L88)
  initializes the security-register device and DDR driver. The
  [F2SDRAM firewall routine](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/drivers/ddr/altera/sdram_soc64.c#L334-L385)
  programs detected DRAM-bank base/limit registers and excludes the first
  1 MiB of bank zero in the SPL/ATF flow. Detected banks, later firmware changes
  and final permissions still require observation.
- The [selected SMMU object](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/mach-socfpga/Makefile#L83-L96)
  is `smmu_agilex5_dv.o`. Its
  [test routine's bypass loop](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/mach-socfpga/smmu_agilex5_dv.c#L371-L385)
  is not executed by the audited
  [normal `socfpga_init_smmu` entry point](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/mach-socfpga/smmu_agilex5_dv.c#L435-L454),
  where `psi_test()` is commented out. The presence of this code proves no
  active bypass stream table.

FIT configuration `board-0` selects an ATF payload of 61,501 bytes, SHA-256
`0d29d7f1ff43afa267f63c1599657eea6d7fcdc8fd53addf8beb2d4f7089fb3c`.
Its printable banner is `v2.12.0(release):de25_nano_revA_v1.0`; it supplies no
Git commit. The same-named Terasic ATF branch resolved during review to
`580dcb5c4e10aa7daff2bf69d493c076a6d3e665`. Its
[BL31 setup calls `setup_smmu_stream_id`](https://github.com/terasic/arm-trusted-firmware/blob/580dcb5c4e10aa7daff2bf69d493c076a6d3e665/plat/intel/soc/agilex5/bl31_plat_setup.c#L54-L68),
but that reference is not a proven source/build identity for the selected ATF
binary and does not establish its runtime stream tables.

## Reset and error-response integration gaps

The retained `qsys_top/synth/qsys_top.v:11–12,649–650` exports the HPS warm-reset
request/acknowledge interface. `golden_top.v:210–274` omits both connections.
No stop/drain/acknowledge lifecycle is established there. A new consumer must
retain its allocation while requests or responses remain outstanding; local
IDLE after accounting is reset cannot prove earlier traffic disappeared.

The F2SDRAM adapter preserves AXI RRESP at line 124. However the retained debug
path's `ip/qsys_top/ext_hps_f2sdram_master/synth/ext_hps_f2sdram_master.v:11–28`
has no Avalon response port. In
`qsys_top/altera_mm_interconnect_1920/synth/qsys_top_altera_mm_interconnect_1920_lmsafsa.v:308–309,351–352`,
its translator disables read/write response support, terminates the response
and supplies success on the upstream universal interface. Thus the old debug
path does not deliver AXI read errors to its client. A new probe must expose
response status and verify propagation through its freshly generated
interconnect/width adapter, or use an AXI path preserving RRESP/RLAST. An RTL
bench that directly injects a response is insufficient to prove that generated
path. The newer generated mapping is reviewed separately below.

## Fresh probe path: source review and transport simulation

The new generated project is `out/de25-ddr-probe-8f54e4f89e/project` in the
`out/dev/de25-hps-interface` worktree, producer
`8f54e4f89e5b12ae18df70c542bc5846d0452ce6`. This section does not relabel the
older project or claim the in-progress Quartus compile has completed.
Its `inputs.json` SHA-256 is
`a49101da9b94c4c6289342415c647bbc3d73b9f6511a288535f34c05d49be27e`.

The exact new interconnect is
`qsys_top/altera_mm_interconnect_1920/synth/qsys_top_altera_mm_interconnect_1920_7nkwjpa.v`,
SHA-256 `69fe13fb37b94747d4ba8f4d14b7d82c65f15a3afcc6fa7befbd0ddcf83c3c9d`.
Lines `457,491` enable and export the probe translator's read response;
`1041,1075` preserve it in the probe master agent. AXI RRESP passes through
the translator at `697,741` and the AXI master agent at `1254`. The legacy
debug master's separate response-disabled interface is not reused by the probe.
The wrapper instantiation in `qsys_top/synth/qsys_top.v:143,831,865` retains
the probe's two response bits.

The command width adapter selects `PACKING=0`, `ENABLE_ADDRESS_ALIGNMENT=0`
at interconnect lines `1838,1841`. Its generated implementation,
`qsys_top/altera_merlin_width_adapter_1970/synth/qsys_top_altera_merlin_width_adapter_1970_o2ilxeq.sv:1986,2000–2004`,
retains the byte address and incoming burst size. Its SHA-256 is
`6df3e73567eeec8822f2f489203f3f33b807d3b0e62ce692840c8b801573f914`.
These settings are consistent with narrow AXI reads on the 256-bit interface;
the functional check below observes the actual requests.

Executed from that worktree, after IP generation completed:

```sh
python3 sources/misteross/boards/de25nano/hps/interconnect_sim.py \
  --project out/de25-ddr-probe-8f54e4f89e/project \
  --output out/de25-ddr-interconnect-8f54e4f89e-03
```

The helper copies the generated QIP-selected RTL inputs and the actual copied
probe/headers to a new private directory. A guarded wrapper instantiates the
unchanged generated probe-to-AXI interconnect and the real probe; unrelated
debug master inputs are inactive. Unknown port forms or master widths fail
closed. Compilation uses two workers. It writes no producer-project files
and checks source bytes again after simulation. Future runs require a new
output directory. The test responder models AXI transport and address-specific
lane data; it does not model MPFE, TBU/SMMU, firewall, EMIF or physical DDR.

All ten cases passed under `/usr/bin/verilator`, version
`5.032 2025-01-01 rev (Debian 5.032-1+b2)`:

| Case | Returned 128-bit beats |
| --- | ---: |
| `aligned-single` | 1 |
| `upper-half-single` | 1 |
| `upper-half-odd-three` | 3 |
| `aligned-odd-five` | 5 |
| `upper-half-sixteen` | 16 |
| `upper-half-seventeen` | 17 |
| `4k-upper-half-tail` | 3 |
| `4k-head-tail-nineteen` | 19 |
| `slverr-upper-half` | 3 |
| `decerr-aligned` | 3 |

Every observed AXI request used `ARSIZE=4`: 16-byte narrow transfers on the
256-bit bus. Per-request assertions check the entire AXI byte footprint stays
inside the granted allocation and does not cross 4 KiB. The first case grants
only 16 bytes; no widening to a 32-byte allocation footprint occurred. The
upper-half and odd-length cases compare every returned word against its
requested byte address, then check the real probe's accepted/completed byte
counts, checksum and last-data lanes. AR backpressure and gaps in RVALID are
exercised. No R-channel backpressure was observed. Both injected AXI error
codes reach the probe through the generated path, set its response fault,
prevent DONE and drain all three accepted beats. The cases do not test a reset
or cancellation across this generated transport.

The retained receipt is
`out/de25-ddr-interconnect-8f54e4f89e-03/receipt.json`, SHA-256
`e27005a6faa4f0fd013c51dbab5e96066b2ff0adfd63fd20987d681db1556b2b`.
It records all 114 captured RTL/header input hashes, the frozen producer's
input receipt, wrapper, compiler command, case-by-case AXI address/length/size
traces and these execution identities:

| Item | SHA-256 |
| --- | --- |
| Helper | `9a46249c89e57b660a73e1ea936470339330337b74dcb1435a1819bca4ddd243` |
| C++ bench | `ccbe0f529b7b13e112b35a72dcc3e7cbd2e32ab4fe4611c62834f11b794ce701` |
| Generated wrapper | `f6e2318ec0fb0639768af1309eb1fb9b8d8883e8d07edcc005171a15824b21da` |
| Simulation executable | `a24aeeccb111aef4d4acdb1eeb843ad69a663a722e4218de7891ce41b95e90f0` |
| Simulation log | `fc382b9be5e95ae40546479bc45c52dad43f8c15c66b925a217e2978e213c51f` |

Two earlier helper setup attempts remain in private outputs ending `-01` and
`-02`; they stopped before simulation while the source index was corrected to
handle generated module declarations. The successful run above uses unchanged
producer RTL. This is source-bound functional transport evidence only. It
admits no physical bridge, DMA mapping, memory allocation or hardware use.

## Reviewed source SHA-256 identities

The paths in this table use the retained project root stated above. Hashes bind
the source evidence, not a new probe build or runtime state.

| File | SHA-256 |
| --- | --- |
| `hps_subsys/ip/hps_subsys/agilex_hps.ip` | `ab5fed80711d8a9e8af9bca1364d10119d4633ce1381339679ad8493a7f61332` |
| `hps_subsys/hps_subsys/synth/hps_subsys.v` | `5455b7961cc53405b8b5aafc44a8454a7c8df97cd4a5a1a2f23fa05c6e8d5ec6` |
| `hps_subsys/ip/hps_subsys/f2sdram_adapter/f2sdram_adapter_10/synth/f2sdram_adapter_f2sdram_adapter_10_ebbsmiq.v` | `fcc90a71082c709a648265d09d58322ed535ce1499dff474aaf8bb55c53a3431` |
| `hps_subsys/ip/hps_subsys/agilex_hps/intel_sundancemesa_mpfe_100/synth/agilex_hps_intel_sundancemesa_mpfe_100_u4663zy.sv` | `06ed6af205e97c2f4dd47dbc74b416a02c9a6eb3e6b85c8e6b47e3c8b997ffae` |
| `qsys_top/qsys_top.sopcinfo` | `060f22dc030bb3eae94209777471bf85f818be7ee00cf72b8b6fc6bb03da4ec2` |
| `ip/qsys_top/ext_hps_f2sdram_master/synth/ext_hps_f2sdram_master.v` | `8488503faa4ead868f74b61705923d0b6982cf4e642f165a9c171d2090e43512` |
| `qsys_top/altera_mm_interconnect_1920/synth/qsys_top_altera_mm_interconnect_1920_lmsafsa.v` | `7381a260bcb8eeafe44cb6bebb7d0442a35776fd14bd4e0b10531d61c46381c8` |
| `qsys_top/synth/qsys_top.v` | `6bef6aae5a3a14b65349362861e53906633d7d39a93b81bd2d5303c538fdacad` |
| `golden_top.v` | `3a24fa4f5df2e35f9f4e03369f53b208c8cd1ea27172259101b2af1b40100ef7` |

Additional audited source hashes: installed `parameters_data.tcl`
`7b7b65f95ab1a70f1bbd1a985893c6425d0366896070ff6160d1ed9eded5591c`;
U-Boot revision above, `arch/arm/dts/socfpga_agilex5-u-boot.dtsi`
`c2ffee0acc1c1f961ace9c7bb976c8aef34d1e6e61bc4b837ab5afba3ad2f9a9`,
`drivers/ddr/altera/sdram_soc64.c`
`e97b2013929c5e5147486a71b5de0d80310bac876086f7ea3c524ca81975bd8c`,
and `arch/arm/mach-socfpga/smmu_agilex5_dv.c`
`a60e30b98ab0fbaab4d63f7df809bcff2635b6889099fd434477a4df5c8c706a`.
