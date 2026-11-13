# Nano SDM discovery and retained target recognition

FES-owned Linux-free exploratory work on `codex/de25-embedded-os`, based on
`7ae85cadcf52a961af11f6cbdd34a026ef2da92e`. The result is the commit containing
this record. Evidence is host software/fault tests, native AArch64 QEMU model
launches and Nano cross-builds. No physical hardware, shared kit, FPGA
programming, GPU or Powerboat process was used.

## Implemented boundary

The Nano-only `fpga` diagnostic reaches TF-A through the ARM64 SMC instruction
and the v1 generic mailbox service. It issues only CONFIG_STATUS and GET_IDCODE,
validates both return status domains and exact response lengths, and reports
the observed state and silicon ID. It does not use FPGA_CONFIG_ISDONE or direct
mailbox MMIO. It rejects partial/error responses, retires without retry and
retains its service arena until reboot. Concurrent probes cannot interleave.

Profile schema 5 adds an optional, explicitly owned 2 MiB service arena outside
Go RAM, firmware, network DMA and boot display. Firmware must provide the
synchronous v1 service, exclusive mailbox ownership, bounded return and response
buffer bounds. These are declared requirements, not observed firmware facts.
An EL1 timeout cannot interrupt a hung SMC. Disabled service is explicit `null`;
stale profiles, overlapping windows and unsupported services reject. The build
runner refuses this diagnostic on QEMU and refuses a Nano build without its
service declaration before creating an output image.

FogCast's existing retained package validator now recognizes exactly format 2,
platform `de25_nano`, device `A5EB013BB23BE4SCS`. Exact target bytes participate
in the existing package identity; all archive/payload validation and private
retention remain in that reader. Formats 3/4 still reject Nano because their
linkers are Cyclone V specific. No Nano ABI/profile has been admitted. A test
verifies rejection before the current model owner changes or any command is
issued. The unchanged production C++ compatibility checker independently
rejects the synthetic Nano package as `unsupported_target`.

## Configuration finding and next gate

The [source review and exact references](../../../platform/embedded/tamago/sdm/README.md)
identify two blockers in the reviewed TF-A v2.12 implementation: configuration
completion can reopen bridges automatically, and configuration START changes
the Agilex 5 HPS remapper. Dedicated WRITE also returns BUSY after accepting a
buffer when its queue becomes full. A future transfer adapter must account for
those accepted buffers without replay and retain unknown ownership on failure.

Physical programming stays unavailable. The next integration step is a reviewed
firmware configuration path that keeps containment until live package/build
identity is checked, plus explicit DMA mapping behaviour. FPGA scanout also
needs live identity and generation-bound disable/drain acknowledgements. The
existing boot framebuffer stays reserved even after tenfoot exits. No matching
Nano scanout RTL, bitstream, handoff recipe or physical drain has been produced.

## Checks and evidence

The adjacent receipt records source/test/reference hashes and selected run
reports. Full logs and cross-built ELFs remain under this worktree's `out/`.

- Entire embedded module with race detection and retained model fixtures.
- FogCast corepackage race suite, including Nano format-2 retention, mixed
  targets, target-bound identity and continued rejection of Nano ROM formats.
- SDM faults: busy/rejected/no response/error/unknown service, mailbox errors,
  wrong/oversized/high-bit word counts, invalid IDs and partial observations;
  every failed client refuses a second call. Concurrent probe tests check exact
  register arguments and read-only command ordering.
- Nine board-profile tests, including explicit memory and firmware obligations.
- Nano `fpga` and `tenfoot` cross-builds with enabled service, and `probe` with
  service disabled. Selected source/ELF/compiler hashes are in each run report.
  The disassembly shows the expected x0..x6 loads, SMC #0 opcode `d4000003`,
  barriers and x0..x3 stores; this is structural evidence, not executed SMC.
- Native QEMU `play`: all eleven actual tenfoot launch/return/failure cases
  passed, including program failure, wrong identity, interrupted launch,
  save retry and retained-ROM/part failures. These use the software FPGA model.
- `make test-changed` against the base and `make deadcode`; committed-source
  consistency is checked separately after committing.

FogCast reader behaviour changes as described above. Production Linux runtime,
shared schemas/ABI definitions, generated consumers and FPGA RTL are unchanged.
This establishes no physical Nano support or exact-artifact hardware acceptance.
