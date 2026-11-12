# DE25 ordinary image, verification and export entrypoints

Tested implementation: `c1152ba3cd4e2d7bdd079e0cc4e40478aee23d44`.
Baseline publication: `1e15f24653611dfa916be81c106b4deb1d360d9a`.
This is FES image/media integration; component source, ABI definitions, kernel,
Buildroot implementation/configuration and FPGA inputs are unchanged. The
ordinary builder and artifact selector change and remain bound by the existing
factory recipe; no old factory is promoted as evidence for this increment.
Subsequent publication adds documentation only.

The ordinary builder now accepts an independent `--board de25-nano` with its
explicit profile, matching owner kernel/compiler and selected artifact set.
`make image BOARD=de25-nano` invokes the existing `managed-factory` producer,
preserving package admission, kernel/SDK checks, source snapshots, output/cache
locks, actual image boot evidence and one real build pass. Its default output
is `out/de25-nano/factory`. It creates no alternate OS builder.

`make verify BOARD=de25-nano` always runs the existing fresh factory boundary;
optional supplied artifact/kernel/compiler inputs must match its recorded
selection. Foreign profiles, a profile without independent board selection,
DE10 kernel/output arguments and invented two-pass production are refused.
The ordinary default stays DE10. DE25 source `build`/`rebuild`/`dev` and
board-specific `host`/`doctor` routes remain explicitly refused; factory
verification is not the complete DE10 integration check.

Artifact export requires the independently selected DE25 and a real factory.
After fresh factory verification it reads actual programs from ext4 using
fixed read-only `debugfs` commands. Four bundle executables must match the
selected software hashes; the two base helpers must match the factory's
separately bound rebuilt-helper hashes. These helper bytes may legitimately
differ from the original artifact-set helpers. The existing packer checks ELF,
MENU interfaces and the exact package members. The result binds rootfs,
factory-receipt and original FES identities, rechecks the source before atomic
publication and refuses output overlap/replacement. All three operations use
the shared media/build lease. Common `BOARD` selection supplies the appliance
board default while its existing explicit override remains available.

## Executed scope

- All 68 focused cases pass in 0.9 s, including 11 new command/export cases
  and existing DE10, artifact, appliance selection and publication controls.
  The new export cases use real `mke2fs`, `debugfs`, ELF/interface inspection
  and packing on 8 MiB files. Their ELF/package payloads and factory authority
  are explicitly unqualified unit fixtures. Factory authentication/verification
  is mocked only for those positive fixtures. No CPU or physical board executes
  their payloads. Tests reject wrong ext4 bundle/helper bytes, a source receipt
  changed during extraction and factory/output overlap without publication.
- Nine actual Python/Make refusal cases pass in 0.9 s, including ordinary
  verification/export against the real D-Bus-corrected managed-base intermediate.
  Its original rootfs, Image and base receipt hashes remain unchanged. No
  refused factory/export output is created. Three Make dry runs test forwarding
  for image, verification and release only.
- Actual SDK/source authentication passes in 2.3 s. All 374 canonical MENU,
  59 combined-compiler and 58 runtime-test inputs are unchanged. No new actual
  managed image, full 14-scenario native/AArch64 proof or Quartus job is run.
- Committed-source/package closure passes in 5.7 s: 40 generated consumers
  and 42 fixture copies match.
- All six current parent commands pass in 868.5 s, including the complete
  1,389-case parent selection with 47 skips (862.746 s) and generated
  consistency. [checks.json](checks.json) retains those commands and explicit content-based reuse of the
  preceding host/UI, runtime, contract and FPGA software evidence. Every
  component tree, platform, image directory, profile and shared boot lock has
  the same Git object as the baseline. The original baseline aggregate timeout
  is unchanged; its completed prefix and successful standalone FPGA continuation
  remain separate. The ordinary ten-command affected plan is preserved in the
  source archive; this is not a new execution of that whole plan.

[refusals.json](refusals.json), [source-scope.json](source-scope.json) and
[retention.json](retention.json) preserve exact commands, source/artifact
identities and independently restored source.
Source/archive verification passes in 1.6 s: the exact Git tree is independently
restored and all 31 regular proof members verify in the 83,631-byte archive.
Final completion records separately retain their hashes and verify that all
12 known owned job process/group identities are absent.

No complete admitted factory, exported production artifact set or card is
produced. Sealed MENU still needs HDMI output/whole-reset signoff, native
firmware qualification and an eligible independent identical-RBF rebuild.
Persistent data/expansion, source/incremental integration and native
watchdog/update behavior remain open. Full PID1/FAT/root/SSH/peripheral,
owner ABI3/SID/SMMU/coherent-DDR/security, MENU/catalog ZX81/HID/BASIC/persistence
and restart/adoption/recovery acceptance is still required.

No kit UART, network/SSH, capture, allocator, FPGA, SD, installed software or
kernel, flash or power is accessed. No baseline is restored, promoted or
released. Bottom TRWU8J8G remains at its last authenticated ABI2 observation;
top TRWU8CBL is untouched. The full normal-kit goal remains active. Only the
two approved asynchronous-input exceptions apply; no PCB delay, lower clock,
extra exception or SMMU bypass is introduced. Both consolidated PRs stay ready
and Deano-owned, without merge or handoff.
