# DE25 MENU HPS boot-package proof

FPGA producer: `1c5f24e1878328edb7a4e92cb0396aaac6aa336a`.
Tested boot-package code: `c056078a3c2e68a749811902a7e914c2ee5e2f88`.
Subsequent publication changes documentation only. No board is accessed.

The actual MENU producer records `scanout_build_id`. An initial external proof
instead accessed `computer_build_id` and failed before conversion. Inspection
found the same computer-only assumption in the existing bootstrap preparation
and verifier. The original failed proof and log remain retained; they do not
claim an actual pre-fix conversion attempt.

The existing tool now selects the identity field from the current producer's
explicit classification. Computer bundles retain their computer identity;
MENU bundles retain their scanout identity. Missing, unknown, malformed,
mismatched and mixed identities are refused before conversion. The manifest
format, closed file inventory, payload verification, rollback locks and
unqualified status remain unchanged. No second packager or identity alias is
introduced. FPGA, kernel and runtime source trees are unchanged by this fix.

The fresh actual Quartus Pro 26.3 build on `fes-build-3090` passes compile-only
production in 564.1 s, including IP generation, fitting, Linux RBF conversion
and fresh source/RBF/database-bound review of the two approved asynchronous
HDMI inputs. BUILD_ID is `267d7051c43aa59b84e3bef9d900f89f`; the unsealed,
unprogrammed core RBF SHA256 is
`4b014205b1d9669cdbce3721a1bbaecb3fa36f60e9ce38c28aa9597114744c29`.
All 69 reported timing checks pass. The 85-rule inventory still reports
TMC-20012:30, TMC-20011:2 and RDC-50003:2, with zero waived findings. Only the
two approved TMC-20011 inputs have an accepted review; HDMI output and whole
reset remain two unreviewed failed rule groups. Timing admission is false.
No MENU package is exported and no eligible independent identical-RBF rebuild
is claimed.

The corrected existing preparation and verifier run actual Quartus conversion
and inspection in 4.8 s. They bind the typed scanout identity, verify the split
core, HPS-only recovery, MT25QU128/HPS_FIRST metadata, matching HPS IO hashes
and locked vendor rollback. A separate actual public CLI verification passes
without changing any selected file's bytes, inode or modification time.
Vendor rollback is the pinned BSP image, not a board readback.

The resulting JIC, SOF, HPS-only RBF and core all differ from the recorded
approved baseline's file hashes. Embedded vendor SPL and HPS IO hashes match
that record. Matching those two inputs does not establish native boot
compatibility. Different complete-file hashes alone do not establish changed
programmed payload or a need to replace flash. No flash decision or deployment
approval is inferred. [Checks](checks.json) retain the exact candidate and
recorded-baseline identities.

All seven focused bootstrap cases pass in 0.008 s (0.1 s job). Their conversion
fixture is mocked; the actual Quartus proof above is separate. The complete
current six-command affected plan plus `make check` pass in 210.5 s: 1,344
parent cases in 198.896 s with six skips, plus planner and consistency checks
covering 40 generated consumers and 42 copies. This current parent-only plan
selects no extended video/media or full-race mode; no changed component lane
is omitted. Prior CPU and extended software evidence retains its original
source identity and is not relabelled as this result.

The complete fitted project and boot bundle are archived separately from the
proof metadata. Independent local extraction verifies all 3,332 regular files
and the actual bootstrap verifier in 2.033 s (2.4 s job). All 374 canonical
MENU, 59 compiler and 58 runtime-test source inputs remain unchanged. Separate
source restoration and all 55 proof-archive member digests pass in a 2.2 s
retention job. [Retention](retention.json) identifies the archives, restored
source tree and preserved original failure. Completed local and remote job
process/group identities are checked absent.

HDMI output and whole-reset signoff, disabled-F2H firmware/native
qualification, eligible independent RBF reproduction, admitted MENU,
complete factory/card and native ABI3/MENU/catalog/ZX81/recovery acceptance
remain open. There are no additional PCB/trace-delay files from the user.
No delay, clock relaxation, additional exception or hardware acceptance is
invented. Installed software, kernel, boot flash, claims and baselines are
unchanged by this work.
