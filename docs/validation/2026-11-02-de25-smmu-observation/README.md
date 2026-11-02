# Native SMMUv3 observation on TRWU8CBL

The source-bound serial-only image completes the real SPL/EL3/EL2/EL1/Go path
on TRWU8CBL and records 26 stable SMMUv3 register reads. Both security states'
translation, bypass-abort and alternate-attribute controls are clear in this
boot. No FPGA DDR traffic or native SD/device activation was requested.
This is hardware diagnostic evidence; general core admission and active
launch/return remain open.

Base: `97c50728ba8aa8f34f49af4c1e05cc832383c002`. Implementation and exact
software/build/hardware source: `64157280e2e7b06d08c1e43fb7ceff6c0214e3ed`.
The first programming failure, later successful trace and cleanup are retained
as separate evidence scopes.

## Implementation

`nano_smmu.c` samples fixed architectural offsets twice and requires matching
words and trusted closed ownership around every read. It accesses the secure
control bank only when the secure implementation bit reports it. A changed
sample, failed read or lost ownership retires that observation without polling
or replay. The helper has no register-write, reset or bridge interface.

The new source-controlled `smmu-profile.json` accepts only the native serial
`probe`. SPL and BL31 bind the same profile in their cold chain. The secure
provider observes after accepting the boot record and before BL33. Other
serial/SD profiles keep this boot observation disabled. The already contained
DDR configuration profile additionally observes after its SDM readiness query,
before opening control. Its access and activation remain unqualified.

The bounded serial observer requires the SMMU record at its exact position,
checks read count/security-bank handling, and independently checks the printed
interpretation against the raw words. Even `bypass_controls_clear=1` always
retains `access_qualified=false`. Neither that field nor a successful serial
boot supplies a memory/firewall/drain receipt.

## Actual board result

The [successful native trace](resume-native-console.log) and independently
[decoded result](postcheck.json) complete the matched loader, cold ownership,
EL2/EL1 and Go checkpoints. The [native receipt](resume-native-result.json)
records the exact SOF/FIT and decoder identity.

| Sampled field | Value |
| --- | --- |
| `IDR0`, `IDR1`, `AIDR` | `0x080f7e3f`, `0x0e739d18`, `0x1` |
| `S_IDR1` | `0x80000018`; secure implementation present |
| Non-secure `CR0`, `CR0ACK`, `GBPA`, `AGBPA` | all zero |
| Secure `CR0`, `CR0ACK`, `INIT`, `GBPA`, `AGBPA` | all zero |
| Observation | status 0; 26 reads; identified/settled/clear controls all 1 |
| Access qualification / core-load acceptance | false / false |

The [first experiment](first-experiment-result.json) successfully booted the
unchanged recovery loader and uploaded the 4,832,860-byte native FIT. Independent
SD readback/hash/byte comparison and protected boot-file checks passed:
[upload receipt](upload-boot.json). Native SOF programming then returned
`JTAG chain broken` and a rescan error: [programmer log](first-native-program.log).
No native UART bytes or `fesboot` command were captured in that attempt.

Two [read-only post-stop queries](first-poststop.json) again returned
`4362C0DD`; they established chain response, not boot state. The same error had
previously occurred while loading the unchanged recovery image in the
[earlier reader preparation](../2026-11-02-de25-reader-native-preparation/README.md).
This does not identify the cause of either failure.

One final bounded programming attempt reused the exact SOF and the independently
verified on-card FIT, with a new fuse and no SD write. The
[resume receipt](resume-experiment-result.json) explicitly binds that carried
upload; it does not claim another fresh readback. The second load and complete
native observation passed. Both attempts restored the original 15 MHz clock
and USB permissions. Final independent audit confirms the UART is free.
The diagnostic and spare card remain in the exclusively claimed top board;
Deano authorized leaving the diagnostic, and no factory restore or other-kit
operation was performed.

## Software and artifact checks

| Check | Result |
| --- | --- |
| Exact affected plan | All 10 commands pass on the implementation source: [plan](software-plan.json), [results](software-result.json) |
| Parent and FPGA software suites | 1,265 tests / 2 skipped; 1,576 tests / 75 skipped: [parent](software-04.log), [FPGA](software-09.log) |
| Standalone sampler and actual strong provider | 269 read-only C cases plus enabled/aborting/unstable and owner-loss cases through actual provider/dispatch: [log](boot-profile-cold.log) |
| Serial ordering and interpretation | Correct, missing, duplicate, misplaced, malformed and contradictory SMMU records checked: [log](smmu-trace.log) |
| Profile/cold handoff selection | Dedicated profile/header and source restrictions pass: [handoff](boot-profile-handoff.log), [context](profile-context.log) |
| Register source audit | All 13 offsets and enable/abort/update fields match pinned Terasic U-Boot/TF-A sources: [audit](register-audit.json) |
| Real cross-build and file composition | Native Go, entry, pinned Terasic BL31/U-Boot, independently decoded replacement SPL and preserved recovery files: [build](boot-cross-result.json), [package](package.json) |
| Shared consumers | `make check`: 35 consumers / 37 fixture copies match; no shared wire definition changed: [log](make-check.log) |

Arm's [SMMUv3 specification](https://documentation-service.arm.com/static/5f900e9ef86e16515cdc0907)
distinguishes disabled translation from disabled traffic that is aborted by
bypass controls. Altera's [MPFE TBU description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/transaction-buffer-unit-interface?contentId=g1aFQI3joZYcYR61GOj4oA)
places translation on the FPGA-to-DDR path. This observation resolves the live
control-state uncertainty for the sampled cold boot. It does not qualify
MPFE/firewall/FPGA traffic attributes or later state changes. The next step is
to bind the main reader, inherited probe and statically held maintenance
masters into real stop/drain callbacks, recheck addressing at the point of
use, then test bounded DDR read/drain/reclose before runtime admission.

Regular-file build outputs, both attempts and helpers are retained at
`/home/deano/fes-retained/de25-smmu-observation-20261102/` with an independently
restored source bundle and a fully checked archive. Private claim/credential
files are not part of that retention. This record's files are hashed in
[manifest.json](manifest.json).
