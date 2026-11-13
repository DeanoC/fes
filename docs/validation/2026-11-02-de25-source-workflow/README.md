# DE25 committed-source development through shared builders

Tested code: `0c77fe27ffcba21202c5308e9642088704a49467`.
Baseline publication: `7295715ae961a826e94fc5713e72b356c1759a7b`.
Subsequent publication adds documentation only.

Ordinary DE25 `build`, `rebuild` and `dev` select the board/profile independently
and resolve MENU through the common recipe/package resolver before target
SDK, source staging or compilation. They require clean committed source, a
verified managed SDK/base and matching owner kernel/compiler. The existing
Go/C++ software builders compile the selected component commits, and the
existing managed-factory producer assembles each complete generation. Its
compiler/base cache and runtime refresh remain authoritative. No second builder
or artifact-only alias replaces source compilation. Host/doctor use the shared
frontend and host producer.

Source outputs retain immutable generations, publish Linux/kernel copies and
bound source/software-build data, and update their success receipt last. Cache
hits reauthenticate inputs, verify the recorded factory and compare published
image/kernel copies. Rebuilds keep previous same-input generations. Failed
source/compiler/factory attempts keep the previous complete generation. The
runtime builder provisions through its existing pinned wrapper, authenticates
the SDK/builder match before C++ execution and never claims an existing private
volume. Managed-base ABI extraction does not require a factory software tree.
Default DE10 source/image/host behavior is preserved.

## Executed evidence

- All 192 focused orchestration, package, SDK, host-route and planner cases pass
  in 5.099 s (5.3 s job). Fixtures explicitly mock package/SDK/factory authority;
  they establish real filesystem generation/publication, refusal, cache and
  failure behavior, rather than package or factory qualification.
- The exact current six parent/always commands plus `make check` pass in
  856.8 s. The full selected parent suite passes 1,417 cases in 845.315 s,
  with 47 skips and extended video/media enabled. Generated closure verifies
  40 consumers and 42 copies. The normal ten-command plan is not rerun as an
  aggregate: prior host/UI, runtime and contract software evidence is retained
  through identical component/runner objects; the latest full FPGA software
  result is 1,762 cases with 76 skips at unchanged `6909f1afd` component bytes.
  Original older aggregate timeout evidence remains unchanged.
- Actual AArch64 Go/C++ source production passes in 41.0 s using the shared
  SDK/cache/runtime builders, managed-base ABI mode and pinned builder
  provisioning. The original SDK archive is
  `d5bac20323fe6225b984e5a0f35ac087bfb4379dca2b40245e4eab97bd019bf7`.
  The runtime archive passes its canonical 29-production-member audit.
  Five runtime models run successfully under Cortex-A76 QEMU. All three
  real static AArch64 Go programs pass their entrypoint probes under that CPU:
  agent maintenance-contract print, kit UI selection and tenfoot help.
  Tenfoot's help return code 2 is expected. These are CPU/model/entrypoint
  checks, not a rendered UI, complete factory boot or board acceptance.
- Actual public `make doctor BOARD=de25-nano`, host construction and a verified
  unchanged-binary cache hit pass in 20.4 s. Both resulting programs are
  Linux/amd64 ELF. Their actual source, host receipt and canonical input
  sidecar verify through the normal reader. No server is started.
- Actual public Make/dev on the 3090 passes the refusal proof in 59.6 s.
  Installed Pro 26.3 authenticates the canonical MENU source/tools, then the
  native producer refuses its deliberately absent vendor archive. The Make
  command returns 2 as expected. No target SDK/source/software/OS stage runs,
  no success receipt appears and no Quartus compile is attempted.
- Source scope passes in 0.5 s: all 374 canonical MENU, 59 combined-compiler
  and 58 runtime-test inputs are unchanged. The effective managed recipe
  changes with its parent builder API; the original base stays an authenticated
  compiler/ABI intermediate, with its protected inputs unchanged. No managed
  base, admitted MENU or complete factory is built here.

Original failures are retained: temporary-filesystem capacity in the fixture,
injected-failure restoration, the room-selection ratio and the external proof
script's initial missing root import. The direct CLI explicitly bootstraps its
package imports. Seven unchanged DE25 SDK/kernel/factory cases now belong to
media; class AST identity and existing native tests are preserved. The strict
below-half rule remains, selecting 663 of 1,334 routine cases for room changes.
[checks.json](checks.json), [aarch64.json](aarch64.json), [host.json](host.json),
[refusal.json](refusal.json) and [source-scope.json](source-scope.json) keep each
result's exact source and scope.

Independent source restoration and all 91
regular archive members verify in 3.3 s.
The archive is 30,677,163 bytes and retains actual Go/runtime/
host binaries, CPU logs, command logs and original failures. [retention.json](retention.json)
binds these files and 22 completed local job process/group identities; the two
remote identities and both private runtime volumes are also verified absent.

## Remaining normal-kit work

The source workspace still needs its standard verify and flat package-selection/
acceptance integration. Current factory verification uses the complete
`generations/.../factory` path printed by the source build; it does not yet
select that source workspace automatically. The target acceptance reader needs
its closed flat selection set. Reuse the existing package publisher and factory
verifier for that work. Development-snapshot authority and optional two-pass
factory evidence are separate gates.

An actual admitted MENU and full source-produced factory remain unavailable.
HDMI output/whole-reset, disabled-F2H firmware/native qualification and eligible
independent identical-RBF rebuild remain open. The historical d854 candidate
retains its original source and is not requalified. No additional PCB files are
available; only the two approved asynchronous-input exceptions may be reviewed.
No invented timing, lower clocks, new exceptions or SMMU bypass is introduced.
Full native factory/services, owner ABI3/SID/SMMU/coherent-DDR/security, MENU and
catalog ZX81 launch/stop/return/relaunch, HID/BASIC LOAD/RUN/SAVE/reload and
persistence, watchdog/update/restart/adoption/restore and bounded recovery remain
required for the DE10-equivalent development-kit goal.

No kit UART, SSH/network, capture, allocator, FPGA, SD, installed kernel/software,
QSPI or power action occurs. Bottom TRWU8J8G keeps its claim and last authenticated
ABI2 observation; top TRWU8CBL is untouched. No baseline is restored, promoted or
released. Both consolidated PRs remain ready in `lane:deano`, without merge or
handoff. The full goal remains active.
