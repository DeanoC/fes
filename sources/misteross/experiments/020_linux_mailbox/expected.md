# Expected behavior

After configuration the FPGA exposes HELLO (`0xD3100000`) on GPI and keeps it
there regardless of the inherited GPO value, including an inherited exact
START.  The HPS must first clock a zero on GPO; only an exact START word
(`0xAC100000`) observed on a later clock edge advances the transaction.

The fixed payload is the twelve-byte sequence `OSS FPGA OK\n`.  Each DATA word
contains signature `0xD3`, version `1`, opcode `1` (`0xD311` prefix), the current eight-bit
sequence, and one payload byte.  It remains stable until GPO contains the
exact ACK made by replacing only the signature with `0xAC`.  A wrong START or
wrong ACK leaves the current word unchanged.  The sequence increments only
after an exact ACK and wraps from `255` to `0`.

After the newline is acknowledged, END (`0xD312` prefix) carries the next sequence and a zero
byte.  The exact END ACK changes the state to DONE, which carries the END
sequence and zero byte (`0xD3130C00` for the fixed message).  DONE remains stable forever and is never
acknowledged.  DONE's terminal hold is permanent.  A reconfiguration restarts the state machine at HELLO with
sequence zero.

The simulation-only primitive model lets the testbench set GPO and observe
GPI.  It is not a production source and is excluded from the OSS build.  A parameterized two-byte simulation transaction separately checks
the defined `255` then `0` sequence wrap.
