# Native programming stopped on JTAG scan error

The additional attempt on operator source `c070bbe3c` stopped before native
boot. This procedure is consumed. Together with the earlier host-observer stop,
the unchanged `f8653867f` image has now reached this procedure's two-attempt limit.
No programming retry or reset was issued after the failure.

Qualified recovery and all twelve Ethernet writes/readbacks plus twelve fresh
final reads passed. Before native programming, the adapter query reported
`4362C0DD` and the qualified `6M` clock. The programmer then returned:

```text
Error (213019): Can't scan JTAG chain. Error code 87.
```

It also reported error 213002 for the programming operation. Programming did not
complete; `fesboot` was not sent. The native console is empty, and no core
commands were issued. This is no test of the repaired observer's live full cycle.

[hardware-result.json](hardware-result.json) binds the exact logs and receipts.
The SOF bytes and the programming, clock-check, receive and failure-tail function
ASTs match the preceding attempt, where native programming succeeded. That
comparison establishes unchanged inputs and helper logic; it does not identify
the cause of this new transport failure. The earlier adapter enumeration is
insufficient evidence that programming-time chain access will succeed.

Independent host cleanup passed: exact original USB permissions, recorded 15M
readback, private server gone and UART free. The last verified board state was
the qualified recovery prompt after upload verification, followed by the failed
programming attempt. No later FPGA/CPU state or native Park is inferred. The
continuing TRWU8CBL claim remains held; no factory restoration was performed.
The original factory card remains aside and the spare retains the verified files.
The bottom kit was excluded.

Preserve both failed attempts and diagnose JTAG transport before considering a
new programming procedure. Read-only chain diagnostics must stay top-only and
must not become a repeated programming attempt. Full menu/core return, shutdown,
physical display/input and normal core-load readiness remain unproven.
