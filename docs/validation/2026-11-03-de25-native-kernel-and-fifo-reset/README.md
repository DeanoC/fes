# DE25 native kernel, retained FIFO reset and simulator validation — 3 November 2026

The bottom board boots the new FES kernel, obtains Ethernet connectivity and
accepts authenticated SSH. Native ABI3 unconfigured-owner checks pass before
and after one software reboot without a power cycle. This is a kernel/negative
owner milestone; positive DDR allocation and the native FES core lifecycle
remain unaccepted.

The private MENU integration also closes the generated FIFO reset/clock-crossing
findings in two fresh canonical builds. External HDMI output timing still blocks
package admission. [results.json](results.json) binds the source, artifact,
validation and independently restored archive identities.

## Source and behavior

The FPGA and combined-client producer is `b3adb3a41f147a42873a42894f54e138ea3f5007`.
The branch-local proof fixture is refreshed verbatim at
`55ca607d29193115ba0ff68547674947403a372b`; all 379 canonical inputs and the
65 compiler/58 runtime proof inputs match the tested producers.

The retained MENU visibility handshake holds pending clock faults until the
pixel-side reply closes the visible line. FIFO functional state uses its own
existing parent reset asynchronously and its five-edge release flag as a
synchronous hold. Only the two exact peer-reset-to-first-D ingress paths are
cut; later stages, local reset release and functional consumers remain timed.
Actual generated RAM metadata comes from the guarded elaboration, including
the 131-bit, 256-entry response FIFO.

The long computer and combined proofs expose internal signals read-only.
Their C++ benches still drive the top-level pins and inspect every existing
clock/pixel/assertion point. The board PLL-injection bench retains writable
access. A fixed 10-million-cycle control matches CPU, RAM and video state
while improving execution from 15.782 to 5.318 seconds. C++ `-O3` alone had
still timed out at 600 seconds; that failed candidate is retained. Neither
clocks, assertions nor the 600/1800-second case limits were relaxed.

The complete canonical identity includes these simulation scripts. A comparison
against the earlier `441ebc8a9` record correctly rejected the two changed files;
no earlier RBF was retagged. Fresh builds bind the complete updated 379-file
closure. The original clone-URL setup failures are retained alongside the
successful builds from new output directories.

## Observed results

| Scope | Result |
| --- | --- |
| Exact native kernel, root-only owner device and ABI3 | PASS, negative/unconfigured checks twice |
| Software reboot on that kernel baseline | PASS once; new boot ID, stable pinned SSH key and MAC |
| Entire ZX81 endpoint/machine test file | PASS: 34 tests, 249.2 seconds |
| Entire DE25 link-proof test file | PASS: 27 tests, 8.2 seconds |
| Combined native/static AArch64 Cortex-A76 QEMU proof | PASS: 14 cases, 992.8 seconds |
| Real BASIC SAVE/LOAD/RUN/edit/reload in that model | PASS on both clients; same unmodified 923-byte SAVE image; two RUNs print 7 |
| Canonical FPGA builds | PASS: 616.7 and 616.6 seconds; identical functional record and RBF |
| Actual generated board/transport/startup models | PASS: 13 + 10 + 72 cases |
| Fitted paths at all four reported corners | PASS: 280 board + 3456 FIFO paths per build; minimum 0.098 ns |
| Reported timing checks and signoff rules | 69 checks pass per build; all reset/CDC/RES violations zero; zero waivers |
| External I/O admission | OPEN: 30 output findings; only the two approved asynchronous inputs reviewed |

The generated models use behavioral M20K storage and modeled PLL/I2C completion
boundaries. They do not establish physical DDR, metastability, HDMI or global
reset acceptance. Fitted audits resolve actual physical endpoints and account
for tool duplicates separately; their producer projects remain byte-unchanged.

The original broad run is retained as failed: 1788 FPGA cases, nine failures,
one error and 19 skips. The fresh complete 34/27-file reruns close its machine
timeout and stale proof fixture. Eight unchanged Cyclone V cases retain their
previous user-approved N/A classification; no additional tests were excluded
and the original aggregate was not relabeled green. Parent consistency passes
with 40 generated consumers, 42 fixture copies and zero copied source pins.

## Native evidence and limits

The native image is `429a787525751594f8989ec96c861b27ef46505672cd831e17aa300133ece056`.
Its kernel producer is `59d3b43997b5827dda55125f0f651248a51e6ebf` and image
producer `5d839c7f62df4040b9f5a89d7bb41fa53bc9f08f`. Exact kernel notes,
configuration and installed-manifest hashes distinguish it from the older
kernel with the same release name. The installed manifest is distinct from
the source build receipt; their initial association/capture failure and the
corrected immutable-rootfs mapping are retained.

UART identity precedes pinned SSH authentication and the native diagnostic.
The static AArch64 diagnostic verifies ABI3 before testing unknown/reserved,
unconfigured-allocation and capability refusals. It performs no positive
allocation, mapping, publication, DDR DMA or FPGA programming. Temporary native
files are removed; the board remains running and claimed, without a baseline
promotion. The top board is untouched. The first-boot collector attests its
selected files and observations, not the whole running SD image.

The new FPGA RBF is `8d9384c1c5807aa00d2d991e86c183ac5e77eef5b6848badaa2c68b8bb04d201`,
build ID `1084e8d183fd372a77a702fce1aba7d6`. It is unprogrammed and not admitted.
No new QSPI/boot-configuration deployment follows from these results.

## Retention and next acceptance

The private native, software and FPGA archives are identified in
[results.json](results.json). Independent restoration verifies every retained
byte and mode, Git source, public timing/source readers, actual model and
client binaries, both SAVE files and the fitted endpoint coverage. Restoring
the archives does not rerun native operations, simulations or Quartus.

Next acceptance requires the external HDMI output timing/electrical/board-path
contract, then positive native owned-buffer/DDR and global reset checks,
followed by admitted MENU and ZX81 launch, LOAD/RUN/reload, Stop/relaunch and
recovery. No additional PCB files are available and no further I/O exception
is approved.
