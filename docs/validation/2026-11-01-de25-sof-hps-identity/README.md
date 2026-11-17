# Nano SOF embedded SPL identity guard

Native packaging now independently unpacks the bounded gzip SOF and decodes
its counted HPS literal chunks. It compares every SPL byte with a
checksum-checked, contiguous Intel HEX input and requires the reference's
section topology and other FPGA/configuration sections to remain identical.
Unknown headers, duplicated sections, changed data, gaps, overlaps, malformed
markers, truncation, trailing bytes and resource-limit violations reject
before SD image assembly. The package receipt binds the inspector source and
reports the decoded byte count/hash/chunks. See [evidence](evidence.json).

Seven focused tests cover both extended HEX address modes, real 64 KiB chunk
boundaries, one through four chunks, complete identity and rejection paths.
The inspector decoded four retained real artifacts: native v12/v13/v14 and
known recovery. The header word matches the chunk count: three in the native
controls and four in recovery. A complete package build passed the guard and
retained exactly the original v14 SOF/FIT/native-profile bytes. All six affected checks pass, including 1,234 parent tests (40 skipped) and
generated consistency. The tested source commit is recorded in the evidence.

This is payload identity and preservation evidence. The limited decoder does
not validate SDM command semantics, security hashes or configuration CRC, and
it grants no programming/boot acceptance. The original
[v14 programming failures](../2026-11-01-de25-final-handoff-diagnostic/README.md)
remain open; the newly inspected package was not programmed. The kit was restored to known recovery after those failures. Subsequent
[paired CPU controls](../2026-11-01-de25-v14-startup-controls/README.md) ran
and parked; the test card remains inserted and the original factory card is safe
outside the board.
