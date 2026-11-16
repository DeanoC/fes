# Serial Tenfoot ownership and lifecycle observer

`platform/embedded/nano/tenfoot_trace.py` checks the separate App state stream.
It validates complete typed JSON, the matched cold boot, immutable expected
catalog/identities, monotonic native state/generation transitions and the real
keypress sequence. Firmware configuration and master-stop observations use the
existing unchanged service trace checkers, with additional four-credit/queue
checks. Configuration evidence must precede publication of the running state.
Quit during Run must close the owned session and exit at generation six.

The client sends each key once through an injected exchange hook, with the
existing 100-second launch, 20-second firmware configuration and 5-second control
bounds. Loss of claim, backwards time, late/malformed output, failure status,
wrong identity or damaged firmware evidence retires it without another key.
The final sequence loads selections 0,1,0 and performs owned Stop/Stop/Quit.

Seven focused test methods and all six affected checks passed. Fixtures combine
synthetic App states with previously captured firmware observations. They cover
initial boot/catalog binding, malformed/duplicate/nonfinite fields, source
identity, queue/transfer/drain damage, early publication, state regression,
clock/deadline/claim loss and permanent retirement. This is off-board evidence.
The matched native image remains source e225ea89b; no firmware/FPGA bits changed.

The continuing TRWU8CBL claim, exact USB identity and host recovery-network route
were rechecked read-only. No UART open, JTAG programming or SD write occurred.
The separately staged operator still needs its own fixture/preflight checks
before the bounded physical App procedure. Existing recovery/JTAG/upload bodies
are being retained with explicit source and seven-file upload verification.
