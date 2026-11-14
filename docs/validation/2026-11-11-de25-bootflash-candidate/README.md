# File-only matched HPS-first bootflash candidate

Native boot `548caa50395f3d94` now has a passing bounded volatile-JTAG automatic
startup and two-core cycle. This next candidate prepares persistent boot files;
it does not qualify phase1-only startup or authorize programming factory flash.

## Generated result

The vendor board script selects `MT25QU128`, flash loader `A5EB013BB23B` and
Active Serial x4. The isolated conversion uses those parameters with the exact
qualified SPL/raw SOF, HPS-first mode and plain RPD output. The same local
Quartus Pro 26.3 producer and shared packaging lock are used. The official
[HPS-first QSPI flow](https://docs.altera.com/r/docs/813762/26.1/hard-processor-system-booting-user-guide-agilex-3-and-agilex-5-socs/hps-boot-first?contentId=HMcwH9e3qZ21n1VAWN2tGA)
separates the small phase1 boot image from the later FPGA configuration; its
[CLI packaging instructions](https://docs.altera.com/r/docs/813762/26.1/hard-processor-system-booting-user-guide-agilex-3-and-agilex-5-socs/creating-configuration-files-from-command-line?contentId=gbhfadnQxkJIRLtwfFinKw)
require matched generator/firmware and HPS IO settings.

Generated files under `out/nano-bootflash-candidate-20261111/files-r4/`:

- HPS-first JIC: 16,777,447 bytes (programming container, not raw flash size).
- RPD: 2,494,464 bytes. The map places BOOT_INFO at 0 through 0x1fffff and
  phase1 at 0x200000 through 0x260fff.
- Phase2 core RBF: 1,654,784 bytes, exactly equal to the qualified matched core.
- Native SPL independently decoded from the intermediate SOF: 164,001 bytes.
  All eleven preserved SOF sections match the qualified reference. This does
  not independently verify the JIC's security hash or configuration CRC.

PFG inspection confirms the selected flash/device, HPS-first order, SDM QSPI
ownership and matching phase1/phase2 HPS IO hash. Full hashes, tool identity,
commands, exact proof scope and output metadata are in `result.json`. Every
input hash from the completed hardware trial was checked again after generation.

## Isolation and evidence repair

An earlier file-only conversion wrote PFG's automatic intermediate SOF beside
the sealed source input. Retention correctly rejected that changed hash. The
modified intermediate was preserved with the earlier outputs; the exact frozen
file was restored from the independently verified retained archive. All frozen
input hashes then matched again. The passing hardware archive is retained
separately. Final conversion uses private regular-file input copies, and its
post-conversion checks confirm no hardware-trial input changed. Failed file
attempts and logs remain alongside the final candidate; no board action was
performed by any packaging attempt.

## Remaining boot gates

The physical trial loaded a complete matched SOF through volatile JTAG. QSPI
HPS-first startup initially provides phase1 only, so that is a separate behavior
to test. The installed programmer's local operation help documents volatile
`p;file.rbf`; the next proposed bounded operator must explicitly admit the exact
phase1 RBF and verify that the native runtime configures its first menu when no
phase2 core is already present. `next-trial-plan.json` records this scope.

Actual flash part/geometry identification, a verified complete factory backup,
rollback preparation, explicit approval for the reviewed flash change, and a
separate power-on test remain open. The recovery SPI driver enables automatic
unlock on probe, so a generic `sf probe` is not yet an admitted read-only backup
step. The programmer's `R` operation means erase; its Examine operation requires
its own verified helper/device plan. No flash command was executed or approved.

Native Ethernet, current physical HDMI/key observations, broader package/device
coverage and full DE10-equivalent appliance acceptance remain separate work.
