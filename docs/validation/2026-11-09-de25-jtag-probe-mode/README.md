# Scan-tool executable-mode repair

The [MMU trial's host operation](../2026-11-09-de25-mmu-trial-v2/hardware-result.json)
stopped before its first scan: `shutil.copyfile` copied the qualified probe bytes
into a new file with mode `0664`, losing the source's executable bits. The recovery
reservation was already durably consumed. No scan, FPGA programming, native boot
or SD write occurred. Independent cleanup passed, and the complete failed
operation has a verified archive at
`/home/deano/fes-retained/de25-mmu-trial-hardware-20261109-r1`.

`jtag_probe.checked` requires an explicit selected SHA-256 and executable mode,
a regular absolute file without symlinks, actual execution access and stable
file identity/metadata across hashing. It returns file observations only.
The existing `ProgramAttempt` implementation and source hash are unchanged.

The corrected staged operator is `out/nano-mmu-probe-repair-20261109`. Its new
private probe preserves the original qualified mode `0775` and identical bytes.
The failed copy remains `0664`; its consumed reservation is retained. Preflight
checks the frozen execution mode before transport setup. The actual programmer
checks it before creating an attempt and again during every fresh transport
observation. A mode change after a scan remains terminal and consumed; restoring
permissions cannot revive the operation.

Focused tests execute the real file checker and unchanged programming policy,
plus extracted actual staged preflight/programmer functions with injected I/O.
They cover lost copy permissions, denied access, changed bytes/modes, symlinks,
changes during hashing, missing mode selection, rejection before any reservation
or transport call, and post-scan mode loss with replay rejection. No probe binary
or hardware API is executed by these models.

The [affected checks](checks.json) pass 1,775 parent tests with three existing
skips, plus generated-consumer consistency. Twelve focused checker/policy tests
and six staged programmer/preflight tests pass. The actual failed file is
rejected and the corrected copy accepted by the file-only checker. Firmware and
FPGA source bytes are unchanged, and their earlier software evidence is retained.

The corrected operator still needs fresh complete file admission, a frozen
one-use procedure and verified prepared retention before another board operation.
The matched MMU image remains `e4ecad3bc8a33357`; its hardware startup, private
memory diagnostic and original one-second tape deadline are still untested.
This repair supplies no core-load or cache-coherency acceptance.
