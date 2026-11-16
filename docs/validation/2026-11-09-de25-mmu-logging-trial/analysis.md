# CCU rejected a transaction at the OCRAM stack

Image `2ed0757748552e49` passed recovery, thirteen verified uploads/fresh SD
reads, native programming, the cold provider and fifteen MMU steps. The logger
verified its zero baseline and armed decode detection before the activation
record. Both complete 27-row inventories and the terminal fault were retained.

The fault again has ESR `be000011`, SPSR `2cc`, SCR `238`, SCTLR `cd183f`,
TCR `80803520`, MAIR `4400ff` and interrupted SP_EL0 `51e40`. The matched
disassembly locates ELR in `tf_log` at `STR X7,[SP,#152]`. Delivery remains
asynchronous; that instruction and FAR `5c780` are not proven causes.

This time the DSU/CCU status changes from zero to stable `20430071`, with
location0 `51e40` and location1 zero. Decode detection reads8 and interrupts
remain0 during the fault inventory. The retained official register map decodes
the status as valid, error type7 (decode), subtype3 (illegal DII access type),
transaction ID129. The logged location matches the interrupted OCRAM stack
pointer. These observations identify a rejected transaction involving the stack
within this recording window; they do not identify its exact instruction,
request opcode or establish that every OCRAM cache configuration is unsupported.

The recording lifetime then reports restore-before and restored. Its matched
stack-free code verifies control0, unchanged interrupts and unchanged sticky
status before reporting restoration. No acknowledgement or status clear occurs.
The observer retains restoration as diagnostic evidence while boot admission
remains closed. Independent raw-console replay exactly reproduces the report.

There is no returned SCTLR callback, private memory probe, restored normal
vector, `fesboot`, launcher or tape transfer. Independent cleanup confirms15M,
original USB permissions, termination of the owned private server and free UART.
Both programming operations are consumed and this image is retired. The top
kit remains in the terminal firmware park with its spare installed, under the
continuing exclusive claim and factory-restore waiver; the bottom kit was untouched.

The pinned vendor BL31 map explicitly selects secure, writable non-cacheable
OCRAM. The selected experimental tables instead make the private stack/BSS
subset write-back cacheable. The smallest next repair is to restore non-cacheable
OCRAM attributes while retaining private DDR caching, every permission and the
existing reservations. Move the cache-test witness into the existing private
DDR data reservation so its clean/invalidate/readback still tests cached memory.
Validate both memory types and all bounds, then prepare a new matched image and
one-use test with the original tape deadline. Do not replay this image, clear
sticky status to infer health, disable ECC or broaden memory permissions.
