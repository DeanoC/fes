# Selected secure control MMIO candidate

The last physical image `e3de630956f0c154` completed the menu FPGA
configuration but produced no first menu frame. It stopped after SMMU/firewall
reports; the exact instruction remains unproven. Source inspection shows later
bridge control still uses the original NS System Manager mapping, and the
SMMU secure-bank ID sample is zero through its NS view. This cannot satisfy
the unchanged later secure-bank gate. Neither that later gate nor a game launch
was observed.

Candidate `bd103b1a199bc2be`, built against base
`2fa0bb4838666d27f706006b9d2fe36de911d1ee`, replaces the remapper-only helper
with a single pinned BL31 MMIO address selector. All existing System Manager
page accesses reuse its secure RW alias after MMU activation, including raw
bridge controls and the cold boot-kind read. Existing secure SMMU bank reads
use one new secure RO alias; SMMU writes keep their original physical path.
All pre-MMU and unrelated accesses retain their paths. Original driver/service
operations, values, guards, offset allowlists, reset behavior and deadlines
remain; no retries or device operations are added. The old helper is removed.

Both source inventories and the final installed-header verification bind this
change. The private code/tables/data relocate by one page, retaining the same
2 MiB firmware exclusion and canonical permissions. External mappings differ
only in the one new RO alias, after normalizing table pointers. BL31 is
315840 bytes and the matched FIT is 8387240 bytes, within the original 8 MiB
upload limit. The FPGA source/project and native SDK/entry are reused only
through their current hash checks; this is a firmware change.

Selected software/integration checks, actual accessor and SMMU observer host
tests, linked-code checks, matched transition checks and the build/composition
all pass. QEMU executes every complete linked selector instance (nine generic,
one cold boot-kind specialization), with 654 successful calls across two CPU
states and nine rejected code mutations. The actual production tables pass 36
translation/permission queries and reject 19 descriptor mutations, including
secure SMMU writes and neighboring-page widening. No Nano device operation or
physical cache behavior is exercised in QEMU. The initial selector-test setup
failed before running compiled code, and a later launch stopped at the disk
floor; those records are retained. The corrected run passes.

Shared-host space pressure required the existing user-authorized housekeeping:
445 inactive completed fixture directories were archived, every member read
back, then generated products removed while logs/receipts were retained. Net
reclaimed space was 9.2 GiB. Current build/test/recovery inputs were excluded.
The build/test 30 GiB admission floor was not changed. The archive and audit are
`/home/deano/fes-retained/powerboat-tidy-models-20261109` and
`out/nano-secure-control-20261109/fixture-housekeeping`.

This candidate has not been run on hardware. Next retain the source and exact
matched image, prepare fresh single-use operations, then run the existing
bounded TRWU8CBL upload/boot/launch-return exercise. Keep the exclusive top-kit
claim and restore waiver, original factory card safe, bottom kit excluded,
fixed qualified JTAG transport, first-failure stop and independent cleanup.
Preserve native 450 s / outer 800 s / host 890 s / initial Ready 100 s / tape
1 s bounds and all historical consumed operations. Full ordinary package/core
admission, visible menu/game behavior, input and normal return remain required;
no off-board or contained diagnostic result establishes appliance readiness.
