# Read-only installed-record check for the next two-core trial

Against `26d07c469`, the recovery UploadSession can verify an independently
expected installed record. The caller supplies bounded immutable JSON bytes;
the reader checks the installed-record envelope and derives the only allowed
filename as `FES/INSTALLED/<sha256-of-expected-bytes>.JSN`. This adds no upload
purpose or write destination. It shares the fixed-file reader's fresh marker,
scratch-map, claim ownership and protected-prefix/FIT/manifest checks. Failures
retire the session before another command. Success proves byte identity only;
native discovery still admits the record and every referenced content object.

The upload suite passes 47 tests, including successful read-only access, exact
content-derived paths, wrong card/loader, prefix/FIT changes, ambiguous byte
counts, same-length corruption, ownership loss and session/marker reuse. Invalid
envelopes, duplicate fields, nonfinite constants and oversized inputs reject before
device commands. The related installer suites pass 45 tests. The affected checks
pass, including 1,950 parent tests with three skips, planner checks, whitespace
and generated-consumer consistency. Command and log hashes are in result.json.

The independently derived 754-byte prior ZX81 record passes a model read at its
expected content-hash path. This uses the existing retained first-install input;
it does not read the current card or establish preservation or persistence.
The new fourteen-file staging plan selects the actual ZX81 trial and ZX80 trial
assets, retains the existing ZX81 title, and separates the prior-record read from
staging writes. It has no execution or live-admission authority.

An initial model harness selected the generic recovery loader for the explicit
return-trace kit and correctly rejected its package type. Using the trace-aware
loader then rejected the old uploader source binding. Both failed checks remain
saved. The existing producer generated a fresh recovery package using its normal
validators. Its firmware FIT, SPL, FPGA image, manifest and protected card prefix
are byte-identical to the previous package. This refresh changes host bindings;
it is not new firmware or FPGA hardware acceptance. Old receipts are unchanged.

Private evidence and the fresh package are in
`out/nano-installed-record-read-20261111`. No UART/JTAG, card writes, board resets
or physical test occurred. The next step is to bind the live two-core operator
to this package and fourteen-file plan, check its failure handling, then perform
fresh hardware admission and the bounded install/launch/return trial. A separate
fresh-boot discovery and prior-record readback are still required afterward.
