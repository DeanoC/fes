# Nano held-ROM package retention

`76dbb98c8` extends the existing format-3 package path with Nano's `cpu-csr-v1`
held-ROM encoding. `7eeba3247` corrects the oversized negative fixture's trailing
whitespace. All selected validation below uses the latter committed source.

The sealed map declares one 4/8 KiB firmware source, the unchanged FPGA payload
digest and required held-ROM interface. It contains no physical addresses,
bridge masks or CRAM locations. The existing retained package/link object owns
private ROM bytes and a package/map/source/payload-bound activation receipt.
The FPGA payload remains byte-identical. Wrong roles, sizes, maps, receipts,
parts and two-source Nano packages reject. The Cyclone V linker/staging path
cannot consume this Nano encoding. The Linux runtime continues to reject Nano
at its target compatibility boundary; a new C++ test verifies that boundary.

Shared schema, Go/Python readers and generated fixture copies agree on 19 new
Nano cases. All 46 previous fixture records and identities are unchanged. The
storage tests exercise actual package retention, expected receipt comparison,
file closure, SD containment, retained reads without further filesystem access,
resume and stale-handoff rejection for both sizes. These are software models,
not card or FPGA operations.

All 37 commands selected by the affected-check planner have terminal evidence.
34 passed directly. Three aggregates reached the private runner's unchanged
900-second cap: parent regressions, C64 and Coleco. Their original results and
logs remain failed/timed-out; they are not reported as aggregate passes.

The parent log supplies 1,083 exact same-line terminal successes/skips. Seven
bounded groups execute the other 482 selected cases, including uncertain output
and the interrupted case. Their union is exactly all 1,565 selected parent
cases: 1,523 passed and 42 skipped. Most skips are duplicate inner-container
cases executed through their host drivers; the retained reasons also include
two fixture-specific ownership skips, a macOS-only layout and an absent locked
upstream U-Boot cache. No selected case was silently dropped.

C64 reached its clock test after completing the earlier serial prerequisites.
The interrupted test, remaining prerequisites and original main recipe passed
in eight separately bounded phases. Coleco reached its OSS sprite-board test;
that test and eight remaining OSS prerequisites passed separately. Completed
prerequisites were retained rather than rerun. Source hashes, phase commands
and original logs are recorded alongside these coverage results.

The full FPGA Python suite passed (1,580 cases, 75 skipped), as did the remaining
selected RTL simulations, exhaustive host/appliance/expansion race and UI suites,
runtime software tests, compatibility parity, storage/native integration,
contract fixtures and generated-consumer checks. Neither timing/seal checks nor
simulation expectations were relaxed. No Quartus build or board operation ran.

Sources, tests, phase coverage, original timeout/preparation logs and verified
archive are retained at
`/home/deano/fes-retained/de25-rom-retention-20261105-r1/`. The incremental source
bundle and its explicitly listed prerequisite bundles restore into a fresh
repository and pass strict Git fsck. The unfinished native publisher copy is
kept separately in the development stage and is excluded from this milestone's
archive and validation claim.

This adds retained content support. The current diagnostic FPGA producer still
exports format 2; native ROM-profile selection/publication, a matched boot image,
physical ROM loading and ordinary runtime admission remain open. The next change
connects the retained ROM, compiled resources and version-two descriptor to the
owned native loader, while keeping SDM transfer bytes limited to the FPGA image.
Full DE10-equivalent core loading is not complete.
