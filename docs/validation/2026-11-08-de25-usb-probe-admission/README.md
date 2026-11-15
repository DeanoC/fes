# Separate USB probe file admission

Source `7990523595f31e16f2fafeeeb2aa782b61e1139e`. The default API still admits the paired `fpgaload` prototype.
An explicit USB scope selects a distinct record tag, `probe-profile.json` and
one complete sealed project. It requires matched contained USB entry/firmware
context and pins, with no ROM, computer, program, menu or SD resources. Rehashing
a different scope tag cannot turn either record into the other kind.

All 16 focused tests and six affected checks pass. Parent regressions pass
1,719 tests with three existing skips; committed-source `make check` passes
52 generated consumers and 37 fixture copies. Tests exercise both scopes,
changed resources/source/membership, rejected full validation, wrong profiles,
mutation during validation, caller inputs and ownership loss.

The actual retained USB candidate passes complete validation and a fresh cached
reload: 4,013 files across 7 source/project trees.
Full validation took 4.740 seconds and reload
0.542 seconds. These are host file checks, not board timing.
The existing compiled USB image is unchanged. The cache supplies no live device,
memory, reset, FPGA programming or ordinary core-load authority.

The USB-only operator is being prepared separately. It must freeze its source,
inputs and one-attempt allowance, then pass fresh top-board admission before
running. The previous secure-tape image remains consumed. Physical USB startup,
bulk tape delivery/ROM consumption and full core-load appliance acceptance remain
open. See [result.json](result.json) for exact identities and evidence.
