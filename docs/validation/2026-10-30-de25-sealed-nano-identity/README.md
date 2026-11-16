# DE25 sealed identity diagnostic — 2026-10-30

This record covers a diagnostic Nano package and host-model integration. No
board was accessed or reconfigured during this run. The earlier physical JTAG
diagnostic used a different FPGA image and an older, non-package fingerprint;
its evidence does not qualify this package.

The FES base was `8092f066a45eec5bc09702c34ea4d91433140f18`. The package was
built from committed overlay source `2f6df2d8e5b5dd8f55ec7b4f6f98337837d94749`
and a separately captured board snapshot from
`c413228810a4f9e74318f161e75c952327e08da8`. Native runtime/model verification
used `9a68561a8`. The functional record binds both source snapshots, the locked
vendor archive, selected executable hashes and explicit build parameters. The
standard exporter verifies the overlay source closure; the diagnostic verifier
checks the board snapshot against its own Git revision.

| Item | Value |
| --- | --- |
| Build ID in the FPGA parameters and package manifest | `1beee83f206209fc4c733ec41532e828` |
| Build-input record SHA-256 | `0b11234ba54d6a7623b58eab6526fce076194cc04210d711219536e808f8aedd` |
| Format-2 package ID | `09188130987fa4e5a88e076ff8acbadfcc4cf7d1d51632aba7e3e21b8d45d0cd` |
| Archive SHA-256 | `364948d9807e8f63b35f265d154593301a99019718c22ffa979c47132a944bff` |
| RBF SHA-256 / bytes | `fb379de135c620cb46970e9aa426553aba08114972eba48b309b6c6685025404` / 2,195,456 |
| Raw SOF SHA-256 | `fa5a5431f451a08b574c2a33831ab90fd00db8daf470e9d37fbd8f96f9d4bf8e` |
| Vendor-SPL SOF SHA-256 | `d868ea52a68e19ed6531c33c59637c57e7e8ff5c18580177acf0c595f01d6b08` |
| Vendor SPL SHA-256 | `096f53ea0bb0ba6e7449b2acf012eae734840dbcdd2943b506567bad2f72f823` |
| Host integration report SHA-256 | `90371e0d7c057828cbee50cdef027628315aadd4135de941310a8cfb32722120` |

Quartus Pro 26.3 generated the project, fitted the design and produced the
SOF/RBF. Its timing summary contains 46 reported rows with minimum slack 0.000
and no negative rows. This does not establish full external I/O, MTBF or
whole-board timing acceptance. The generated Qsys map places the 256-byte
endpoint at peripheral-relative `0x10400` and JTAG `0x00110400`; neither is an
established CPU physical address. Handoff tests reject a changed base, missing
JTAG connection, wrong span and modified generated endpoint source.

The Quartus converter rejected the raw HPS SOF because bootloader information
was missing. The diagnostic producer copied the matching vendor SPL from the
locked archive into a combined SOF, then converted that full SoC image to RBF.
The effect of loading this RBF through SDM while the HPS and DDR are running is
not established. **It is not admitted for physical HPS reconfiguration.**
The package and record are content-hash sealed; they are not signed or a
production toolchain attestation.

`verify_sealed.py` retained the exact archive and used its package/build IDs
through the Go admission gate, the bounded CPU aperture adapter, the contained
TF-A function bodies, and Verilated endpoint RTL. CPU access, bridge operations,
memory and SDM were simulated. The test transferred the full retained 2,195,456
bytes, launched and returned; a wrong live ID led to contained idle recovery,
while a lost transfer retained the buffer and required recovery. A separate
Avalon-wrapper model verified the same ID, ABI, drain and fault paths. The
real tenfoot model completed launch/return and rejected wrong live identity,
missing model decision, and invalid link receipt/source/part/composition
selections before release. A valid but different archive was also rejected as
the selected package. The existing C++ physical-runtime compatibility oracle
returned `unsupported_target` for the exact package.

The TF-A reference was v2.12 Git blob
`3c223c1498e3a412e075e4f921cd78acd3d0fcd6`; the prepared source SHA-256 was
`e619a7c8f9dbea4abff2506b214e1194a61731385cf57c0a64416706fa5b2cb5`.
The Nano AArch64 probe cross-build used a synthetic firmware handoff and
produced ELF SHA-256
`552ca458e4e4b1e856606373feb4f9e07259b5ee0a2f9e7bd64911d297ce8287`.
A second, never-executed link-only AArch64 fixture produced ELF SHA-256
`73de57f80ffe12ca78ce69d3ca3ef6be379ac3e05fbcecbcd8a6a73b5b70b5e5`.
Its symbol table contains the Nano physical read, write and Invoke methods,
the ordered load/store helpers and the `smc` assembly; disassembly contains
the SMC instruction between the barriers. Neither check executed SMC or CPU
MMIO on hardware.

The reproducible local artifacts are in `out/de25-sealed-board-v1/`,
`out/de25-sealed-native-final/`, `out/de25-sealed-handoff-v1/` and
`out/de25-sealed-cpu-cross-final/`, and `out/de25-transport-linkcheck/`. The
first combined verifier run is retained
at `out/de25-sealed-native-v1/`; it passed the firmware and tenfoot model cases
but its nested RTL verifier failed to select the Go toolchain with offline
settings. The final verifier selects the already discovered toolchain and
passes. The raw SOF conversion failure is retained under
`out/de25-rbf-format-check/` and was not a hardware operation.

Checks on the code revisions above:

- `make check` passed at `9a68561a8` (24 generated consumers and 37 fixture
  copies agree).
- `make test-changed TEST_CHANGED_ARGS='--base 2f6df2d8e --jobs 2'` passed at `9a68561a8`: 1,085 parent
  tests and 1,370 producer tests, plus the native firmware/FPGA and Avalon
  integrations. The planned skips are recorded in
  `out/de25-sealed-test-changed-final.log`.
- The broader `make test-changed TEST_CHANGED_ARGS='--base 8092f066a --jobs 2'` also passed, with 1,126
  parent and 1,370 producer tests. That run started at `2f6df2d8e` and finished
  after the later runtime/test commit, so its workspace evidence spans both
  revisions rather than representing a single frozen checkout.
- Focused package/export unit tests, FogCast Nano retained inspection, and
  race-enabled runtime and tenfoot tests passed. The final combined verifier
  records the exact sealed package, oracle, firmware and source hashes.

Physical activation still requires a reviewed complete firmware/boot chain,
actual CPU CSR mapping, DMA-safe memory reservations, bridge and other-master
containment, a configuration image demonstrably safe for a running HPS, and
matching on-board identity/drain evidence. The current endpoint's responder is
on-chip test logic, not a DDR scanout path or global containment mechanism.
