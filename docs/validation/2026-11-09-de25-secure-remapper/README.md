# Selected secure remapper access

Physical `8bc1ed9a9478e26d` again received a successful FPGA_CONFIG_COMP reply,
then printed the marker before remapper setup without its post-call marker.
The exact failed instruction, actual callee entry and live firewall setting
remain unproven. Inspection of the actual linked descriptors shows the original
remapper's System Manager accesses use Device/NS/XN translation. The [vendor firewall description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/firewall-and-security?contentId=mGRpDpvDnBWJrlTo1TUEyw)
documents security checks on target transactions, including the System Manager;
a security-class mismatch is a candidate cause, not a measured root cause.

The fresh matched candidate `e3de630956f0c154` adds one private EL3
Device/secure/RW/XN page: VA `0xa0000` maps PA `0x10d12000`. Every other
descriptor and the complete layout remain identical. A pinned, reversible
transformation changes only the original remapper's one read and two conditional
write addresses. With SCTLR_EL3.M clear they retain the physical base; with it
set they use the secure alias. Conditions, values, null-pointer handling,
CPU-to-SDM pointer translation, ownership checks, commands, retries, resets,
response rules and deadlines remain unchanged. The mailbox driver/header and
all protected control/tape sources remain identical.

Both producer inventories include `nano_remapper.py`: the firmware source list
and the cold boot identity list. The first unprogrammed prototype
`aba177a64f33ac8f` omitted it from the latter. The parent regression caught that
omission before hardware, and that image is retired. A fresh matched chain was
built after the inventory correction. Its failure logs and artifacts are retained.

The actual remapper function passes 192 before/after model comparisons across
four CPU states, both remap/bypass branches, null and non-null pointers and
interrupted reads/writes. Invalid bindings and source changes cannot publish a
partial transformation. The current compiled 108-byte function is checked,
including three address selections, accesses, original values and pointer
handling; twelve instruction mutations reject. Its actual first 24 address
selection bytes execute before/after EL3 MMU activation in emulation, with an
explicit eight-byte test return adapter and four negative controls. No remapper
MMIO or complete remapper execution occurs in that emulation. Twenty-five actual
table translation queries pass with thirteen negative descriptor controls.
The secure mailbox selector and canonical transition checks also pass.

The fresh parent regression passes 1833 tests with three existing skips, and
native launch/return integration passes. A later producer check stopped at its
30 GiB disk gate. Ten inactive, archived U-Boot source copies were verified and
removed, preserving every build image, active input and recovery source. The
stopped producer check and remaining integration checks now pass with their
original caps; completed checks remain retained. No test, timing or hardware deadline was relaxed.

BL31 and FIT remain within the existing bounds; FIT is 8383144 bytes. The exact
qualified recovery artifacts remain unchanged. A new one-use top-only hardware
trial is still required, with 13 verified uploads/readbacks, complete original
boot proof, the original seven-config/three-launch/four-menu/tape exercise and
independent cleanup. Ordinary package/appliance acceptance remains open.
