# DE25 source-workspace verification and shared package publication

Tested code: `8540fc3afae867b3f9516aee3d55ee9b75553abd`. Baseline: `3f1a6821bc887f4e91edca0766390afd6e32a979`.
Subsequent publication changes documentation only.

Source builds publish the verified factory MENU through the existing closed,
transactional package publisher. The standard target-acceptance reader can now
read that workspace's inputs and flat package selection. The normal verify
command accepts a source workspace or explicit complete factory and refuses
ambiguous defaults. Development verification takes its explicit workspace;
cold source verification also requires matching host outputs.

Verification independently checks the committed component/profile/recipe
selection, recomputes the source input key, invokes the real factory/package and
bound virtual-boot verifier, and compares flat image/kernel/package copies,
compiler records and the complete receipt inventory. It never resolves a missing
MENU or produces target software. The inherited factory verifier's host package
preflight now uses the external cache; its selected image mount is read-only.
Overlapping factory/cache directories are refused. Cache and verifier-helper
work outside the selected image remains separate from artifact publication.

All 153 focused cases pass in 12.510 s (12.8 s job). The
complete current six-command affected plan plus `make check` pass in
795.9 s: 1425 parent cases in 784.856 s, 47 skips,
extended video/media enabled, 40 generated consumers and 42 copies. No changed
component lane is omitted from this current plan. The command-count failure in
the original external proof wrapper is retained; it precedes test execution.

Actual public Make verification refuses five cases: missing default output,
the real managed-base intermediate, incomplete source output, a foreign profile
and an invented second image pass. Actual retained SDK authentication also
passes after the verifier recipe change. The combined job takes
3.3 s. Protected receipt/rootfs/SDK/kernel inputs and
incomplete source files preserve their bytes, inodes and modification times.
No success receipt, target compilation, Quartus run or factory is produced.

Positive source/factory/host authority fixtures are explicitly unqualified.
They prove filesystem publication, receipt closure, refusal, read-only behavior
and offline acceptance-reader compatibility. They cannot admit MENU or qualify
a factory, a virtual full-system boot, or a physical board. Earlier actual
AArch64 source/CPU evidence stays bound to `0c77fe27f` and is not rerun or
relabelled as this head. Shared software builders and component trees remain
unchanged; all 374 canonical MENU, 59 compiler and 58 runtime-test inputs match.
The effective managed recipe changes, requiring a fresh actual factory.

The original missing-variable failure and the deliberately stopped old-source
parent run remain retained. The latter stops after 187.7 s because inspection
found the inherited cache-write path; it is not a completed test result. The
final corrected source is tested separately. Independent source restoration
and every archived regular-file digest are verified; [retention](retention.json)
and [checks](checks.json) identify the exact evidence and prior input bindings.

No kit operation occurs. Admitted MENU, HDMI output and whole-reset signoff,
firmware/native F2H qualification, independent identical-RBF rebuild, actual
full factory/card boot, ABI3 ownership, MENU/catalog/ZX81 and recovery acceptance
remain open. Only the two previously approved asynchronous HDMI inputs have
reviewed exceptions. No additional PCB files are available.
