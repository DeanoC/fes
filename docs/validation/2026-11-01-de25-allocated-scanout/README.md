# DE25 allocated scanout: failed frame proof and guarded recovery

The default-off scanout diagnostic was tested twice on bottom board TRWU8J8G.
Both full Linux FPGA loads and the explicit permanent-owner marker succeed.
The kernel issues an 8 MiB CLOEXEC allocation, mapping and first publication;
frame acceptance fails on the first POLL. Neither attempt acknowledges a
completely displayed frame. Further retries on this FPGA image are stopped.

The second attempt adds a cached INFO dump before closing the grant. The
[actual fault record](second-allocated-frames.log) is generation 2/state 14,
status 25/errors 4, underflows 0/outstanding 0, accepted 1/displayed 0/pending 1.
Errors 4 is the generated RESPONSE bit: an AXI memory-read response error,
not an underflow. Its origin is not yet established. INFO reads cached kernel
metadata without another hardware command. Last close otherwise destroys the
fault record when safe revocation completes. Six focused client tests and
20 paired native/AArch64 modeled-kernel cases pass at producer `8b4d66cf1`;
[their receipt](client-cpu-results.json) does not model physical DMA.

The programmer is the existing LinuxDe25FpgaManager at `3d9410e75`, using
explicit allocated_scanout selection. Its only new overlay property is a
scanout-compatible marker for the permanent owner; no address, supplier,
topology or DMA grant is supplied by userspace. Full native routine/archive
checks pass in 195.3 seconds, and dynamic SDK/AArch64 modeled-programmer checks
pass in 4.6 seconds. Default-overlay parsing, invalid selections, exact replacement,
inherited-overlay refusal and sticky uncertainty are covered. This is an
explicit diagnostic, not a production factory or MENU admission.

The FPGA is producer `d4c6544e3`, RBF
`bc4290c152a280afa7c5202726051cbc769708d59bbb8a2721e121410a644e68`.
Quartus Pro 26.3 passes in 489.7 seconds; all 19 report hashes match and all 74
summary timing rows are nonnegative. The [generated transport proof](transport-receipt.json)
passes eight cases in 26.3 seconds WITH MODELED M20K STORAGE. HPS/physical memory
responses are modeled there; those checks do not establish SMMU or DDR operation.
Five other timing signoff rules and #1284 remain open.

## Actual recovery

On both failures the client submits no retry or owner REAP. Kernel last-close
revocation performs STOP, confirms drain, and CLEAR. Read-only inspection finds
a healthy guard, epoch 1 then 2, Drained status 8 and zero grant, generation,
sequence, error and outstanding fields. The [second observation](second-observe.log)
retains both the original cached fault and the later cleared bank. The relevant
DMA/owner/guard/supplier code is unchanged from the physically running kernel
producer29e0eca19. After inspecting that proof, the operator performs one
[guarded owner REAP](second-reap.log), which returns 0 and releases the retained
owner handle. [Owned cleanup](second-cleanup.log) then succeeds under the lease.

The same boot 2b179a1a remains reachable, with unchanged kernel notes, root receipt,
CID and owner permissions. No power cycle, reboot, SD/kernel/QSPI write or bundle
installation is used. No current own allocation/overlay/firmware/mount/tmp remains.
The claim remains retained; no baseline promotion/release or SRAM erasure is
asserted. The first cleanup helper refused its own nested lease before mutation;
that sequencing mistake and the corrected cleanup are retained privately.

## Read-only route investigation

The [live register record](route-readonly.log) shows SMMU CR0/ACK=0x1d, no
GERROR and no event-queue entries/interrupts. All bridge reset bits read zero.
The enabled F2SDRAM firewall window is 0x80100000–0xbfffffff. These observations
neither prove that the failed requests reached translation nor identify why the
memory path returned an error. No register was written or firewall relaxed.
Addresses came from the exact bootloader commit printed by UART,
[U-Boot d0f8813fd6bf](https://github.com/altera-fpga/u-boot-socfpga/blob/d0f8813fd6bf/arch/arm/mach-socfpga/include/mach/base_addr_soc64.h),
its [DDR firewall setup](https://github.com/altera-fpga/u-boot-socfpga/blob/d0f8813fd6bf/drivers/ddr/altera/sdram_soc64.c),
and the selected kernel SMMU definitions. Altera documents the firewall at the
[MPFE_TBU output](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/f2sdram-firewall?contentId=JZo73fDtBno9kTQOAdevlQ)
and the [SMMU FPGA signal requirements](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/configure-the-fpga-ip-in-platform-designer?contentId=95Bla_jm4TEfuflkVvpkbA).

The [off-board HPS-phase comparison](phase-comparison.json) finds different
hashes for new ZX81 and scanout HPS phases despite identical HPS IO hashes.
The [already working bootstrap versus ZX81 control](phase-control-comparison.json)
also differs in 181 bytes in the same four blocks; 180 of those changed offsets
are shared with the scanout comparison. Byte-hash differences alone therefore
do not establish a bootstrap incompatibility or justify changing flash.

The running kernel lacks FTRACE and mapping debug support. The separate
[trace-enabled kernel candidate](tracing-kernel-build.json), producer
`3dab0b06b`, builds successfully in 218.2 seconds; all six affected checks
pass in 196.3 seconds. Its configuration enables the four required tracing
flags, and System.map contains both mapping tracepoints. The matching image
is assembled twice identically in 21.5 seconds and
[independently verified](tracing-image-independent-verify.json), SHA256
`7eb6266188823c9cd4810259fc7cc73eb70ec8f58129e5718f127fd17dd32a0b`.
It includes the SD-CID-derived Ethernet address fix. It has not been written
to a card or booted.

The actual diagnostic adds `--allocation-only` at producer `5a99f2073`. Seven
focused tests and [21 paired native/AArch64 model cases](allocation-only-cpu-results.json)
pass, including the added case: one allocation, zero mappings, publications or
POLLs, one owner reap and one close. Existing default frame-mode fault tests
still pass. This prepares observation without another frame submission; it
does not establish the physical mapping or fix the response fault. A mapped domain is insufficient proof of
physical translation. No DMA bypass or raw-address userspace interface is added.
Full normal MENU/catalog/runtime, safe restart, physical memory/translation,
SAVE, failure/reset fencing and timing acceptance remain required.


## Integration after the programmer change

The fresh [combined endpoint/client/ZX81/PR RTL run](programmer-combined-proof.json)
passes all 14 native and QEMU-AArch64 cases at `159bcf52c` in 1,412.8 seconds.
The actual report refreshes the branch fixture; all 16 source-closure recipe
checks pass after integration. This is model evidence, including BASIC
LOAD/RUN/reload, rather than a new board or physical persona-programming pass.
The allocation-only and trace-kernel changes do not alter that captured
compiler/runtime input closure. Generated consistency passes with 38 consumers
and 41 fixture copies; documentation links and whitespace checks pass.

The [six-command affected regression run](allocation-trace-affected-results.json)
passes at integrated source `93f6c9f44` in 284.3 seconds, including the selected
parent Python/media checks. Its code is identical after the final child merge;
only the actual combined-proof fixture and dated evidence are subsequently
updated. This is a separate run from the earlier trace-only affected checks.
