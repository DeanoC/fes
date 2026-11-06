# Final SPL exclusion check for candidate firmware RAM

The shared-USB, held-ROM cold profile now checks SPL's live allocations after
its existing final ATF trace and before the no-return firmware branch. This is
the first part of the on-chip RAM ownership contract. It does not reserve RAM,
change firmware placement, or admit an on-chip firmware image.

The candidate remains `[0x50000, 0x60000)`. The final check requires a finished
cold producer, EL3 with interrupts masked and translation/data cache disabled,
the recorded boot CPU, and the exact cold-record pointer. It consumes its
single attempt before observation. Success and failure both reject a repeated
call without new observations. The source-bound chain includes the shared
predicate header; the producer installs the check only in the shared-ROM path.

The actual SP must be aligned and at least 0x61000, below the pinned 0x71000
stack top and no higher than live global data. The complete global-data object
must fit below the early heap `[0x6f000, 0x71000)`. SPL's loaded image must end
at or below 0x40000, with the complete cold record inside it. The main heap
must remain `[0xbfa00000, 0xbff00000)` and ATF parameters inside the separate
SPL BSS range `[0xbff00000, 0xc0000000)`. Compile-time assertions reject a
different configured allocation scheme or relocated SPL stack. Runtime checks
still require the actual observed addresses.

The audit corrects the earlier linker-feasibility prose: the measured cold
record is OCRAM `.data` at 0x21e90, size 224; SPL BSS is in DDR. The earlier
retained archive remains historical evidence, with its correction here and in
the current linker-feasibility guide.

Validation exercises the actual C predicate, actual one-shot function, injected
CPU-register snapshot and generated final caller. Cases cover overlap, range
overflow, alignment, changed CPU state, incomplete/failed cold setup, failed
observations and replay after success/failure. The generated caller reaches
firmware only after the trace and successful check. Separate tests keep the
ordinary DDR-only ELF gate closed to zero-file-byte OCRAM NOBITS segments.
The existing cold/ROM/USB ownership suites passed: 29 tests before the added
admission regression, then all 10 firmware/admission tests passed.
The required changed-input integration run also passed all ten steps on
46e1aa52b358e6e83252e8ce342c88fd88affa3e: parent regressions (1,498 tests),
producer regressions (1,581 tests), policy checks, generated-consumer
consistency, native firmware/FPGA lifecycle, producer identity and firmware/RTL
integration. These are host/model checks, separate from physical acceptance.

Both changed SPL translation units compiled against the pinned vendor's actual
headers, configuration and original compiler flags. The final guard is a
zero-stack-frame leaf; its enclosing caller uses 64 static bytes, and the
successful guard-to-firmware path contains no further calls or stack writes.
Nine actual ARM compilations rejected changed image, stack, early heap, BSS,
main heap and stack-relocation configurations. An initial cross-compile found
a missing `hang.h` include in the generated caller; the fixed successful
compilation and the failed log are both retained.

The [receipt](result.json) binds exact implementation bytes and diagnostic
evidence under `out/nano-rom-spl-ram-guard-20261106`. This is not a linked or
matched boot-chain build. No image was uploaded, programmed or booted, and no
SD write was performed. TRWU8CBL's claim/test card remain retained after the
previous O2 ROM stop; its factory card and the other board are untouched.

Still required before an on-chip placement trial: a complete early-BL31
reservation contract covering its first BSS clear and stack selection, exclusion
from the native CPU map, ownership of other possible OCRAM users, and strict
matched-ELF acceptance of only that selected layout. The current ELF gate still
rejects the private linker prototype. There is no new latency, ROM-completion,
CPU Run/return, physical input, ordinary package or DE10-equivalence claim.
