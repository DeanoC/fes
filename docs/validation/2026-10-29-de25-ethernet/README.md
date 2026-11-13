# Nano XGMAC and shared network service prototype

FES-owned exploratory native-base work on `codex/de25-embedded-os`, based on
`e1279dae39b2bef97d7eb79aaa33af67a15011cf`. The result is the commit containing
this record. Evidence is host register/descriptor models, actual userspace
ARP/IPv4/TCP/HTTP exchanges, AArch64 QEMU execution and Nano cross-compilation.
No physical board, kit, FPGA, GPU or Powerboat process was used. Production
Linux components and shared/generated schemas are unchanged.

## Implemented path

Nano now opens an XGMAC 2.10 MAC through bounded Clause-22 PHY identity/link
checks and a four-entry polling DMA transport. A schema-3 firmware profile
specifies the actual MAC/PHY, clocks, static IPv4, RX register layout and an
exclusive identity-reachable 2 MiB DMA window. Firmware still owns PHY reset
and autonegotiation, RGMII delays/routing and 125 MHz reference, MAC clocks/reset,
SMMU/firewalls and matching board-revision configuration. Only negotiated
1000BASE-T full duplex is admitted. MAC index/PHY data in the tests are synthetic.

The DMA window is disjoint from Go RAM and every firmware reservation. The
pinned-runtime table mapper was extracted from QEMU into one shared implementation;
each board retains its own resource policy. It checks alignment/overlap, existing
L1/L2 identity mappings and page-table ownership before remapping only explicitly
reserved blocks as Normal noncacheable. No new tables are allocated in an unknown
owner. QEMU retains its original RAM/DMA addresses; Nano uses only the profile.

DMA descriptors/buffers remain pinned for the entire boot. CPU publishes OWN
only after descriptor/data barriers and copies RX only after completion/acquire.
CRC/size failures drop. Contexts/fragments, fatal DMA state, stopped engines,
link loss and ambiguous TX timeout retire the MAC. Clearing start bits is only
best effort containment, not a DMA fence or permission to reuse memory.
Cancellation likewise does not release DMA or permit a second socket provider.

The socket/IP service now accepts an explicit board Device. QEMU defaults and
VirtIO discovery stay in its separate entrypoint. The shared Link registers its
TX callback before Configure starts Lneto workers, propagates device failures,
and stops callback work on cancellation. The previous per-VirtIO bridge was
removed. `cmd/network` reads the existing FAT32 BOOT.JSON URL/Host override and
fetches the real games catalog without a display or USB input device.

## Evidence and limits

- `GOWORK=off go -C platform/embedded/tamago test -race ./...`: passed.
- XGMAC models cover both explicit RX field layouts; TX/RX integrity and ring
  wrap; MDIO/reset/PHY identity/negotiation/capability rejection; CRC/length drops;
  bad contexts/fragments; stopped hardware, link loss, bus error, completion
  timeout and frozen clocks; no memory or register reuse after a fatal failure.
- An HTTP catalog payload crosses the modeled DMA rings through real Lneto
  Ethernet/ARP/IPv4/TCP and an independent gVisor TCP/HTTP peer. There are no host
  TCP sockets or fake HTTP RoundTrippers in that test. The final test passes
  under the race detector. It exercises the same Link as the native client.
- An earlier all-Lneto host test exposed the preexisting callback setup race and
  upstream listener shutdown races. Our callback ordering is corrected. This
  client-only study does not qualify Lneto listener shutdown; the independent
  gVisor peer supplies the test server. No dependency cache/source was patched.
- Embedded Python regressions: passed, including profile bounds/identity/link
  obligations and serial fragmentation. The initial probe runner saw a partial
  UART completion prefix; it now waits for the complete catalog/statistics line,
  with every prefix length and duplicate/malformed completions covered.
- Native QEMU `network`: actual VirtIO/static IPv4/TCP/HTTP catalog IDs match a
  private FogCast host; FAT32 hash unchanged. Native QEMU `tenfoot`: actual USB
  navigation and HTTP browsing, preferences saved and restored on a fresh boot.
- Native Nano `probe`, `storage`, `keyboard`, `network`, `tenfoot`: cross-built
  with a synthetic enabled profile, plus a probe with Ethernet explicitly disabled.
  AArch64 ELF/layout and QEMU-controller exclusion checks passed.
- `make test-changed` against the base above: passed, including parent regressions
  and generated-consumer consistency. The final serial regression was also run
  through the focused embedded Python checks. `make deadcode` and whitespace
  checks passed.

The [compact receipt](evidence.json) binds final selected sources, tool/ELF
identities, test sources, reference-file hashes and full local receipt/log hashes.
Full logs/captures remain under the named worktree `out/` paths. No portable
release or hardware acceptance is attached.

The [XGMAC contract](../../../platform/embedded/tamago/xgmac/README.md) describes
supported behavior and exact schema-3 fields. In particular, Linux and SoCFPGA
U-Boot disagree on the RX buffer-size field encoding. Selection is mandatory and
explicit; hardware documentation and bounded frame-DMA tests must resolve it
before a real profile is enabled. There is no guessed fallback. No throughput,
RGMII timing, cache/interconnect, PHY, SMMU isolation or physical link acceptance
follows from a register model or cross-build.

First integration gate remains the delivered board's revision, firmware, memory
reservations and serial boot. Run storage and keyboard probes, then validate the
Ethernet contract and network catalog probe using disposable synthetic media.
FPGA display and physical configuration/identity/containment remain unavailable;
this change still cannot run a complete tenfoot session on a physical Nano.
