# Paired v14-seed CPU startup controls

Two separate contained images derived from v14 booted on TRWU8CBL and parked
before DDR, storage or BL31. Both retained the seed's vendor startup object
instructions, and independent SOF extraction matched their exact SPL HEX.
Neither can continue into native entry. See [evidence](evidence.json).

The [console-only control](console_only-console.log), `2ea1714cff150bd9`, skipped
the initial ownership proof, reached EL3/core0, observed reset/power registers
and parked. The [initial-proof control](initial_proof-console.log),
`ff003d0f54b37fb7`, completed all 32 ordered checkpoints, reported `valid=1`
under the corrected controller/ECC rule, retained matching first/current CPU
power observations and parked. Eight/nine fixtures exercised the actual host
acceptance blocks; a rejected first proof can only be captured and parked,
never admitted. Both real hardware captures passed.

These are distinct diagnostic binaries with different layouts/observation
paths. Their success does not prove the full native v14 artifact executed,
drained SD or entered BL31/Go. Its two programmer failures remain open.
Offline controls found identical vendor startup instruction bytes and valid
OCRAM record bounds; v14 repackaged identically, and its independently decoded
SPL matched the input. No cause is inferred from those controls alone.

The kit remains parked in the initial-proof control, with the test card
inserted and original factory card safe outside. Its continuing claim is held,
UART clients are closed and USB permissions restored. Exact top JTAG identity
was rechecked; a read-only cable query returned `15M`, with no clock/daemon
change or power cycle. The next investigation is a separately bounded
transport/startup comparison, preserving the failed native attempts and their
fuses. These observations do not justify replaying native v14 at the unchanged
setting.
