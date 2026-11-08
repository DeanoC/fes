# Top-kit scan-only diagnostic

The preceding ownership-subprofile native trial stopped at Intel AJI return87
before programming. Its reservation remains consumed; this procedure cannot
resume that trial or authorize another programming operation. Last verified
board state was recovery U-Boot after13 verified Ethernet/SD uploads/readbacks.
The native tape deadline and the expensive inner ownership checks remain open.

`operator-source.json` freezes a separate observational experiment on top
TRWU8CBL: adapter-only discovery, fixed6MHz readback, one explicit forced scan,
bounded vendor callback messages, four captured-data parameters and clock
context. A successful scan permits just one non-scanning identity read. Failed
scans never use cached identity. The diagnostic produces no programming grant
and has no FPGA programmer, HPS CPU reset, SD writer or UART command path.

The operator reserves one durable attempt before device access. Its host cleanup
independently attempts original15MHz readback, stops only its own filtered
private server, and restores the same USB node's exact owner/group/mode. An
independent read-only audit checks saved clock evidence, process identity,
permissions and the unused UART. No operation touches bottom TRWU8J8G.

Intel's pinned `jtagconfig.cpp` maps `--listhardware` to `noscan=true`. The old
`--enum` path can request automatic chain reads/scans before clock selection.
This diagnostic removes that implicit scan. This source finding does not prove
the cause of the historical87; a new successful scan would also change the
server and elapsed-time conditions. Capture availability is explicitly reported,
and unsupported parameters neither prove a chain fault nor trigger a retry.

The actual probe compiles against the pinned Intel headers and installed vendor
libraries. Controlled API tests cover success/failure, bounded captures and
messages, argument refusal and rejection by the programming-grant decoder.
Injected host lifecycle tests exercise uncertain clock setters, probe failure,
cleanup failures and consumed reservation refusal. These are software checks,
not native launch, fabric drain, physical display or ordinary appliance proof.
`software-results.json` records the completed affected software checks.

Source and operator/input bytes are retained before scanning, with archive
readback and a verified Git update bundle referring to the retained preceding
board result. Hardware results and cleanup evidence will be recorded separately.
