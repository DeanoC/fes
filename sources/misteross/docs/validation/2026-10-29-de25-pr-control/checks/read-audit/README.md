# Existing compiler read-audit cleanup failure follow-up

The broad `make test-changed` run failed one of 1,483 FPGA tests (35 skips):
`ReadAuditTests.test_timeout_cancels_multithreaded_child_group_without_masking_timeout`.
Its expected `ProcessLookupError` did not occur when checking a child PID after
the timeout cleanup. [aggregate-failure.txt](aggregate-failure.txt) preserves the
traceback; [checks.json](checks.json) hashes the full aggregate log. This follow-up
does not make the aggregate pass.

One isolated rerun of the exact method passed in 0.513 seconds, with the test's
0.5-second timeout unchanged. [exact-method.log](exact-method.log) and
[checks.json](checks.json) retain the command, result and source hashes. The audit
implementation, exact test, and direct exporter/fixture imports are byte-identical
to base `f5542eaba6989b5612adbbefc6290eb225910d49`. No separate baseline execution
or broader repetition was run.

The aggregate log did not retain the failing PID or its process state. It cannot
distinguish a still-running tracee from an exited but unreaped zombie; the cause
is unresolved. No production code, test or timeout was changed. This is a
software-test follow-up and provides no hardware acceptance.
