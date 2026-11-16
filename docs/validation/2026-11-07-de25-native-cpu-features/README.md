# Native EL1 CPU feature discovery, 2026-11-07

The [previous bounded menu test](../2026-11-07-de25-native-menu-publication-trial/README.md)
passes three real Tenfoot frame acknowledgements and independent secure return,
but presentations take about 3.8 seconds. Inspection of the selected native
compiler showed an empty optional ARM64 CPU feature initializer, so SHA-256
used its generic implementation. This record prepares a measured comparison;
it does not yet claim a native speedup.

`--arm64-features` explicitly selects an overlay for the pinned single-CPU
TamaGo/ARM64 EL1 build. It reads the existing architectural instruction and
processor feature registers and invokes Go's existing parser before standard
crypto dispatch initialization. No ISA bit, CPU model or crypto implementation
is forced. The installed compiler remains unchanged. Unknown initializer,
parser or register-reader source hashes reject before generating an overlay.
The new selected source and overlay join the existing native source proof and
are checked again after compilation. Other execution environments and the
ordinary QEMU runner reject this Nano option. All memory/ownership/integrity
checks, one-second endpoint, four-second CPU publication, 64 polls, outer
procedure bounds and FPGA timing rules remain unchanged.

The standalone verifier runs the actual native Go runtime at QEMU EL1 in three
cases: baseline, real feature discovery and discovery with advertised SHA2
support masked. Each verifies twelve input sizes across SHA-1, SHA-256, chunked
SHA-256 and SHA-384, plus AES-128/256, AES-GCM known encryption/decryption and
corrupt-tag rejection, and IEEE/Castagnoli CRC32 vectors. QMP reads both live
SHA2-dispatch flags: 0, 1 and 0 respectively. Masking is a test input on a CPU
that still implements SHA2, not proof on physically unsupported silicon. All
1,315 selected model sources were unchanged; verifier/helper/probe inputs match
the committed source. Two earlier wrapper attempts failed while constructing
the evidence report or correcting its syntax; those failures remain retained.
The final complete crypto check passes.

All affected plan commands pass, including 1,638 parent tests with three
existing skips and five new guard tests. The prior native lifecycle/firmware/RTL
integration evidence is reused only after verifying its exact 327 source hashes
and fixture hashes unchanged; the new native dispatch is covered separately by
QEMU and still needs physical qualification. No unrelated broad suite is rerun.

A fresh SDK, entry, BL31, SPL and HPS-first composition pass the matched audit:
4,460 recorded inputs, actual firmware BSS end `0x5afc0`, final SPL allocation
guard after trace and before ATF, rejection branch and private cookie preserved.
The FPGA package/build identity and core bytes are unchanged. The FIT stays
below the existing 8 MiB limit. Exact sources, hashes and check receipts are in
[result.json](result.json). The next distinct top-kit attempt must repeat all
three frame acknowledgements, zero underflows, local drain, independent master
stop/bridge closure and host cleanup before any performance comparison.

The original card and bottom kit remain untouched. Physical HDMI, a complete
menu/core/menu application cycle, ordinary package admission and full timing
signoff remain open.
